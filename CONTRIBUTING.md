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
| `make fmt`   | Formats the tree with `gofmt`                   |
| `make vuln`  | Checks dependencies with `govulncheck`          |
| `make fuzz`  | Fuzzes each HAR parser (`FUZZTIME=2m` to extend) |
| `make help`  | Lists every target                              |

`make lint` needs [golangci-lint](https://golangci-lint.run/welcome/install/)
on your `PATH`. Everything else uses only the Go toolchain.

## Tests

Please include a test with any bug fix, written so it fails before your change.
The suite uses only the standard library: table-driven tests plus
`net/http/httptest` for anything that makes requests. Small inline HAR literals
are preferred over the fixtures in `test/` for edge cases, since they keep the
expected behaviour visible in the test.

If you touch HAR parsing, run `make fuzz` as well.

## A note on the helper programs

`tools/build-version.go` and `tools/build-date.go` carry the `ignore` build tag
so each can be a standalone `package main`. Because `go mod tidy` skips
`ignore`-constrained files, `tools/deps.go` exists to pin their dependencies.
Do not delete it, or `make build` will stop working after the next tidy.
