ARG GO_BUILDER_VERSION

FROM ghcr.io/gythialy/golang-cross:$GO_BUILDER_VERSION

RUN apt-get update; \
    apt-get --no-install-recommends -y -q install protobuf-compiler; \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

ENV PATH="/root/go/bin:$PATH"
