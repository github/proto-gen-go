// The proto-gen-go command runs an explicitly versioned protoc
// command, with Go and Twirp plugins, inside a container, to generate
// Go declarations for protocol messages and Twirp RPC interfaces in a
// set of .proto files.  Run this program manually (or via Make) after
// changing your .proto files.
//
// Usage:
//
//    $ go run github.com/github/proto-gen-go@latest [protoc-flags] [proto files]
//
// When invoked from build scripts, it is best to use an explicit
// module version (not 'latest') to ensure build reproducibility.
// All of the tool's own dependencies are explicitly versioned.
//
// If you add this special comment to a Go source file in your proto/ directory:
//
//    package proto
//    //go:generate sh -c "go run github.com/github/proto-gen-go@latest ..."
//
// then you'll be able to update your generated code by running this
// command from the root:
//
//    $ go generate ./proto
//
// All flags and arguments are passed directly to protoc.  Assuming a
// go:generate directive in the proto/ directory, typical arguments are:
//
//   --proto_path=$(pwd)              Root of proto import tree; absolute path recommended.
//   --go_out=..                      Root of tree for generated files for messages.
//   --twirp_out=.                    Root of tree for generated files for Twirp services.
//   --go_opt=paths=source_relative   Generated filenames mirror source file names.
//   messages.proto services.proto    List of proto files.
//
// Protoc is quite particular about the use of absolute vs. relative
// paths, which is why the example above used "sh -c", to allow
// arguments to reference $(pwd).
//
// This program uses Docker to ensure maximum reproducibility and
// minimum side effects.
package main

// TODO: rename to protoc-docker

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// dockerfile contains the docker specification for our versioned dependencies
//go:embed Dockerfile
var dockerfile string

func main() {
	log.SetPrefix("proto-gen-go: ")
	log.SetFlags(0)
	flag.Parse()

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Build the protoc container image specified by the Dockerfile.
	// The docker context is empty.
	log.Printf("building protoc container image...")
	cmd := exec.Command("docker", "build", "-q", "-")
	cmd.Stdin = strings.NewReader(dockerfile)
	cmd.Stderr = os.Stderr
	cmd.Stdout = new(bytes.Buffer)
	if err := cmd.Run(); err != nil {
		log.Fatalf("docker build failed: %v", err)
	}
	id := strings.TrimSpace(fmt.Sprint(cmd.Stdout)) // docker image id

	// Log the command, neatly.
	protocArgs := flag.Args()
	cmdstr := "protoc " + strings.ReplaceAll(strings.Join(protocArgs, " "), pwd, "$(pwd)")
	log.Println(cmdstr)

	// Run protoc, in a container.
	// We assume pwd does not conflict with some critical part
	// of the docker image, and volume-mount it.
	cmd = exec.Command("docker", "run", "-v", pwd+":"+pwd, id)
	cmd.Args = append(cmd.Args, protocArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("protoc command failed: %v", err)
	}
	log.Println("done")
}
