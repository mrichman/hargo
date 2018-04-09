package hargo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"

	"golang.org/x/net/lex/httplex"

	log "github.com/Sirupsen/logrus"
)

// Decode reads from a reader and returns Har object
func Decode(r *bufio.Reader) (Har, error) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)

	if err != nil {
		log.Error(err)
	}

	return har, err
}

// EntryToRequest converts a HAR entry type to an http.Request
func EntryToRequest(entry *Entry) (*http.Request, error) {
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
		if httplex.ValidHeaderFieldName(h.Name) && httplex.ValidHeaderFieldValue(h.Value) {
			req.Header.Add(h.Name, h.Value)
		}
	}

	for _, c := range entry.Request.Cookies {
		cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
		req.AddCookie(cookie)
	}

	return req, nil
}

func check(err error) {
	if err != nil {
		log.Error(err)
	}
}
