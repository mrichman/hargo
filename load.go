package hargo

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"
)

var useInfluxDB = true // just in case we can't connect, run tests without recording results

// LoadTest executes all HTTP requests in order concurrently
// for a given number of workers.
func LoadTest(harfile string, r *bufio.Reader, workers int, timeout time.Duration, u url.URL, ignoreHarCookies bool, insecureSkipVerify bool) error {
	log.Infof("Starting load test with %d workers. Duration %v.", workers, timeout)

	results := make(chan TestResult)
	defer close(results)
	stop := make(chan bool)
	entries := make(chan Entry)

	go readHARStream(r, entries, stop)

	if (url.URL{}) != u {
		go WritePoint(u, results)
	} else {
		go func(results chan TestResult) {
			for {
				<-results
			}
		}(results)
	}

	go wait(stop, timeout, workers)

	for i := 0; i < workers; i++ {
		go processEntries(harfile, entries, results, ignoreHarCookies, insecureSkipVerify, stop)
	}

	for {
		select {
		case <-stop:
			log.Infoln("stop main")
		}
		break
	}
	fmt.Printf("\nTimeout of %.1fs elapsed. Terminating load test.\n", timeout.Seconds())
	return nil
}

func wait(stop chan bool, timeout time.Duration, workers int) {
	time.Sleep(timeout)
	log.Infoln("TIMEOUT")
	close(stop)
}

func processEntries(harfile string, entries chan Entry, results chan TestResult, ignoreHarCookies bool, insecureSkipVerify bool, stop chan bool) {
	jar, _ := cookiejar.New(nil)

	httpClient := http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
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
			break
		case entry := <-entries:
			msg := fmt.Sprintf("[%d,%d] %s", 1, iter, entry.Request.URL)

			req, err := EntryToRequest(&entry, ignoreHarCookies)

			check(err)

			jar.SetCookies(req.URL, req.Cookies())

			startTime := time.Now()
			resp, err := httpClient.Do(req)
			endTime := time.Now()
			latency := int(endTime.Sub(startTime) / time.Millisecond)
			method := req.Method

			if err != nil {

				log.Error(err)
				log.Error(entry)
				tr := TestResult{
					URL:       req.URL.String(),
					Status:    0,
					StartTime: startTime,
					EndTime:   endTime,
					Latency:   latency,
					Method:    method,
					HarFile:   harfile}
				results <- tr
				continue
			}

			if resp != nil {
				resp.Body.Close()
			}

			msg += fmt.Sprintf(" %d %dms", resp.StatusCode, latency)

			log.Infoln(msg)

			tr := TestResult{
				URL:       req.URL.String(),
				Status:    resp.StatusCode,
				StartTime: startTime,
				EndTime:   endTime,
				Latency:   latency,
				Method:    method,
				HarFile:   harfile}

			results <- tr
		}
		iter++
	}
}
