package hargo

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: any response body was written under the resource's own name, so a
// directory of 404 pages was reported as a clean run.
func TestFetchTreatsErrorStatusAsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404 Not Found</html>"))
	}))
	defer srv.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/app.js"))

	err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out})
	if err == nil {
		t.Fatal("Fetch() error = nil, want non-nil for a 404 response")
	}

	if _, statErr := os.Stat(filepath.Join(out, "app.js")); statErr == nil {
		t.Error("the error page was written to app.js; want no file at all")
	}
}

// A caller who does want the error bodies can ask for them.
func TestFetchAcceptErrorStatusSavesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/app.js"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{
		OutDir:            out,
		AcceptErrorStatus: true,
	}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(out, "app.js"))
	if err != nil {
		t.Fatalf("reading app.js: %v", err)
	}
	if string(got) != "nope" {
		t.Errorf("app.js = %q, want the error body saved on request", got)
	}
}

// A 3xx that the client follows must still succeed.
func TestFetchFollowsRedirect(t *testing.T) {
	var target *httptest.Server
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final.txt" {
			_, _ = w.Write([]byte("payload"))
			return
		}
		http.Redirect(w, r, target.URL+"/final.txt", http.StatusFound)
	}))
	defer target.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", target.URL+"/start.txt"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

// Regression: URL.Opaque was set to the decoded path in CheckRedirect, which
// emits a malformed request line for a target containing an escape such as %20.
func TestFetchHandlesEscapedRedirectTarget(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "a b") {
			_, _ = w.Write([]byte("payload"))
			return
		}
		http.Redirect(w, r, srv.URL+"/a%20b.txt", http.StatusFound)
	}))
	defer srv.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/start.txt"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out}); err != nil {
		t.Fatalf("Fetch() error = %v, want a redirect to an escaped path to work", err)
	}
}

// Regression: Fetch built its request with a nil body, so a recorded POST
// replayed empty and usually saved an error page.
func TestFetchSendsRecordedBody(t *testing.T) {
	type received struct {
		body        string
		contentType string
	}
	got := make(chan received, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		got <- received{body: buf.String(), contentType: r.Header.Get("Content-Type")}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"POST","url":"` + srv.URL + `/submit","httpVersion":"HTTP/1.1",
		"postData":{"mimeType":"application/json","text":"{\"k\":\"v\"}"}}}]}}`

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: t.TempDir()}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	r := <-got
	if want := `{"k":"v"}`; r.body != want {
		t.Errorf("server received body %q, want %q", r.body, want)
	}
	if want := "application/json"; r.contentType != want {
		t.Errorf("server received Content-Type %q, want %q", r.contentType, want)
	}
}

// Regression: HTTP "deflate" is the zlib format per RFC 9110, but the reader only
// handled raw deflate, so a conformant server's body was written corrupted.
func TestFetchDecodesZlibDeflate(t *testing.T) {
	const payload = "console.log('hello world, this is the real payload');"

	var encoded bytes.Buffer
	zw := zlib.NewWriter(&encoded)
	if _, err := zw.Write([]byte(payload)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write(encoded.Bytes())
	}))
	defer srv.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/app.js"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(out, "app.js"))
	if err != nil {
		t.Fatalf("reading app.js: %v", err)
	}
	if string(got) != payload {
		t.Errorf("app.js = %q, want the zlib body decoded to %q", got, payload)
	}
}

// Raw deflate is non-conformant but common, so it must keep working.
func TestFetchDecodesRawDeflate(t *testing.T) {
	const payload = "raw deflate payload that is long enough to compress"

	var encoded bytes.Buffer
	fw, err := flate.NewWriter(&encoded, flate.DefaultCompression)
	if err != nil {
		t.Fatalf("flate writer: %v", err)
	}
	if _, err := fw.Write([]byte(payload)); err != nil {
		t.Fatalf("flate write: %v", err)
	}
	if err := fw.Close(); err != nil {
		t.Fatalf("flate close: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write(encoded.Bytes())
	}))
	defer srv.Close()

	out := t.TempDir()
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/raw.txt"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(out, "raw.txt"))
	if err != nil {
		t.Fatalf("reading raw.txt: %v", err)
	}
	if string(got) != payload {
		t.Errorf("raw.txt = %q, want %q", got, payload)
	}
}

// A HAR is untrusted input, so a URL path must never name anything but a single
// file inside the output directory.
func TestSafeBaseName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "/app.js", want: "app.js"},
		{name: "nested", in: "/a/b/c/app.js", want: "app.js"},
		{name: "root", in: "/", want: "index.html"},
		{name: "empty", in: "", want: "index.html"},
		{name: "dot", in: "/.", want: "index.html"},
		{name: "dotdot", in: "/..", want: "index.html"},
		{name: "bare dotdot", in: "..", want: "index.html"},
		{name: "unix traversal", in: "/../../etc/passwd", want: "passwd"},
		{name: "trailing slash", in: "/a/b/", want: "index.html"},
		{name: "windows traversal", in: `/x\..\..\evil.txt`, want: "evil.txt"},
		{name: "backslash only", in: `x\y\z.txt`, want: "z.txt"},
		{name: "windows illegal chars", in: "/a:b*c?d.txt", want: "a_b_c_d.txt"},
		{name: "dotfile", in: "/.env", want: ".env"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeBaseName(tt.in)
			if got != tt.want {
				t.Errorf("safeBaseName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// Whatever the input, the result must stay inside the directory.
			if strings.ContainsAny(got, `/\`) {
				t.Errorf("safeBaseName(%q) = %q, which contains a path separator", tt.in, got)
			}
			if got == "." || got == ".." {
				t.Errorf("safeBaseName(%q) = %q, which names a directory", tt.in, got)
			}
		})
	}
}

// An over-long path segment must be clamped rather than rejected by the
// filesystem with ENAMETOOLONG, which would lose the resource.
func TestSafeBaseNameClampsLength(t *testing.T) {
	long := "/" + strings.Repeat("a", 500) + ".js"

	got := safeBaseName(long)
	if len(got) > maxBaseNameLen {
		t.Errorf("safeBaseName returned %d bytes, want at most %d", len(got), maxBaseNameLen)
	}
	if !strings.HasSuffix(got, ".js") {
		t.Errorf("safeBaseName(%q...) = %q, want the extension preserved", long[:20], got)
	}
}

// A very long URL path must actually download rather than fail on the file name.
func TestFetchHandlesVeryLongPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	out := t.TempDir()
	long := strings.Repeat("segment", 60)
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/"+long+".txt"))

	if err := Fetch(t.Context(), strings.NewReader(har), FetchOptions{OutDir: out}); err != nil {
		t.Fatalf("Fetch() error = %v, want a long path clamped to a usable name", err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("reading outdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files, want 1", len(entries))
	}
}

// A dotfile collision must not produce "-1.env".
func TestNameAllocatorHandlesDotfileCollision(t *testing.T) {
	dir := t.TempDir()
	alloc := newNameAllocator(dir)

	for range 2 {
		f, name, err := alloc.create("/.env")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		_ = f.Close()
		base := filepath.Base(name)
		if strings.HasPrefix(base, "-") {
			t.Errorf("allocated %q, want a name that does not start with the collision suffix", base)
		}
	}
}
