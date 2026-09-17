package hargo

import (
	"strings"
	"testing"
)

func TestFromEntryBasics(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Entry)
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "GET with no extras",
			mutate: func(e *Entry) {
				e.Request.Method = "GET"
				e.Request.URL = "http://example.com/a"
			},
			wantContain: []string{"curl -X GET", "http://example.com/a"},
			wantAbsent:  []string{"-d ", "-b ", "-0"},
		},
		{
			name: "HTTP/1.0 adds -0",
			mutate: func(e *Entry) {
				e.Request.Method = "GET"
				e.Request.HTTPVersion = "HTTP/1.0"
				e.Request.URL = "http://example.com/a"
			},
			wantContain: []string{"curl -X GET -0"},
		},
		{
			name: "HTTP/1.1 does not add -0",
			mutate: func(e *Entry) {
				e.Request.Method = "GET"
				e.Request.HTTPVersion = "HTTP/1.1"
				e.Request.URL = "http://example.com/a"
			},
			wantAbsent: []string{"-0"},
		},
		{
			name: "headers become -H",
			mutate: func(e *Entry) {
				e.Request.Method = "GET"
				e.Request.URL = "http://example.com/a"
				e.Request.Headers = []NVP{
					{Name: "Accept", Value: "application/json"},
					{Name: "X-Token", Value: "abc"},
				}
			},
			wantContain: []string{"-H 'Accept: application/json'", "-H 'X-Token: abc'"},
		},
		{
			name: "cookies become -b",
			mutate: func(e *Entry) {
				e.Request.Method = "GET"
				e.Request.URL = "http://example.com/a"
				e.Request.Cookies = []Cookie{
					{Name: "session", Value: "abc123"},
					{Name: "theme", Value: "dark"},
				}
			},
			wantContain: []string{"-b 'session=abc123&theme=dark'"},
		},
		{
			name: "POST with text body",
			mutate: func(e *Entry) {
				e.Request.Method = "POST"
				e.Request.URL = "http://example.com/a"
				e.Request.PostData.Text = `{"k":"v"}`
			},
			wantContain: []string{"curl -X POST", `-d '{"k":"v"}'`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e Entry
			if tt.mutate != nil {
				tt.mutate(&e)
			}
			got, err := fromEntry(e)
			if err != nil {
				t.Fatalf("fromEntry() error = %v", err)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("fromEntry() = %q\n  missing %q", got, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("fromEntry() = %q\n  should not contain %q", got, absent)
				}
			}
		})
	}
}

// Regression: the body was only emitted when the method was exactly "POST",
// so PUT/PATCH/DELETE bodies were silently dropped.
func TestFromEntryEmitsBodyForNonPostMethods(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			var e Entry
			e.Request.Method = method
			e.Request.URL = "http://example.com/a"
			e.Request.PostData.Text = `{"k":"v"}`

			got, err := fromEntry(e)
			if err != nil {
				t.Fatalf("fromEntry() error = %v", err)
			}
			if !strings.Contains(got, `-d '{"k":"v"}'`) {
				t.Errorf("%s body dropped: %q", method, got)
			}
		})
	}
}

// Regression: only PostData.Text was read, so URL-encoded form params
// (PostData.Params) never made it into the curl command.
func TestFromEntryEmitsFormParams(t *testing.T) {
	var e Entry
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/login"
	e.Request.PostData.MimeType = "application/x-www-form-urlencoded"
	e.Request.PostData.Params = []PostParam{
		{Name: "user", Value: "bob"},
		{Name: "pass", Value: "s3cret"},
	}

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}
	if !strings.Contains(got, "-d 'pass=s3cret&user=bob'") {
		t.Errorf("form params dropped: %q", got)
	}
}

func TestFromEntryShellEscaping(t *testing.T) {
	var e Entry
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/a?q=1&r=2"
	e.Request.Headers = []NVP{{Name: "X-Evil", Value: "a'; rm -rf /; echo '"}}
	e.Request.PostData.Text = "$(whoami)`id`"

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}

	// Command substitution must be quoted, not left bare for the shell.
	if strings.Contains(got, "-d $(whoami)") {
		t.Errorf("post data not escaped: %q", got)
	}
	// The URL contains & and must be quoted so the shell does not background it.
	if !strings.Contains(got, "'http://example.com/a?q=1&r=2'") {
		t.Errorf("URL not quoted: %q", got)
	}
}

func TestToCurlMultipleEntries(t *testing.T) {
	har := harWith(
		entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/one") + "," +
			entryJSON("2024-01-01T00:00:02.000Z", "GET", "http://example.com/two"))

	got, err := ToCurl(NewReader(strings.NewReader(har)))
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}

	for _, want := range []string{"http://example.com/one", "http://example.com/two"} {
		if !strings.Contains(got, want) {
			t.Errorf("ToCurl() missing %q\ngot:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "curl -X"); n != 2 {
		t.Errorf("got %d curl commands, want 2\n%s", n, got)
	}
}

// Regression: the JSON decode error was logged and then discarded, so callers
// received ("", nil) and could not tell that nothing had been converted.
func TestToCurlMalformedJSONReturnsError(t *testing.T) {
	got, err := ToCurl(NewReader(strings.NewReader(`{"log":{ THIS IS NOT JSON`)))
	if err == nil {
		t.Fatalf("ToCurl() error = nil, want non-nil (got %q)", got)
	}
	if got != "" {
		t.Errorf("ToCurl() = %q, want empty string alongside error", got)
	}
}

func TestToCurlEmptyEntries(t *testing.T) {
	got, err := ToCurl(NewReader(strings.NewReader(harWith(""))))
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}
	if got != "" {
		t.Errorf("ToCurl() = %q, want empty string", got)
	}
}

func TestToCurlRealHarFixture(t *testing.T) {
	f := openFixture(t, "test/golang.org.har")
	got, err := ToCurl(NewReader(f))
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}
	if !strings.Contains(got, "curl -X GET") {
		t.Errorf("expected at least one GET command, got %d bytes", len(got))
	}
}

// Regression: HTTP/2 pseudo-headers were emitted verbatim as -H ':method: GET',
// which curl rejects because a colon is illegal in a field name. fetch.go and
// EntryToRequest already dropped them; fromEntry did not.
func TestFromEntryDropsPseudoHeaders(t *testing.T) {
	var e Entry
	e.Request.Method = "GET"
	e.Request.URL = "https://example.com/"
	e.Request.Headers = []NVP{
		{Name: ":method", Value: "GET"},
		{Name: ":authority", Value: "example.com"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/"},
		{Name: "accept", Value: "text/html"},
	}

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}

	for _, ph := range []string{":method", ":authority", ":scheme", ":path"} {
		if strings.Contains(got, ph) {
			t.Errorf("pseudo-header %s leaked into curl command: %q", ph, got)
		}
	}
	if !strings.Contains(got, "-H 'accept: text/html'") {
		t.Errorf("real header was dropped: %q", got)
	}
}

func TestFromEntryDropsInvalidHeaderNames(t *testing.T) {
	var e Entry
	e.Request.Method = "GET"
	e.Request.URL = "https://example.com/"
	e.Request.Headers = []NVP{
		{Name: "Bad\tName", Value: "x"},
		{Name: "Good-Name", Value: "y"},
	}

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}
	if strings.Contains(got, "Bad") {
		t.Errorf("invalid header name leaked: %q", got)
	}
	if !strings.Contains(got, "-H 'Good-Name: y'") {
		t.Errorf("valid header dropped: %q", got)
	}
}

// When -b already carries the cookies, the recorded Cookie header would
// duplicate them.
func TestFromEntrySkipsDuplicateCookieHeader(t *testing.T) {
	var e Entry
	e.Request.Method = "GET"
	e.Request.URL = "https://example.com/"
	e.Request.Cookies = []Cookie{{Name: "session", Value: "abc123"}}
	e.Request.Headers = []NVP{{Name: "Cookie", Value: "session=abc123"}}

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}
	if !strings.Contains(got, "-b session=abc123") {
		t.Errorf("expected -b cookie flag: %q", got)
	}
	if strings.Contains(got, "-H 'Cookie:") {
		t.Errorf("Cookie header duplicated alongside -b: %q", got)
	}
}

// With no cookies array, the recorded Cookie header is the only source, so it
// must be preserved.
func TestFromEntryKeepsCookieHeaderWhenNoCookiesArray(t *testing.T) {
	var e Entry
	e.Request.Method = "GET"
	e.Request.URL = "https://example.com/"
	e.Request.Headers = []NVP{{Name: "Cookie", Value: "session=abc123"}}

	got, err := fromEntry(e)
	if err != nil {
		t.Fatalf("fromEntry() error = %v", err)
	}
	if !strings.Contains(got, "-H 'Cookie: session=abc123'") {
		t.Errorf("Cookie header dropped with no cookies array: %q", got)
	}
}

// The real fixture is full of HTTP/2 pseudo-headers.
func TestToCurlFixtureHasNoPseudoHeaders(t *testing.T) {
	f := openFixture(t, "test/golang.org.har")
	got, err := ToCurl(NewReader(f))
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}
	if n := strings.Count(got, "-H ':"); n != 0 {
		t.Errorf("fixture produced %d pseudo-headers, want 0", n)
	}
}
