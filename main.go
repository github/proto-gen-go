// The proto-gen-go command runs an explicitly versioned protoc
// command, with Go and Twirp plugins, inside a container, to generate
// Go declarations for protocol messages and Twirp RPC interfaces in a
// set of .proto files.  Run this program manually (or via Make) after
// changing your .proto files.
//
// Usage:
//
//	$ go run github.com/github/proto-gen-go@<version> [flags] [--] [protoc-flags] [proto files]
//
// When invoked from build scripts, it is best to use an explicit
// module version (not 'latest') to ensure build reproducibility.
// All of the tool's own dependencies are explicitly versioned.
//
// If you add this special comment to a Go source file in your proto/ directory:
//
//	package proto
//	//go:generate sh -c "go run github.com/github/proto-gen-go@<version> ..."
//
// then you'll be able to update your generated code by running this
// command from the root:
//
//	$ go generate ./proto
//
// All flags and arguments are passed directly to protoc.  Assuming a
// go:generate directive in the proto/ directory, typical arguments are:
//
//	--proto_path=$(pwd)              Root of proto import tree; absolute path recommended.
//	--go_out=..                      Root of tree for generated files for messages.
//	--twirp_out=.                    Root of tree for generated files for Twirp services.
//	--go_opt=paths=source_relative   Generated filenames mirror source file names.
//	messages.proto services.proto    List of proto files.
//
// Protoc is quite particular about the use of absolute vs. relative
// paths, which is why the example above used "sh -c", to allow
// arguments to reference $(pwd).
//
// This program uses Docker to ensure maximum reproducibility and
// minimum side effects. In particular:
//   - Thanks to volume mounts, the program can only change files
//     beneath $(pwd); changes outside this tree are not reflected
//     outside the container. If you want the command to write the
//     generated files outside the proto/ tree, you'll need to use
//     'cd .. && go run ...' and adjust the flags accordingly.
//   - By always running protoc on Linux, we needn't worry about
//     downloading an appropriate executable.
package main

import (
	"crypto/sha256"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// dockerfile contains the docker specification for our versioned dependencies
//
//go:embed Dockerfile
var dockerfile string

const platform = "linux/amd64"

func main() {
	log.SetPrefix("proto-gen-go: ")
	log.SetFlags(0)
	flag.Parse()

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := run(pwd, flag.Args()); err != nil {
		log.Fatal(err)
	}
}

func run(pwd string, protocArgs []string) error {
	id, err := compilerImage(dockerfile)
	if err != nil {
		return err
	}

	// Log the command, neatly.
	cmdstr := "protoc " + strings.ReplaceAll(strings.Join(protocArgs, " "), pwd, "$(pwd)")
	log.Println(cmdstr)

	// Run protoc, in a disposable container.
	// We assume pwd does not conflict with some critical part
	// of the docker image, and volume-mount it.
	cmd := exec.Command("docker", "run", "--rm", "-v", pwd+":"+pwd, "--platform="+platform, id)
	cmd.Args = append(cmd.Args, protocArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("protoc command failed: %w", err)
	}
	log.Println("done")
	return nil
}

func compilerImage(dockerfile string) (string, error) {
	name := fmt.Sprintf("proto-gen-go:toolchain-%x", sha256.Sum256([]byte(platform+"\n"+dockerfile)))
	// Listing an absent image succeeds with no output, unlike daemon failures.
	cmd := exec.Command("docker", "image", "ls", "--quiet", "--no-trunc", name)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker image lookup failed: %w", err)
	}
	if id := strings.TrimSpace(string(out)); id != "" {
		return id, nil
	}

	// Build and tag the image only when this toolchain is not cached locally.
	// The dockerized program assumes linux/amd64, and the --platform flag enables
	// dynamic binary translation on M1 hardware.
	// The docker context is empty.
	log.Printf("building protoc container image...")
	cmd = exec.Command("docker", "build", "--platform="+platform, "--tag", name, "-q", "-")
	cmd.Stdin = strings.NewReader(dockerfile)
	cmd.Stderr = os.Stderr
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}
	// Use this build's ID even if a concurrent build replaces the shared tag.
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("docker build returned an empty image ID")
	}
	return id, nil
}
