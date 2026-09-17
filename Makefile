.PHONY: all build install update test race cover cover-html lint actionlint fmt vet vuln tidy fuzz check clean docker release-check release-snapshot help

IMAGE := hargo
BUILD_DATE := $(shell date -R)
VCS_URL := $(shell basename `git rev-parse --show-toplevel`)
VCS_REF := $(shell git log -1 --pretty=%h)
VERSION = $(shell go run tools/build-version.go)
HASH = $(shell git rev-parse --short HEAD)
DATE = $(shell go run tools/build-date.go)
GOBIN ?= $($GOPATH)/bin
GO = $(shell which go)
FUZZTIME ?= 30s

# Everything CI enforces, in one command.
all: check

check: tidy fmt vet lint actionlint test vuln

# Builds hargo
build:
	$(GO) build -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" -o hargo ./cmd/hargo

# Same as 'build' but installs to $GOBIN afterward
install:
	$(GO) install -ldflags "-s -w -X main.Version=$(VERSION) -X main.CommitHash=$(HASH) -X 'main.CompileDate=$(DATE)'" ./cmd/hargo

update:
	git pull
	$(GO) install

test:
	$(GO) test ./...

# Tests under the race detector, as CI runs them
race:
	$(GO) test -race -timeout 10m ./...

# Same as 'test' but reports per-function statement coverage
cover:
	$(GO) test -short -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out

# Opens the HTML coverage report in a browser
cover-html: cover
	$(GO) tool cover -html=coverage.out

lint:
	golangci-lint run --timeout 5m ./...

# Lints the GitHub Actions workflows
actionlint:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@latest -color

fmt:
	gofmt -l -w .

vet:
	$(GO) vet ./...

# Reports known vulnerabilities in dependencies
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Fails if 'go mod tidy' would change go.mod/go.sum. Compares before and after
# rather than against git, so it is correct with uncommitted work in the tree.
tidy:
	@cp go.mod go.mod.tidybak && cp go.sum go.sum.tidybak
	@$(GO) mod tidy
	@if cmp -s go.mod go.mod.tidybak && cmp -s go.sum go.sum.tidybak; then \
		rm -f go.mod.tidybak go.sum.tidybak; \
	else \
		rm -f go.mod.tidybak go.sum.tidybak; \
		echo "go.mod/go.sum were not tidy and have now been updated; review the diff"; \
		exit 1; \
	fi

# Fuzzes each HAR parser for FUZZTIME (default 30s)
fuzz:
	for target in FuzzDecode FuzzValidate FuzzToCurl FuzzNewReader FuzzEntryToRequest; do \
		echo "== $$target =="; \
		$(GO) test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZTIME) . || exit 1; \
	done

clean:
	rm -f hargo coverage.out
	rm -rf dist
	$(GO) clean -testcache

# Validates .goreleaser.yml without building anything
release-check:
	$(GO) run github.com/goreleaser/goreleaser/v2@latest check

# Builds all release artefacts into dist/ without publishing. Use this to verify
# a release before tagging; CI runs 'goreleaser release' on v* tags.
release-snapshot:
	$(GO) run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean

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

help:
	@echo "build       build the hargo binary"
	@echo "install     build and install to \$$GOBIN"
	@echo "test        run all tests"
	@echo "race        run all tests under the race detector"
	@echo "cover       run tests and print per-function coverage"
	@echo "cover-html  open the HTML coverage report"
	@echo "lint        run golangci-lint"
	@echo "actionlint  lint the GitHub Actions workflows"
	@echo "fmt         gofmt the tree in place"
	@echo "vet         run go vet"
	@echo "vuln        run govulncheck"
	@echo "tidy        verify go.mod/go.sum are tidy"
	@echo "fuzz        fuzz each parser for FUZZTIME (default $(FUZZTIME))"
	@echo "check       tidy + fmt + vet + lint + actionlint + test + vuln"
	@echo "clean       remove build and coverage artefacts"
	@echo "docker      build the container image"
	@echo "release-check     validate .goreleaser.yml"
	@echo "release-snapshot  build release artefacts into dist/ without publishing"
