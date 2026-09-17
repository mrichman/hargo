package hargo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Regression: every worker was handed the caller's Progress writer directly.
// os.Stdout tolerates that because os.File.Write holds an internal lock, but a
// bytes.Buffer does not, so the obvious caller got a data race and torn output.
//
// Run this with -race, which is where the original defect shows.
func TestLoadTestProgressWriterIsRaceFree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/b")))

	// A plain buffer, deliberately not safe for concurrent use.
	var buf bytes.Buffer

	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Workers:            16,
		Duration:           300 * time.Millisecond,
		Progress:           &buf,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("Progress received nothing")
	}

	// Torn writes would corrupt the line structure. Per-request lines start with
	// the worker index in brackets; the trailing summary block is indented, so it
	// is checked separately below rather than treated as malformed.
	sawRequestLine := false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "["):
			sawRequestLine = true
		case line == "Load test complete." || strings.HasPrefix(line, "  "):
			// Part of the summary.
		default:
			t.Errorf("progress line is malformed, suggesting interleaved writes: %q", line)
		}
	}

	if !sawRequestLine {
		t.Error("Progress contained no per-request lines")
	}

	// The summary must survive concurrent progress writes intact.
	for _, want := range []string{"Load test complete.", "requests", "failures", "latency"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("Progress missing %q from the summary", want)
		}
	}
}

// Regression: the response body was closed without being read, which tears the
// connection down in net/http, so every request paid a fresh TCP and TLS
// handshake despite KeepAlive being configured. Draining it allows reuse.
//
// Connections are counted by distinct RemoteAddr: each new client connection gets
// its own local port. That avoids configuring ConnState, which cannot be assigned
// on an already-serving httptest server without racing net/http.
func TestLoadTestReusesConnections(t *testing.T) {
	var mu sync.Mutex
	peers := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers[r.RemoteAddr]++
		mu.Unlock()
		// A body large enough that not draining it is clearly visible.
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Workers:            1,
		Duration:           500 * time.Millisecond,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	mu.Lock()
	conns := len(peers)
	requests := 0
	for _, n := range peers {
		requests += n
	}
	mu.Unlock()

	t.Logf("%d requests over %d connections", requests, conns)

	if requests < 5 {
		t.Skipf("only %d requests completed; too few to judge connection reuse", requests)
	}
	// With reuse, one worker should need far fewer connections than requests.
	if conns >= requests {
		t.Errorf("opened %d connections for %d requests, want the connection reused",
			conns, requests)
	}
}

// Latency must cover receiving the whole response, not just the headers.
func TestLoadTestLatencyIncludesBodyTransfer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Headers are already sent; the body arrives well afterwards.
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("late body"))
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/slow")))

	results := make(chan TestResult, 64)
	var maxLatency int64
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for r := range results {
			if int64(r.Latency) > atomic.LoadInt64(&maxLatency) {
				atomic.StoreInt64(&maxLatency, int64(r.Latency))
			}
		}
	}()

	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Workers:            1,
		Duration:           600 * time.Millisecond,
		Results:            results,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}
	close(results)
	<-drained

	got := atomic.LoadInt64(&maxLatency)
	t.Logf("max recorded latency: %dms", got)
	if got < 100 {
		t.Errorf("latency = %dms, want it to include the ~150ms body transfer", got)
	}
}
