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
// creating it if necessary.
func FetchTo(r *bufio.Reader, outdir string) error {
	har, err := Decode(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return err
	}

	for _, entry := range har.Log.Entries {

		//TODO create goroutine here to parallelize requests

		fmt.Println("URL: " + entry.Request.URL)

		req, err := http.NewRequest(entry.Request.Method, entry.Request.URL, nil)
		if err != nil {
			log.Errorf("skipping entry %s: %v", entry.Request.URL, err)
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

		err = downloadFile(req, outdir)

		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

// uniqueName derives an output file name from urlPath, appending a numeric
// suffix when that name is already taken so that entries sharing a basename do
// not silently overwrite each other.
func uniqueName(outdir, urlPath string) (string, error) {
	// path.Base returns "/" for a root path and "." for an empty one; neither is
	// a usable file name. It also strips any ".." traversal.
	base := path.Base(urlPath)
	if base == "/" || base == "" || base == "." {
		base = "index.html"
	}

	candidate := filepath.Join(outdir, base)
	if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	}

	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 10000; i++ {
		candidate = filepath.Join(outdir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("cannot find an unused file name for %q in %s", base, outdir)
}

func downloadFile(req *http.Request, outdir string) error {
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

	// Name the file only once the response is in hand, so a failed request does
	// not leave an empty file behind or consume a name.
	fileName, err := uniqueName(outdir, req.URL.Path)
	if err != nil {
		log.Error(err)
		return err
	}

	file, err := os.Create(fileName)
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
