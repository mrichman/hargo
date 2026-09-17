package hargo

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// FetchOptions controls how Fetch downloads the resources a HAR references.
// The zero value writes to a new timestamped directory in the working
// directory and produces no output.
type FetchOptions struct {
	// OutDir is the directory to write into, created if necessary. When empty a
	// directory named hargo-fetch-<timestamp> is created in the working
	// directory.
	OutDir string

	// AcceptErrorStatus saves the response body even when the server reported an
	// error status. By default a 4xx or 5xx response is treated as a failed
	// download and nothing is written, so the output directory does not fill with
	// error pages named after the resources they failed to be.
	AcceptErrorStatus bool

	// Filter selects which entries to download. A zero Filter selects all of them.
	// Entries are selected before downloading begins, so the failure tally counts
	// only the entries actually attempted.
	Filter EntryFilter

	// Logger receives diagnostics about skipped and failed entries. A nil
	// Logger discards them.
	Logger *slog.Logger

	// Progress receives one line per resource. A nil Progress discards it.
	Progress io.Writer
}

// outDir returns the directory to download into.
func (o FetchOptions) outDir() string {
	if o.OutDir != "" {
		return o.OutDir
	}
	datestring := time.Now().Format("20060102150405")
	return "." + string(filepath.Separator) + "hargo-fetch-" + datestring
}

// Fetch downloads every resource referenced by a HAR document.
//
// An entry that cannot be built or downloaded is reported to opts.Logger and
// skipped, so one bad resource does not abandon the rest; Fetch then returns an
// error naming how many failed. Cancelling ctx stops the download.
func Fetch(ctx context.Context, r io.Reader, opts FetchOptions) error {
	if err := opts.Filter.compile(); err != nil {
		return err
	}

	logger := loggerOrDiscard(opts.Logger)
	progress := writerOrDiscard(opts.Progress)

	har, err := Decode(r)
	if err != nil {
		return err
	}

	// Selecting up front rather than skipping inside the loop keeps the tally's
	// denominator equal to the number of entries actually attempted.
	selected := filterEntries(har.Log.Entries, &opts.Filter)

	outdir := opts.outDir()
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return err
	}

	alloc := newNameAllocator(outdir)
	failed := 0

	for _, entry := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}

		// TODO: create a goroutine here to parallelize requests.

		// Progress is a caller-supplied writer; a failed write there is not
		// something this loop can act on.
		_, _ = fmt.Fprintln(progress, "URL: "+entry.Request.URL)

		// The recorded body is sent too: a POST or PUT entry replayed with an empty
		// body usually returns an error page, which would then be saved as if it
		// were the resource.
		body := postBody(entry.Request.PostData)
		req, err := http.NewRequestWithContext(ctx, entry.Request.Method, entry.Request.URL,
			strings.NewReader(body))
		if err != nil {
			logger.Error("skipping entry", "url", entry.Request.URL, "err", err)
			failed++
			continue
		}

		recordedHost := ""
		for _, h := range entry.Request.Headers {
			// An HTTP/2 capture carries the host in :authority, which is not a legal
			// field name and so is only read here.
			if strings.EqualFold(h.Name, ":authority") && recordedHost == "" {
				recordedHost = h.Value
			}
			if !isReplayableHeader(h.Name, h.Value) {
				continue
			}
			// Cookie is applied from entry.Request.Cookies below.
			if strings.EqualFold(h.Name, "Cookie") {
				continue
			}
			// net/http ignores a Host entry in the header map.
			if strings.EqualFold(h.Name, "Host") {
				recordedHost = h.Value
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
		if recordedHost != "" {
			req.Host = recordedHost
		}

		if body != "" && req.Header.Get("Content-Type") == "" && entry.Request.PostData.MimeType != "" {
			req.Header.Set("Content-Type", entry.Request.PostData.MimeType)
		}

		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}

		// A single unreachable asset must not abandon the remaining downloads.
		if err := downloadFile(req, alloc, opts, logger, progress); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			logger.Error("download failed", "url", entry.Request.URL, "err", err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(selected))
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
	base := safeBaseName(urlPath)

	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	// A dotfile such as ".env" is all extension and no stem, which would produce
	// "-1.env" on collision.
	if stem == "" {
		stem = base
		ext = ""
	}

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

// maxBaseNameLen is a conservative bound; NAME_MAX is 255 on Linux and macOS,
// and room is left for a "-<n>" collision suffix.
const maxBaseNameLen = 200

// safeBaseName reduces a URL path to a single file name that cannot escape the
// output directory or be rejected by the filesystem.
//
// A HAR is untrusted input, so this cannot rely on path.Base alone: it splits on
// forward slashes only, which leaves a backslash-separated path intact for
// filepath.Join to then interpret as directories on Windows.
func safeBaseName(urlPath string) string {
	// Treat both separators as separators, whatever the host OS, and take the
	// last segment.
	base := urlPath
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}

	// Replace anything that names a directory, traverses, or is illegal in a
	// Windows file name. NUL is rejected by every filesystem.
	base = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, base)

	// "." and ".." name directories, and an empty base has no name at all.
	if base == "" || base == "." || base == ".." {
		base = "index.html"
	}

	if len(base) > maxBaseNameLen {
		// Keep the extension, which is what makes the file recognisable.
		ext := path.Ext(base)
		if len(ext) > maxBaseNameLen/2 {
			ext = ""
		}
		base = base[:maxBaseNameLen-len(ext)] + ext
	}

	return base
}

func downloadFile(req *http.Request, alloc *nameAllocator, opts FetchOptions,
	logger *slog.Logger, progress io.Writer) error {
	jar, _ := cookiejar.New(nil)

	jar.SetCookies(req.URL, req.Cookies())

	// The default redirect policy is correct here. Rewriting URL.Opaque used to
	// force the request line to the decoded path, which emits a malformed request
	// for any redirect target containing an escape such as %20.
	client := http.Client{Jar: jar}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// An error page saved under the resource's own name is worse than no file at
	// all: the download looks like it succeeded.
	if resp.StatusCode >= http.StatusBadRequest && !opts.AcceptErrorStatus {
		return fmt.Errorf("%s returned %s", req.URL, resp.Status)
	}

	// net/http only decompresses automatically when it set Accept-Encoding
	// itself. A server may still return an encoded body, so undo it here.
	body := io.Reader(resp.Body)
	switch enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("decoding gzip body for %s: %w", req.URL, err)
		}
		defer func() { _ = zr.Close() }()
		body = zr
	case "deflate":
		// HTTP "deflate" is the zlib format (RFC 9110 8.4.1.2), but servers in the
		// wild also send raw deflate, so fall back to that.
		rc, err := newDeflateReader(resp.Body)
		if err != nil {
			return fmt.Errorf("decoding deflate body for %s: %w", req.URL, err)
		}
		defer func() { _ = rc.Close() }()
		body = rc
	default:
		// br and friends are not in the standard library; store as received.
		logger.Warn("unsupported Content-Encoding, saving encoded bytes",
			"encoding", enc, "url", req.URL.String())
	}

	// Name and create the file only once the response is in hand, so a failed
	// request does not leave an empty file behind or consume a name.
	file, fileName, err := alloc.create(req.URL.Path)
	if err != nil {
		return err
	}

	size, err := io.Copy(file, body)
	if err != nil {
		_ = file.Close()
		return err
	}

	// A failed close can mean the file was not fully written.
	if err := file.Close(); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(progress, "Downloaded %s [%v bytes]\n", fileName, size)
	return nil
}

// newDeflateReader decodes an HTTP "deflate" body, which RFC 9110 defines as the
// zlib format. Raw deflate is non-conformant but common, so it is accepted too.
func newDeflateReader(r io.Reader) (io.ReadCloser, error) {
	buf := bufio.NewReader(r)

	// A zlib stream opens with a two-byte header whose low nibble is the
	// compression method (8 for deflate) and whose big-endian value is a multiple
	// of 31. Peeking rather than attempting zlib.NewReader matters: on a raw
	// deflate body that call would consume the bytes it then rejected.
	if h, err := buf.Peek(2); err == nil {
		if h[0]&0x0f == 8 && (uint16(h[0])<<8|uint16(h[1]))%31 == 0 {
			return zlib.NewReader(buf)
		}
	}

	return flate.NewReader(buf), nil
}
