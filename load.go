package hargo

import (
	"bufio"
	"fmt"
	"net/url"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

var useInfluxDB = true // just in case we can't connect, run tests without recording results

// LoadTest executes all HTTP requests in order concurrently
// for a given number of workers.
func LoadTest(harfile string, r *bufio.Reader, workers int, timeout time.Duration, u url.URL, ignoreHarCookies bool, insecureSkipVerify bool) error {
	log.Infof("Starting load test with %d workers. Duration %v.", workers, timeout)
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Add(1)
	wg.Add(1)

	// tr := make(chan TestResult)
	// defer close(tr)

	e := make(chan Entry)
	defer close(e)

	stop := make(chan bool)
	defer close(stop)

	go readHARStream(r, e, &wg, stop)

	go wait(stop, timeout, workers, &wg)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go processEntries(harfile, e, &wg, ignoreHarCookies, insecureSkipVerify, stop)
	}

	// go func(harfile string, e chan Entry, wg *sync.WaitGroup, ignoreHarCookies bool, insecureSkipVerify bool, results chan TestResult, workers int, done chan int, stop chan bool) {

	// 	done <- 0
	// 	defer wg.Done()
	// 	j := 0
	// loop:
	// 	for {
	// 		select {
	// 		case <-stop:

	// 			break loop
	// 		case entry := <-e:
	// 			for {
	// 				i := <-done
	// 				j = j + i
	// 				if j < workers {
	// 					done <- 1
	// 					wg.Add(1)
	// 					go processEntries(harfile, entry, wg, ignoreHarCookies, insecureSkipVerify, tr, done)
	// 					break
	// 				}
	// 			}
	// 		}
	// 	}
	// 	log.Infoln("stop processing")
	// }(harfile, e, &wg, ignoreHarCookies, insecureSkipVerify, tr, workers, done, stop)

loop:
	for {
		select {
		case <-stop:
			log.Infoln("stop main")
			break loop
		}
	}

	defer wg.Wait()
	fmt.Printf("\nTimeout of %.1fs elapsed. Terminating load test.\n", timeout.Seconds())
	return nil
}

func wait(stop chan bool, timeout time.Duration, workers int, wg *sync.WaitGroup) {
	defer wg.Done()
	time.Sleep(timeout)
	log.Infoln("TIMEOUT")
	// once for the timer and once for the entry queue
	stop <- true
	stop <- true
	stop <- true
	for i := 0; i < workers; i++ {
		stop <- true
	}
}

func processEntries(harfile string, e chan Entry, wg *sync.WaitGroup, ignoreHarCookies bool, insecureSkipVerify bool, stop chan bool) {
	defer wg.Done()
process:
	for {
		select {
		case <-stop:
			break process
		case entry := <-e:
			time.Sleep(1 * time.Second)
			log.Infoln(entry.Request.URL)
		}
	}

	// log.Infoln(entry)
	// jar, _ := cookiejar.New(nil)

	// httpClient := http.Client{
	// 	Transport: &http.Transport{
	// 		Dial: (&net.Dialer{
	// 			Timeout:   1 * time.Second,
	// 			KeepAlive: 1 * time.Second,
	// 		}).Dial,
	// 		TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureSkipVerify},
	// 		TLSHandshakeTimeout:   1 * time.Second,
	// 		ResponseHeaderTimeout: 1 * time.Second,
	// 		ExpectContinueTimeout: 1 * time.Second,
	// 	},
	// 	CheckRedirect: func(r *http.Request, via []*http.Request) error {
	// 		r.URL.Opaque = r.URL.Path
	// 		return nil
	// 	},
	// 	Jar: jar,
	// }

	// iter := 0

	// msg := fmt.Sprintf("[%d,%d] %s", 1, iter, entry.Request.URL)

	// req, err := EntryToRequest(&entry, ignoreHarCookies)

	// check(err)

	// jar.SetCookies(req.URL, req.Cookies())

	// startTime := time.Now()
	// resp, err := httpClient.Do(req)
	// endTime := time.Now()
	// latency := int(endTime.Sub(startTime) / time.Millisecond)
	// method := req.Method

	// if err != nil {

	// 	log.Error(err)
	// 	log.Error(entry)
	// 	tr := TestResult{
	// 		URL:       req.URL.String(),
	// 		Status:    0,
	// 		StartTime: startTime,
	// 		EndTime:   endTime,
	// 		Latency:   latency,
	// 		Method:    method,
	// 		HarFile:   harfile}

	// 	results <- tr
	// 	return
	// }

	// if resp != nil {
	// 	resp.Body.Close()
	// }

	// msg += fmt.Sprintf(" %d %dms", resp.StatusCode, latency)

	// log.Debug(msg)

	// tr := TestResult{
	// 	URL:       req.URL.String(),
	// 	Status:    resp.StatusCode,
	// 	StartTime: startTime,
	// 	EndTime:   endTime,
	// 	Latency:   latency,
	// 	Method:    method,
	// 	HarFile:   harfile}
	// results <- tr

}
