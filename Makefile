.PHONY: runtime

IMAGE := hargo
VERSION := $(shell git rev-parse HEAD)
BUILD_DATE := $(shell date -R)
VCS_URL := $(shell basename `git rev-parse --show-toplevel`)
VCS_REF := $(shell git log -1 --pretty=%h)
VERSION = $(shell go run tools/build-version.go)
HASH = $(shell git rev-parse --short HEAD)
DATE = $(shell go run tools/build-date.go)
GOMINORVERSION = $(shell go version | cut -d ' ' -f 3 | cut -d '.' -f 2)
GOBIN ?= $($GOPATH)/bin
GO = $(shell which go)

# Builds hargo
build:
	$(GO) build -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" -o hargo ./cmd/hargo

# Same as 'build' but installs to $GOBIN afterward
install:
	$(GO) install -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" ./cmd/har$(GO)

update:
	git pull
	$(GO) install

test:
	$(GO) install
	$(GO) test ./cmd/hargo/main

clean:
	rm -f hargo

docker:
	docker build --rm -t ${IMAGE} \
	--build-arg VERSION="${VERSION}" \
	--build-arg BUILD_DATE="${BUILD_DATE}" \
	--build-arg DATE="${DATE}" \
	--build-arg HASH="${HASH}" \
	--build-arg VERSION="${VERSION}" \
	--build-arg VCS_URL="${VCS_URL}" \
	--build-arg VCS_REF="${VCS_REF}" \
	--build-arg NAME="${NAME}" \
	--build-arg VENDOR="${VENDOR}" .
