package hargo

import (
	"strings"
	"testing"
)

// Regression: json.Decoder.Decode consumes a single JSON value and stops, so
// anything following the document passed validation unnoticed.
func TestValidateRejectsTrailingGarbage(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "prose after the document",
			input: `{"log":{"version":"1.2","entries":[]}}THIS IS NOT JSON AT ALL`,
		},
		{
			name:  "a second document",
			input: `{"log":{"version":"1.2","entries":[]}}{"log":{"version":"1.2","entries":[]}}`,
		},
		{
			name:  "a stray bracket",
			input: `{"log":{"version":"1.2","entries":[]}}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(strings.NewReader(tt.input)); err == nil {
				t.Error("Validate() error = nil, want non-nil for trailing data")
			}
		})
	}
}

// Trailing whitespace is not garbage.
func TestValidateAllowsTrailingWhitespace(t *testing.T) {
	in := `{"log":{"version":"1.2","entries":[]}}` + "\n\n  \t\n"
	if err := Validate(strings.NewReader(in)); err != nil {
		t.Errorf("Validate() error = %v, want nil for trailing whitespace", err)
	}
}

// Regression: a document with no log object reported the misleading
// `unsupported HAR version ""`.
func TestValidateReportsMissingLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty object", input: `{}`, want: `no "log" object`},
		{name: "log is null", input: `{"log":null}`, want: `no "log" object`},
		{name: "wrong key", input: `{"logs":{"version":"1.2"}}`, want: `no "log" object`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// Regression: a log with no entries array validated, even though every other
// operation in this package needs one.
func TestValidateRequiresEntriesArray(t *testing.T) {
	err := Validate(strings.NewReader(`{"log":{"version":"1.2"}}`))
	if err == nil {
		t.Fatal("Validate() error = nil, want non-nil for a log with no entries")
	}
	if !strings.Contains(err.Error(), "entries") {
		t.Errorf("error = %q, want it to mention the missing entries array", err)
	}
}

// An empty entries array is legal; a HAR can record nothing.
func TestValidateAllowsEmptyEntries(t *testing.T) {
	if err := Validate(strings.NewReader(`{"log":{"version":"1.2","entries":[]}}`)); err != nil {
		t.Errorf("Validate() error = %v, want nil for an empty entries array", err)
	}
}

// Validate is what should catch a timestamp Run cannot pace with, rather than
// leaving it to be discovered mid-replay.
func TestValidateRejectsUnparseableTimestamp(t *testing.T) {
	tests := []struct {
		name string
		ts   string
	}{
		{name: "missing", ts: ""},
		{name: "garbage", ts: "nonsense"},
		{name: "wrong shape", ts: "01/02/2024 10:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := `{"log":{"version":"1.2","entries":[{"startedDateTime":"` + tt.ts +
				`","request":{"method":"GET","url":"http://example.com/a"}}]}}`

			err := Validate(strings.NewReader(in))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil for an unusable timestamp")
			}
			if !strings.Contains(err.Error(), "entry 0") {
				t.Errorf("error = %q, want it to identify the entry", err)
			}
		})
	}
}

// Timestamp formats that Run now handles must validate.
func TestValidateAcceptsRealWorldTimestamps(t *testing.T) {
	for _, ts := range []string{
		"2024-01-01T00:00:00.001Z",
		"2024-01-01T00:00:00Z",
		"2009-07-24T19:20:30.45+01:00",
		"2024-01-01T00:00:00.123456Z",
	} {
		t.Run(ts, func(t *testing.T) {
			in := `{"log":{"version":"1.2","entries":[{"startedDateTime":"` + ts +
				`","request":{"method":"GET","url":"http://example.com/a"}}]}}`

			if err := Validate(strings.NewReader(in)); err != nil {
				t.Errorf("Validate() error = %v", err)
			}
		})
	}
}

// A request the replay could not build should be reported by the validator.
func TestValidateRejectsUnusableRequests(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    string
	}{
		{name: "no method", request: `{"url":"http://example.com/a"}`, want: "no method"},
		{name: "no url", request: `{"method":"GET"}`, want: "no url"},
		{name: "no scheme", request: `{"method":"GET","url":"example.com/a"}`, want: "no scheme"},
		{
			name:    "unparseable url",
			request: `{"method":"GET","url":"http://[::1]:namedport/x"}`,
			want:    "request url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := `{"log":{"version":"1.2","entries":[` +
				`{"startedDateTime":"2024-01-01T00:00:00.001Z","request":` + tt.request + `}]}}`

			err := Validate(strings.NewReader(in))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// The offending entry must be identified, not just the fact that one is bad.
func TestValidateIdentifiesTheFailingEntry(t *testing.T) {
	in := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T00:00:00.001Z","request":{"method":"GET","url":"http://example.com/a"}},
		{"startedDateTime":"2024-01-01T00:00:00.002Z","request":{"method":"GET","url":"http://example.com/b"}},
		{"startedDateTime":"broken","request":{"method":"GET","url":"http://example.com/c"}}
	]}}`

	err := Validate(strings.NewReader(in))
	if err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "entry 2") {
		t.Errorf("error = %q, want it to name entry 2", err)
	}
}

// Anything Validate accepts must be replayable, which is the contract that makes
// the command useful in a pipeline.
func TestValidateAcceptsWhatRunCanReplay(t *testing.T) {
	for _, name := range []string{"testdata/golang.org.har", "testdata/en.wikipedia.org.har"} {
		t.Run(name, func(t *testing.T) {
			if err := Validate(openFixture(t, name)); err != nil {
				t.Fatalf("Validate(%s) error = %v", name, err)
			}

			// Every entry must also yield a request, which is what a replay needs.
			har, err := Decode(openFixture(t, name))
			if err != nil {
				t.Fatalf("Decode(%s) error = %v", name, err)
			}
			for i, entry := range har.Log.Entries {
				if _, err := EntryToRequest(t.Context(), &entry, EntryOptions{}); err != nil {
					t.Errorf("entry %d validated but could not be built: %v", i, err)
				}
			}
		})
	}
}
