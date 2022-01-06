// The proto-gen-go command generates Go declarations for all protocol
// messages and Twirp RPC interfaces. Run this program manually (or
// via Make) after changing your .proto files.
//
// Run this command from the root of your repository:
//
//    $ go run github.com/github/proto-gen-go@latest
//
// When invoked from build scripts, it is best to use an explicit
// module version (not 'latest') to ensure build reproducibility.
// All of the tool's own dependencies are explicitly versioned.
//
// It assumes that the working directory is the root of a repository
// whose proto/ subdirectory is a tree containing one or more .proto
// files, and it generates output to the subdirectory corresponding to
// the 'go_package' option specified in each .proto file.
//
// If you add this special comment to a Go source file in your proto/ directory:
//
//    package proto
//    //go:generate sh -c "cd .. && go run github.com/github/proto-gen-go@latest"
//
// then you can update your generated code by running this command from the root:
//
//    $ go generate ./proto
//
// This program uses Docker to ensure maximum reproducibility and
// minimum side effects.
package main

// TODO(adonovan):
// - repo hygiene (ACL, branch protection, etc)
// - reject 'option go_package = "./a/relative/path"', as used in some repos.
//   According to this doc, it should be the complete import path:
//   https://developers.google.com/protocol-buffers/docs/reference/go-generated#package
//   (Currently the script silently fails to generate the service.)
// - support cross-repo proto imports
// - tests
// - test on Linux

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	log.SetPrefix("proto-gen-go: ")
	log.SetFlags(0)

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

	// Run protoc (in a container) on each .proto file.
	//
	// The explicit PWDs are required to appease protoc's
	// rather sensitive file name expectations.
	//
	// All files in a single protoc invocation must belong
	// to the same proto package, hence the loop.
	found := false
	filepath.Walk("proto", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(info.Name(), ".proto") {
			log.Printf("compiling %s...", path)
			// We assume pwd does not conflict with some critical part
			// of the docker image, and volume-mount it.
			found = true
			cmd := exec.Command("docker", "run", "-v", pwd+":"+pwd, id,
				"--proto_path="+pwd+"/proto",
				"--go_out="+pwd,
				"--twirp_out="+pwd,
				"--go_opt=paths=source_relative",
				pwd+"/"+path,
			)
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("protoc command (%s) failed: $v", err)
			}
		}
		return nil
	})
	if !found {
		log.Fatal("found no .proto files")
	}
	log.Println("done")
}

// This Dockerfile produces an image that runs the protocol compiler
// to generate Go declarations for messages and Twirp RPC interfaces.
//
// For build reproducibility, it is explicit about the versions of its
// dependencies, which include:
// - the golang base docker image (linux, go, git),
// - protoc,
// - Go packages (protoc-gen-go and protoc-gen-twirp),
// - apt packages (unzip).
const dockerfile = `
FROM golang:1.16.5

WORKDIR /work

RUN apt-get update && \
    apt-get install -y unzip=6.0-23+deb10u2 && \
    curl --location --silent -o protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v3.13.0/protoc-3.13.0-linux-x86_64.zip && \
    unzip protoc.zip -d /usr/local/ && \
    rm -fr protoc.zip

RUN go get google.golang.org/protobuf/cmd/protoc-gen-go@v1.20.0 \
           github.com/twitchtv/twirp/protoc-gen-twirp@v5.12.1+incompatible

ENTRYPOINT ["protoc"]
`
