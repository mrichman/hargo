package hargo

import (
	"fmt"
	"io"
	"sort"
	"time"
)

// LoadSummary reports what a load test observed. A caller obtains one by setting
// [LoadTestOptions.Summary].
type LoadSummary struct {
	// Requests is how many results were recorded.
	Requests int

	// Failures counts results that either never completed or returned a status of
	// 400 or above.
	Failures int

	// ErrorRate is Failures divided by Requests, or 0 when nothing ran.
	ErrorRate float64

	// Elapsed is the measured wall clock time of the test. It is not the
	// configured duration, because a cancelled run ends early.
	Elapsed time.Duration

	// RequestsPerSec is Requests divided by Elapsed.
	RequestsPerSec float64

	// Latency distribution across every recorded result.
	Min, P50, P95, P99, Max time.Duration

	// StatusCounts counts results by status code. A key of 0 means the request
	// never completed.
	StatusCounts map[int]int
}

// summaryAccumulator collects results as they arrive and produces a LoadSummary.
//
// Latencies are retained individually so the percentiles are exact rather than
// bucketed. That costs 8 bytes per request, so a very long high-throughput run
// holds a correspondingly large slice; at ten million requests that is about
// 80 MB. Nothing is discarded silently, so the numbers can be trusted.
type summaryAccumulator struct {
	latencies    []time.Duration
	statusCounts map[int]int
	failures     int
	started      time.Time
}

func newSummaryAccumulator() *summaryAccumulator {
	return &summaryAccumulator{
		statusCounts: make(map[int]int),
		started:      time.Now(),
	}
}

// add records one result.
func (a *summaryAccumulator) add(result TestResult) {
	a.latencies = append(a.latencies, result.Duration)
	a.statusCounts[result.Status]++

	// outcome is shared with the InfluxDB tags, so the summary and the recorded
	// series always agree on what counts as a failure.
	if outcome(result.Status) != "ok" {
		a.failures++
	}
}

// summary computes the final figures.
func (a *summaryAccumulator) summary() LoadSummary {
	elapsed := time.Since(a.started)

	s := LoadSummary{
		Requests:     len(a.latencies),
		Failures:     a.failures,
		Elapsed:      elapsed,
		StatusCounts: a.statusCounts,
	}

	if s.Requests > 0 {
		s.ErrorRate = float64(s.Failures) / float64(s.Requests)
	}
	if elapsed > 0 {
		s.RequestsPerSec = float64(s.Requests) / elapsed.Seconds()
	}

	if len(a.latencies) > 0 {
		sort.Slice(a.latencies, func(i, j int) bool { return a.latencies[i] < a.latencies[j] })
		s.Min = a.latencies[0]
		s.Max = a.latencies[len(a.latencies)-1]
		s.P50 = percentile(a.latencies, 50)
		s.P95 = percentile(a.latencies, 95)
		s.P99 = percentile(a.latencies, 99)
	}

	return s
}

// percentile returns the p-th percentile of an already sorted slice using nearest
// rank, which needs no interpolation and always returns an observed value.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	// Nearest rank: ceil(p/100 * N), clamped into range.
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// writeTo renders s in a form meant to be read by a person.
func (s LoadSummary) writeTo(w io.Writer) {
	ew := &errWriter{w: w}

	ew.printf("\nLoad test complete.\n")
	ew.printf("  requests    %d in %s (%.1f/sec)\n",
		s.Requests, s.Elapsed.Round(time.Millisecond), s.RequestsPerSec)
	ew.printf("  failures    %d (%.1f%%)\n", s.Failures, s.ErrorRate*100)
	ew.printf("  latency     min %s  p50 %s  p95 %s  p99 %s  max %s\n",
		round(s.Min), round(s.P50), round(s.P95), round(s.P99), round(s.Max))

	if len(s.StatusCounts) > 0 {
		codes := make([]int, 0, len(s.StatusCounts))
		for code := range s.StatusCounts {
			codes = append(codes, code)
		}
		sort.Ints(codes)

		ew.printf("  status     ")
		for _, code := range codes {
			label := fmt.Sprint(code)
			if code == 0 {
				// A zero status means the request never got a response at all.
				label = "no response"
			}
			ew.printf(" %s=%d", label, s.StatusCounts[code])
		}
		ew.printf("\n")
	}
}

// round trims a duration to something readable without losing sub-millisecond
// detail on a fast local server.
func round(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d.Round(time.Microsecond)
	}
	return d.Round(100 * time.Microsecond)
}
