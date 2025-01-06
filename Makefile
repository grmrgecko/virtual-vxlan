VERSION ?= $(shell git describe --tags `git rev-list --tags --max-count=1`)
GITREV = $(shell git rev-parse --short HEAD)
BUILDTIME = $(shell date +'%FT%TZ%z')
PACKAGE_NAME        := github.com/grmrgecko/virtual-vxlan
GO_BUILDER_VERSION  ?= 1.23

.PHONY: default
default: build ;

.PHONY: build-sysroot
build-sysroot:
	./sysroot/build.sh

.PHONY: build-docker-image
build-docker-image:
	docker build -t goreleaser-cross --build-arg GO_BUILDER_VERSION=$(GO_BUILDER_VERSION) .

.PHONY: deps
deps:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

.PHONY: build
build:
	go build

.PHONY: generate
generate:
	go mod tidy
	go get golang.org/x/tools/cmd/stringer@latest
	go generate

.PHONY: clean
clean:
	rm -rf virtual-vxlan* dist CHANGELOG.md

.PHONY: snapshot
snapshot:
	mkdir -p .cache/go
	docker run \
		--rm  --privileged \
		--user 1000:1000 \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(CURDIR):/go/src/$(PACKAGE_NAME) \
		-v $(CURDIR)/sysroot:/sysroot \
		-v $(CURDIR)/.cache:/.cache \
		-v $(CURDIR)/.cache/go:/go \
		-w /go/src/$(PACKAGE_NAME) \
		goreleaser-cross:latest \
		--clean --skip=publish --snapshot --verbose

.PHONY: release
release:
	mkdir -p .cache/go
	docker run \
		--rm  --privileged \
		--user 1000:1000 \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(CURDIR):/go/src/$(PACKAGE_NAME) \
		-v $(CURDIR)/sysroot:/sysroot \
		-v $(CURDIR)/.cache:/.cache \
		-v $(CURDIR)/.cache/go:/go \
		-w /go/src/$(PACKAGE_NAME) \
		goreleaser-cross:latest \
		--clean --skip=validate

.PHONY: lint
lint:
	golangci-lint run --fix
