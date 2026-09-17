# Migrating from hargo v1 to v2

v2 changes the import path, so nothing breaks until you choose to upgrade:

```go
import "github.com/mrichman/hargo/v2"
```

The command line interface is unchanged apart from being quieter by default.
Every flag, argument, and exit code behaves as it did in v1, so if you only use
the `hargo` binary there is nothing to migrate.

## Why v2

v1 was published as `v1.0.1` in 2021. Because `v0.2.0` sorts below that, the
work released afterwards was invisible to `go get`, and `go install
github.com/mrichman/hargo/cmd/hargo@latest` silently fetched the 2021 code. The
`/v2` suffix makes the current code the version the module proxy resolves.

## The three things to know

1. **The operations that do network I/O take a `context.Context` first**: `Run`,
   `Fetch`, `LoadTest`, `ReadStream`, and `EntryToRequest`. Cancelling it stops
   the work, including while a replay is waiting out a recorded delay. The pure
   parsers — `Decode`, `Validate`, `ToCurl`, `Dump`, and `DumpTo` — do not take
   one, because there is nothing to cancel.
2. **Every entry point takes an `io.Reader`,** not a `*bufio.Reader`. The BOM
   skip that `NewReader` provides now happens internally, so you no longer need
   to call it.
3. **The library is silent.** It never writes to stdout and never logs unless
   you pass a `Logger` or a `Progress` writer.
4. **Every entry point takes an options struct,** even when you do not need any
   of the options. Pass the zero value — `hargo.CurlOptions{}` — and behaviour is
   unchanged. This is what makes it possible to add entry filtering to all of
   them without another round of signature churn.

## Entry filtering

Every operation accepts an `EntryFilter` on its options struct. A zero filter
selects everything, and each criterion that is set must match:

```go
err := hargo.Run(ctx, f, hargo.RunOptions{
    Filter: hargo.EntryFilter{
        URL:    `/api/`,      // unanchored regular expression
        Method: []string{"POST"},
        Status: []int{200, 204},
    },
})
```

`URL` is a regular expression rather than a glob, because a glob's `*` does not
cross `/` — the intuitive `*.js` would match no full URL at all.

A pattern that does not compile is reported as an error, so a typo does not look
like a HAR with nothing in it.

## Function by function

### Decode, Validate, ToCurl

`Validate` no longer returns a redundant `bool`; a nil error means valid.
`ToCurl` takes a `CurlOptions`, and gains a streaming form.

```go
// v1
har, err := hargo.Decode(hargo.NewReader(f))
ok, err := hargo.Validate(hargo.NewReader(f))
cmd, err := hargo.ToCurl(hargo.NewReader(f))

// v2
har, err := hargo.Decode(f)
err := hargo.Validate(f)
cmd, err := hargo.ToCurl(f, hargo.CurlOptions{})
err := hargo.ToCurlTo(os.Stdout, f, hargo.CurlOptions{}) // streams
```

### Dump

`Dump` returns an error instead of logging one, and both forms take a
`DumpOptions`.

```go
// v1
hargo.Dump(hargo.NewReader(f))                 // errors were logged and lost
err := hargo.DumpTo(os.Stdout, hargo.NewReader(f))

// v2
err := hargo.Dump(f, hargo.DumpOptions{})              // writes to os.Stdout
err := hargo.DumpTo(os.Stdout, f, hargo.DumpOptions{})
```

### Run

`Run` and `RunWithOptions` are now one function. Its boolean parameters moved
onto `RunOptions`, and output is opt-in.

```go
// v1
err := hargo.Run(hargo.NewReader(f), ignoreHarCookies, insecureSkipVerify)
err := hargo.RunWithOptions(hargo.NewReader(f), hargo.RunOptions{Speed: 2})

// v2
err := hargo.Run(ctx, f, hargo.RunOptions{
	IgnoreHARCookies:   ignoreHarCookies,
	InsecureSkipVerify: insecureSkipVerify,
	Logger:             slog.Default(), // omit to stay silent
	Progress:           os.Stdout,      // omit to stay quiet
})
```

The zero `RunOptions` replays in real time, as `Run` did in v1.

### Fetch

`FetchTo` is gone; its output directory is a field on `FetchOptions`. Leaving
`OutDir` empty keeps v1's timestamped-directory behaviour.

```go
// v1
err := hargo.Fetch(hargo.NewReader(f))            // ./hargo-fetch-<timestamp>
err := hargo.FetchTo(hargo.NewReader(f), outdir)

// v2
err := hargo.Fetch(ctx, f, hargo.FetchOptions{})                  // timestamped
err := hargo.Fetch(ctx, f, hargo.FetchOptions{OutDir: outdir})
```

Two behaviour changes worth noting:

- **An error response is now a failed download.** v1 wrote the body whatever the
  status, so a 404 page was saved under the resource's own name and counted as a
  success. A 4xx or 5xx response now writes nothing and is reported in the
  failure tally. Set `AcceptErrorStatus` to get v1's behaviour back.
- **The recorded request body is now sent.** v1 built every request with a nil
  body, so a recorded POST or PUT replayed empty and usually saved an error page.

### LoadTest

The seven positional parameters became `LoadTestOptions`, and the `InfluxDB` URL
is now a `*url.URL` where nil means "do not record".

```go
// v1
err := hargo.LoadTest(harfile, file, workers, timeout, u, ignoreHarCookies, insecureSkipVerify)

// v2
err := hargo.LoadTest(ctx, file, hargo.LoadTestOptions{
	HARFile:            harfile,
	Workers:            workers,
	Duration:           timeout,
	InfluxDBURL:        &u,   // nil to discard results
	IgnoreHARCookies:   ignoreHarCookies,
	InsecureSkipVerify: insecureSkipVerify,
	Results:            results, // optional: receive every result yourself
})
```

Two behaviour changes worth noting:

- **An unreachable InfluxDB now fails the run immediately.** v1 logged a warning
  and replayed the whole HAR with every result discarded. If you relied on that,
  pass a nil `InfluxDBURL` instead.
- **The timeout is a deadline, not an error.** A run that ends because
  `Duration` elapsed, or because `ctx` was cancelled, returns nil.

`LoadTestOptions.Results` is new. It hands you every `TestResult` without
needing InfluxDB; you must keep reading from the channel for the duration of the
test, and closing it is your responsibility.

### EntryToRequest

```go
// v1
req, err := hargo.EntryToRequest(&entry, ignoreHarCookies)

// v2
req, err := hargo.EntryToRequest(ctx, &entry, hargo.EntryOptions{
	IgnoreHARCookies: ignoreHarCookies,
})
```

The returned request is bound to `ctx`.

### ReadStream

The `stop chan bool` is replaced by `ctx`, and the reader is an
`io.ReadSeeker` rather than an `*os.File`. Errors are returned instead of
logged, and cancellation is honoured even while blocked on a send.

```go
// v1
go hargo.ReadStream(file, entries, stop)
close(stop)

// v2
go func() { err := hargo.ReadStream(ctx, file, entries, hargo.ReadOptions{Logger: logger}) }()
cancel()
```

`ReadStream` still closes `entries` before returning. A filter that selects none
of the entries present is now an error rather than a silent end, since the replay
loop would otherwise spin re-reading a document it discards.

## Renamed identifiers

`Har` was an initialism spelled as a word. It is now `HAR` throughout, matching
Go's convention:

| v1                 | v2                 |
| ------------------ | ------------------ |
| `Har`              | `HAR`              |
| `HarVersion`       | `HARVersion`       |
| `IgnoreHarCookies` | `IgnoreHARCookies` |
| `HarFile`          | `HARFile`          |

The timing types are renamed to match the HAR specification, which they did not
before. `Entry.PageTimings` was tagged `pageTimings`, but the spec calls an
entry's field `timings` and reserves `pageTimings` for a page — so entry timings
never decoded at all:

| v1                              | v2                              |
| ------------------------------- | ------------------------------- |
| `Entry.PageTimings PageTimings` | `Entry.Timings Timings`         |
| `Page.PageTiming PageTiming`    | `Page.PageTimings PageTimings`  |
| `Request.HeaderSize`            | `Request.HeadersSize`           |

Timing fields are now `float64` rather than `int`, and `Entry.Time` is `float64`
rather than `float32`. HAR timings are fractional milliseconds — real recordings
contain values such as `2.7129996047616007` — which an integer field cannot hold.
`Cookie.Comment` is now `string` rather than `bool`, so a spec-legal cookie
comment no longer makes the whole file fail to decode.

## Removed from the public API

`WritePoint`, `queryDB`, and `newInfluxDBClient` are unexported. They shared a
package-level `var db string` that was written by one and read by the others,
which was unsafe with more than one destination in a process. InfluxDB is now
reached through `LoadTestOptions.InfluxDBURL`.

`logrus` is no longer a dependency. The library uses `log/slog` and, by default,
discards everything.
