# hargo task runner.
#
# This mirrors the Makefile; either can be used. Run `just` with no arguments to
# list the recipes, which are documented by the comment above each one, so there
# is no separate help text to drift out of date.

image := "hargo"
default_fuzztime := "30s"

# List the available recipes.
default:
    @just --list --unsorted

# Everything CI enforces, in one command.
check: tidy fmt vet lint actionlint test vuln

# Alias for `check`.
all: check

# Build the hargo binary into ./hargo.
build:
    #!/usr/bin/env bash
    set -euo pipefail
    go build -ldflags "$(just _ldflags)" -o hargo ./cmd/hargo

# Build and install hargo to $GOBIN.
install:
    #!/usr/bin/env bash
    set -euo pipefail
    go install -ldflags "$(just _ldflags)" ./cmd/hargo

# Pull the latest commit and reinstall.
update:
    git pull
    go install ./cmd/hargo

# Run all tests.
test:
    go test ./...

# Run all tests under the race detector, as CI does.
race:
    go test -race -timeout 10m ./...

# Run tests and print per-function statement coverage.
cover:
    go test -short -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -func=coverage.out

# Open the HTML coverage report in a browser.
cover-html: cover
    go tool cover -html=coverage.out

# Run golangci-lint.
lint:
    golangci-lint run --timeout 5m ./...

# Lint the GitHub Actions workflows.
actionlint:
    go run github.com/rhysd/actionlint/cmd/actionlint@latest -color

# Format the tree in place.
fmt:
    gofmt -l -w .

# Run go vet.
vet:
    go vet ./...

# Report known vulnerabilities in dependencies.
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Fail if `go mod tidy` would change go.mod or go.sum.
tidy:
    #!/usr/bin/env bash
    set -euo pipefail
    # Compare before and after rather than against git, so this is correct with
    # uncommitted work in the tree.
    cp go.mod go.mod.tidybak
    cp go.sum go.sum.tidybak
    trap 'rm -f go.mod.tidybak go.sum.tidybak' EXIT
    go mod tidy
    if ! cmp -s go.mod go.mod.tidybak || ! cmp -s go.sum go.sum.tidybak; then
        echo "go.mod/go.sum were not tidy and have now been updated; review the diff" >&2
        exit 1
    fi

# Fuzz each HAR parser. Pass a duration to override the default, e.g. `just fuzz 2m`.
fuzz fuzztime=default_fuzztime:
    #!/usr/bin/env bash
    set -euo pipefail
    for target in FuzzDecode FuzzValidate FuzzToCurl FuzzNewReader FuzzEntryToRequest; do
        echo "== $target =="
        go test -run '^$' -fuzz "^${target}\$" -fuzztime {{ fuzztime }} .
    done

# Remove build and coverage artefacts.
clean:
    rm -f hargo coverage.out
    rm -rf dist
    go clean -testcache

# Validate .goreleaser.yml without building anything.
release-check:
    go run github.com/goreleaser/goreleaser/v2@latest check

# Build all release artefacts into dist/ without publishing, to verify a release before tagging.
release-snapshot:
    # CI runs `goreleaser release` on v* tags; this is the local dry run.
    go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean

# Build the container image.
docker:
    #!/usr/bin/env bash
    set -euo pipefail
    docker build --rm -t {{ image }} \
        --build-arg VERSION="$(go run tools/build-version.go)" \
        --build-arg HASH="$(git rev-parse --short HEAD)" \
        --build-arg DATE="$(go run tools/build-date.go)" \
        --build-arg BUILD_DATE="$(date -R)" \
        --build-arg VCS_URL="$(basename "$(git rev-parse --show-toplevel)")" \
        --build-arg VCS_REF="$(git rev-parse --short HEAD)" \
        --build-arg NAME="{{ image }}" \
        --build-arg VENDOR="Mark A. Richman" \
        .

# Print the linker flags that stamp version metadata into the binary.
#
# CompileDate contains spaces, so it is wrapped in single quotes: go's -ldflags
# parser does its own quote-aware splitting, and without them the date would be
# split into separate flags. Verified to produce a binary byte-for-byte
# identical to the Makefile's.
[private]
_ldflags:
    #!/usr/bin/env bash
    set -euo pipefail
    printf -- "-s -w -X main.Version=%s -X main.CommitHash=%s -X 'main.CompileDate=%s'" \
        "$(go run tools/build-version.go)" \
        "$(git rev-parse --short HEAD)" \
        "$(go run tools/build-date.go)"
