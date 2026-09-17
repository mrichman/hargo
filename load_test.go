package hargo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadTestRunsAndTerminates(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/b")))

	done := make(chan error, 1)
	go func() {
		// A nil InfluxDBURL selects the no-InfluxDB path.
		done <- LoadTest(t.Context(), f, LoadTestOptions{
			HARFile:            "test.har",
			Workers:            4,
			Duration:           300 * time.Millisecond,
			IgnoreHARCookies:   true,
			InsecureSkipVerify: true,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LoadTest() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadTest did not return after its duration elapsed")
	}

	if got := atomic.LoadInt64(&hits); got == 0 {
		t.Error("LoadTest made no requests")
	}
}

// Regression: results was closed with `defer close(results)` while workers were
// still sending, which panics with "send on closed channel". Run this with
// -race to also catch the unsynchronised close.
func TestLoadTestDoesNotSendOnClosedChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Many workers against a fast server maximises the chance that a worker is
	// mid-send when the duration elapses.
	for range 3 {
		f := writeTempHAR(t, harWith(
			entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

		done := make(chan error, 1)
		go func() {
			done <- LoadTest(t.Context(), f, LoadTestOptions{
				HARFile:            "test.har",
				Workers:            16,
				Duration:           200 * time.Millisecond,
				IgnoreHARCookies:   true,
				InsecureSkipVerify: true,
			})
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("LoadTest() error = %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("LoadTest hung")
		}
	}
}

// A HAR with no usable entries must not keep the load test spinning; it should
// still return cleanly when the duration elapses.
func TestLoadTestWithNoUsableEntries(t *testing.T) {
	f := writeTempHAR(t, harWith(""))

	done := make(chan error, 1)
	go func() {
		done <- LoadTest(t.Context(), f, LoadTestOptions{
			HARFile:            "empty.har",
			Workers:            2,
			Duration:           200 * time.Millisecond,
			IgnoreHARCookies:   true,
			InsecureSkipVerify: true,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LoadTest() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadTest did not return for a HAR with no entries")
	}
}

// An unreachable InfluxDB used to nil-dereference: newInfluxDBClient returned a
// nil client and WritePoint called Write on it. It now fails the run up front
// instead, rather than replaying the whole HAR and discarding every result.
func TestLoadTestUnreachableInfluxFailsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: connecting to InfluxDB retries with 10s sleeps")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	// Port 0 on localhost is never listening.
	influx, err := url.Parse("http://127.0.0.1:0/hargo")
	if err != nil {
		t.Fatalf("parsing influx url: %v", err)
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		// The duration is generous so that the connection failure, not the
		// duration bound, is what ends the run.
		done <- LoadTest(t.Context(), f, LoadTestOptions{
			HARFile:            "test.har",
			Workers:            2,
			Duration:           5 * time.Minute,
			InfluxDBURL:        influx,
			IgnoreHARCookies:   true,
			InsecureSkipVerify: true,
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("LoadTest() error = nil, want non-nil for an unreachable InfluxDB")
		}
		// It must give up during connection setup, not run for the full duration.
		if elapsed := time.Since(start); elapsed > 2*time.Minute {
			t.Errorf("LoadTest took %v to report an unreachable InfluxDB", elapsed)
		}
	case <-time.After(3 * time.Minute):
		t.Fatal("LoadTest hung with an unreachable InfluxDB")
	}
}

// The duration bound is an ordinary deadline, so a run that ends because it
// elapsed is a success, not an error.
func TestLoadTestDurationBoundIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	start := time.Now()
	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Duration:           150 * time.Millisecond,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v, want nil when the duration elapses", err)
	}

	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("LoadTest returned after %v, want it to run for ~150ms", elapsed)
	}
}

// Cancelling the context must stop the test promptly, well before the duration.
func TestLoadTestHonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)
	go func() {
		done <- LoadTest(ctx, f, LoadTestOptions{
			HARFile:            "test.har",
			Workers:            4,
			Duration:           5 * time.Minute,
			IgnoreHARCookies:   true,
			InsecureSkipVerify: true,
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LoadTest() error = %v, want nil after cancellation", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadTest did not return after its context was cancelled")
	}
}

func TestLoadTestRejectsNegativeOptions(t *testing.T) {
	f := writeTempHAR(t, harWith(""))

	tests := []struct {
		name string
		opts LoadTestOptions
	}{
		{name: "negative workers", opts: LoadTestOptions{Workers: -1}},
		{name: "negative duration", opts: LoadTestOptions{Duration: -time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LoadTest(t.Context(), f, tt.opts); err == nil {
				t.Error("LoadTest() error = nil, want non-nil")
			}
		})
	}
}

// The Results channel gives a caller the per-request detail without InfluxDB.
func TestLoadTestDeliversResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	f := writeTempHAR(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	results := make(chan TestResult, 1024)

	// Drain concurrently: LoadTest blocks on this channel, and never closes it.
	var got int64
	var status int64
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for r := range results {
			atomic.AddInt64(&got, 1)
			atomic.StoreInt64(&status, int64(r.Status))
		}
	}()

	if err := LoadTest(t.Context(), f, LoadTestOptions{
		HARFile:            "test.har",
		Workers:            2,
		Duration:           200 * time.Millisecond,
		Results:            results,
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	close(results)
	<-drained

	if atomic.LoadInt64(&got) == 0 {
		t.Fatal("LoadTest delivered no results on the Results channel")
	}
	if s := atomic.LoadInt64(&status); s != http.StatusTeapot {
		t.Errorf("result Status = %d, want %d", s, http.StatusTeapot)
	}
}
