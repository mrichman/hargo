package hargo

import (
	"bufio"
	"encoding/json"
	"net/http"

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
	req, _ := http.NewRequest(entry.Request.Method, entry.Request.URL, nil)

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
