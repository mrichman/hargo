package hargo

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/cookiejar"
)

// Run executes all entries in .har file
func Run(r *bufio.Reader) error {

	har, err := Decode(r)

	check(err)

	jar, _ := cookiejar.New(nil)

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}

	for _, entry := range har.Log.Entries {
		fmt.Printf("[%s] URL: %s\n", entry.Request.Method, entry.Request.URL)

		req, err := EntryToRequest(&entry)

		check(err)

		jar.SetCookies(req.URL, req.Cookies())

		resp, err := client.Do(req)

		check(err)

		if resp != nil {
			resp.Body.Close()
		}

	}

	return nil
}
