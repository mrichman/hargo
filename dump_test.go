package hargo

import (
	"bytes"
	"strings"
	"testing"
)

func TestDumpToWritesEntryDetails(t *testing.T) {
	har := `{"log":{"version":"1.2","creator":{"name":"Firefox","version":"120"},"entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"serverIPAddress":"93.184.216.34",
		"request":{
			"method":"POST",
			"url":"http://example.com/submit?q=hello",
			"httpVersion":"HTTP/1.1",
			"headers":[{"name":"Accept","value":"application/json"}],
			"queryString":[{"name":"q","value":"hello"}],
			"cookies":[{"name":"session","value":"abc123"}],
			"postData":{"mimeType":"application/x-www-form-urlencoded","params":[{"name":"user","value":"bob"}]}
		},
		"response":{"status":201,"headers":[{"name":"Location","value":"/created"}]}
	}]}}`

	var buf bytes.Buffer
	if err := DumpTo(&buf, strings.NewReader(har), DumpOptions{}); err != nil {
		t.Fatalf("DumpTo() error = %v", err)
	}

	out := buf.String()
	want := []string{
		"HAR Version: 1.2",
		"Firefox 120",
		"2024-01-01T00:00:00.001Z",
		"http://example.com/submit?q=hello",
		"POST",
		"HTTP/1.1",
		"201",
		"93.184.216.34",
		"Accept: application/json",
		"q: hello",
		"session=abc123",
		"application/x-www-form-urlencoded",
		"user: bob",
		"Location: /created",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("DumpTo() output missing %q\n---\n%s", w, out)
		}
	}
}

func TestDumpToMalformedJSONReturnsError(t *testing.T) {
	var buf bytes.Buffer
	if err := DumpTo(&buf, strings.NewReader(`{"log":{ BROKEN`), DumpOptions{}); err == nil {
		t.Error("DumpTo() error = nil, want non-nil for malformed JSON")
	}
}

func TestDumpToEmptyEntries(t *testing.T) {
	var buf bytes.Buffer
	if err := DumpTo(&buf, strings.NewReader(harWith("")), DumpOptions{}); err != nil {
		t.Fatalf("DumpTo() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "HAR Version: 1.2") {
		t.Errorf("expected header even with no entries, got %q", out)
	}
	if strings.Contains(out, "Request URL:") {
		t.Errorf("expected no entry output, got %q", out)
	}
}

func TestDumpToSeparatesEntries(t *testing.T) {
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/a") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://example.com/b"))

	var buf bytes.Buffer
	if err := DumpTo(&buf, strings.NewReader(har), DumpOptions{}); err != nil {
		t.Fatalf("DumpTo() error = %v", err)
	}

	out := buf.String()
	if n := strings.Count(out, "Request URL:"); n != 2 {
		t.Errorf("got %d entries in output, want 2", n)
	}
	const separator = "----------------------------------------------------------------------"
	if n := strings.Count(out, separator); n != 2 {
		t.Errorf("got %d separators, want 2", n)
	}
}

func TestDumpToRealHARFixture(t *testing.T) {
	var buf bytes.Buffer
	if err := DumpTo(&buf, openFixture(t, "testdata/en.wikipedia.org.har"), DumpOptions{}); err != nil {
		t.Fatalf("DumpTo() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "HAR Version:") {
		t.Error("expected a HAR Version line")
	}
	if !strings.Contains(out, "Request URL:") {
		t.Error("expected at least one entry")
	}
}
