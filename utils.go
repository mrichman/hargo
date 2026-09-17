package hargo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
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
		return har, err
	}

	// Delete ws:// and wss:// entries as they block execution.
	// Filter in place rather than swap-deleting, which reads past the end of
	// the truncated slice once more than one entry is dropped.
	kept := har.Log.Entries[:0]
	for _, entry := range har.Log.Entries {
		if isWebSocket(entry.Request.URL) {
			continue
		}
		kept = append(kept, entry)
	}
	har.Log.Entries = kept

	// Sort the entries by StartedDateTime to ensure they will be processed
	// in the same order as they happened
	sort.Slice(har.Log.Entries, func(i, j int) bool {
		return har.Log.Entries[i].StartedDateTime < har.Log.Entries[j].StartedDateTime
	})

	return har, nil
}

// isWebSocket reports whether rawURL uses a WebSocket scheme. An http.Client
// cannot dial these, so they are dropped before replay.
func isWebSocket(rawURL string) bool {
	return strings.HasPrefix(rawURL, "ws://") || strings.HasPrefix(rawURL, "wss://")
}

// postBody returns the request body described by pd, preferring the
// URL-encoded params when present and falling back to the raw text.
func postBody(pd PostData) string {
	if len(pd.Params) > 0 {
		form := url.Values{}
		for _, p := range pd.Params {
			form.Add(p.Name, p.Value)
		}
		return form.Encode()
	}
	return pd.Text
}

// isPseudoHeader reports whether name is an HTTP/2 pseudo-header such as
// ":method" or ":authority". Browsers record these in HAR files, but a colon is
// not legal in an HTTP field name, so they must never be replayed or emitted.
func isPseudoHeader(name string) bool {
	return strings.HasPrefix(name, ":")
}

// isReplayableHeader reports whether a recorded HAR header can be sent on a real
// HTTP request.
func isReplayableHeader(name, value string) bool {
	if isPseudoHeader(name) {
		return false
	}
	return httpguts.ValidHeaderFieldName(name) && httpguts.ValidHeaderFieldValue(value)
}

// EntryToRequest converts a HAR entry type to an http.Request
func EntryToRequest(entry *Entry, ignoreHarCookies bool) (*http.Request, error) {
	body := postBody(entry.Request.PostData)

	req, err := http.NewRequest(entry.Request.Method, entry.Request.URL, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}

	for _, h := range entry.Request.Headers {
		// Cookie is skipped in favour of entry.Request.Cookies.
		if isReplayableHeader(h.Name, h.Value) && !strings.EqualFold(h.Name, "Cookie") {
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

// NewReader returns a bufio.Reader that will skip over initial UTF-8 byte order marks.
// https://tools.ietf.org/html/rfc7159#section-8.1
func NewReader(r io.Reader) *bufio.Reader {

	buf := bufio.NewReader(r)
	b, err := buf.Peek(3)
	if err != nil {
		// not enough bytes
		return buf
	}
	if b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		log.Warn("BOM detected. Skipping first 3 bytes of file. Consider removing the BOM from this file. " +
			"See https://tools.ietf.org/html/rfc7159#section-8.1 for details.")
		// The reader is only advanced past a confirmed BOM, so Discard cannot
		// fail here.
		_, _ = buf.Discard(3)
	}
	return buf
}
