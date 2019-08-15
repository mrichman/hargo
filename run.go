package hargo

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/cookiejar"
)

// Run executes all entries in .har file
func Run(r *bufio.Reader, ignoreHarCookies bool, insecureSkipVerify bool) error {

	har, err := Decode(r)

	check(err)

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

	for _, entry := range har.Log.Entries {
		req, err := EntryToRequest(&entry, ignoreHarCookies)

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
