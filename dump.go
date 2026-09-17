package hargo

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Dump writes a human-readable summary of every entry in a HAR document to
// stdout.
func Dump(r io.Reader) error {
	return DumpTo(os.Stdout, r)
}

// errWriter records the first write error so that a long series of writes does
// not need an error check on every line.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) println(args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintln(ew.w, args...)
}

// DumpTo writes a human-readable summary of every entry in a HAR document to w.
func DumpTo(w io.Writer, r io.Reader) error {
	dec := json.NewDecoder(NewReader(r))
	var har HAR
	if err := dec.Decode(&har); err != nil {
		return err
	}

	ew := &errWriter{w: w}

	ew.println("HAR Version: " + har.Log.Version)
	ew.println("Creator: ", har.Log.Creator.Name+" "+har.Log.Creator.Version)

	for _, entry := range har.Log.Entries {
		ew.println("----------------------------------------------------------------------")
		ew.println("Timestamp: ", entry.StartedDateTime)
		ew.println("Request URL: ", entry.Request.URL)
		ew.println("Request Method: ", entry.Request.Method)
		ew.println("HTTP Version: ", entry.Request.HTTPVersion)
		ew.println("Status Code: ", entry.Response.Status)
		ew.println("Server IP Address: ", entry.ServerIPAddress)

		ew.println("Request Headers:")

		for _, req := range entry.Request.Headers {
			ew.println("\t" + req.Name + ": " + req.Value)
		}

		ew.println("Querystring Parameters:")

		for _, qs := range entry.Request.QueryString {
			ew.println("\t" + qs.Name + ": " + qs.Value)
		}

		ew.println("Cookies:")

		for _, cookie := range entry.Request.Cookies {
			ew.println("\t🍪 " + cookie.Name + "=" + cookie.Value)
		}

		ew.println("POST Data:")

		ew.println("\tMIME Type: " + entry.Request.PostData.MimeType)

		for _, params := range entry.Request.PostData.Params {
			ew.println("\t" + params.Name + ": " + params.Value)
		}

		ew.println("Response Headers:")

		for _, res := range entry.Response.Headers {
			ew.println("\t" + res.Name + ": " + res.Value)
		}
	}

	return ew.err
}
