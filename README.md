# proto-gen-go

This is a proof of concept of a script to make it easy to reliably generate and update Go definitions for messages and services defined in .proto files.

In your Go project's proto directory, add a `gen.go` file with the following contents:

```go
package proto
//go:generate sh -c "curl -s URL | /bin/bash /dev/stdin github.com/github/example proto"
```

The two arguments are the Go module name, and the relative path to the proto directory within the module.

The URL should be expanded out to the stable URL of the raw [gen.sh](https://raw.githubusercontent.com/github/proto-gen-go/main/gen.sh?token=ABLFMPY3D2BDVXPE5X2MJXDBIDJEC) script in this repository. Bear with us---we don't have a nice short URL yet.

Now, when you run go generate in your proto directory, the script will re-run the protocol compiler on all .proto files, and generate go files into the obvious relative locations. Commit them along with your source code.
