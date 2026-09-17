package hargo

import (
	"strings"
	"testing"
)

// filterFixture is a HAR with entries that differ in URL, method, and status, so
// each filter criterion can be exercised independently.
const filterFixture = `{
  "log": {
    "version": "1.2",
    "creator": {"name": "test", "version": "1"},
    "entries": [
      {"startedDateTime": "2024-01-01T00:00:00.000Z",
       "request": {"method": "GET", "url": "https://example.com/app.js", "httpVersion": "HTTP/1.1", "headers": [], "queryString": [], "cookies": [], "headersSize": -1, "bodySize": -1},
       "response": {"status": 200, "statusText": "OK", "httpVersion": "HTTP/1.1", "headers": [], "cookies": [], "content": {"size": 0, "mimeType": "text/javascript"}, "redirectURL": "", "headersSize": -1, "bodySize": 0},
       "cache": {}, "timings": {"send": 0, "wait": 0, "receive": 0}},
      {"startedDateTime": "2024-01-01T00:00:01.000Z",
       "request": {"method": "POST", "url": "https://example.com/api/login", "httpVersion": "HTTP/1.1", "headers": [], "queryString": [], "cookies": [], "headersSize": -1, "bodySize": -1},
       "response": {"status": 302, "statusText": "Found", "httpVersion": "HTTP/1.1", "headers": [], "cookies": [], "content": {"size": 0, "mimeType": "text/html"}, "redirectURL": "", "headersSize": -1, "bodySize": 0},
       "cache": {}, "timings": {"send": 0, "wait": 0, "receive": 0}},
      {"startedDateTime": "2024-01-01T00:00:02.000Z",
       "request": {"method": "GET", "url": "https://cdn.example.net/style.css", "httpVersion": "HTTP/1.1", "headers": [], "queryString": [], "cookies": [], "headersSize": -1, "bodySize": -1},
       "response": {"status": 404, "statusText": "Not Found", "httpVersion": "HTTP/1.1", "headers": [], "cookies": [], "content": {"size": 0, "mimeType": "text/css"}, "redirectURL": "", "headersSize": -1, "bodySize": 0},
       "cache": {}, "timings": {"send": 0, "wait": 0, "receive": 0}}
    ]
  }
}`

// urlsOf returns the request URL of each entry, for comparing selections.
func urlsOf(entries []Entry) []string {
	urls := make([]string, 0, len(entries))
	for _, e := range entries {
		urls = append(urls, e.Request.URL)
	}
	return urls
}

func TestEntryFilterSelectsByEachCriterion(t *testing.T) {
	har, err := Decode(strings.NewReader(filterFixture))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	const (
		js    = "https://example.com/app.js"
		login = "https://example.com/api/login"
		css   = "https://cdn.example.net/style.css"
	)

	tests := []struct {
		name   string
		filter EntryFilter
		want   []string
	}{
		{"zero filter selects everything", EntryFilter{}, []string{js, login, css}},
		{"url regex", EntryFilter{URL: `\.js$`}, []string{js}},
		{"url regex spanning slashes", EntryFilter{URL: `example\.com/api`}, []string{login}},
		{"url regex matching several", EntryFilter{URL: `example\.(com|net)`}, []string{js, login, css}},
		{"method", EntryFilter{Method: []string{"POST"}}, []string{login}},
		{"method is case insensitive", EntryFilter{Method: []string{"post"}}, []string{login}},
		{"several methods", EntryFilter{Method: []string{"GET", "POST"}}, []string{js, login, css}},
		{"status", EntryFilter{Status: []int{404}}, []string{css}},
		{"several statuses", EntryFilter{Status: []int{200, 404}}, []string{js, css}},
		// Criteria are combined with AND, so a conflicting pair selects nothing.
		{"criteria combine with and", EntryFilter{Method: []string{"GET"}, Status: []int{200}}, []string{js}},
		{"conflicting criteria select nothing", EntryFilter{Method: []string{"POST"}, Status: []int{200}}, nil},
		{"url and status combined", EntryFilter{URL: `example`, Status: []int{302}}, []string{login}},
		{"no match", EntryFilter{URL: `nothing-matches-this`}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filter
			if err := f.compile(); err != nil {
				t.Fatalf("compile() error = %v", err)
			}

			got := urlsOf(filterEntries(har.Log.Entries, &f))
			if len(got) != len(tt.want) {
				t.Fatalf("filterEntries() selected %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("filterEntries()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEntryFilterIsZero(t *testing.T) {
	tests := []struct {
		name   string
		filter EntryFilter
		want   bool
	}{
		{"zero value", EntryFilter{}, true},
		{"url set", EntryFilter{URL: "x"}, false},
		{"method set", EntryFilter{Method: []string{"GET"}}, false},
		{"status set", EntryFilter{Status: []int{200}}, false},
		// An empty slice is not a criterion, so it must not disable everything.
		{"empty slices", EntryFilter{Method: []string{}, Status: []int{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEntryFilterReportsInvalidRegex checks that a mistyped pattern is reported
// rather than silently matching nothing.
func TestEntryFilterReportsInvalidRegex(t *testing.T) {
	f := EntryFilter{URL: "([unclosed"}

	err := f.compile()
	if err == nil {
		t.Fatal("compile() error = nil, want non-nil for an invalid regex")
	}
	// The message must name the offending pattern, or the user cannot tell which
	// of several flags was wrong.
	if !strings.Contains(err.Error(), "([unclosed") {
		t.Errorf("compile() error = %q, want it to quote the pattern", err)
	}
}

// TestEntryFilterUncompiledMatchesNothing pins the behaviour of Match on a filter
// whose pattern was never compiled: it must not panic on the nil regexp.
func TestEntryFilterUncompiledMatchesNothing(t *testing.T) {
	f := EntryFilter{URL: "example"}

	if f.Match(Entry{Request: Request{URL: "https://example.com/"}}) {
		t.Error("Match() = true on an uncompiled filter, want false")
	}
}

// TestEntryFilterCompileIsIdempotent checks that compiling twice is harmless,
// since several entry points may compile the same filter.
func TestEntryFilterCompileIsIdempotent(t *testing.T) {
	f := EntryFilter{URL: `\.js$`}

	for i := range 2 {
		if err := f.compile(); err != nil {
			t.Fatalf("compile() attempt %d error = %v", i+1, err)
		}
	}

	if !f.Match(Entry{Request: Request{URL: "https://example.com/app.js"}}) {
		t.Error("Match() = false after compiling twice, want true")
	}
}

// TestFilterEntriesReturnsInputWhenZero checks that the common case does no work.
func TestFilterEntriesReturnsInputWhenZero(t *testing.T) {
	entries := []Entry{{Request: Request{URL: "https://example.com/"}}}
	f := EntryFilter{}

	got := filterEntries(entries, &f)
	if len(got) != len(entries) {
		t.Fatalf("filterEntries() selected %d entries, want %d", len(got), len(entries))
	}
	if &got[0] != &entries[0] {
		t.Error("filterEntries() copied the slice for a zero filter, want the input returned as is")
	}
}
