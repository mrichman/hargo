package hargo

import (
	"bufio"
	"encoding/json"
	"net/url"
	"strings"

	log "github.com/Sirupsen/logrus"
)

// ToCurl converts a HAR Entry to a curl command line
// curl -X <method> -b "<name=value&name=value...>" -H <name: value> ... -d "<postData>" <url>
func ToCurl(r *bufio.Reader) (string, error) {
	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)

	if err != nil {
		log.Error(err)
	}

	var command string

	for _, entry := range har.Log.Entries {
		cmd, err := fromEntry(entry)

		if err != nil {
			log.Error(err)
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
		command += " -b \"" + strings.Join(cookies[:], "&") + "\" "
	}

	for _, h := range entry.Request.Headers {
		command += " -H \"" + h.Name + ": " + h.Value + "\" "
	}

	if entry.Request.Method == "POST" && len(entry.Request.PostData.Text) > 0 {
		command += "-d \"" + entry.Request.PostData.Text + "\""
	}

	command += " \"" + entry.Request.URL + "\""

	return command, nil
}
