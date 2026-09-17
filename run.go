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

// RunOptions controls how Run replays a HAR file. The zero value replays in
// real time with cookies applied and TLS verified, matching Run.
type RunOptions struct {
	// IgnoreHarCookies drops the cookies recorded in the HAR.
	IgnoreHarCookies bool

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Speed scales the recorded delay between entries: 1 replays in real time
	// and 2 replays twice as fast. Zero or unset means 1, so that the zero value
	// of RunOptions behaves like Run. Negative values are rejected.
	//
	// Speed cannot express "no delay at all" because 0 is the zero value; use
	// NoWait for that.
	Speed float64

	// NoWait issues every request back to back, ignoring the recorded timings
	// entirely. It takes precedence over Speed and MaxDelay.
	NoWait bool

	// MaxDelay caps the wait before any single entry, applied after Speed. Zero
	// means no cap. Useful for HARs containing minutes of user idle time.
	MaxDelay time.Duration
}

// delayBefore returns how long to wait for a recorded inter-entry gap of d.
func (o RunOptions) delayBefore(d time.Duration) time.Duration {
	if o.NoWait || d <= 0 {
		return 0
	}

	speed := o.Speed
	if speed <= 0 {
		speed = 1
	}

	scaled := time.Duration(float64(d) / speed)
	if o.MaxDelay > 0 && scaled > o.MaxDelay {
		return o.MaxDelay
	}
	return scaled
}

// Run executes all entries in .har file in real time. It is equivalent to
// RunWithOptions with only IgnoreHarCookies and InsecureSkipVerify set.
func Run(r *bufio.Reader, ignoreHarCookies bool, insecureSkipVerify bool) error {
	return RunWithOptions(r, RunOptions{
		IgnoreHarCookies:   ignoreHarCookies,
		InsecureSkipVerify: insecureSkipVerify,
	})
}

// RunWithOptions executes all entries in .har file. Individual entries that
// cannot be built or sent are logged and skipped so that one bad entry does not
// abort the replay, but it reports how many failed so callers can exit non-zero.
func RunWithOptions(r *bufio.Reader, opts RunOptions) error {
	if opts.Speed < 0 {
		return fmt.Errorf("speed must not be negative, got %v", opts.Speed)
	}

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
			TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify},
		},
	}

	if len(har.Log.Entries) == 0 {
		return nil
	}

	first, _ := time.Parse("2006-01-02T15:04:05.000Z", har.Log.Entries[0].StartedDateTime)

	failed := 0

	for _, entry := range har.Log.Entries {

		st, _ := time.Parse("2006-01-02T15:04:05.000Z", entry.StartedDateTime)
		if d := opts.delayBefore(st.Sub(first)); d > 0 {
			time.Sleep(d)
		}
		first = st

		req, err := EntryToRequest(&entry, opts.IgnoreHarCookies)
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
