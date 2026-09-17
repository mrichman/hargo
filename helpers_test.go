package hargo

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// openFixture opens a .har file from the repository's testdata/ directory and
// registers it for cleanup.
func openFixture(t *testing.T, rel string) *os.File {
	t.Helper()

	f, err := os.Open(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("opening fixture %s: %v", rel, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// captureOutput redirects os.Stdout and os.Stderr for the duration of fn and
// returns whatever was written to each. It is how the "silent by default"
// guarantee is enforced: a library that prints anywhere fails here.
//
// Output is drained concurrently so that fn cannot deadlock by filling the pipe
// buffer.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr pipe: %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	outCh := drain(outR)
	errCh := drain(errR)

	// Restore on the way out even if fn panics, or every later test in the
	// package writes into a closed pipe.
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
	}()

	fn()

	// The writers must close before the readers see EOF.
	_ = outW.Close()
	_ = errW.Close()

	return <-outCh, <-errCh
}

// drain reads r to EOF in the background and delivers the result.
func drain(r *os.File) <-chan string {
	ch := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		_ = r.Close()
		ch <- string(b)
	}()
	return ch
}
