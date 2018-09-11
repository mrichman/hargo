.PHONY: runtime

IMAGE := hargo
VERSION := $(shell git rev-parse HEAD)
BUILD_DATE := $(shell date -R)
VCS_URL := $(shell basename `git rev-parse --show-toplevel`)
VCS_REF := $(shell git log -1 --pretty=%h)
VERSION = $(shell go run tools/build-version.go)
HASH = $(shell git rev-parse --short HEAD)
DATE = $(shell go run tools/build-date.go)

GOBIN ?= $($GOPATH)/bin

# Builds hargo after checking dependencies
build: deps
	go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" -o hargo ./cmd/hargo
# Builds hargo after checking dependencies
build-all: build

# Builds hargo without checking for dependencies
build-quick:
	go build -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" -o hargo ./cmd/hargo

# Same as 'build' but installs to $GOBIN afterward
install: deps
	go install -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" ./cmd/hargo

# Same as 'build-all' but installs to $GOBIN afterward
install-all: install

# Same as 'build-quick' but installs to $GOBIN afterward
install-quick:
	go install -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" ./cmd/hargo

# Checks for dependencies
deps:
	glide install

update:
	git pull
	glide install

test:
	glide install
	go test ./cmd/hargo/main

clean:
	rm -f hargo

docker:
	docker build --rm -t ${IMAGE} --build-arg VERSION="${VERSION}" \
	--build-arg BUILD_DATE="${BUILD_DATE}" \
	--build-arg VCS_URL="${VCS_URL}" \
	--build-arg VCS_REF="${VCS_REF}" \
	--build-arg NAME="${NAME}" \
	--build-arg VENDOR="${VENDOR}" .