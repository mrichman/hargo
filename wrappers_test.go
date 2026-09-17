package hargo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Dump is a thin wrapper over DumpTo that targets stdout; this only checks that
// the wrapper wires up without panicking.
func TestDumpWritesToStdout(t *testing.T) {
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/a"))
	Dump(NewReader(strings.NewReader(har)))
}

func TestDumpMalformedInputDoesNotPanic(t *testing.T) {
	Dump(NewReader(strings.NewReader(`{"log":{ BROKEN`)))
}

// Fetch is a thin wrapper over FetchTo that derives a timestamped output
// directory. Chdir into a temp dir so the artefacts do not land in the repo.
func TestFetchCreatesTimestampedDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	t.Chdir(tmp)

	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/f.txt"))
	if err := Fetch(NewReader(strings.NewReader(har))); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmp, "hargo-fetch-*"))
	if err != nil {
		t.Fatalf("globbing: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d hargo-fetch-* dirs, want 1", len(matches))
	}

	got, err := os.ReadFile(filepath.Join(matches[0], "f.txt"))
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("f.txt = %q, want %q", got, "payload")
	}
}

func TestFetchMalformedJSONReturnsError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Fetch(NewReader(strings.NewReader(`{"log":{ BROKEN`))); err == nil {
		t.Error("Fetch() error = nil, want non-nil for malformed JSON")
	}
}
