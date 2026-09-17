package hargo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeInfluxDB is enough of the InfluxDB 1.x HTTP API to exercise influxWriter:
// /ping for the version probe, /query for CREATE DATABASE, and /write for
// points.
type fakeInfluxDB struct {
	mu      sync.Mutex
	queries []string
	writes  []string
	auth    string

	// version is reported by /ping; an empty value omits the header.
	version string
	// pingStatus, when non-zero, is returned by /ping instead of 204.
	pingStatus int
	// writeStatus, when non-zero, is returned by /write instead of 204.
	writeStatus int
}

func (f *fakeInfluxDB) server(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Every handler records the credentials it was sent.
	recordAuth := func(r *http.Request) {
		f.mu.Lock()
		f.auth = r.Header.Get("Authorization")
		f.mu.Unlock()
	}

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		recordAuth(r)
		if f.version != "" {
			w.Header().Set("X-Influxdb-Version", f.version)
		}
		if f.pingStatus != 0 {
			w.WriteHeader(f.pingStatus)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		recordAuth(r)
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.Query().Get("q"))
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"statement_id": 0}},
		})
	})

	mux.HandleFunc("/write", func(w http.ResponseWriter, r *http.Request) {
		recordAuth(r)
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		f.mu.Lock()
		f.writes = append(f.writes, string(body))
		f.mu.Unlock()

		if f.writeStatus != 0 {
			w.WriteHeader(f.writeStatus)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeInfluxDB) recorded() (queries, writes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...), append([]string(nil), f.writes...)
}

// lastAuth returns the most recent Authorization header the fake received.
func (f *fakeInfluxDB) lastAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth
}

// parseBasicAuth decodes a Basic authorization header value.
func parseBasicAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	user, pass, ok = strings.Cut(string(raw), ":")
	return user, pass, ok
}

// writeAll drains results into w, mirroring what LoadTest's consumer goroutine
// does. The writer records one result at a time so that a single consumer can
// both accumulate a summary and forward to InfluxDB.
func writeAll(w *influxWriter, results <-chan TestResult) {
	w.start()
	for r := range results {
		w.writeOne(r)
	}
}

// influxURL turns a test server URL into the form the CLI passes: the path names
// the database.
func influxURL(t *testing.T, srvURL, db string) url.URL {
	t.Helper()

	u, err := url.Parse(srvURL + "/" + db)
	if err != nil {
		t.Fatalf("parsing influx url: %v", err)
	}
	return *u
}

func TestNewInfluxWriterCreatesDatabase(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	if w.db != "hargo" {
		t.Errorf("db = %q, want %q", w.db, "hargo")
	}

	queries, _ := fake.recorded()
	if len(queries) == 0 {
		t.Fatal("no query was issued, want a CREATE DATABASE")
	}
	if !strings.Contains(queries[0], `CREATE DATABASE "hargo"`) {
		t.Errorf("query = %q, want it to create the database", queries[0])
	}
}

// A server that answers /ping without a version header must still be usable:
// the probe succeeded, which is what matters. The original retry loop spun
// forever on this input because it incremented its counter only on error.
func TestNewInfluxWriterAcceptsPingWithoutVersion(t *testing.T) {
	fake := &fakeInfluxDB{}
	srv := fake.server(t)

	done := make(chan error, 1)
	go func() {
		w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
		if w != nil {
			_ = w.close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("newInfluxWriter() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("newInfluxWriter hung on a ping with no version header")
	}
}

func TestInfluxWriterWritesPoints(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	results := make(chan TestResult, 2)
	results <- TestResult{
		URL:       "http://example.com/a",
		Status:    200,
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Latency:   12,
		Method:    http.MethodGet,
		HARFile:   "test.har",
	}
	results <- TestResult{
		URL:       "http://example.com/b",
		Status:    404,
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Latency:   34,
		Method:    http.MethodGet,
		HARFile:   "test.har",
	}
	close(results)

	// write consumes until the channel is closed.
	writeAll(w, results)

	_, writes := fake.recorded()
	if len(writes) != 2 {
		t.Fatalf("got %d writes, want 2", len(writes))
	}
	for _, want := range []string{"test_result", "example.com"} {
		if !strings.Contains(writes[0], want) {
			t.Errorf("write payload = %q, want it to contain %q", writes[0], want)
		}
	}
}

// A write rejected by the server must be reported and the loop must continue,
// not abandon the remaining results.
func TestInfluxWriterContinuesAfterWriteError(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10", writeStatus: http.StatusInternalServerError}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	results := make(chan TestResult, 3)
	for range 3 {
		results <- TestResult{URL: "http://example.com/a", Status: 200, Method: http.MethodGet}
	}
	close(results)

	writeAll(w, results)

	if _, writes := fake.recorded(); len(writes) != 3 {
		t.Errorf("got %d writes, want all 3 attempted despite errors", len(writes))
	}
}

// An unreachable InfluxDB must produce an error rather than a nil client that
// later nil-dereferences.
func TestNewInfluxWriterUnreachableReturnsError(t *testing.T) {
	// Port 0 on localhost is never listening.
	u, err := url.Parse("http://127.0.0.1:0/hargo")
	if err != nil {
		t.Fatalf("parsing url: %v", err)
	}

	// A short deadline keeps the retry sleeps from dominating the test.
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	w, err := newInfluxWriter(ctx, *u, nil)
	if err == nil {
		if w != nil {
			_ = w.close()
		}
		t.Fatal("newInfluxWriter() error = nil, want non-nil for an unreachable server")
	}
	if w != nil {
		t.Error("newInfluxWriter() returned a non-nil writer alongside an error")
	}
}

func TestInfluxWriterQuery(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	srv := fake.server(t)

	w, err := newInfluxWriter(t.Context(), influxURL(t, srv.URL, "hargo"), nil)
	if err != nil {
		t.Fatalf("newInfluxWriter() error = %v", err)
	}
	defer func() { _ = w.close() }()

	if _, err := w.query("SHOW MEASUREMENTS"); err != nil {
		t.Fatalf("query() error = %v", err)
	}

	queries, _ := fake.recorded()
	if len(queries) < 2 {
		t.Fatalf("got %d queries, want the CREATE plus the explicit one", len(queries))
	}
	if got := queries[len(queries)-1]; got != "SHOW MEASUREMENTS" {
		t.Errorf("last query = %q, want %q", got, "SHOW MEASUREMENTS")
	}
}

// LoadTest must record its results when given an InfluxDB URL.
func TestLoadTestRecordsToInfluxDB(t *testing.T) {
	fake := &fakeInfluxDB{version: "1.8.10"}
	influx := fake.server(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", target.URL+"/a")))

	u := influxURL(t, influx.URL, "hargo")
	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Workers:            2,
		Duration:           200 * time.Millisecond,
		InfluxDBURL:        &u,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if _, writes := fake.recorded(); len(writes) == 0 {
		t.Error("LoadTest recorded no points to InfluxDB")
	}
}
