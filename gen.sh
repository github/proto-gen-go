[ -p /dev/stdin ] && x=$(mktemp) && { echo; cat; } > $x && chmod +x $x && exec $x "$@"
# This Bash script is designed to be executed by 'curl $url | bash /dev/stdin ...'.
# The line above detects this invocation and writes it to a regular file.
# It must be line 1 to avoid disturbing the line numbers in error messages.

# gen.sh: generates Go declarations for all protocol messages and
#  Twirp RPC interfaces defined by .proto files beneath the current
#  working directory. Requires Docker.
#
# Typical usage: in your project's root proto/ directory, create a Go
# source file containing a generate command such as this:
#
#    package proto
#    //go:generate sh -c "curl https://.../gen.sh | bash /dev/stdin github.com/github/example proto"
#
# The positional arguments to Bash are the Go module name as it
# appears in the go.mod file, and the name of the package directory
# relative to it.

# TODO(adonovan):
# - package using curl
# - tests
# - test on Linux
# - support cross-repo proto imports
# - specify all versions in Dockerfile

# set -x

set -eu

[ $# = 2 ] || { echo "Usage: $0 go-module go-package-dir"; exit 1; } >&2
module=$1; shift
package=$1; shift

# Build the image specified by the Dockerfile appended to this shell script.
# The docker context is empty.
# The awk filter preserves line numbers.
id=$(awk '/^### Dockerfile/ {on=1} on {print} !on {print("")}' $0 | docker build -q -)

function protoc {
  # We assume PWD does not conflict with some critical part of the docker image.
  docker run -v "$PWD:$PWD" $id "$@"
}

echo "Generating Go declarations for protocols in $module/$package:"

# I can't figure out what flags will cause protoc-gen-twirp to
# generate files into a single directory, so we'll use a temporary
# tree and then flatten it.
twirp="$PWD"/$(mktemp -d twirp-XXXXXX)
trap "rm -fr $twirp" EXIT

# The explicit PWDs are required to appease protoc's
# rather sensitive file name expectations.
#
# All files in a single protoc invocation must belong
# to the same proto package, hence the loop.
for file in $(find * -name \*.proto -print); do
  echo "- compiling $file" >&2
  protoc --proto_path="$PWD" \
	 --go_out="$PWD" \
         --twirp_out="$twirp" \
         --go_opt=paths=source_relative \
     "$PWD/$file" || { echo "protoc failed"; exit 1; } >&2
done

# Move the Twirp-generated code (if any) from the temp dir to the
# source tree by removing the unwanted module/package prefix.
genroot="$twirp/$module/$package"
# Ensure the chdir below doesn't silently fail
# if there were no service declarations.
if [ -d "$genroot" ]; then
  for file in $(cd "$genroot" >/dev/null >&2 && find * -name \*.go); do
    mkdir -p $(dirname "$file")
    mv -f "$twirp/$module/$package/$file" "$file"
  done
fi

exit 0

### Dockerfile

# This Dockerfile produces an image that runs the protocol compiler
# to generate Go declarations for messages and Twirp RPC interfaces.
#
# It uses gh-builder-bionic (built from github.com/github/gh-base-image), and the
# latest versions of go, protoc-gen-go, and protoc-gen-twirp.
FROM ghcr.io/github/gh-base-image/gh-builder-bionic:20211214-001229-gfd004f1a1

WORKDIR /work

RUN go get google.golang.org/protobuf/cmd/protoc-gen-go@v1.27.1 \
           github.com/twitchtv/twirp/protoc-gen-twirp@v8.1.1

ENTRYPOINT ["protoc"]
