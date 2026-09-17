package hargo

import (
	"bufio"
	"encoding/json"
	"net/url"
	"strings"

	"al.essio.dev/pkg/shellescape"
	log "github.com/sirupsen/logrus"
)

// ToCurl converts a HAR Entry to a curl command line
// curl -X <method> -b "<name=value&name=value...>" -H <name: value> ... -d "<postData>" <url>
func ToCurl(r *bufio.Reader) (string, error) {
	dec := json.NewDecoder(r)
	var har Har
	if err := dec.Decode(&har); err != nil {
		log.Error(err)
		return "", err
	}

	var command string

	for _, entry := range har.Log.Entries {
		cmd, err := fromEntry(entry)
		if err != nil {
			return "", err
		}

		command += cmd + "\n\n"
	}

	return command, nil
}

func fromEntry(entry Entry) (string, error) {
	// inspired by https://github.com/snoe/harToCurl/blob/master/harToCurl

	command := "curl -X " + entry.Request.Method

	if entry.Request.HTTPVersion == "HTTP/1.0" {
		command += " -0"
	}

	var cookies []string

	if len(entry.Request.Cookies) > 0 {
		for _, cookie := range entry.Request.Cookies {
			cookies = append(cookies, url.QueryEscape(cookie.Name)+"="+url.QueryEscape(cookie.Value))
		}
		command += " -b " + shellescape.Quote(strings.Join(cookies[:], "&")) + " "
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
		command += " -H " + shellescape.Quote(h.Name+": "+h.Value) + " "
	}

	// Emit the body for any method that carries one, not just POST, and
	// fall back to URL-encoded params when the HAR has no raw text.
	if body := postBody(entry.Request.PostData); len(body) > 0 {
		command += "-d " + shellescape.Quote(body)
	}

	command += " " + shellescape.Quote(entry.Request.URL)

	return command, nil
}
