# This Dockerfile produces an image that runs the protocol compiler
# to generate Go declarations for messages and Twirp RPC interfaces.
#
# For build reproducibility, it is explicit about the versions of its
# dependencies, which include:
# - the golang base docker image (linux, go, git),
# - protoc,
# - Go packages (protoc-gen-go and protoc-gen-twirp),
# - apt packages (unzip).

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