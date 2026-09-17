# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `Decode` no longer panics with an index-out-of-range when a HAR contains more
  than one trailing `ws://` entry. The WebSocket filter now filters in place
  instead of swap-deleting while ranging the original slice, and `wss://`
  entries are dropped as well.
- `EntryToRequest` no longer discards `http.NewRequest`'s error and return
  `(nil, nil)` for an invalid method or URL, which callers then dereferenced.
- `Run` no longer panics on an entry whose URL cannot be parsed. Individual bad
  or unreachable entries are logged and skipped so the rest of the replay
  proceeds, and `Run` reports how many failed.
- `ToCurl` no longer swallows the JSON decode error and return `("", nil)`.
- `hargo curl` no longer emits HTTP/2 pseudo-headers such as `-H ':method: GET'`,
  which curl rejects because a colon is not legal in a header field name.
- `hargo curl` now emits a request body for any method that carries one, not just
  `POST`, and reads `postData.params` in addition to `postData.text`, so form
  bodies are no longer dropped.
- `hargo fetch` no longer writes gzip- or deflate-encoded bytes to disk under a
  `.html`/`.js` name. The recorded `Accept-Encoding` is no longer replayed, and a
  still-encoded response body is decompressed before being saved.
- `hargo fetch` no longer overwrites files when two entries share a basename; the
  second is saved with a numeric suffix that preserves the extension.
- `hargo fetch` no longer fails outright on a URL with no path, where
  `path.Base` yields `"."` and the file could not be created.
- `hargo fetch` no longer leaves a zero-byte file behind when a download fails.
- `Validate` no longer calls `os.Exit(-2)`, which made the failure path
  untestable and terminated any process using the library. It returns a
  descriptive error instead, and reports an unsupported HAR version rather than
  silently returning `(false, nil)`.
- `ReadStream` no longer calls `log.Fatal` on malformed input, and no longer
  spins forever at full CPU when a HAR yields no entries — the outer replay loop
  now observes the stop signal.
- `LoadTest` no longer closes the results channel while workers may still be
  sending to it, which could panic with "send on closed channel". Workers now
  exit when signalled instead of looping indefinitely.
- `WritePoint` no longer calls `Write` on a nil client when InfluxDB is
  unreachable; results are drained and a warning is logged.
- The CLI now propagates errors and exits non-zero. `hargo fetch`, `run`, and
  `load` previously discarded their errors and always exited 0.
- `hargo fetch` now honours the documented `[output dir]` argument, which was
  parsed as usage text but never read.
- `make test` now runs `go test ./...`. It previously targeted
  `./cmd/hargo/main`, a directory that does not exist, so CI never ran a test.
- Build version stamping now works. `tools/build-version.go` failed to parse the
  `v` prefix and the trailing newline from `git describe`, so every build was
  stamped `0.0.0-unknown`.
- `go mod tidy` no longer drops `blang/semver` and break `make build`; a
  build-tagged `tools/deps.go` pins it.
- Removed six unreachable `os.Exit(-1)` calls that followed `log.Fatal`, which
  already exits.

### Added

- A test suite covering the library, from zero tests to full coverage of every
  exported function except the CLI wiring.
- Fuzz targets for `Decode`, `Validate`, `ToCurl`, `NewReader`, and
  `EntryToRequest`.
- `DumpTo(w io.Writer, r *bufio.Reader) error` and
  `FetchTo(r *bufio.Reader, outdir string) error`, so output is injectable.
  `Dump` and `Fetch` delegate to them.
- Exported `HarVersion` constant.
- GitHub Actions CI: test matrix across Linux/macOS/Windows, race detector,
  `golangci-lint`, `govulncheck`, a fuzz smoke run, tidy/gofmt verification, and
  a multi-platform Docker build.
- `.golangci.yml`, `.goreleaser.yml`, and Dependabot configuration.
- Makefile targets: `race`, `lint`, `fmt`, `vet`, `vuln`, `tidy`, `fuzz`,
  `cover`, `cover-html`, `check`, and `help`.

### Changed

- Go 1.27.1 is now the minimum version.
- Dependencies updated: `logrus` 1.8.1 to 1.10.2, `urfave/cli` 1.21.0 to
  1.22.17, `golang.org/x/net` to v0.59.0, `blang/semver` to v4, and
  `github.com/alessio/shellescape` to `al.essio.dev/pkg/shellescape` v1.6.1
  following its module rename.
- `Transport.Dial` replaced with `DialContext`, deprecated since Go 1.7.
- Travis CI removed in favour of GitHub Actions.
- The example Compose stack now pins InfluxDB 1.8.10 and Grafana 11.6.6, and
  drops the obsolete `version:` key.

### Notes on behaviour changes

- `hargo validate` on an unsupported HAR version now prints an error and exits
  254. It previously printed nothing and exited 0. The exit code for malformed
  input is unchanged at 254.
- `hargo run` now exits non-zero if any entry failed, having attempted them all.

## [0.2.0]

See the git history for releases prior to this changelog.
