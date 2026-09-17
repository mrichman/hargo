package hargo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ParseEntryTime must accept the formats real recorders emit. The replay used to
// parse with a single layout, "2006-01-02T15:04:05.000Z", which rejected all but
// the Chrome spelling.
func TestParseEntryTimeAcceptsRealWorldFormats(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "chrome, three fractional digits and literal Z", in: "2024-01-01T00:00:00.001Z"},
		{name: "six fractional digits", in: "2024-01-01T00:00:00.123456Z"},
		{name: "seven fractional digits", in: "2024-01-01T00:00:00.1234567Z"},
		{name: "no fractional part", in: "2024-01-01T00:00:00Z"},
		{name: "HAR spec example, numeric offset", in: "2009-07-24T19:20:30.45+01:00"},
		{name: "firefox, positive offset", in: "2016-04-06T14:21:26.842+02:00"},
		{name: "negative offset", in: "2024-01-01T00:00:00.001-07:00"},
		{name: "offset without a colon", in: "2024-01-01T00:00:00.001+0100"},
		{name: "no zone at all", in: "2024-01-01T00:00:00.001"},
		{name: "date only", in: "2024-01-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEntryTime(tt.in)
			if err != nil {
				t.Fatalf("ParseEntryTime(%q) error = %v", tt.in, err)
			}
			if got.IsZero() {
				t.Errorf("ParseEntryTime(%q) returned the zero time", tt.in)
			}
		})
	}
}

func TestParseEntryTimeRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "garbage", "not a timestamp", "2024-13-45T99:99:99Z"} {
		if _, err := ParseEntryTime(in); err == nil {
			t.Errorf("ParseEntryTime(%q) error = nil, want non-nil", in)
		}
	}
}

// Regression: an entry whose timestamp failed to parse was assigned the zero
// time, so the gap to the NEXT entry was ~2023 years. time.Sub saturates rather
// than wrapping, so delayBefore returned ~292 years and the replay slept on it.
// Only MaxDelay masked this, and it defaults to zero.
func TestRunDoesNotSleepOnUnparseableTimestamp(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Entry 2's timestamp is unparseable by the old layout and poisons the gap
	// used for entry 3.
	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T00:00:00.000Z","request":{"method":"GET","url":"` + srv.URL + `/a"}},
		{"startedDateTime":"nonsense","request":{"method":"GET","url":"` + srv.URL + `/b"}},
		{"startedDateTime":"2024-01-01T00:00:00.010Z","request":{"method":"GET","url":"` + srv.URL + `/c"}}
	]}}`

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- Run(t.Context(), strings.NewReader(har), RunOptions{})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run hung: an unparseable timestamp produced an enormous delay")
	}

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Run took %v, want it to skip the delay for the bad timestamp", elapsed)
	}
	if hits != 3 {
		t.Errorf("server received %d requests, want all 3 entries replayed", hits)
	}
}

// The same poisoning applied when the FIRST timestamp was unparseable.
func TestRunHandlesUnparseableFirstTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"","request":{"method":"GET","url":"` + srv.URL + `/a"}},
		{"startedDateTime":"2024-01-01T00:00:00.010Z","request":{"method":"GET","url":"` + srv.URL + `/b"}}
	]}}`

	done := make(chan error, 1)
	go func() { done <- Run(t.Context(), strings.NewReader(har), RunOptions{}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run hung on an unparseable first timestamp")
	}
}

// A timestamp format the old layout rejected must still be paced, not silently
// replayed with no delay at all.
func TestRunHonoursDelayForOffsetTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 300ms apart, expressed with a numeric offset rather than Z.
	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T01:00:00.000+01:00","request":{"method":"GET","url":"` + srv.URL + `/a"}},
		{"startedDateTime":"2024-01-01T01:00:00.300+01:00","request":{"method":"GET","url":"` + srv.URL + `/b"}}
	]}}`

	start := time.Now()
	if err := Run(t.Context(), strings.NewReader(har), RunOptions{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("Run finished in %v, want it to wait out the recorded ~300ms gap", elapsed)
	}
}

// Decode must order entries chronologically, not by raw string comparison, which
// disagrees whenever offsets differ.
func TestDecodeSortsAcrossTimezoneOffsets(t *testing.T) {
	// 13:21Z sorts after "2016-04-06T14:21..." as a string, but is EARLIER in
	// real time than 14:21+02:00 (12:21Z).
	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2016-04-06T13:21:26.842Z","request":{"method":"GET","url":"http://example.com/second"}},
		{"startedDateTime":"2016-04-06T14:21:26.842+02:00","request":{"method":"GET","url":"http://example.com/first"}}
	]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(got.Log.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(got.Log.Entries))
	}

	if got.Log.Entries[0].Request.URL != "http://example.com/first" {
		t.Errorf("first entry = %q, want the chronologically earlier 14:21+02:00 entry",
			got.Log.Entries[0].Request.URL)
	}
}

// Equal timestamps must keep their recorded order rather than being permuted
// arbitrarily by an unstable sort.
func TestDecodeSortIsStableForEqualTimestamps(t *testing.T) {
	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T00:00:00.000Z","request":{"method":"GET","url":"http://example.com/a"}},
		{"startedDateTime":"2024-01-01T00:00:00.000Z","request":{"method":"GET","url":"http://example.com/b"}},
		{"startedDateTime":"2024-01-01T00:00:00.000Z","request":{"method":"GET","url":"http://example.com/c"}}
	]}}`

	want := []string{"http://example.com/a", "http://example.com/b", "http://example.com/c"}

	// Repeat, since an unstable sort may only occasionally reorder.
	for range 20 {
		got, err := Decode(strings.NewReader(har))
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		for i, w := range want {
			if got.Log.Entries[i].Request.URL != w {
				t.Fatalf("entry %d = %q, want %q", i, got.Log.Entries[i].Request.URL, w)
			}
		}
	}
}

// Regression: isWebSocket was case-sensitive, so an upper-case scheme survived
// the filter and later failed as an unsupported protocol scheme.
func TestDecodeDropsWebSocketEntriesCaseInsensitively(t *testing.T) {
	har := `{"log":{"version":"1.2","entries":[
		{"startedDateTime":"2024-01-01T00:00:00.001Z","request":{"method":"GET","url":"WS://example.com/s"}},
		{"startedDateTime":"2024-01-01T00:00:00.002Z","request":{"method":"GET","url":"WSS://example.com/s"}},
		{"startedDateTime":"2024-01-01T00:00:00.003Z","request":{"method":"GET","url":"Ws://example.com/s"}},
		{"startedDateTime":"2024-01-01T00:00:00.004Z","request":{"method":"GET","url":"http://example.com/keep"}}
	]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if len(got.Log.Entries) != 1 {
		for _, e := range got.Log.Entries {
			t.Logf("kept: %s", e.Request.URL)
		}
		t.Fatalf("got %d entries, want only the http one", len(got.Log.Entries))
	}
	if got.Log.Entries[0].Request.URL != "http://example.com/keep" {
		t.Errorf("kept %q, want the http entry", got.Log.Entries[0].Request.URL)
	}
}

// Regression: a recorded Host header was added to the header map, which net/http
// ignores, so the recorded virtual host was silently lost.
func TestEntryToRequestAppliesHostHeader(t *testing.T) {
	e := &Entry{}
	e.Request.Method = http.MethodGet
	e.Request.URL = "http://192.0.2.1/a"
	e.Request.Headers = []NVP{{Name: "Host", Value: "www.example.com"}}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}

	if req.Host != "www.example.com" {
		t.Errorf("req.Host = %q, want the recorded Host header", req.Host)
	}
}

// An HTTP/2 capture has no Host header; the :authority pseudo-header carries it.
func TestEntryToRequestAppliesAuthorityPseudoHeader(t *testing.T) {
	e := &Entry{}
	e.Request.Method = http.MethodGet
	e.Request.URL = "http://192.0.2.1/a"
	e.Request.Headers = []NVP{{Name: ":authority", Value: "www.example.com"}}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}

	if req.Host != "www.example.com" {
		t.Errorf("req.Host = %q, want the recorded :authority", req.Host)
	}
	// The pseudo-header itself must still never be sent as a field name.
	if _, ok := req.Header[":authority"]; ok {
		t.Error("the :authority pseudo-header leaked into the header map")
	}
}
