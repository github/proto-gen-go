# proto-gen-go

[![Docker](https://github.com/github/proto-gen-go/actions/workflows/docker.yaml/badge.svg)](https://github.com/github/proto-gen-go/actions/workflows/docker.yaml)

This tool is a thin wrapper around protoc, the protocol compiler. It
makes it easy to reliably generate and update Go definitions for
messages and services defined in .proto files. It uses a docker
container with explicitly versioned dependencies to ensure maximum
reproducibility and minimum side effects.

In your Go project's proto directory, add a `gen.go` file with the following contents:

```go
package proto
//go:generate sh -c "go run github.com/github/proto-gen-go@<version> [flags] [--] [protoc flags] [proto files]"
```

(The `go run module@version` command requires Go 1.23 or later.)

Now, when you run `go generate` in your proto directory, the script
will re-run the protocol compiler on all .proto files, and generate go
files into the obvious relative locations. Commit them along with your
source code.

The compiler container is automatically removed when it exits, including
when compilation fails. The tool keeps a local image tagged
`proto-gen-go:toolchain-<hash>`, keyed by the embedded Dockerfile and the
`linux/amd64` platform. Repeated invocations reuse that image without
rebuilding it; a changed toolchain gets a different tag. If the image is
removed, the next invocation rebuilds it.

Cold builds are serialized per toolchain across processes using a file lock
in the current user's cache directory. After acquiring the lock, the tool
checks the image again so concurrent invocations reuse the first build
instead of replacing its tag. The operating system releases the lock if
the process exits; the small lock file is intentionally retained.

Images for older toolchains remain available for reproducible generation.
The tool does not prune Docker images, build caches, or containers left
behind by older releases.
