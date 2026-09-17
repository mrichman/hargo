# Contributing

If you find any bugs, please report them! I am also happy to accept pull requests from anyone.

You can use the [GitHub issue tracker](https://github.com/mrichman/hargo/issues) to report bugs, ask questions, or suggest new features.

For security issues, please follow [SECURITY.md](SECURITY.md) rather than opening a public issue.

## Getting set up

hargo requires Go 1.27.1 or newer and has no other build dependencies.

```sh
git clone https://github.com/mrichman/hargo.git
cd hargo
make build
```

Note that `make build` and `make install` stamp the version using
`git describe --tags`, so build from a clone with tags rather than a source
tarball.

## Before opening a pull request

Run the same checks CI runs:

```sh
make check     # tidy + fmt + vet + lint + test + vuln
```

Or individually:

| Command      | What it does                                    |
| ------------ | ----------------------------------------------- |
| `make test`  | Runs all tests                                  |
| `make race`  | Runs all tests under the race detector          |
| `make cover` | Prints per-function statement coverage          |
| `make lint`  | Runs `golangci-lint`                            |
| `make actionlint` | Lints the GitHub Actions workflows         |
| `make fmt`   | Formats the tree with `gofmt`                   |
| `make vuln`  | Checks dependencies with `govulncheck`          |
| `make fuzz`  | Fuzzes each HAR parser (`FUZZTIME=2m` to extend) |
| `make help`  | Lists every target                              |

`make lint` needs [golangci-lint](https://golangci-lint.run/welcome/install/)
on your `PATH`. Everything else uses only the Go toolchain.

### just

A [justfile](justfile) mirrors every target above, if you prefer
[just](https://just.systems/):

```sh
just           # list the recipes
just check     # the same aggregate check
just fuzz 2m   # a recipe argument rather than FUZZTIME=2m
```

`just build` produces a binary byte-for-byte identical to `make build`. Both
files are maintained together, so a change to one belongs in the other.

## Releasing

Releases are built by [goreleaser](https://goreleaser.com/) from
`.goreleaser.yml`, driven by CI. Tagging is the only manual step:

```sh
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

The `release` job is gated on every other job passing, so a tag cannot publish
artefacts that fail their own test suite. It creates a draft release; review and
publish it from the GitHub UI.

To check the release before tagging:

```sh
make release-check      # validate .goreleaser.yml
make release-snapshot   # build all artefacts into dist/ without publishing
```

Both invoke goreleaser via `go run`, so they need network access but no local
install. CI also runs `goreleaser check` on every push, so a broken release
config is caught long before tag time.

## Tests

Please include a test with any bug fix, written so it fails before your change.
The suite uses only the standard library: table-driven tests plus
`net/http/httptest` for anything that makes requests. Small inline HAR literals
are preferred over the fixtures in `testdata/` for edge cases, since they keep the
expected behaviour visible in the test.

If you touch HAR parsing, run `make fuzz` as well.

## A note on the helper programs

`tools/build-version.go` and `tools/build-date.go` carry the `ignore` build tag
so each can be a standalone `package main`. Because `go mod tidy` skips
`ignore`-constrained files, `tools/deps.go` exists to pin their dependencies.
Do not delete it, or `make build` will stop working after the next tidy.
