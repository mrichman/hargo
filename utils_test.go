package hargo

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"testing"
)

// harWith wraps entries JSON in a minimal HAR envelope.
func harWith(entries string) string {
	return `{"log":{"version":"1.2","creator":{"name":"test","version":"1"},"entries":[` + entries + `]}}`
}

func entryJSON(started, method, rawURL string) string {
	return `{"startedDateTime":"` + started + `","request":{"method":"` + method +
		`","url":"` + rawURL + `","httpVersion":"HTTP/1.1"}}`
}

func TestDecodeSortsEntriesByStartedDateTime(t *testing.T) {
	har, err := Decode(strings.NewReader(harWith(
		entryJSON("2024-01-01T00:00:03.000Z", "GET", "http://example.com/third") + "," +
			entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/first") + "," +
			entryJSON("2024-01-01T00:00:02.000Z", "GET", "http://example.com/second"))))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	want := []string{
		"http://example.com/first",
		"http://example.com/second",
		"http://example.com/third",
	}
	if len(har.Log.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(har.Log.Entries), len(want))
	}
	for i, w := range want {
		if got := har.Log.Entries[i].Request.URL; got != w {
			t.Errorf("entry %d URL = %q, want %q", i, got, w)
		}
	}
}

// Regression: the previous swap-delete implementation indexed past the end of
// the truncated slice and panicked once more than one entry was dropped.
func TestDecodeRemovesWebSocketEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries string
		want    []string
	}{
		{
			name:    "no websocket entries",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/a"),
			want:    []string{"http://example.com/a"},
		},
		{
			name: "single ws entry",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "ws://example.com/sock") + "," +
				entryJSON("2024-01-01T00:00:02.000Z", "GET", "http://example.com/a"),
			want: []string{"http://example.com/a"},
		},
		{
			name: "two trailing ws entries",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/a") + "," +
				entryJSON("2024-01-01T00:00:02.000Z", "GET", "ws://example.com/s1") + "," +
				entryJSON("2024-01-01T00:00:03.000Z", "GET", "ws://example.com/s2"),
			want: []string{"http://example.com/a"},
		},
		{
			name: "all ws entries",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "ws://example.com/s1") + "," +
				entryJSON("2024-01-01T00:00:02.000Z", "GET", "ws://example.com/s2") + "," +
				entryJSON("2024-01-01T00:00:03.000Z", "GET", "ws://example.com/s3"),
			want: nil,
		},
		{
			name: "wss entries are dropped too",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "wss://example.com/s1") + "," +
				entryJSON("2024-01-01T00:00:02.000Z", "GET", "http://example.com/a"),
			want: []string{"http://example.com/a"},
		},
		{
			name: "interleaved",
			entries: entryJSON("2024-01-01T00:00:01.000Z", "GET", "ws://example.com/s1") + "," +
				entryJSON("2024-01-01T00:00:02.000Z", "GET", "http://example.com/a") + "," +
				entryJSON("2024-01-01T00:00:03.000Z", "GET", "wss://example.com/s2") + "," +
				entryJSON("2024-01-01T00:00:04.000Z", "GET", "http://example.com/b"),
			want: []string{"http://example.com/a", "http://example.com/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			har, err := Decode(strings.NewReader(harWith(tt.entries)))
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if len(har.Log.Entries) != len(tt.want) {
				t.Fatalf("got %d entries %v, want %d", len(har.Log.Entries), urls(har.Log.Entries), len(tt.want))
			}
			for i, w := range tt.want {
				if got := har.Log.Entries[i].Request.URL; got != w {
					t.Errorf("entry %d URL = %q, want %q", i, got, w)
				}
			}
		})
	}
}

func urls(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Request.URL)
	}
	return out
}

func TestDecodeMalformedJSONReturnsError(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"log":{ NOT JSON`)); err == nil {
		t.Fatal("Decode() error = nil, want non-nil for malformed JSON")
	}
}

func TestDecodeEmptyEntries(t *testing.T) {
	har, err := Decode(strings.NewReader(harWith("")))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(har.Log.Entries) != 0 {
		t.Errorf("got %d entries, want 0", len(har.Log.Entries))
	}
	if har.Log.Version != "1.2" {
		t.Errorf("Version = %q, want 1.2", har.Log.Version)
	}
}

// Regression: http.NewRequest's error was discarded, so an invalid method or
// URL produced (nil, nil) and every caller nil-dereferenced.
func TestEntryToRequestInvalidInputReturnsError(t *testing.T) {
	tests := []struct {
		name   string
		method string
		url    string
	}{
		{name: "method with a space", method: "BAD METHOD", url: "http://example.com/"},
		{name: "method with a control char", method: "GE\tT", url: "http://example.com/"},
		{name: "unparseable url", method: "GET", url: "://not a url"},
		{name: "url with control char", method: "GET", url: "http://exa\x7fmple.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Entry{}
			e.Request.Method = tt.method
			e.Request.URL = tt.url

			req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
			if err == nil {
				t.Fatalf("EntryToRequest() error = nil, want non-nil (req=%v)", req)
			}
			if req != nil {
				t.Errorf("EntryToRequest() req = %v, want nil alongside error", req)
			}
		})
	}
}

func TestEntryToRequestBuildsRequest(t *testing.T) {
	e := &Entry{}
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/submit?a=1"
	e.Request.Headers = []NVP{
		{Name: "X-Custom", Value: "yes"},
		{Name: "Cookie", Value: "should=beskipped"},
		{Name: "Bad\tName", Value: "dropped"},
	}
	e.Request.PostData.Text = `{"k":"v"}`

	req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", req.Method)
	}
	if req.URL.String() != "http://example.com/submit?a=1" {
		t.Errorf("URL = %q", req.URL.String())
	}
	if got := req.Header.Get("X-Custom"); got != "yes" {
		t.Errorf("X-Custom = %q, want yes", got)
	}
	// The Cookie header is deliberately skipped in favour of entry.Request.Cookies.
	if got := req.Header.Get("Cookie"); got != "" {
		t.Errorf("Cookie header = %q, want empty", got)
	}
	if got := req.Header.Get("Bad\tName"); got != "" {
		t.Errorf("invalid header name was added: %q", got)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != `{"k":"v"}` {
		t.Errorf("body = %q, want %q", body, `{"k":"v"}`)
	}
}

func TestEntryToRequestFormEncodesParams(t *testing.T) {
	e := &Entry{}
	e.Request.Method = "POST"
	e.Request.URL = "http://example.com/login"
	e.Request.PostData.MimeType = "application/x-www-form-urlencoded"
	e.Request.PostData.Params = []PostParam{
		{Name: "user", Value: "bob"},
		{Name: "pass", Value: "s3 cret&"},
	}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	// url.Values.Encode sorts keys, so this is deterministic.
	if want := "pass=s3+cret%26&user=bob"; string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// The recorded text is what the browser actually sent, so it wins over params.
// Reconstructing params instead used to corrupt multipart bodies, which are
// recorded as params but must not be URL-encoded.
func TestEntryToRequestPrefersRecordedText(t *testing.T) {
	e := &Entry{}
	e.Request.Method = http.MethodPost
	e.Request.URL = "http://example.com/login"
	e.Request.PostData.Text = "user=bob&from=text"
	e.Request.PostData.Params = []PostParam{{Name: "user", Value: "reconstructed"}}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	if want := "user=bob&from=text"; string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// With no recorded text, form params are reconstructed.
func TestEntryToRequestReconstructsFormParams(t *testing.T) {
	e := &Entry{}
	e.Request.Method = http.MethodPost
	e.Request.URL = "http://example.com/login"
	e.Request.PostData.MimeType = "application/x-www-form-urlencoded"
	e.Request.PostData.Params = []PostParam{{Name: "user", Value: "bob"}}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	if want := "user=bob"; string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
	if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want it derived from postData.mimeType", got)
	}
}

// A multipart body recorded only as params cannot be reconstructed as a form, so
// it must not be URL-encoded into something the server cannot parse.
func TestEntryToRequestDoesNotFormEncodeMultipart(t *testing.T) {
	e := &Entry{}
	e.Request.Method = http.MethodPost
	e.Request.URL = "http://example.com/upload"
	e.Request.PostData.MimeType = "multipart/form-data; boundary=xyz"
	e.Request.PostData.Params = []PostParam{{Name: "file", Value: "contents", FileName: "a.txt"}}

	req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
	if err != nil {
		t.Fatalf("EntryToRequest() error = %v", err)
	}
	body, _ := io.ReadAll(req.Body)
	if strings.Contains(string(body), "file=contents") {
		t.Errorf("body = %q, want multipart params left alone rather than form-encoded", body)
	}
}

func TestEntryToRequestCookieHandling(t *testing.T) {
	newEntry := func() *Entry {
		e := &Entry{}
		e.Request.Method = "GET"
		e.Request.URL = "http://example.com/"
		e.Request.Cookies = []Cookie{
			{Name: "session", Value: "abc123", Domain: "example.com"},
			{Name: "theme", Value: "dark", Domain: "example.com"},
		}
		return e
	}

	t.Run("cookies applied", func(t *testing.T) {
		req, err := EntryToRequest(t.Context(), newEntry(), EntryOptions{IgnoreHARCookies: false})
		if err != nil {
			t.Fatalf("EntryToRequest() error = %v", err)
		}
		cookies := req.Cookies()
		if len(cookies) != 2 {
			t.Fatalf("got %d cookies, want 2", len(cookies))
		}
		if got := req.Header.Get("Cookie"); !strings.Contains(got, "session=abc123") ||
			!strings.Contains(got, "theme=dark") {
			t.Errorf("Cookie header = %q", got)
		}
	})

	t.Run("cookies ignored", func(t *testing.T) {
		req, err := EntryToRequest(t.Context(), newEntry(), EntryOptions{IgnoreHARCookies: true})
		if err != nil {
			t.Fatalf("EntryToRequest() error = %v", err)
		}
		if got := len(req.Cookies()); got != 0 {
			t.Errorf("got %d cookies, want 0 when ignoreHARCookies is true", got)
		}
	})
}

func TestNewReaderSkipsBOM(t *testing.T) {
	const bom = "\xef\xbb\xbf"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "with BOM", in: bom + `{"a":1}`, want: `{"a":1}`},
		{name: "without BOM", in: `{"a":1}`, want: `{"a":1}`},
		{name: "empty input", in: "", want: ""},
		{name: "shorter than BOM", in: "{}", want: "{}"},
		{name: "BOM only", in: bom, want: ""},
		{name: "BOM-like but not BOM", in: "\xef\xbb\xbe{}", want: "\xef\xbb\xbe{}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := io.ReadAll(NewReader(strings.NewReader(tt.in)))
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Decode applies the BOM skip itself, so a caller need not reach for NewReader.
func TestDecodeSkipsBOM(t *testing.T) {
	har, err := Decode(strings.NewReader("\xef\xbb\xbf" + harWith(
		entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/a"))))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(har.Log.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(har.Log.Entries))
	}
}

func TestPostBody(t *testing.T) {
	tests := []struct {
		name string
		pd   PostData
		want string
	}{
		{name: "empty", pd: PostData{}, want: ""},
		{name: "text only", pd: PostData{Text: "raw=body"}, want: "raw=body"},
		{
			name: "form params only",
			pd:   PostData{Params: []PostParam{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}},
			want: "a=1&b=2",
		},
		{
			name: "recorded text wins over params",
			pd:   PostData{Text: "from=text", Params: []PostParam{{Name: "a", Value: "1"}}},
			want: "from=text",
		},
		{
			name: "multipart params are not form-encoded",
			pd: PostData{
				MimeType: "multipart/form-data; boundary=xyz",
				Params:   []PostParam{{Name: "a", Value: "1"}},
			},
			want: "",
		},
		{
			name: "explicit form mime type encodes params",
			pd: PostData{
				MimeType: "application/x-www-form-urlencoded; charset=UTF-8",
				Params:   []PostParam{{Name: "a", Value: "1"}},
			},
			want: "a=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postBody(tt.pd); got != tt.want {
				t.Errorf("postBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsWebSocket(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "ws://example.com/s", want: true},
		{url: "wss://example.com/s", want: true},
		{url: "http://example.com/", want: false},
		{url: "https://example.com/", want: false},
		{url: "", want: false},
		{url: "https://example.com/?next=ws://x", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isWebSocket(tt.url); got != tt.want {
				t.Errorf("isWebSocket(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// NewReader must not stack another buffer on an input that is already buffered:
// the second wrapper would peek from the first and lose the bytes it consumed.
func TestNewReaderDoesNotDoubleWrap(t *testing.T) {
	inner := bufio.NewReader(strings.NewReader(`{"a":1}`))

	if got := NewReader(inner); got != inner {
		t.Error("NewReader wrapped an already-buffered reader, want it returned as is")
	}
}

// A BOM must still be skipped when the input is already a *bufio.Reader.
func TestNewReaderSkipsBOMOnBufferedInput(t *testing.T) {
	inner := bufio.NewReader(strings.NewReader("\xef\xbb\xbf" + `{"a":1}`))

	got, err := io.ReadAll(NewReader(inner))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("got %q, want %q", got, `{"a":1}`)
	}
}
