package hargo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

// LoadTestOptions controls how LoadTest drives a HAR file. The zero value runs
// a single worker until ctx is cancelled, discarding results.
type LoadTestOptions struct {
	// HARFile labels the results with the name of the file under test.
	HARFile string

	// Workers is how many concurrent workers to run. Zero or unset means 1.
	Workers int

	// Duration bounds the test. Zero means run until ctx is cancelled.
	Duration time.Duration

	// InfluxDBURL is where results are recorded. A nil URL discards them.
	InfluxDBURL *url.URL

	// IgnoreHARCookies drops the cookies recorded in the HAR.
	IgnoreHARCookies bool

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Results, when non-nil, receives a copy of every result. LoadTest does not
	// close it. A caller that supplies this channel must keep reading from it
	// for the duration of the test, or the workers will block.
	Results chan<- TestResult

	// Logger receives diagnostics about failed requests. A nil Logger discards
	// them.
	Logger *slog.Logger

	// Progress receives one line per request. A nil Progress discards it.
	//
	// Writes are serialized internally, so it need not be safe for concurrent use
	// even though the workers run in parallel.
	Progress io.Writer
}

// syncWriter serializes concurrent writes to an underlying writer.
//
// Every worker shares one Progress writer. os.Stdout tolerates that because
// os.File.Write holds an internal lock, but the obvious *bytes.Buffer does not,
// and callers should not have to know the difference.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// influxSetupTimeout bounds establishing the InfluxDB connection, which includes
// retry backoff. It is separate from the test duration so that setup neither eats
// the test budget nor reports a connection failure as the test deadline expiring.
const influxSetupTimeout = 90 * time.Second

// workers returns the number of workers to run.
func (o LoadTestOptions) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	return 1
}

// LoadTest replays the entries in a HAR document concurrently until ctx is
// cancelled or opts.Duration elapses, recording the result of every request.
//
// r is read repeatedly from the start, so the test can run for longer than the
// recording.
func LoadTest(ctx context.Context, r io.ReadSeeker, opts LoadTestOptions) error {
	if opts.Workers < 0 {
		return fmt.Errorf("workers must not be negative, got %d", opts.Workers)
	}
	if opts.Duration < 0 {
		return fmt.Errorf("duration must not be negative, got %v", opts.Duration)
	}

	logger := loggerOrDiscard(opts.Logger)
	// Workers write concurrently, so the caller's writer is wrapped rather than
	// used directly.
	progress := &syncWriter{w: writerOrDiscard(opts.Progress)}
	workers := opts.workers()

	// A duration bound is just another way to cancel the run.
	if opts.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Duration)
		defer cancel()
	}

	logger.Info("starting load test", "workers", workers, "duration", opts.Duration)

	// The writer is established before any worker starts, so that a bad InfluxDB
	// URL fails the run immediately rather than after the full duration.
	//
	// Setup uses its own deadline: it should not consume the test's budget, and a
	// connection failure should be reported as itself rather than surfacing as the
	// test's deadline expiring.
	var writer *influxWriter
	if opts.InfluxDBURL != nil {
		setupCtx, cancelSetup := context.WithTimeout(ctx, influxSetupTimeout)
		w, err := newInfluxWriter(setupCtx, *opts.InfluxDBURL, opts.Logger)
		cancelSetup()
		if err != nil {
			return fmt.Errorf("connecting to InfluxDB: %w", err)
		}
		defer func() { _ = w.close() }()
		writer = w
	}

	results := make(chan TestResult)
	entries := make(chan Entry, workers)

	var readErr error
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		readErr = ReadStream(ctx, r, entries, opts.Logger)
	}()

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		if writer != nil {
			writer.write(results)
			return
		}
		// Drain, so a run without InfluxDB never blocks its workers.
		for range results {
		}
	}()

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			processEntries(ctx, worker, entries, results, opts, logger, progress)
		}(i)
	}

	// Close results only once every producer has exited, otherwise a worker
	// still in flight sends on a closed channel and panics.
	wg.Wait()
	close(results)
	<-consumerDone
	<-readDone

	_, _ = fmt.Fprintf(progress, "\nLoad test complete.\n")

	// Cancellation and the duration bound are how this test is meant to end.
	if readErr != nil && !isDone(readErr) {
		return readErr
	}
	return nil
}

// isDone reports whether err is the expected end of a bounded or cancelled run.
func isDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func processEntries(ctx context.Context, worker int, entries <-chan Entry,
	results chan<- TestResult, opts LoadTestOptions, logger *slog.Logger, progress io.Writer) {
	jar, _ := cookiejar.New(nil)

	transport := &http.Transport{
		// DialContext rather than the deprecated Dial, so an in-flight dial
		// is cancelled as soon as it is no longer needed.
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// A hand-built Transport has no idle timeout at all, unlike
		// http.DefaultTransport, so idle sockets would be held until the process
		// exited.
		IdleConnTimeout: 90 * time.Second,
	}
	// Each worker owns its transport, so it has to release the sockets itself; a
	// library caller running several load tests would otherwise leak them.
	defer transport.CloseIdleConnections()

	httpClient := http.Client{
		Transport: transport,
		// The default redirect policy is correct. Rewriting URL.Opaque forced the
		// request line to the decoded path, which emits a malformed request for a
		// redirect target containing an escape such as %20.
		Jar: jar,
	}

	iter := 0
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}

			req, err := EntryToRequest(ctx, &entry, EntryOptions{IgnoreHARCookies: opts.IgnoreHARCookies})
			if err != nil {
				logger.Error("skipping entry", "url", entry.Request.URL, "err", err)
				continue
			}

			jar.SetCookies(req.URL, req.Cookies())

			startTime := time.Now()
			resp, err := httpClient.Do(req)

			// Do returns once the response headers are read. Draining the body
			// before stopping the clock makes Latency the time to receive the whole
			// response, and it is also what lets the connection be reused: closing
			// an undrained body tears the connection down, so every request would
			// otherwise pay a fresh TCP and TLS handshake.
			var status int
			if err == nil {
				status = resp.StatusCode
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}

			endTime := time.Now()
			latency := int(endTime.Sub(startTime) / time.Millisecond)

			result := TestResult{
				URL:       req.URL.String(),
				Status:    status,
				StartTime: startTime,
				EndTime:   endTime,
				Latency:   latency,
				Method:    req.Method,
				HARFile:   opts.HARFile,
			}

			if err != nil {
				// A cancelled run surfaces here as a request error; it is the
				// expected ending, not a failed request worth recording.
				if ctx.Err() != nil {
					return
				}
				logger.Error("request failed", "url", entry.Request.URL, "err", err)
			} else {
				// Progress writes are serialized by syncWriter; a failed write is
				// not something a worker can act on.
				_, _ = fmt.Fprintf(progress, "[%d,%d] %s %d %dms\n",
					worker, iter, entry.Request.URL, result.Status, latency)
			}

			if !send(ctx, results, result) {
				return
			}
			if opts.Results != nil && !send(ctx, opts.Results, result) {
				return
			}
		}
		iter++
	}
}

// send delivers result on ch, reporting false if ctx is cancelled first. A
// plain send would block forever once the consumer has stopped reading.
func send(ctx context.Context, ch chan<- TestResult, result TestResult) bool {
	select {
	case ch <- result:
		return true
	case <-ctx.Done():
		return false
	}
}
