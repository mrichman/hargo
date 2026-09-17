package hargo

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// LoadTest executes all HTTP requests in order concurrently
// for a given number of workers.
func LoadTest(harfile string, file *os.File, workers int, timeout time.Duration, u url.URL, ignoreHarCookies bool, insecureSkipVerify bool) error {
	log.Infof("Starting load test with %d workers. Duration %v.", workers, timeout)

	results := make(chan TestResult)
	stop := make(chan bool)
	entries := make(chan Entry, workers)

	go ReadStream(file, entries, stop)

	// if a InfluxDB URL is given the metrics will be written to that instance
	// if not the dummy consumer is initiated.
	consumerDone := make(chan struct{})
	if (url.URL{}) != u {
		go func() {
			defer close(consumerDone)
			WritePoint(u, results)
		}()
	} else {
		go func() {
			defer close(consumerDone)
			for range results {
			}
		}()
	}

	go wait(stop, timeout)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			processEntries(harfile, worker, entries, results, ignoreHarCookies, insecureSkipVerify, stop)
		}(i)
	}

	<-stop

	// Close results only once every producer has exited, otherwise a worker
	// still in flight sends on a closed channel and panics.
	wg.Wait()
	close(results)
	<-consumerDone

	fmt.Printf("\nTimeout of %.1fs elapsed. Terminating load test.\n", timeout.Seconds())
	return nil
}

// wait will close the stop chan when the timeout is hit.
func wait(stop chan bool, timeout time.Duration) {
	time.Sleep(timeout)
	close(stop)
}

func processEntries(harfile string, worker int, entries chan Entry, results chan TestResult, ignoreHarCookies bool, insecureSkipVerify bool, stop chan bool) {
	jar, _ := cookiejar.New(nil)

	httpClient := http.Client{
		Transport: &http.Transport{
			// DialContext rather than the deprecated Dial, so an in-flight dial
			// is cancelled as soon as it is no longer needed.
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureSkipVerify},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}
	iter := 0
	for {
		select {
		case <-stop:
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}

			msg := fmt.Sprintf("[%d,%d] %s", worker, iter, entry.Request.URL)

			req, err := EntryToRequest(&entry, ignoreHarCookies)
			if err != nil {
				log.Errorf("skipping entry %s: %v", entry.Request.URL, err)
				continue
			}

			jar.SetCookies(req.URL, req.Cookies())

			startTime := time.Now()
			resp, err := httpClient.Do(req)
			endTime := time.Now()
			latency := int(endTime.Sub(startTime) / time.Millisecond)
			method := req.Method

			if err != nil {
				log.Error(err)
				log.Error(entry)
				results <- TestResult{
					URL:       req.URL.String(),
					Status:    0,
					StartTime: startTime,
					EndTime:   endTime,
					Latency:   latency,
					Method:    method,
					HarFile:   harfile}
				continue
			}

			status := resp.StatusCode
			_ = resp.Body.Close()

			msg += fmt.Sprintf(" %d %dms", status, latency)

			log.Infoln(msg)

			results <- TestResult{
				URL:       req.URL.String(),
				Status:    status,
				StartTime: startTime,
				EndTime:   endTime,
				Latency:   latency,
				Method:    method,
				HarFile:   harfile}
		}
		iter++
	}
}
