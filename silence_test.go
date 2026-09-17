package hargo

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The central promise of the v2 API: a library call writes nothing and logs
// nothing unless the caller supplies a Logger or a Progress writer. Anything
// that leaks to os.Stdout or a package-level logger is a defect.
func TestLibraryIsSilentByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	// A HAR mixing a reachable entry with an unbuildable one, so both the
	// success and the failure paths are exercised.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://[::1]:namedport/bad"))

	tests := []struct {
		name string
		call func(t *testing.T)
	}{
		{
			name: "Run",
			call: func(t *testing.T) {
				// The malformed entry makes this return an error; that is the
				// reporting channel, and it must not also print.
				_ = Run(t.Context(), strings.NewReader(har), RunOptions{NoWait: true})
			},
		},
		{
			name: "Fetch",
			call: func(t *testing.T) {
				_ = Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: t.TempDir()})
			},
		},
		{
			name: "Decode",
			call: func(t *testing.T) {
				_, _ = Decode(strings.NewReader(`{"log":{ BROKEN`))
			},
		},
		{
			name: "Validate",
			call: func(t *testing.T) {
				_ = Validate(strings.NewReader(`{"log":{ BROKEN`))
			},
		},
		{
			name: "ToCurl",
			call: func(t *testing.T) {
				_, _ = ToCurl(strings.NewReader(`{"log":{ BROKEN`), CurlOptions{})
			},
		},
		{
			name: "Dump",
			call: func(t *testing.T) {
				// DumpTo writes to the caller's writer; Dump with a broken
				// document must still print nothing of its own.
				_ = Dump(strings.NewReader(`{"log":{ BROKEN`), DumpOptions{})
			},
		},
		{
			name: "LoadTest",
			call: func(t *testing.T) {
				// A nil Progress must suppress both the per-request lines and the
				// summary block, not just the former.
				_ = LoadTest(t.Context(), strings.NewReader(har), LoadTestOptions{
					Workers:  1,
					Duration: 100 * time.Millisecond,
				})
			},
		},
		{
			name: "NewReader with BOM",
			call: func(t *testing.T) {
				_, _ = io.ReadAll(NewReader(strings.NewReader("\xef\xbb\xbf{}")))
			},
		},
		{
			name: "ReadStream",
			call: func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				f := writeTempHAR(t, `{"log":{"version":"1.2","entries":[{"request":`)
				entries := make(chan Entry, 8)
				_ = ReadStream(ctx, f, entries, ReadOptions{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := captureOutput(t, func() { tt.call(t) })
			if stdout != "" {
				t.Errorf("wrote %q to stdout, want nothing", stdout)
			}
			if stderr != "" {
				t.Errorf("wrote %q to stderr, want nothing", stderr)
			}
		})
	}
}

// A supplied Logger and Progress writer must actually receive output, otherwise
// "silent by default" would just be "silent".
func TestRunWritesToSuppliedLoggerAndProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://[::1]:namedport/bad"))

	var logBuf, progBuf bytes.Buffer
	err := Run(t.Context(), strings.NewReader(har), RunOptions{
		NoWait:   true,
		Logger:   slog.New(slog.NewTextHandler(&logBuf, nil)),
		Progress: &progBuf,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil for the unbuildable entry")
	}

	if !strings.Contains(logBuf.String(), "skipping entry") {
		t.Errorf("Logger got %q, want it to mention the skipped entry", logBuf.String())
	}
	if !strings.Contains(progBuf.String(), srv.URL+"/a") {
		t.Errorf("Progress got %q, want it to mention the replayed URL", progBuf.String())
	}
}

func TestFetchWritesToSuppliedProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/f.txt"))

	var progBuf bytes.Buffer
	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{
		OutDir:   t.TempDir(),
		Progress: &progBuf,
	}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	for _, want := range []string{srv.URL + "/f.txt", "Downloaded", "7 bytes"} {
		if !strings.Contains(progBuf.String(), want) {
			t.Errorf("Progress got %q, want it to contain %q", progBuf.String(), want)
		}
	}
}

// Cancelling the context must abandon a replay part-way rather than run to
// completion.
func TestRunHonoursContextCancellation(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a"))

	err := Run(ctx, strings.NewReader(har), RunOptions{NoWait: true})
	if !isDone(err) {
		t.Errorf("Run() error = %v, want a context error", err)
	}
	if hits != 0 {
		t.Errorf("server received %d requests, want 0 for a cancelled context", hits)
	}
}

// A cancelled context must stop the replay while it is waiting out a recorded
// delay, not only between requests.
func TestRunCancellationInterruptsDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// An hour between the two entries: without cancellation this would hang.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.000Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T01:00:00.000Z", "GET", srv.URL+"/b"))

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := Run(ctx, strings.NewReader(har), RunOptions{})
	elapsed := time.Since(start)

	if !isDone(err) {
		t.Errorf("Run() error = %v, want a context error", err)
	}
	if elapsed > 30*time.Second {
		t.Errorf("Run took %v to observe cancellation during a delay", elapsed)
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a"))

	err := Fetch(ctx, strings.NewReader(har), FetchOptions{OutDir: t.TempDir()})
	if !isDone(err) {
		t.Errorf("Fetch() error = %v, want a context error", err)
	}
	if hits != 0 {
		t.Errorf("server received %d requests, want 0 for a cancelled context", hits)
	}
}

// EntryToRequest must bind the request to the caller's context, so that
// cancelling it cancels the request.
func TestEntryToRequestBindsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	e := &Entry{}
	e.Request.Method = http.MethodGet
	e.Request.URL = "http://example.com/a"

	req, err := EntryToRequest(ctx, e, EntryOptions{})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}

	if req.Context() == context.Background() {
		t.Error("request is bound to context.Background(), want the supplied context")
	}

	cancel()
	select {
	case <-req.Context().Done():
	case <-time.After(5 * time.Second):
		t.Error("cancelling the supplied context did not cancel the request")
	}
}

// The zero RunOptions must replay in real time, which means honouring a
// recorded gap rather than treating an unset Speed as "no delay".
func TestRunZeroOptionsHonoursRecordedDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 300ms between the entries.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.000Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:00:00.300Z", "GET", srv.URL+"/b"))

	start := time.Now()
	if err := Run(t.Context(), strings.NewReader(har), RunOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("Run finished in %v, want it to wait out the recorded ~300ms gap", elapsed)
	}
}

func TestLoggerOrDiscard(t *testing.T) {
	if got := loggerOrDiscard(nil); got == nil {
		t.Fatal("loggerOrDiscard(nil) = nil, want a usable logger")
	}
	if loggerOrDiscard(nil).Enabled(context.Background(), slog.LevelError) {
		t.Error("the fallback logger is enabled, want everything discarded")
	}

	want := slog.New(slog.NewTextHandler(io.Discard, nil))
	if got := loggerOrDiscard(want); got != want {
		t.Error("loggerOrDiscard did not return the supplied logger")
	}
}

func TestWriterOrDiscard(t *testing.T) {
	if got := writerOrDiscard(nil); got != io.Discard {
		t.Errorf("writerOrDiscard(nil) = %v, want io.Discard", got)
	}

	var buf bytes.Buffer
	if got := writerOrDiscard(&buf); got != &buf {
		t.Error("writerOrDiscard did not return the supplied writer")
	}
}
