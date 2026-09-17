package hargo

import (
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

	f := writeTempHar(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")+","+
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/b")))

	done := make(chan error, 1)
	go func() {
		// url.URL{} selects the no-InfluxDB path.
		done <- LoadTest("test.har", f, 4, 300*time.Millisecond, url.URL{}, true, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LoadTest() error = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("LoadTest did not return after its timeout elapsed")
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
	// mid-send when the timeout fires.
	for i := 0; i < 3; i++ {
		f := writeTempHar(t, harWith(
			entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

		done := make(chan error, 1)
		go func() {
			done <- LoadTest("test.har", f, 16, 200*time.Millisecond, url.URL{}, true, true)
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
// still return cleanly when the timeout elapses.
func TestLoadTestWithNoUsableEntries(t *testing.T) {
	f := writeTempHar(t, harWith(""))

	done := make(chan error, 1)
	go func() {
		done <- LoadTest("empty.har", f, 2, 200*time.Millisecond, url.URL{}, true, true)
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

// Regression: newInfluxDBClient returns a nil client when InfluxDB is
// unreachable, and WritePoint then called c.Write on it and nil-dereferenced.
// A bad InfluxDB URL must degrade to "results not recorded", not a panic.
func TestLoadTestUnreachableInfluxDoesNotPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: newInfluxDBClient retries with 10s sleeps")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := writeTempHar(t, harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a")))

	// Port 0 on localhost is never listening.
	influx, err := url.Parse("http://127.0.0.1:0/hargo")
	if err != nil {
		t.Fatalf("parsing influx url: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- LoadTest("test.har", f, 2, 500*time.Millisecond, *influx, true, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("LoadTest() error = %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("LoadTest hung with an unreachable InfluxDB")
	}
}

func TestWaitClosesStopChannel(t *testing.T) {
	stop := make(chan bool)
	start := time.Now()

	go wait(stop, 50*time.Millisecond)

	select {
	case <-stop:
		if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
			t.Errorf("stop closed after %v, want at least ~50ms", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait() did not close the stop channel")
	}
}
