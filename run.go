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

	for _, entry := range har.Log.Entries {
		fmt.Printf("URL: %s\n", entry.Request.URL)

		req, err := EntryToRequest(&entry)

		check(err)

		jar, _ := cookiejar.New(nil)

		jar.SetCookies(req.URL, req.Cookies())

		client := http.Client{
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				r.URL.Opaque = r.URL.Path
				return nil
			},
			Jar: jar,
		}

		resp, err := client.Do(req)

		check(err)

		if resp != nil {
			resp.Body.Close()
		}

	}

	return nil
}
