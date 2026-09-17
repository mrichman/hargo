package hargo

import (
	"context"
	"testing"
	"time"
)

// Regression: the scan for the entries array matched any string token, key or
// value, so a HAR whose log carried a value equal to "entries" before the real key
// aborted the whole load test with a misleading "cannot decode HAR entry".
func TestReadStreamIgnoresEntriesAsAValue(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "comment value equal to entries",
			content: `{"log":{"version":"1.2","comment":"entries",
				"entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
				"request":{"method":"GET","url":"http://example.com/wanted"}}]}}`,
		},
		{
			name: "creator name equal to entries",
			content: `{"log":{"version":"1.2","creator":{"name":"entries","version":"1"},
				"entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
				"request":{"method":"GET","url":"http://example.com/wanted"}}]}}`,
		},
		{
			name: "nested key named entries in another object",
			content: `{"log":{"version":"1.2","pages":[{"id":"p1","title":"t","entries":"decoy"}],
				"entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
				"request":{"method":"GET","url":"http://example.com/wanted"}}]}}`,
		},
		{
			name: "array of strings containing entries",
			content: `{"log":{"version":"1.2","_tags":["entries","other"],
				"entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
				"request":{"method":"GET","url":"http://example.com/wanted"}}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := writeTempHAR(t, tt.content)
			entries := make(chan Entry, 8)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			go func() { _ = ReadStream(ctx, f, entries, ReadOptions{}) }()

			select {
			case e, ok := <-entries:
				if !ok {
					t.Fatal("entries channel closed without delivering the entry")
				}
				if e.Request.URL != "http://example.com/wanted" {
					t.Errorf("got URL %q, want http://example.com/wanted", e.Request.URL)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for the entry")
			}
		})
	}
}

// An "entries" key on some object other than the log must not be mistaken for the
// log's own entries array, even when it appears first and at the same depth.
func TestReadStreamPrefersTheLogEntriesKey(t *testing.T) {
	// A sibling object carrying its own "entries" key, serialized before "log".
	content := `{"_meta":{"entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"GET","url":"http://example.com/decoy"}}]},
		"log":{"version":"1.2","entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"GET","url":"http://example.com/wanted"}}]}}`

	f := writeTempHAR(t, content)
	entries := make(chan Entry, 8)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = ReadStream(ctx, f, entries, ReadOptions{}) }()

	select {
	case e, ok := <-entries:
		if !ok {
			t.Fatal("entries channel closed without delivering the entry")
		}
		if e.Request.URL != "http://example.com/wanted" {
			t.Errorf("got URL %q, want the log's own entries (http://example.com/wanted)",
				e.Request.URL)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an entry")
	}
}

// A HAR with no entries key at all must report that, not hang.
func TestReadStreamReportsMissingEntriesKey(t *testing.T) {
	f := writeTempHAR(t, `{"log":{"version":"1.2","creator":{"name":"x","version":"1"}}}`)
	entries := make(chan Entry, 8)

	done := make(chan error, 1)
	go func() { done <- ReadStream(t.Context(), f, entries, ReadOptions{}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("ReadStream() error = nil, want non-nil when there is no entries array")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadStream hung on a HAR with no entries key")
	}
}
