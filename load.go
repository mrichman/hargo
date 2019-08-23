package hargo

import (
	"bufio"
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

	tr := make(chan TestResult)
	defer close(tr)

	e := make(chan Entry)
	defer close(e)

	t := make(chan bool)
	defer close(t)

	var wg sync.WaitGroup

	wg.Add(1)
	go readHARStream(r, e, &wg)

	go wait(t, timeout)

	go func() {
		for {
			select {
			case <-t:
				break
			case entry := <-e:
				wg.Add(1)
				go processEntries(harfile, entry, &wg, ignoreHarCookies, insecureSkipVerify, tr)
			}
		}
	}()

	for {
		select {
		case <-t:
			wg.Wait()
			return nil
		}
	}
}

func wait(t chan bool, timeout time.Duration) {
	time.Sleep(timeout)
	// once for the timer and once for the enrty queue
	t <- true
	t <- true
}

func processEntries(harfile string, entry Entry, wg *sync.WaitGroup, ignoreHarCookies bool, insecureSkipVerify bool, results chan TestResult) {
	defer wg.Done()

	time.Sleep(1 * time.Second)
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

	// log.Infoln("DONE!?")
	return
}
