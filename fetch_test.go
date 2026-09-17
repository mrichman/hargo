package hargo

import (
	"compress/flate"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchToDownloadsEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body of " + r.URL.Path))
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/app.js") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/style.css"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}

	for name, want := range map[string]string{
		"app.js":    "body of /app.js",
		"style.css": "body of /style.css",
	} {
		got, err := os.ReadFile(filepath.Join(outdir, name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestFetchToCreatesOutputDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	// Nested path that does not exist yet.
	outdir := filepath.Join(t.TempDir(), "a", "b", "c")
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/f.txt"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outdir, "f.txt")); err != nil {
		t.Errorf("expected f.txt in %s: %v", outdir, err)
	}
}

func TestFetchToRootPathBecomesIndexHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	tests := []struct {
		name string
		url  string
	}{
		{name: "trailing slash", url: srv.URL + "/"},
		{name: "no path at all", url: srv.URL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outdir := filepath.Join(t.TempDir(), "out")
			har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", tt.url))

			if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
				t.Fatalf("FetchTo() error = %v", err)
			}
			got, err := os.ReadFile(filepath.Join(outdir, "index.html"))
			if err != nil {
				t.Fatalf("expected index.html: %v", err)
			}
			if string(got) != "<html></html>" {
				t.Errorf("index.html = %q", got)
			}
		})
	}
}

func TestFetchToMalformedJSONReturnsError(t *testing.T) {
	outdir := filepath.Join(t.TempDir(), "out")
	if err := FetchTo(NewReader(strings.NewReader(`{"log":{ BROKEN`)), outdir); err == nil {
		t.Error("FetchTo() error = nil, want non-nil for malformed JSON")
	}
}

func TestFetchToPropagatesRequestFailure(t *testing.T) {
	outdir := filepath.Join(t.TempDir(), "out")
	// Port 0 is never listening.
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://127.0.0.1:0/dead"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err == nil {
		t.Error("FetchTo() error = nil, want non-nil when the request cannot be made")
	}
}

// An entry whose URL cannot be turned into a request is skipped, and the
// remaining entries still download.
func TestFetchToSkipsUnbuildableEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "BAD METHOD", srv.URL+"/skipped") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/kept.txt"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outdir, "kept.txt")); err != nil {
		t.Errorf("expected kept.txt to be downloaded: %v", err)
	}
}

// Entries sharing a basename must not overwrite each other; the second gets a
// numeric suffix.
func TestFetchToUniqueNamesOnBasenameCollision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("from " + r.URL.Path))
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/a/logo.png") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/b/logo.png") + "," +
			entryJSON("2024-01-01T00:00:00.003Z", "GET", srv.URL+"/c/logo.png"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}

	entries, err := os.ReadDir(outdir)
	if err != nil {
		t.Fatalf("reading outdir: %v", err)
	}
	if len(entries) != 3 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("got %d files %v, want 3 distinct files", len(entries), names)
	}

	// The suffix preserves the extension so the file stays openable.
	for name, want := range map[string]string{
		"logo.png":   "from /a/logo.png",
		"logo-1.png": "from /b/logo.png",
		"logo-2.png": "from /c/logo.png",
	} {
		got, err := os.ReadFile(filepath.Join(outdir, name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// Regression: Fetch replayed the recorded Accept-Encoding header, which stops
// net/http from decompressing, so gzipped bytes were written under a .html name.
func TestFetchToDecompressesGzip(t *testing.T) {
	const plain = "<html>hello world hello world hello world</html>"

	var sawAcceptEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(plain))
		_ = gz.Close()
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"GET","url":"` + srv.URL + `/page.html","httpVersion":"HTTP/1.1",
		"headers":[{"name":"accept-encoding","value":"gzip, deflate, br"}]}}]}}`

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outdir, "page.html"))
	if err != nil {
		t.Fatalf("reading page.html: %v", err)
	}

	if len(got) >= 2 && got[0] == 0x1f && got[1] == 0x8b {
		t.Fatalf("page.html is still raw gzip (%d bytes), want decompressed HTML", len(got))
	}
	if string(got) != plain {
		t.Errorf("page.html = %q, want %q", got, plain)
	}
	// The recorded header must not be forwarded, otherwise net/http will not
	// negotiate compression on our behalf.
	if strings.Contains(sawAcceptEncoding, "br") {
		t.Errorf("server saw replayed Accept-Encoding %q, want net/http's own", sawAcceptEncoding)
	}
}

func TestFetchToDecompressesDeflate(t *testing.T) {
	const plain = "deflate payload deflate payload deflate payload"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		fw, _ := flate.NewWriter(w, flate.DefaultCompression)
		_, _ = fw.Write([]byte(plain))
		_ = fw.Close()
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/data.txt"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err != nil {
		t.Fatalf("FetchTo() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(outdir, "data.txt"))
	if err != nil {
		t.Fatalf("reading data.txt: %v", err)
	}
	if string(got) != plain {
		t.Errorf("data.txt = %q, want %q", got, plain)
	}
}

// A failed request must not leave a zero-byte file behind.
func TestFetchToLeavesNoEmptyFileOnFailure(t *testing.T) {
	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://127.0.0.1:0/dead.js"))

	if err := FetchTo(NewReader(strings.NewReader(har)), outdir); err == nil {
		t.Fatal("FetchTo() error = nil, want non-nil")
	}

	entries, err := os.ReadDir(outdir)
	if err != nil {
		t.Fatalf("reading outdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d files, want 0 after a failed download", len(entries))
	}
}

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		urlPath string
		want    string
	}{
		{name: "normal file", urlPath: "/app.js", want: "app.js"},
		{name: "root path", urlPath: "/", want: "index.html"},
		{name: "empty path", urlPath: "", want: "index.html"},
		{name: "nested", urlPath: "/a/b/c/style.css", want: "style.css"},
		{name: "traversal is stripped", urlPath: "/../../etc/passwd", want: "passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := uniqueName(dir, tt.urlPath)
			if err != nil {
				t.Fatalf("uniqueName() error = %v", err)
			}
			if filepath.Base(got) != tt.want {
				t.Errorf("uniqueName(%q) = %q, want basename %q", tt.urlPath, got, tt.want)
			}
			if filepath.Dir(got) != dir {
				t.Errorf("uniqueName(%q) escaped outdir: %q", tt.urlPath, got)
			}
		})
	}
}
