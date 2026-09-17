package hargo

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// RunOptions controls how Run replays a HAR file. The zero value replays in
// real time with cookies applied, TLS verified, and no output.
type RunOptions struct {
	// IgnoreHARCookies drops the cookies recorded in the HAR.
	IgnoreHARCookies bool

	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool

	// Speed scales the recorded delay between entries: 1 replays in real time
	// and 2 replays twice as fast. Zero or unset means 1, so that the zero value
	// of RunOptions replays in real time. Negative values are rejected.
	//
	// Speed cannot express "no delay at all" because 0 is the zero value; use
	// NoWait for that.
	Speed float64

	// NoWait issues every request back to back, ignoring the recorded timings
	// entirely. It takes precedence over Speed and MaxDelay.
	NoWait bool

	// MaxDelay caps the wait before any single entry, applied after Speed. Zero
	// means no cap. Useful for HARs containing minutes of user idle time.
	MaxDelay time.Duration

	// Filter selects which entries to replay. A zero Filter selects all of them.
	// Entries are selected before replay begins, so the failure tally counts only
	// the entries actually attempted.
	Filter EntryFilter

	// FailOnStatus counts a response of 400 or above as a failed entry, so that a
	// replay of a broken recording exits non-zero. By default any response the
	// server actually returned is a success, since a recorded 404 is often the
	// expected result.
	FailOnStatus bool

	// Logger receives diagnostics about skipped and failed entries. A nil
	// Logger discards them.
	Logger *slog.Logger

	// Progress receives one line per replayed entry. A nil Progress discards it.
	Progress io.Writer
}

// delayBefore returns how long to wait for a recorded inter-entry gap of d.
func (o RunOptions) delayBefore(d time.Duration) time.Duration {
	if o.NoWait || d <= 0 {
		return 0
	}

	speed := o.Speed
	if speed <= 0 {
		speed = 1
	}

	scaled := time.Duration(float64(d) / speed)
	if o.MaxDelay > 0 && scaled > o.MaxDelay {
		return o.MaxDelay
	}
	return scaled
}

// Run replays every entry in a HAR document, honouring the recorded delays
// unless opts says otherwise.
//
// An entry that cannot be built or sent is reported to opts.Logger and skipped,
// so one bad entry does not abandon the replay; Run then returns an error
// naming how many failed. Cancelling ctx stops the replay.
func Run(ctx context.Context, r io.Reader, opts RunOptions) error {
	if opts.Speed < 0 {
		return fmt.Errorf("speed must not be negative, got %v", opts.Speed)
	}
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

	if len(selected) == 0 {
		return nil
	}

	jar, _ := cookiejar.New(nil)

	// The default redirect policy is correct. Rewriting URL.Opaque forced the
	// request line to the decoded path, which emits a malformed request for any
	// redirect target containing an escape such as %20.
	client := http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify},
		},
	}

	// prev is the start time of the last entry whose timestamp parsed. An entry
	// with an unparseable timestamp must not overwrite it: treating that entry as
	// the zero time would make the gap to the following entry astronomically
	// large, and time.Sub saturates rather than wrapping, so the replay would
	// then sleep for centuries.
	var prev time.Time
	var havePrev bool

	failed := 0

	for _, entry := range selected {
		st, err := ParseEntryTime(entry.StartedDateTime)
		switch {
		case err != nil:
			// Report it rather than silently replaying with no pacing at all.
			logger.Warn("cannot parse startedDateTime, not delaying before this entry",
				"value", entry.StartedDateTime, "url", entry.Request.URL)
		case havePrev:
			if d := opts.delayBefore(st.Sub(prev)); d > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(d):
				}
			}
			prev = st
		default:
			prev, havePrev = st, true
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := EntryToRequest(ctx, &entry, EntryOptions{IgnoreHARCookies: opts.IgnoreHARCookies})
		if err != nil {
			// A malformed entry must not abort the remaining replay.
			logger.Error("skipping entry", "url", entry.Request.URL, "err", err)
			failed++
			continue
		}

		jar.SetCookies(req.URL, req.Cookies())

		resp, err := client.Do(req)
		if err != nil {
			// A cancelled context surfaces here as a request error; report it as
			// cancellation rather than as a failed entry.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			logger.Error("request failed", "url", entry.Request.URL, "err", err)
			failed++
			continue
		}

		// Progress is a caller-supplied writer; a failed write there is not
		// something this loop can act on.
		_, _ = fmt.Fprintf(progress, "[%s,%v] URL: %s\n", entry.Request.Method, resp.StatusCode, entry.Request.URL)

		_ = resp.Body.Close()

		if opts.FailOnStatus && resp.StatusCode >= http.StatusBadRequest {
			logger.Error("entry returned an error status",
				"url", entry.Request.URL, "status", resp.StatusCode)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(selected))
	}

	return nil
}
