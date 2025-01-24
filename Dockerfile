# This Dockerfile produces an image that runs the protocol compiler
# to generate Go declarations for messages and Twirp RPC interfaces.
#
# For build reproducibility, it is explicit about the versions of its
# dependencies, which include:
# - the golang base docker image (linux, go, git),
# - protoc,
# - Go packages (protoc-gen-go and protoc-gen-twirp),
# - apt packages (unzip).

FROM golang:1.23

WORKDIR /work

RUN apt-get update && \
    apt-get install -y unzip=6.0-28 && \
    curl --location --silent -o protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v27.2/protoc-27.2-linux-x86_64.zip && \
    unzip protoc.zip -d /usr/local/ && \
    rm -fr protoc.zip

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1 && \
        go install github.com/twitchtv/twirp/protoc-gen-twirp@v8.1.3+incompatible && \
        go install github.com/github/twirp-ruby/protoc-gen-twirp_ruby@v1.10.0 && \
        go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2

ENTRYPOINT ["protoc"]
