package hargo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/http/httpguts"

	log "github.com/sirupsen/logrus"
)

// Decode reads from a reader and returns Har object
func Decode(r *bufio.Reader) (Har, error) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)

	if err != nil {
		log.Error(err)
	}

	// Delete ws:// entries as they block execution
	for i, entry := range har.Log.Entries {
		if strings.HasPrefix(entry.Request.URL, "ws://") {
			har.Log.Entries[i] = har.Log.Entries[len(har.Log.Entries)-1]
			har.Log.Entries = har.Log.Entries[:len(har.Log.Entries)-1]
		}
	}

	// Sort the entries by StartedDateTime to ensure they will be processed
	// in the same order as they happened
	sort.Slice(har.Log.Entries, func(i, j int) bool {
		return har.Log.Entries[i].StartedDateTime < har.Log.Entries[j].StartedDateTime
	})

	return har, err
}

// EntryToRequest converts a HAR entry type to an http.Request
func EntryToRequest(entry *Entry, ignoreHarCookies bool) (*http.Request, error) {
	body := ""

	if len(entry.Request.PostData.Params) == 0 {
		body = entry.Request.PostData.Text
	} else {
		form := url.Values{}
		for _, p := range entry.Request.PostData.Params {
			form.Add(p.Name, p.Value)
		}
		body = form.Encode()
	}

	req, _ := http.NewRequest(entry.Request.Method, entry.Request.URL, bytes.NewBuffer([]byte(body)))

	for _, h := range entry.Request.Headers {
		if httpguts.ValidHeaderFieldName(h.Name) && httpguts.ValidHeaderFieldValue(h.Value) && h.Name != "Cookie" {
			req.Header.Add(h.Name, h.Value)
		}
	}

	if !ignoreHarCookies {
		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}
	}

	return req, nil
}

func check(err error) {
	if err != nil {
		log.Error(err)
	}
}
