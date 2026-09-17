package hargo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTempHar writes content to a temp file and returns the open *os.File,
// since ReadStream needs a seekable file rather than an io.Reader.
func writeTempHar(t *testing.T, content string) *os.File {
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
	f := writeTempHar(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://example.com/b")))

	entries := make(chan Entry, 8)
	stop := make(chan bool)

	go ReadStream(f, entries, stop)

	first := <-entries
	if first.Request.URL != "http://example.com/a" {
		t.Errorf("first entry URL = %q, want http://example.com/a", first.Request.URL)
	}

	// ReadStream checks stop only after handing over an entry, so signalling
	// here terminates it on the next iteration.
	close(stop)

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
			t.Fatal("ReadStream did not close the entries channel after stop")
		}
	}
}

func TestReadStreamLoopsOverFile(t *testing.T) {
	f := writeTempHar(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/only")))

	entries := make(chan Entry, 64)
	stop := make(chan bool)

	go ReadStream(f, entries, stop)

	// The single entry should be replayed repeatedly as the reader seeks back
	// to the start of the file.
	for i := 0; i < 3; i++ {
		select {
		case e := <-entries:
			if e.Request.URL != "http://example.com/only" {
				t.Fatalf("iteration %d URL = %q", i, e.Request.URL)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for entry %d", i)
		}
	}
	close(stop)
}

// Regression: ReadStream called log.Fatal on a malformed file, terminating the
// caller's process. It must report the problem and close the channel instead.
func TestReadStreamMalformedInputDoesNotExit(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "no entries key", content: `{"log":{"version":"1.2"}}`},
		{name: "entries not an array", content: `{"log":{"version":"1.2","entries":42}}`},
		{name: "truncated entry", content: `{"log":{"version":"1.2","entries":[{"request":`},
		{name: "empty file", content: ``},
		{name: "not json", content: `hello world`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := writeTempHar(t, tt.content)
			entries := make(chan Entry, 8)
			stop := make(chan bool)
			defer close(stop)

			done := make(chan struct{})
			go func() {
				defer close(done)
				ReadStream(f, entries, stop)
			}()

			// The channel must be closed rather than the process killed.
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("ReadStream did not return on malformed input")
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

	f := writeTempHar(t, content)
	entries := make(chan Entry, 8)
	stop := make(chan bool)

	go ReadStream(f, entries, stop)

	var got []string
	for i := 0; i < 2; i++ {
		select {
		case e := <-entries:
			got = append(got, e.Request.URL)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}
	}
	close(stop)

	want := []string{"http://example.com/a", "http://example.com/c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry %d = %q, want %q", i, got[i], w)
		}
	}
}
