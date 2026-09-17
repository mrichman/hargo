package hargo

import (
	"encoding/json"
	"io"
	"strings"

	"al.essio.dev/pkg/shellescape"
)

// CurlOptions controls how a HAR is converted to curl command lines. The zero
// value converts every entry.
type CurlOptions struct {
	// Filter selects which entries to convert. A zero Filter selects all of them.
	Filter EntryFilter
}

// ToCurl converts the entries a HAR document to curl command lines.
//
// curl -X <method> -b '<name=value; name=value...>' -H '<name: value>' ... -d '<postData>' <url>.
//
// For a large HAR prefer [ToCurlTo], which streams instead of assembling the
// whole output in memory.
func ToCurl(r io.Reader, opts CurlOptions) (string, error) {
	var buf strings.Builder
	if err := ToCurlTo(&buf, r, opts); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ToCurlTo writes a curl command line for each selected entry to w.
func ToCurlTo(w io.Writer, r io.Reader, opts CurlOptions) error {
	if err := opts.Filter.compile(); err != nil {
		return err
	}

	dec := json.NewDecoder(NewReader(r))
	var har HAR
	if err := dec.Decode(&har); err != nil {
		return err
	}

	// errWriter latches the first write error, so the loop does not need to check
	// every line.
	ew := &errWriter{w: w}

	for _, entry := range har.Log.Entries {
		if !opts.Filter.Match(entry) {
			continue
		}
		ew.printf("%s\n\n", fromEntry(entry))
	}

	return ew.err
}

// fromEntry renders one HAR entry as a curl command line.
//
// Arguments are collected and joined with a single space rather than each branch
// appending its own separator, which is how a missing space used to slip between
// the method and -d and produce "curl -X POST-d ...".
func fromEntry(entry Entry) string {
	// inspired by https://github.com/snoe/harToCurl/blob/master/harToCurl

	// Every value that reaches the shell is quoted, including the method: a HAR
	// is untrusted input, and the output is meant to be pasted into a shell.
	args := []string{"curl", "-X", shellescape.Quote(entry.Request.Method)}

	if entry.Request.HTTPVersion == "HTTP/1.0" {
		args = append(args, "-0")
	}

	var cookies []string
	for _, cookie := range entry.Request.Cookies {
		// RFC 6265 separates cookie-pairs with "; ", and the recorded value is
		// emitted as the browser sent it. URL-encoding it here would turn a space
		// into "+" and percent-encode octets that are legal in a cookie.
		cookies = append(cookies, cookie.Name+"="+cookie.Value)
	}
	if len(cookies) > 0 {
		args = append(args, "-b", shellescape.Quote(strings.Join(cookies, "; ")))
	}

	for _, h := range entry.Request.Headers {
		// HTTP/2 pseudo-headers (":method", ":authority", ...) are recorded by
		// browsers but are not valid header fields, so curl rejects them.
		if !isReplayableHeader(h.Name, h.Value) {
			continue
		}
		// Avoid sending cookies twice when -b already carries them.
		if len(cookies) > 0 && strings.EqualFold(h.Name, "Cookie") {
			continue
		}
		args = append(args, "-H", shellescape.Quote(h.Name+": "+h.Value))
	}

	// Emit the body for any method that carries one, not just POST.
	body := postBody(entry.Request.PostData)
	if len(body) > 0 {
		args = append(args, "-d", shellescape.Quote(body))

		// Without a Content-Type the server cannot interpret the body, and the
		// type is recorded on postData rather than always duplicated as a header.
		if entry.Request.PostData.MimeType != "" && !hasHeader(entry, "Content-Type") {
			args = append(args, "-H",
				shellescape.Quote("Content-Type: "+entry.Request.PostData.MimeType))
		}
	}

	args = append(args, shellescape.Quote(entry.Request.URL))

	return strings.Join(args, " ")
}

// hasHeader reports whether entry records the named request header.
func hasHeader(entry Entry, name string) bool {
	for _, h := range entry.Request.Headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}
