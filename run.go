package hargo

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"

	log "github.com/sirupsen/logrus"
)

// Run executes all entries in .har file. Individual entries that cannot be
// built or sent are logged and skipped so that one bad entry does not abort the
// replay, but Run reports how many failed so callers can exit non-zero.
func Run(r *bufio.Reader, ignoreHarCookies bool, insecureSkipVerify bool) error {

	har, err := Decode(r)

	if err != nil {
		return err
	}

	jar, _ := cookiejar.New(nil)

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
		},
	}

	if len(har.Log.Entries) == 0 {
		return nil
	}

	first, _ := time.Parse("2006-01-02T15:04:05.000Z", har.Log.Entries[0].StartedDateTime)

	failed := 0

	for _, entry := range har.Log.Entries {

		st, _ := time.Parse("2006-01-02T15:04:05.000Z", entry.StartedDateTime)
		diffst := st.Sub(first)
		if diffst > 0 {
			time.Sleep(diffst)
		}
		first = st

		req, err := EntryToRequest(&entry, ignoreHarCookies)
		if err != nil {
			// A malformed entry must not abort the remaining replay.
			log.Errorf("skipping entry %s: %v", entry.Request.URL, err)
			failed++
			continue
		}

		jar.SetCookies(req.URL, req.Cookies())

		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("request failed %s: %v", entry.Request.URL, err)
			failed++
			continue
		}

		fmt.Printf("[%s,%v] URL: %s\n", entry.Request.Method, resp.StatusCode, entry.Request.URL)

		_ = resp.Body.Close()
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(har.Log.Entries))
	}

	return nil
}
