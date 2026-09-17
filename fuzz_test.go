package hargo

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// The parsers must never panic on arbitrary input; they may only return errors.
// Decode's WebSocket filter previously panicked with an index-out-of-range on
// certain inputs, which is exactly the class of bug fuzzing catches.

func FuzzDecode(f *testing.F) {
	f.Add(`{"log":{"version":"1.2","entries":[]}}`)
	f.Add(`{"log":{"version":"1.2","entries":[{"startedDateTime":"2024-01-01T00:00:00.001Z","request":{"method":"GET","url":"ws://a/"}}]}}`)
	f.Add(`{"log":{"version":"1.2","entries":[{"request":{"url":"ws://a/"}},{"request":{"url":"ws://b/"}}]}}`)
	f.Add(`{"log":{"version":"1.2","entries":[{"request":{"url":"wss://a/"}},{"request":{"url":"http://b/"}}]}}`)
	f.Add(`{"log":{`)
	f.Add(``)
	f.Add("\xef\xbb\xbf{}")

	f.Fuzz(func(t *testing.T, in string) {
		har, err := Decode(strings.NewReader(in))
		if err != nil {
			return
		}
		// On success no WebSocket entry may survive, and entries must be
		// ordered by start time.
		var prev string
		for i, e := range har.Log.Entries {
			if isWebSocket(e.Request.URL) {
				t.Fatalf("entry %d kept a websocket URL %q", i, e.Request.URL)
			}
			if prev != "" && e.StartedDateTime < prev {
				t.Fatalf("entry %d out of order: %q before %q", i, prev, e.StartedDateTime)
			}
			prev = e.StartedDateTime
		}
	})
}

func FuzzValidate(f *testing.F) {
	f.Add(`{"log":{"version":"1.2","entries":[]}}`)
	f.Add(`{"log":{"version":"1.1","entries":[]}}`)
	f.Add(`{"log":{"version":123}}`)
	f.Add(`{"log":{"version":"1.2","entries":"nope"}}`)
	f.Add(`not json`)
	f.Add(``)

	f.Fuzz(func(t *testing.T, in string) {
		// The only contract is that Validate must not panic; any input either
		// validates or returns a descriptive error.
		_ = Validate(strings.NewReader(in))
	})
}

func FuzzToCurl(f *testing.F) {
	f.Add(`{"log":{"version":"1.2","entries":[{"request":{"method":"GET","url":"http://a/"}}]}}`)
	f.Add(`{"log":{"version":"1.2","entries":[{"request":{"method":"POST","url":"http://a/","postData":{"text":"x=1"}}}]}}`)
	f.Add(`{"log":{"version":"1.2","entries":[{"request":{"method":"GET","url":"http://a/","headers":[{"name":":method","value":"GET"}]}}]}}`)
	f.Add(`{"log":{`)

	f.Fuzz(func(t *testing.T, in string) {
		out, err := ToCurl(strings.NewReader(in))
		if err != nil {
			if out != "" {
				t.Fatalf("ToCurl returned output %q alongside error %v", out, err)
			}
			return
		}
		// Pseudo-headers are not valid curl input and must never be emitted.
		if strings.Contains(out, "-H ':") {
			t.Fatalf("emitted an HTTP/2 pseudo-header: %q", out)
		}
	})
}

func FuzzNewReader(f *testing.F) {
	f.Add([]byte("\xef\xbb\xbfhello"))
	f.Add([]byte("hello"))
	f.Add([]byte(""))
	f.Add([]byte("\xef"))
	f.Add([]byte("\xef\xbb"))

	f.Fuzz(func(t *testing.T, in []byte) {
		got, err := io.ReadAll(NewReader(bytes.NewReader(in)))
		if err != nil {
			t.Fatalf("NewReader produced a read error on %q: %v", in, err)
		}

		bom := []byte{0xef, 0xbb, 0xbf}
		want := in
		if bytes.HasPrefix(in, bom) {
			want = in[len(bom):]
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("NewReader(%q) = %q, want %q", in, got, want)
		}
	})
}

func FuzzEntryToRequest(f *testing.F) {
	f.Add("GET", "http://example.com/", "name", "value")
	f.Add("POST", "http://example.com/", "Cookie", "a=b")
	f.Add("BAD METHOD", "http://example.com/", "x", "y")
	f.Add("GET", "://not a url", ":method", "GET")

	f.Fuzz(func(t *testing.T, method, rawURL, hName, hValue string) {
		e := &Entry{}
		e.Request.Method = method
		e.Request.URL = rawURL
		e.Request.Headers = []NVP{{Name: hName, Value: hValue}}

		req, err := EntryToRequest(t.Context(), e, EntryOptions{IgnoreHARCookies: true})
		// The (nil, nil) return was a real bug: callers dereferenced it.
		if err == nil && req == nil {
			t.Fatal("EntryToRequest returned (nil, nil)")
		}
		if err != nil && req != nil {
			t.Fatalf("EntryToRequest returned a request alongside error %v", err)
		}
		if req == nil {
			return
		}
		for name := range req.Header {
			if strings.HasPrefix(name, ":") {
				t.Fatalf("pseudo-header %q was added to the request", name)
			}
		}
	})
}
