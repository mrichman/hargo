package hargo

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// Run executes all entries in .har file
func Run(r *bufio.Reader, ignoreHarCookies bool) error {

	har, err := Decode(r)

	if err != nil {
		return err
	}

	check(err)

	jar, _ := cookiejar.New(nil)

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}

	if len(har.Log.Entries) == 0 {
		return nil
	}

	first, _ := time.Parse("2006-01-02T15:04:05.000Z", har.Log.Entries[0].StartedDateTime)

	for _, entry := range har.Log.Entries {

		st, _ := time.Parse("2006-01-02T15:04:05.000Z", entry.StartedDateTime)
		diffst := st.Sub(first)
		if diffst > 0 {
			time.Sleep(diffst * time.Nanosecond)
		}
		first = st

		req, err := EntryToRequest(&entry, ignoreHarCookies)

		if err != nil {
			return err
		}

		check(err)

		jar.SetCookies(req.URL, req.Cookies())

		resp, err := client.Do(req)

		check(err)

		fmt.Printf("[%s,%v] URL: %s\n", entry.Request.Method, resp.StatusCode, entry.Request.URL)

		if resp != nil {
			resp.Body.Close()
		}

	}

	return nil
}
