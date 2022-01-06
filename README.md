# proto-gen-go

This tool makes it easy to reliably generate and update Go definitions
for messages and services defined in .proto files.

In your Go project's proto directory, add a `gen.go` file with the following contents:

```go
package proto
//go:generate sh -c "cd .. && go run github.com/github/proto-gen-go@latest"
```

Now, when you run `go generate` in your proto directory, the script
will re-run the protocol compiler on all .proto files, and generate go
files into the obvious relative locations. Commit them along with your
source code.
