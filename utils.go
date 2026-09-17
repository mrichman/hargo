package hargo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"
)

// loggerOrDiscard returns l, or a logger that discards everything when l is
// nil. A library must be silent unless the caller asks for output.
func loggerOrDiscard(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}

// writerOrDiscard returns w, or io.Discard when w is nil.
func writerOrDiscard(w io.Writer) io.Writer {
	if w != nil {
		return w
	}
	return io.Discard
}

// Decode reads a HAR document from r and returns it.
//
// WebSocket entries are removed because an http.Client cannot dial them, and
// the remaining entries are ordered by their recorded start time.
func Decode(r io.Reader) (HAR, error) {
	dec := json.NewDecoder(NewReader(r))
	var har HAR

	if err := dec.Decode(&har); err != nil {
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

	// Order the entries as they happened. Comparing the raw strings would only
	// agree with chronological order when every timestamp shares one format and
	// one UTC offset, so compare parsed times instead. The sort is stable so that
	// entries recorded in the same instant keep their recorded order.
	//
	// The parsed time travels with its entry: a parallel array indexed by
	// position would be read through positions the sort is still permuting.
	type timedEntry struct {
		start time.Time
		entry Entry
	}
	decorated := make([]timedEntry, len(har.Log.Entries))
	for i, entry := range har.Log.Entries {
		// An unparseable timestamp sorts as the zero time, which keeps it where
		// the recording put it relative to its unparseable neighbours.
		start, _ := ParseEntryTime(entry.StartedDateTime)
		decorated[i] = timedEntry{start: start, entry: entry}
	}
	sort.SliceStable(decorated, func(i, j int) bool {
		return decorated[i].start.Before(decorated[j].start)
	})
	for i, d := range decorated {
		har.Log.Entries[i] = d.entry
	}

	return har, nil
}

// entryTimeLayouts are the layouts accepted for a HAR startedDateTime, most
// specific first. The spec says ISO 8601, which browsers and proxies interpret
// liberally: Chrome writes exactly three fractional digits and a literal Z,
// Firefox writes a numeric offset, and other tools omit the fraction or the zone
// altogether.
var entryTimeLayouts = []string{
	time.RFC3339Nano,                     // any number of fractional digits, Z or +01:00
	"2006-01-02T15:04:05.999999999-0700", // numeric offset without a colon
	"2006-01-02T15:04:05.999999999",      // no zone at all
	"2006-01-02",                         // date only
}

// ParseEntryTime parses a HAR startedDateTime.
//
// A caller that ignores the error gets the zero time, which is why the replaying
// operations report it instead: treating an unparseable timestamp as year one
// makes the gap to the next entry astronomically large.
func ParseEntryTime(s string) (time.Time, error) {
	for _, layout := range entryTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse startedDateTime %q as an ISO 8601 timestamp", s)
}

// isWebSocket reports whether rawURL uses a WebSocket scheme. An http.Client
// cannot dial these, so they are dropped before replay.
func isWebSocket(rawURL string) bool {
	// URL schemes are case-insensitive, so WS:// must be caught too; otherwise it
	// survives the filter and fails later as an unsupported protocol scheme.
	lower := strings.ToLower(rawURL)
	return strings.HasPrefix(lower, "ws://") || strings.HasPrefix(lower, "wss://")
}

// postBody returns the request body described by pd.
//
// The raw text is preferred whenever it is present, because it is what the
// browser actually sent. Params are only URL-encoded for a form body: doing so
// for multipart/form-data would produce a body that contradicts the recorded
// Content-Type, which the server then cannot parse.
func postBody(pd PostData) string {
	if pd.Text != "" {
		return pd.Text
	}

	if len(pd.Params) > 0 && isFormEncoded(pd.MimeType) {
		form := url.Values{}
		for _, p := range pd.Params {
			form.Add(p.Name, p.Value)
		}
		return form.Encode()
	}

	return ""
}

// isFormEncoded reports whether mimeType is application/x-www-form-urlencoded,
// ignoring any parameters such as a charset. An empty type is treated as a form
// body, since that is the only shape params can be reconstructed into.
func isFormEncoded(mimeType string) bool {
	base, _, _ := strings.Cut(mimeType, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	return base == "" || base == "application/x-www-form-urlencoded"
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

// EntryOptions controls how a HAR entry is turned into an http.Request.
type EntryOptions struct {
	// IgnoreHARCookies drops the cookies recorded in the HAR entry.
	IgnoreHARCookies bool
}

// EntryToRequest converts a HAR entry into an http.Request bound to ctx.
func EntryToRequest(ctx context.Context, entry *Entry, opts EntryOptions) (*http.Request, error) {
	body := postBody(entry.Request.PostData)

	req, err := http.NewRequestWithContext(ctx, entry.Request.Method, entry.Request.URL,
		bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}

	// http.NewRequest already sets req.Host from the URL, so the recorded value is
	// tracked separately rather than by testing req.Host for emptiness.
	recordedHost := ""

	for _, h := range entry.Request.Headers {
		// An HTTP/2 capture has no Host header; :authority carries the same
		// information. It is not a legal field name, so it is only read here.
		if strings.EqualFold(h.Name, ":authority") && recordedHost == "" {
			recordedHost = h.Value
		}

		if !isReplayableHeader(h.Name, h.Value) {
			continue
		}
		// Cookie is skipped in favour of entry.Request.Cookies.
		if strings.EqualFold(h.Name, "Cookie") {
			continue
		}
		// net/http ignores a Host entry in the header map; the value has to go on
		// the request itself or the recorded virtual host is silently lost. An
		// explicit Host header wins over :authority.
		if strings.EqualFold(h.Name, "Host") {
			recordedHost = h.Value
			continue
		}
		req.Header.Add(h.Name, h.Value)
	}

	if recordedHost != "" {
		req.Host = recordedHost
	}

	// Without a Content-Type the server cannot interpret the body, and browsers
	// record the type on postData rather than always duplicating it as a header.
	if body != "" && req.Header.Get("Content-Type") == "" && entry.Request.PostData.MimeType != "" {
		req.Header.Set("Content-Type", entry.Request.PostData.MimeType)
	}

	if !opts.IgnoreHARCookies {
		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}
	}

	return req, nil
}

// NewReader returns a bufio.Reader that skips an initial UTF-8 byte order mark.
// The parsing entry points apply this themselves, so callers rarely need it.
//
// https://tools.ietf.org/html/rfc7159#section-8.1
func NewReader(r io.Reader) *bufio.Reader {
	// Avoid stacking another layer when the input is already buffered.
	if buf, ok := r.(*bufio.Reader); ok {
		skipBOM(buf)
		return buf
	}

	buf := bufio.NewReader(r)
	skipBOM(buf)
	return buf
}

// skipBOM discards a leading UTF-8 byte order mark if one is present.
func skipBOM(buf *bufio.Reader) {
	b, err := buf.Peek(3)
	if err != nil {
		// Not enough bytes for a BOM.
		return
	}
	if b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		// The reader is only advanced past a confirmed BOM, so Discard cannot
		// fail here.
		_, _ = buf.Discard(3)
	}
}
