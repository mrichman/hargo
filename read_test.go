package hargo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTempHAR writes content to a temp file and returns the open *os.File,
// since ReadStream needs a seekable reader rather than a plain io.Reader.
func writeTempHAR(t *testing.T, content string) *os.File {
	t.Helper()

	p := filepath.Join(t.TempDir(), "test.har")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp har: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("opening temp har: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestReadStreamDeliversEntries(t *testing.T) {
	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://example.com/b")))

	entries := make(chan Entry, 8)
	ctx, cancel := context.WithCancel(t.Context())

	go func() { _ = ReadStream(ctx, f, entries, nil) }()

	first := <-entries
	if first.Request.URL != "http://example.com/a" {
		t.Errorf("first entry URL = %q, want http://example.com/a", first.Request.URL)
	}

	// Cancelling terminates the stream; it is observed both between entries and
	// while blocked on a send.
	cancel()

	// Drain until the producer closes the channel.
	drained := 1
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-entries:
			if !ok {
				if drained < 1 {
					t.Errorf("drained %d entries, want at least 1", drained)
				}
				return
			}
			drained++
		case <-timeout:
			t.Fatal("ReadStream did not close the entries channel after cancellation")
		}
	}
}

func TestReadStreamLoopsOverFile(t *testing.T) {
	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/only")))

	entries := make(chan Entry, 64)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = ReadStream(ctx, f, entries, nil) }()

	// The single entry should be replayed repeatedly as the reader seeks back
	// to the start of the file.
	for i := range 3 {
		select {
		case e := <-entries:
			if e.Request.URL != "http://example.com/only" {
				t.Fatalf("iteration %d URL = %q", i, e.Request.URL)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for entry %d", i)
		}
	}
}

// Cancellation must be observed even when nothing is reading the channel, or the
// goroutine leaks blocked on a send.
func TestReadStreamReturnsWhenNobodyIsReading(t *testing.T) {
	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://example.com/b")))

	// Unbuffered, and never read from, so ReadStream blocks on its first send.
	entries := make(chan Entry)
	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() { done <- ReadStream(ctx, f, entries, nil) }()

	cancel()

	select {
	case err := <-done:
		if !isDone(err) {
			t.Errorf("ReadStream() error = %v, want a context error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadStream did not return while blocked on a send")
	}
}

// Regression: ReadStream called log.Fatal on a malformed file, terminating the
// caller's process. It must report the problem and close the channel instead.
func TestReadStreamMalformedInputDoesNotExit(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "no entries key", content: `{"log":{"version":"1.2"}}`, wantErr: true},
		{name: "entries not an array", content: `{"log":{"version":"1.2","entries":42}}`},
		{name: "truncated entry", content: `{"log":{"version":"1.2","entries":[{"request":`, wantErr: true},
		{name: "empty file", content: ``, wantErr: true},
		{name: "not json", content: `hello world`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := writeTempHAR(t, tt.content)
			entries := make(chan Entry, 8)

			done := make(chan error, 1)
			go func() { done <- ReadStream(t.Context(), f, entries, nil) }()

			// The channel must be closed rather than the process killed.
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("ReadStream did not return on malformed input")
			}

			if tt.wantErr && err == nil {
				t.Error("ReadStream() error = nil, want non-nil for malformed input")
			}

			select {
			case _, ok := <-entries:
				if ok {
					t.Error("got an entry from malformed input, want a closed channel")
				}
			default:
				t.Error("entries channel was neither closed nor readable")
			}
		})
	}
}

func TestReadStreamSkipsEntriesWithoutURL(t *testing.T) {
	// The second entry has no request URL and must be filtered out.
	content := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T00:00:00.001Z","request":{"method":"GET","url":"http://example.com/a"}},
		{"startedDateTime":"2024-01-01T00:00:00.002Z","request":{"method":"GET","url":""}},
		{"startedDateTime":"2024-01-01T00:00:00.003Z","request":{"method":"GET","url":"http://example.com/c"}}
	]}}`

	f := writeTempHAR(t, content)
	entries := make(chan Entry, 8)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = ReadStream(ctx, f, entries, nil) }()

	var got []string
	for range 2 {
		select {
		case e := <-entries:
			got = append(got, e.Request.URL)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}
	}

	want := []string{"http://example.com/a", "http://example.com/c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry %d = %q, want %q", i, got[i], w)
		}
	}
}
