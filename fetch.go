package hargo

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Fetch downloads all resources referenced in .har file into a new
// timestamped directory under the current working directory.
func Fetch(r *bufio.Reader) error {
	datestring := time.Now().Format("20060102150405")
	outdir := "." + string(filepath.Separator) + "hargo-fetch-" + datestring
	return FetchTo(r, outdir)
}

// FetchTo downloads all resources referenced in .har file into outdir,
// creating it if necessary. An entry that cannot be built or downloaded is
// logged and skipped so that one bad resource does not abandon the rest;
// FetchTo reports how many failed.
func FetchTo(r *bufio.Reader, outdir string) error {
	har, err := Decode(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return err
	}

	alloc := newNameAllocator(outdir)
	failed := 0

	for _, entry := range har.Log.Entries {

		//TODO create goroutine here to parallelize requests

		fmt.Println("URL: " + entry.Request.URL)

		req, err := http.NewRequest(entry.Request.Method, entry.Request.URL, nil)
		if err != nil {
			log.Errorf("skipping entry %s: %v", entry.Request.URL, err)
			failed++
			continue
		}

		for _, h := range entry.Request.Headers {
			if !isReplayableHeader(h.Name, h.Value) {
				continue
			}
			// Cookie is applied from entry.Request.Cookies below.
			if strings.EqualFold(h.Name, "Cookie") {
				continue
			}
			// Replaying the recorded Accept-Encoding stops net/http from
			// transparently decompressing the response, which would write
			// gzipped bytes to disk under a .html or .js name.
			if strings.EqualFold(h.Name, "Accept-Encoding") {
				continue
			}
			req.Header.Add(h.Name, h.Value)
		}

		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}

		// A single unreachable asset must not abandon the remaining downloads.
		if err := downloadFile(req, alloc); err != nil {
			log.Errorf("downloading %s: %v", entry.Request.URL, err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(har.Log.Entries))
	}

	return nil
}

// uniqueName derives an output file name from urlPath, appending a numeric
// nameAllocator hands out unique file names within a directory.
//
// It remembers the next suffix to try for each basename, so N entries sharing a
// name cost O(N) probes in total rather than the O(N^2) of rescanning from 1
// every time. Names are claimed with O_EXCL, which also removes the race
// between checking for a free name and creating the file.
type nameAllocator struct {
	dir  string
	next map[string]int
}

func newNameAllocator(dir string) *nameAllocator {
	return &nameAllocator{dir: dir, next: make(map[string]int)}
}

// create opens a new file for urlPath under the allocator's directory, choosing
// a name that is not already taken, and returns it with the chosen path.
func (a *nameAllocator) create(urlPath string) (*os.File, string, error) {
	// path.Base returns "/" for a root path and "." for an empty one; neither is
	// a usable file name. It also strips any ".." traversal.
	base := path.Base(urlPath)
	if base == "/" || base == "" || base == "." {
		base = "index.html"
	}

	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Start from the highest suffix already handed out for this basename. There
	// is deliberately no upper bound: the only limit is the filesystem's.
	for i := a.next[base]; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}

		full := filepath.Join(a.dir, name)
		f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			a.next[base] = i + 1
			return f, full, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
}

func downloadFile(req *http.Request, alloc *nameAllocator) error {
	jar, _ := cookiejar.New(nil)

	jar.SetCookies(req.URL, req.Cookies())

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Error(err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// net/http only decompresses automatically when it set Accept-Encoding
	// itself. A server may still return an encoded body, so undo it here.
	body := io.Reader(resp.Body)
	switch enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			log.Error(err)
			return fmt.Errorf("decoding gzip body for %s: %w", req.URL, err)
		}
		defer func() { _ = zr.Close() }()
		body = zr
	case "deflate":
		fr := flate.NewReader(resp.Body)
		defer func() { _ = fr.Close() }()
		body = fr
	default:
		// br and friends are not in the standard library; store as received.
		log.Warnf("unsupported Content-Encoding %q for %s, saving encoded bytes", enc, req.URL)
	}

	// Name and create the file only once the response is in hand, so a failed
	// request does not leave an empty file behind or consume a name.
	file, fileName, err := alloc.create(req.URL.Path)
	if err != nil {
		log.Error(err)
		return err
	}

	size, err := io.Copy(file, body)
	if err != nil {
		_ = file.Close()
		log.Error(err)
		return err
	}

	// A failed close can mean the file was not fully written.
	if err := file.Close(); err != nil {
		log.Error(err)
		return err
	}

	fmt.Printf("Downloaded %s [%v bytes]\n", fileName, size)
	return nil
}
