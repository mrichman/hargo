package hargo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Regression: results were written with millisecond precision and no tags. A
// point's identity is measurement plus tag set plus timestamp, so concurrent
// results landing in the same millisecond silently overwrote one another.
func TestInfluxWriterDistinguishesConcurrentResults(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	// Three results within the same millisecond, differing only in URL and status.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	results := make(chan TestResult, 3)
	for i, status := range []int{200, 404, 500} {
		results <- TestResult{
			URL:       "http://example.com/a",
			Status:    status,
			StartTime: base.Add(time.Duration(i) * time.Microsecond),
			EndTime:   base.Add(time.Duration(i)*time.Microsecond + time.Millisecond),
			Latency:   i,
			Method:    http.MethodGet,
			HARFile:   "test.har",
		}
	}
	close(results)

	w.write(results)

	_, writes := fake.recorded()
	if len(writes) != 3 {
		t.Fatalf("got %d writes, want 3", len(writes))
	}

	// Every line must carry a distinct timestamp, or InfluxDB would treat the
	// points as the same one.
	seen := map[string]bool{}
	for _, line := range writes {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			t.Fatalf("unexpected line protocol: %q", line)
		}
		ts := fields[len(fields)-1]
		if seen[ts] {
			t.Errorf("duplicate timestamp %s; concurrent results would overwrite", ts)
		}
		seen[ts] = true
	}
}

// Tags make the series queryable and form part of the point identity.
func TestInfluxWriterTagsResults(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	results := make(chan TestResult, 1)
	results <- TestResult{
		URL:       "http://example.com/a",
		Status:    503,
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Latency:   42,
		Method:    http.MethodPost,
		HARFile:   "test.har",
	}
	close(results)

	w.write(results)

	_, writes := fake.recorded()
	if len(writes) != 1 {
		t.Fatalf("got %d writes, want 1", len(writes))
	}

	for _, want := range []string{"method=POST", "status=503", "har=test.har", "outcome=failed"} {
		if !strings.Contains(writes[0], want) {
			t.Errorf("line protocol %q missing tag %q", writes[0], want)
		}
	}
}

// Regression: StartTime and EndTime were time.Time values in the fields map. The
// client formats an unknown field type with %v, so they were stored as strings
// including the monotonic clock reading (m=+0.001234567) and were unqueryable.
func TestInfluxWriterStoresTimesAsNumbers(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	results := make(chan TestResult, 1)
	results <- TestResult{
		URL:       "http://example.com/a",
		Status:    200,
		StartTime: time.Now(), // carries a monotonic reading
		EndTime:   time.Now(),
		Method:    http.MethodGet,
		HARFile:   "test.har",
	}
	close(results)

	w.write(results)

	_, writes := fake.recorded()
	if len(writes) != 1 {
		t.Fatalf("got %d writes, want 1", len(writes))
	}

	if strings.Contains(writes[0], "m=+") {
		t.Errorf("line protocol %q contains a monotonic clock reading", writes[0])
	}
	if strings.Contains(writes[0], "UTC") || strings.Contains(writes[0], "+0000") {
		t.Errorf("line protocol %q stores a formatted time rather than a number", writes[0])
	}
	// An integer field is suffixed with i in line protocol.
	if !strings.Contains(writes[0], "StartTime=") {
		t.Errorf("line protocol %q has no StartTime field", writes[0])
	}
}

// Regression: credentials in the InfluxDB URL were discarded, so an authenticated
// instance failed with an unexplained 401.
func TestNewInfluxWriterUsesURLCredentials(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	u, err := url.Parse(srv.URL + "/hargo")
	if err != nil {
		t.Fatalf("parsing url: %v", err)
	}
	u.User = url.UserPassword("admin", "secret")

	w, err := newInfluxWriter(t.Context(), *u, nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	// The client sends credentials as basic auth on each request.
	if got := fake.lastAuth(); got == "" {
		t.Error("no Authorization header was sent, want the URL credentials used")
	} else {
		user, pass, ok := parseBasicAuth(got)
		if !ok {
			t.Fatalf("Authorization header %q is not basic auth", got)
		}
		if user != "admin" || pass != "secret" {
			t.Errorf("got credentials %q/%q, want admin/secret", user, pass)
		}
	}
}

// The database name is interpolated into InfluxQL, so it must be an identifier.
func TestNewInfluxWriterRejectsUnsafeDatabaseName(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	tests := []struct {
		name string
		db   string
	}{
		{name: "statement separator", db: "x;DROP DATABASE y"},
		{name: "quote", db: `x"y`},
		{name: "space", db: "x y"},
		{name: "empty", db: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(srv.URL + "/" + tt.db)
			if err != nil {
				t.Skipf("URL not parseable for this case: %v", err)
			}

			w, err := newInfluxWriter(t.Context(), *u, nil)
			if err == nil {
				if w != nil {
					_ = w.close()
				}
				t.Errorf("newInfluxWriter() error = nil, want non-nil for database %q", tt.db)
			}
		})
	}
}

// A stalled InfluxDB must not hang the writer: the client has no timeout by
// default, and Ping's argument is a query parameter rather than a client timeout.
func TestInfluxWriterHasAnHTTPTimeout(t *testing.T) {
	if influxHTTPTimeout <= 0 {
		t.Fatal("influxHTTPTimeout must be positive; without it a stalled server hangs the run")
	}
	if influxHTTPTimeout > 2*time.Minute {
		t.Errorf("influxHTTPTimeout = %v, want a bound short enough to unwedge a run", influxHTTPTimeout)
	}
}
