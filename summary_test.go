package hargo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPercentileNearestRank pins the percentile definition against a slice whose
// answers can be checked by hand.
func TestPercentileNearestRank(t *testing.T) {
	// 1..10 milliseconds, already sorted.
	var sorted []time.Duration
	for i := 1; i <= 10; i++ {
		sorted = append(sorted, time.Duration(i)*time.Millisecond)
	}

	tests := []struct {
		p    int
		want time.Duration
	}{
		// Nearest rank is ceil(p/100 * N), so p50 of 1..10 is the 5th value.
		{50, 5 * time.Millisecond},
		{90, 9 * time.Millisecond},
		{95, 10 * time.Millisecond},
		{99, 10 * time.Millisecond},
		{100, 10 * time.Millisecond},
		// A rank below 1 is clamped to the smallest observation.
		{0, 1 * time.Millisecond},
	}

	for _, tt := range tests {
		if got := percentile(sorted, tt.p); got != tt.want {
			t.Errorf("percentile(1..10ms, %d) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestPercentileEdgeCases(t *testing.T) {
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil, 50) = %v, want 0", got)
	}

	one := []time.Duration{7 * time.Millisecond}
	for _, p := range []int{0, 50, 99, 100} {
		if got := percentile(one, p); got != 7*time.Millisecond {
			t.Errorf("percentile(single, %d) = %v, want 7ms", p, got)
		}
	}
}

// TestSummaryAccumulatorComputesFigures checks every derived figure against
// known inputs.
func TestSummaryAccumulatorComputesFigures(t *testing.T) {
	acc := newSummaryAccumulator()

	// Four results: two fine, one 404, one that never completed.
	acc.add(TestResult{Status: 200, Duration: 10 * time.Millisecond})
	acc.add(TestResult{Status: 204, Duration: 20 * time.Millisecond})
	acc.add(TestResult{Status: 404, Duration: 30 * time.Millisecond})
	acc.add(TestResult{Status: 0, Duration: 40 * time.Millisecond})

	// Elapsed is measured from the accumulator's creation, so the clock needs
	// something to measure. Four instant additions genuinely take no time on a
	// platform whose timer granularity is coarse — Windows' is around 15ms — and
	// a zero Elapsed there is a truthful measurement rather than a bug.
	time.Sleep(20 * time.Millisecond)

	s := acc.summary()

	if s.Requests != 4 {
		t.Errorf("Requests = %d, want 4", s.Requests)
	}
	// A 404 and a request that never completed are both failures; 200 and 204
	// are not.
	if s.Failures != 2 {
		t.Errorf("Failures = %d, want 2", s.Failures)
	}
	if s.ErrorRate != 0.5 {
		t.Errorf("ErrorRate = %v, want 0.5", s.ErrorRate)
	}
	if s.Min != 10*time.Millisecond {
		t.Errorf("Min = %v, want 10ms", s.Min)
	}
	if s.Max != 40*time.Millisecond {
		t.Errorf("Max = %v, want 40ms", s.Max)
	}
	if s.P50 != 20*time.Millisecond {
		t.Errorf("P50 = %v, want 20ms", s.P50)
	}

	want := map[int]int{200: 1, 204: 1, 404: 1, 0: 1}
	for code, n := range want {
		if s.StatusCounts[code] != n {
			t.Errorf("StatusCounts[%d] = %d, want %d", code, s.StatusCounts[code], n)
		}
	}

	// Elapsed is measured rather than taken from a configured duration, so it
	// cannot be asserted exactly. The sleep above guarantees at least one clock
	// tick has passed; asserting the full 20ms would itself be flaky, since a
	// 20ms sleep spans one or two 15.6ms ticks and can measure as 15.6ms.
	if s.Elapsed <= 0 {
		t.Errorf("Elapsed = %v, want a positive measured duration", s.Elapsed)
	}
	if s.RequestsPerSec <= 0 {
		t.Errorf("RequestsPerSec = %v, want positive", s.RequestsPerSec)
	}
}

// TestSummaryAccumulatorEmpty checks that a test which recorded nothing produces
// zeroes rather than dividing by zero.
func TestSummaryAccumulatorEmpty(t *testing.T) {
	s := newSummaryAccumulator().summary()

	if s.Requests != 0 || s.Failures != 0 {
		t.Errorf("Requests = %d, Failures = %d, want 0 and 0", s.Requests, s.Failures)
	}
	if s.ErrorRate != 0 {
		t.Errorf("ErrorRate = %v, want 0", s.ErrorRate)
	}
	if s.RequestsPerSec != 0 {
		t.Errorf("RequestsPerSec = %v, want 0", s.RequestsPerSec)
	}
	if s.Min != 0 || s.P50 != 0 || s.Max != 0 {
		t.Errorf("latencies = %v/%v/%v, want zeroes", s.Min, s.P50, s.Max)
	}
}

func TestLoadSummaryWriteTo(t *testing.T) {
	s := LoadSummary{
		Requests:       10,
		Failures:       2,
		ErrorRate:      0.2,
		Elapsed:        2 * time.Second,
		RequestsPerSec: 5,
		Min:            time.Millisecond,
		P50:            2 * time.Millisecond,
		P95:            3 * time.Millisecond,
		P99:            4 * time.Millisecond,
		Max:            5 * time.Millisecond,
		StatusCounts:   map[int]int{200: 8, 500: 1, 0: 1},
	}

	var buf bytes.Buffer
	s.writeTo(&buf)
	out := buf.String()

	for _, want := range []string{
		"Load test complete.",
		"10 in 2s (5.0/sec)",
		"2 (20.0%)",
		"min 1ms",
		"p50 2ms",
		"max 5ms",
		"200=8",
		"500=1",
		// A zero status means the request never got a response; printing "0=1"
		// would read like a status code.
		"no response=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("writeTo() output missing %q\n---\n%s", want, out)
		}
	}
}

// TestLoadTestPopulatesSummary checks the out-param against a live server.
func TestLoadTestPopulatesSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	har := twoPathHAR(srv.URL)

	var summary LoadSummary
	var progress bytes.Buffer

	err := LoadTest(t.Context(), strings.NewReader(har), LoadTestOptions{
		Workers:  2,
		Duration: 200 * time.Millisecond,
		Summary:  &summary,
		Progress: &progress,
	})
	if err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if summary.Requests == 0 {
		t.Fatal("Summary.Requests = 0, want the recorded requests")
	}
	if summary.Failures != 0 {
		t.Errorf("Summary.Failures = %d, want 0 against a healthy server", summary.Failures)
	}
	if summary.StatusCounts[200] != summary.Requests {
		t.Errorf("StatusCounts[200] = %d, want all %d requests", summary.StatusCounts[200], summary.Requests)
	}
	// Duration is recorded at nanosecond resolution, so a fast local server still
	// produces a non-zero maximum. The integer-millisecond Latency field would
	// truncate every one of these to zero.
	if summary.Max == 0 {
		t.Error("Summary.Max = 0, want a measurable latency")
	}
	if summary.RequestsPerSec == 0 {
		t.Error("Summary.RequestsPerSec = 0, want positive")
	}

	// The same figures must reach the progress writer.
	if !strings.Contains(progress.String(), "Load test complete.") {
		t.Errorf("Progress missing the summary\n---\n%s", progress.String())
	}
}

// TestLoadTestSummaryCountsFailures checks that error statuses are counted.
func TestLoadTestSummaryCountsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var summary LoadSummary

	err := LoadTest(t.Context(), strings.NewReader(twoPathHAR(srv.URL)), LoadTestOptions{
		Workers:  2,
		Duration: 200 * time.Millisecond,
		Summary:  &summary,
	})
	if err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if summary.Requests == 0 {
		t.Fatal("Summary.Requests = 0, want the recorded requests")
	}
	if summary.Failures != summary.Requests {
		t.Errorf("Summary.Failures = %d, want all %d requests", summary.Failures, summary.Requests)
	}
	if summary.ErrorRate != 1 {
		t.Errorf("Summary.ErrorRate = %v, want 1", summary.ErrorRate)
	}
}

// TestLoadTestSummaryWithInfluxDB is the regression test for the case that used
// to be impossible: the InfluxDB writer owned the results channel, so configuring
// it silently suppressed the summary.
func TestLoadTestSummaryWithInfluxDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	influx := &fakeInfluxDB{version: "1.8.0"}
	influxSrv := influx.server(t)

	var summary LoadSummary

	u := influxURL(t, influxSrv.URL, "hargo")

	err := LoadTest(t.Context(), strings.NewReader(twoPathHAR(srv.URL)), LoadTestOptions{
		Workers:     2,
		Duration:    200 * time.Millisecond,
		InfluxDBURL: &u,
		Summary:     &summary,
	})
	if err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if summary.Requests == 0 {
		t.Error("Summary.Requests = 0 with InfluxDB configured, want the recorded requests")
	}
	if summary.StatusCounts[200] != summary.Requests {
		t.Errorf("StatusCounts[200] = %d, want all %d requests", summary.StatusCounts[200], summary.Requests)
	}

	// Both jobs must still happen: the summary must not have displaced the
	// recording it used to be mutually exclusive with.
	if _, writes := influx.recorded(); len(writes) == 0 {
		t.Error("InfluxDB received no writes, want the results recorded alongside the summary")
	}
}

// TestLoadTestFilterSelectsSubset checks that filtering reaches the load path.
func TestLoadTestFilterSelectsSubset(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := LoadTest(t.Context(), strings.NewReader(twoPathHAR(srv.URL)), LoadTestOptions{
		Workers:  2,
		Duration: 200 * time.Millisecond,
		Filter:   EntryFilter{URL: `/second`},
	})
	if err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if paths["/second"] == 0 {
		t.Error("the selected entry was never requested")
	}
	if n := paths["/first"]; n != 0 {
		t.Errorf("the unselected entry was requested %d times, want 0", n)
	}
}

// TestLoadTestFilterMatchingNothingIsAnError checks that a filter selecting none
// of the entries present is reported rather than running an empty test.
func TestLoadTestFilterMatchingNothingIsAnError(t *testing.T) {
	var progress bytes.Buffer

	err := LoadTest(t.Context(), strings.NewReader(twoPathHAR("http://127.0.0.1:1")), LoadTestOptions{
		Workers:  1,
		Duration: time.Second,
		Filter:   EntryFilter{URL: `nothing-matches-this`},
		Progress: &progress,
	})
	if err == nil {
		t.Fatal("LoadTest() error = nil, want non-nil when the filter selects nothing")
	}
	if !strings.Contains(err.Error(), "filter selected none") {
		t.Errorf("LoadTest() error = %q, want it to explain that the filter matched nothing", err)
	}

	// A block of zeroes printed above the error would only obscure it.
	if strings.Contains(progress.String(), "Load test complete") {
		t.Errorf("Progress printed a summary for a test that never ran\n---\n%s", progress.String())
	}
}
