# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.1] - 2026-09-17

### Fixed

- A test assertion that failed on Windows, where the clock granularity is around
  15ms: `TestSummaryAccumulatorComputesFigures` recorded four results and then
  required a positive measured elapsed time, but four instant additions
  legitimately take no measurable time on such a platform. No library code
  changed, so v2.0.0 and v2.0.1 are identical for consumers; this release exists
  so the tagged build is green and its release artefacts are published.

## [2.0.0] - 2026-09-17

### Changed

- **The module path is now `github.com/mrichman/hargo/v2`.** v1 was published as
  `v1.0.1` in 2021, and because `v0.2.0` sorts below it, everything released
  afterwards was invisible to `go get`. The `/v2` suffix makes the current code
  resolvable. See [MIGRATING.md](MIGRATING.md) for the upgrade path; the command
  line interface is unaffected.
- Every entry point takes a `context.Context` as its first argument. Cancelling
  it stops the work, including while `Run` is waiting out a recorded delay and
  while `ReadStream` is blocked delivering an entry.
- Every entry point takes an `io.Reader` rather than a `*bufio.Reader`, and
  `ReadStream` takes an `io.ReadSeeker` rather than an `*os.File`. The byte order
  mark is skipped internally, so calling `NewReader` first is no longer required.
- The library no longer writes to stdout and no longer logs. `RunOptions`,
  `FetchOptions`, and `LoadTestOptions` carry an optional `Logger` and
  `Progress` writer; leaving either nil discards that output. `logrus` is
  replaced by the standard library's `log/slog` and is no longer a dependency.
- `LoadTest`'s seven positional parameters are replaced by `LoadTestOptions`,
  whose `InfluxDBURL` is a `*url.URL` where nil means "do not record".
- `Run` and `RunWithOptions` are merged into `Run`; `Fetch` and `FetchTo` are
  merged into `Fetch`, whose output directory is `FetchOptions.OutDir`.
- `EntryToRequest` takes an `EntryOptions` instead of a boolean, and the request
  it returns is bound to the caller's context.
- `Validate` returns only an error; the `bool` duplicated what the error already
  conveyed.
- `Dump` returns an error instead of logging one, and `ReadStream` returns an
  error instead of logging and closing silently.
- Every entry point takes an options struct, so that behaviour can be added
  without further signature churn: `ToCurl` and `ToCurlTo` take a `CurlOptions`,
  `Dump` and `DumpTo` a `DumpOptions`, and `ReadStream` a `ReadOptions` in place
  of its trailing `*slog.Logger`.
- `ReadStream` reports an error when a filter selects none of the entries it
  found, rather than ending as though the HAR were empty. Left silent, its replay
  loop would spin re-reading a document it discards.
- `Har`, `HarVersion`, `IgnoreHarCookies`, and `HarFile` are renamed to `HAR`,
  `HARVersion`, `IgnoreHARCookies`, and `HARFile`, since HAR is an initialism.
- An unreachable InfluxDB now fails a load test up front instead of replaying
  the entire HAR and discarding every result.
- A load test that ends because its duration elapsed, or because its context was
  cancelled, returns nil rather than an error.
- The CLI logs the command it is about to run at debug level rather than info,
  so ordinary runs print only their results. Diagnostics go to stderr, keeping
  `hargo dump` and `hargo curl` safe to pipe.
- `Ctrl-C` and `SIGTERM` now cancel the running command's context, unwinding a
  replay, fetch, or load test cleanly instead of killing it mid-request.

### Added

- **Entry filtering on every operation.** `EntryFilter`, carried on each options
  struct, selects entries by URL (an unanchored regular expression), HTTP method
  (case-insensitive), and recorded response status. Criteria combine, a zero
  filter selects everything, and a pattern that does not compile is reported
  rather than quietly matching nothing. Exposed on the command line as `--url`,
  `--method`, and `--status`, accepted by `fetch`, `curl`, `run`, `dump`, and
  `load`.
- **A load test summary.** `LoadTest` prints the request count, throughput,
  failure rate, exact latency percentiles, and a status breakdown when it
  finishes, and fills in `LoadTestOptions.Summary` for callers that want the
  figures. The elapsed time is measured rather than taken from the configured
  duration, so a test cut short reports what actually happened. This works
  whether or not InfluxDB is configured; previously the InfluxDB writer owned the
  results channel, which is what made a summary impossible alongside it.
- **`-o`/`--output` on `curl` and `dump`,** writing to a file instead of stdout.
  The output is rendered before the file is created, so a malformed HAR leaves no
  truncated file behind and does not clobber an existing one.
- **`RunOptions.FailOnStatus`** and `hargo run --fail-on-status`, which count a
  response of 400 or above as a failed entry. Off by default, because a recorded
  404 is often the expected result.
- `ToCurlTo`, which streams curl command lines to an `io.Writer` instead of
  assembling the whole output in memory.
- `TestResult.Duration`, the round trip as a `time.Duration`. The existing
  `Latency` is whole milliseconds, which truncates every request against a fast
  server to zero and made percentiles meaningless.
- `LoadTestOptions.Results`, an optional channel that receives every
  `TestResult`, so callers can collect results without running InfluxDB.
- `FetchOptions.AcceptErrorStatus`, which saves the response body even when the
  server reported an error status.
- `ParseEntryTime`, which parses a HAR `startedDateTime` across the formats real
  recorders emit.
- `MIGRATING.md`, a v1-to-v2 upgrade guide whose every example is compiled
  against the real API.
- A `justfile` mirroring every Makefile target, for [just](https://just.systems/).

### Removed

- `WritePoint`, `queryDB`, and `newInfluxDBClient` are unexported. They shared a
  package-level `var db string` written by one and read by the others, which was
  unsafe with more than one destination in a process.
- `RunWithOptions` and `FetchTo`, folded into `Run` and `Fetch`.

### Fixed

- `Run` no longer sleeps for centuries. `startedDateTime` was parsed with the
  single layout `2006-01-02T15:04:05.000Z`, which rejects the HAR spec's own
  example (`2009-07-24T19:20:30.45+01:00`), any number of fractional digits other
  than three, a missing fractional part, and an absent value. The error was
  discarded, so the entry became the zero time and the gap to the next entry
  saturated at roughly 292 years, which the replay then waited out. Timestamps are
  now parsed liberally, a failure is reported rather than silently producing no
  pacing at all, and an unparseable entry no longer poisons its successor's delay.
- `Entry` timings now decode. The field was tagged `pageTimings`, but the HAR spec
  names an entry's field `timings` and reserves `pageTimings` for a page, so entry
  timings were always zero. The wrong tag also masked a second defect: the fields
  were `int` while HAR timings are fractional, so correcting the tag alone made
  both checked-in fixtures fail to decode. Both are fixed together.
- `Cookie.Comment` is a `string` rather than a `bool`, so a spec-legal cookie
  comment no longer makes `Decode` and `Validate` reject the entire file.
- `Request.HeadersSize` is tagged `headersSize`, matching the specification and the
  response field; it was `headerSize` and so never decoded.
- `ToCurl` emits a valid command line. `-b` and `-H` each appended a trailing
  space and `-d` relied on that, so an entry with a body and no other flags
  produced `curl -X POST-d ...`, which curl reads as the method `POST-d`.
  Arguments are now joined rather than each branch appending a separator.
- `ToCurl` separates cookies with `"; "` as RFC 6265 requires, instead of `&`,
  which made a server read them as one cookie with an embedded value. Cookie names
  and values are no longer URL-encoded, so a space is not turned into `+` and a
  base64 or JSON value is not mangled.
- `ToCurl` shell-escapes the request method, which was the one HAR-controlled
  value reaching the shell unquoted.
- `ToCurl` derives `Content-Type` from `postData.mimeType` when the HAR records no
  such header, so curl no longer defaults a JSON body to form encoding.
- `Fetch` treats a 4xx or 5xx response as a failed download rather than saving the
  error page under the resource's own name and reporting success. `FetchOptions`
  gains `AcceptErrorStatus` for callers who want the previous behaviour.
- `Fetch` sends the recorded request body; it built every request with a nil body,
  so a recorded POST or PUT replayed empty.
- `Fetch` decodes a `Content-Encoding: deflate` body as zlib, which is what RFC
  9110 defines, falling back to raw deflate for servers that send it.
- A URL path can no longer name anything but a single file inside the output
  directory. `path.Base` only splits on forward slashes, so a backslash-separated
  path survived it and `filepath.Join` then interpreted it as directories on
  Windows. Names are also length-clamped and stripped of characters that would be
  rejected by the filesystem, which previously lost the resource outright.
- `Validate` rejects trailing data after the JSON document, reports a missing
  `log` object as such rather than as `unsupported HAR version ""`, requires an
  entries array, and checks that every entry has a parseable `startedDateTime` and
  a request with a method and a usable URL. It previously checked only that the
  bytes fitted the Go structs and that the version was `1.2`.
- `Decode` orders entries chronologically by parsed time rather than by raw string
  comparison, which disagreed whenever timestamps used different UTC offsets, and
  the sort is now stable so entries recorded in the same instant keep their order.
- `EntryToRequest` applies a recorded `Host` header, and an HTTP/2 `:authority`
  when there is none, by setting `Request.Host`. Adding it to the header map, as
  before, has no effect in `net/http`, so the recorded virtual host was lost.
- `EntryToRequest` derives `Content-Type` from `postData.mimeType` when the HAR
  records no such header.
- `postBody` prefers the recorded text over reconstructing params, and only
  URL-encodes params for a form body. Encoding a `multipart/form-data` body
  produced something that contradicted the recorded `Content-Type`.
- `isWebSocket` is case-insensitive, so a `WS://` entry is dropped rather than
  surviving the filter and failing later as an unsupported protocol scheme.
- `ReadStream` finds the log's own `entries` array. The scan matched any string
  token equal to `"entries"`, key or value, so a HAR containing something like
  `{"comment":"entries"}` or a nested key of that name failed the whole run with a
  misleading decode error.
- Every InfluxDB request is bounded by a timeout. The client defaults to none, and
  `Ping`'s argument is a `wait_for_leader` query parameter rather than a client
  timeout, so a stalled server could hang a load test indefinitely — after its
  workers had already stopped, because the consumer goroutine `LoadTest` joins on
  was the one blocked.
- Results are recorded with nanosecond precision and tagged by method, status, HAR
  file, and outcome. With millisecond precision and no tags, a point's identity
  collided for concurrent results and all but one were silently discarded.
- `StartTime` and `EndTime` are stored as Unix nanoseconds. As `time.Time` values
  they fell through to `%v` formatting, which includes the monotonic clock reading
  and is not queryable as a time. Points are also timestamped when the request
  started rather than when they happened to be recorded.
- Credentials in the InfluxDB URL are used to authenticate instead of being
  discarded, and the database name is validated as an identifier before being
  interpolated into InfluxQL.
- Establishing the InfluxDB connection uses its own deadline, so setup neither
  consumes the test's duration budget nor reports a connection failure as the
  test's deadline expiring.
- `LoadTest` serializes writes to `Progress`. Every worker was handed the caller's
  writer directly, which `os.Stdout` tolerates but a `bytes.Buffer` does not.
- `LoadTest` drains each response body before closing it. Closing an undrained
  body tears the connection down in `net/http`, so every request paid a fresh TCP
  and TLS handshake despite keep-alive being configured; `Latency` also now covers
  receiving the whole response rather than just the headers.
- Each worker's `http.Transport` sets an idle timeout and releases its connections
  when the worker exits. A hand-built transport has no idle expiry, so sockets
  were held until the process exited.
- `--debug` is accepted before or after the subcommand. urfave/cli v1 only
  reorders a command's own flags, so `hargo validate --debug f.har` failed with
  `flag provided but not defined`, and `hargo fetch f.har --debug` silently used
  `--debug` as the output directory and downloaded into a directory of that name.
- The redirect policy no longer rewrites `URL.Opaque` to the decoded path, which
  emitted a malformed request line for any redirect target containing an escape
  such as `%20`.
- Connecting to InfluxDB no longer loops forever when the server answers `/ping`
  without reporting a version. The retry counted only failed attempts, so a
  successful-but-versionless probe advanced neither the counter nor the loop.
- `ReadStream` no longer leaks a goroutine blocked on a channel send when the
  consumer stops reading; the send observes cancellation.
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
- `hargo fetch` no longer abandons every remaining resource when one download
  fails. A failing entry is logged and skipped, matching `hargo run`, and the
  number of failures is reported so the exit code is still non-zero.
- `hargo fetch` no longer needs O(N^2) filesystem probes to name N resources
  sharing a basename, and no longer gives up after 10,000 collisions. Names are
  claimed with `O_EXCL`, which also removes the race between finding a free name
  and creating the file.
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

- `hargo run` can now compress or skip the recorded delays between entries.
  Previously a HAR spanning ten minutes always took ten minutes to replay.
  - `--speed N` scales the delays, so `--speed 2` replays twice as fast.
  - `--no-wait` issues every request back to back.
  - `--max-delay D` caps the wait before any single entry, which is useful for
    HARs containing minutes of user idle time.
- `RunWithOptions(r *bufio.Reader, opts RunOptions) error` exposes the above to
  library callers. `Run` is unchanged and still replays in real time, and the
  zero value of `RunOptions` behaves identically to it.
- Release automation: CI now runs goreleaser on `v*` tags, gated on every other
  job passing, and validates `.goreleaser.yml` on every push. The four
  hand-rolled release scripts in `tools/` (425 lines) are removed in favour of
  it. `make release-check` and `make release-snapshot` verify a release locally
  before tagging.
- `actionlint` runs in CI and via `make actionlint`, so workflow errors are
  caught in review rather than on push.
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
- Docker builds are faster and more reliable: dependencies resolve through the
  module proxy rather than `GOPROXY=direct`, which was prone to truncated
  transfers; `go.mod`/`go.sum` are copied before the source so editing a `.go`
  file no longer re-downloads every module; and `.dockerignore` now excludes
  `.git`, fixtures and docs, cutting the build context from 6.1 MB to 92 KB.
- All GitHub Actions pinned to current majors, clearing the Node 20 deprecation
  warnings.
- The example Compose stack now pins InfluxDB 1.8.10 and Grafana 11.6.6, and
  drops the obsolete `version:` key.

### Notes on behaviour changes

- `hargo validate` on an unsupported HAR version now prints an error and exits
  254. It previously printed nothing and exited 0. The exit code for malformed
  input is unchanged at 254.
- `hargo run` now exits non-zero if any entry failed, having attempted them all.

## [0.2.0]

See the git history for releases prior to this changelog.
