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
// remaining entries still download. The failure is still reported.
func TestFetchToSkipsUnbuildableEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "BAD METHOD", srv.URL+"/skipped") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/kept.txt"))

	err := FetchTo(NewReader(strings.NewReader(har)), outdir)
	if err == nil {
		t.Fatal("FetchTo() error = nil, want non-nil: one entry could not be built")
	}
	if !strings.Contains(err.Error(), "1 of 2 entries failed") {
		t.Errorf("FetchTo() error = %q, want it to report 1 of 2 failures", err)
	}
	if _, err := os.Stat(filepath.Join(outdir, "kept.txt")); err != nil {
		t.Errorf("expected kept.txt to be downloaded: %v", err)
	}
}

// Regression: FetchTo returned on the first download error, abandoning every
// later entry, while Run continued and aggregated. One unreachable asset must
// not stop the rest.
func TestFetchToContinuesAfterDownloadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body of " + r.URL.Path))
	}))
	defer srv.Close()

	outdir := filepath.Join(t.TempDir(), "out")
	// The middle entry points at a port that is never listening.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/first.txt") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "http://127.0.0.1:0/dead.txt") + "," +
			entryJSON("2024-01-01T00:00:00.003Z", "GET", srv.URL+"/third.txt"))

	err := FetchTo(NewReader(strings.NewReader(har)), outdir)
	if err == nil {
		t.Fatal("FetchTo() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "1 of 3 entries failed") {
		t.Errorf("FetchTo() error = %q, want it to report 1 of 3 failures", err)
	}

	// Crucially, the entry *after* the failure must still have downloaded.
	for name, want := range map[string]string{
		"first.txt": "body of /first.txt",
		"third.txt": "body of /third.txt",
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
	if _, err := os.Stat(filepath.Join(outdir, "dead.txt")); err == nil {
		t.Error("dead.txt should not exist for a failed download")
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

func TestNameAllocator(t *testing.T) {
	dir := t.TempDir()
	alloc := newNameAllocator(dir)

	tests := []struct {
		name    string
		urlPath string
		want    string
	}{
		{name: "normal file", urlPath: "/app.js", want: "app.js"},
		{name: "root path", urlPath: "/", want: "index.html"},
		// "/" already took index.html, and the suffix keeps the extension.
		{name: "empty path", urlPath: "", want: "index-1.html"},
		{name: "nested", urlPath: "/a/b/c/style.css", want: "style.css"},
		{name: "traversal is stripped", urlPath: "/../../etc/passwd", want: "passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, got, err := alloc.create(tt.urlPath)
			if err != nil {
				t.Fatalf("create() error = %v", err)
			}
			defer func() { _ = f.Close() }()

			if filepath.Base(got) != tt.want {
				t.Errorf("create(%q) = %q, want basename %q", tt.urlPath, got, tt.want)
			}
			if filepath.Dir(got) != dir {
				t.Errorf("create(%q) escaped outdir: %q", tt.urlPath, got)
			}
			if _, err := os.Stat(got); err != nil {
				t.Errorf("create(%q) did not create the file: %v", tt.urlPath, err)
			}
		})
	}
}

func TestNameAllocatorSuffixesCollisions(t *testing.T) {
	dir := t.TempDir()
	alloc := newNameAllocator(dir)

	var got []string
	for i := 0; i < 5; i++ {
		f, name, err := alloc.create("/img/logo.png")
		if err != nil {
			t.Fatalf("create() %d error = %v", i, err)
		}
		_ = f.Close()
		got = append(got, filepath.Base(name))
	}

	want := []string{"logo.png", "logo-1.png", "logo-2.png", "logo-3.png", "logo-4.png"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("name %d = %q, want %q", i, got[i], w)
		}
	}
}

// The allocator must also step over files it did not create itself, such as
// leftovers from an earlier run into the same directory.
func TestNameAllocatorSkipsPreexistingFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"logo.png", "logo-1.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}

	alloc := newNameAllocator(dir)
	f, name, err := alloc.create("/logo.png")
	if err != nil {
		t.Fatalf("create() error = %v", err)
	}
	_ = f.Close()

	if got := filepath.Base(name); got != "logo-2.png" {
		t.Errorf("got %q, want logo-2.png", got)
	}
	// The pre-existing files must be untouched.
	for _, existing := range []string{"logo.png", "logo-1.png"} {
		b, err := os.ReadFile(filepath.Join(dir, existing))
		if err != nil {
			t.Errorf("reading %s: %v", existing, err)
			continue
		}
		if string(b) != "old" {
			t.Errorf("%s was overwritten: %q", existing, b)
		}
	}
}

// Regression: the previous implementation rescanned from suffix 1 on every call,
// which is O(N^2) in stat calls, and gave up entirely after 10000 collisions.
func TestNameAllocatorHandlesManyCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: creates many files")
	}

	dir := t.TempDir()
	alloc := newNameAllocator(dir)

	const n = 12000
	for i := 0; i < n; i++ {
		f, _, err := alloc.create("/same.txt")
		if err != nil {
			t.Fatalf("create() failed at %d: %v", i, err)
		}
		_ = f.Close()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != n {
		t.Errorf("got %d files, want %d", len(entries), n)
	}
}
