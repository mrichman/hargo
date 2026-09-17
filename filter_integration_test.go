package hargo

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// twoPathHAR builds a HAR with one GET entry for /first and one for /second
// against base.
func twoPathHAR(base string) string {
	return harWith(
		entryJSON("2024-01-01T00:00:01.000Z", "GET", base+"/first") + "," +
			entryJSON("2024-01-01T00:00:02.000Z", "GET", base+"/second"))
}

// TestDumpToFilters checks that filtering reaches the dump path.
func TestDumpToFilters(t *testing.T) {
	var buf bytes.Buffer

	err := DumpTo(&buf, strings.NewReader(filterFixture), DumpOptions{
		Filter: EntryFilter{Method: []string{"POST"}},
	})
	if err != nil {
		t.Fatalf("DumpTo() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "/api/login") {
		t.Errorf("DumpTo() output missing the selected entry\n---\n%s", out)
	}
	if strings.Contains(out, "app.js") || strings.Contains(out, "style.css") {
		t.Errorf("DumpTo() output contains an unselected entry\n---\n%s", out)
	}
}

// TestToCurlFilters checks that filtering reaches the curl path.
func TestToCurlFilters(t *testing.T) {
	got, err := ToCurl(strings.NewReader(filterFixture), CurlOptions{
		Filter: EntryFilter{URL: `\.css$`},
	})
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}

	if !strings.Contains(got, "style.css") {
		t.Errorf("ToCurl() = %q, want the selected entry", got)
	}
	if strings.Contains(got, "app.js") || strings.Contains(got, "/api/login") {
		t.Errorf("ToCurl() = %q, want only the selected entry", got)
	}
}

// TestToCurlToStreamsSameOutput checks that the streaming and buffering forms
// agree, since ToCurl is now implemented in terms of ToCurlTo.
func TestToCurlToStreamsSameOutput(t *testing.T) {
	want, err := ToCurl(strings.NewReader(filterFixture), CurlOptions{})
	if err != nil {
		t.Fatalf("ToCurl() error = %v", err)
	}

	var buf bytes.Buffer
	if err := ToCurlTo(&buf, strings.NewReader(filterFixture), CurlOptions{}); err != nil {
		t.Fatalf("ToCurlTo() error = %v", err)
	}

	if buf.String() != want {
		t.Errorf("ToCurlTo() wrote %q, want %q", buf.String(), want)
	}
}

// TestFilterErrorsAreReported checks that every entry point surfaces an invalid
// pattern instead of quietly selecting nothing.
func TestFilterErrorsAreReported(t *testing.T) {
	const bad = "([unclosed"

	t.Run("ToCurl", func(t *testing.T) {
		if _, err := ToCurl(strings.NewReader(filterFixture), CurlOptions{
			Filter: EntryFilter{URL: bad},
		}); err == nil {
			t.Error("ToCurl() error = nil, want non-nil for an invalid regex")
		}
	})

	t.Run("DumpTo", func(t *testing.T) {
		var buf bytes.Buffer
		if err := DumpTo(&buf, strings.NewReader(filterFixture), DumpOptions{
			Filter: EntryFilter{URL: bad},
		}); err == nil {
			t.Error("DumpTo() error = nil, want non-nil for an invalid regex")
		}
	})

	t.Run("Run", func(t *testing.T) {
		if err := Run(t.Context(), strings.NewReader(filterFixture), RunOptions{
			Filter: EntryFilter{URL: bad},
		}); err == nil {
			t.Error("Run() error = nil, want non-nil for an invalid regex")
		}
	})

	t.Run("Fetch", func(t *testing.T) {
		if err := Fetch(t.Context(), strings.NewReader(filterFixture), FetchOptions{
			OutDir: t.TempDir(),
			Filter: EntryFilter{URL: bad},
		}); err == nil {
			t.Error("Fetch() error = nil, want non-nil for an invalid regex")
		}
	})

	t.Run("ReadStream", func(t *testing.T) {
		entries := make(chan Entry, 1)
		err := ReadStream(t.Context(), strings.NewReader(filterFixture), entries, ReadOptions{
			Filter: EntryFilter{URL: bad},
		})
		if err == nil {
			t.Error("ReadStream() error = nil, want non-nil for an invalid regex")
		}
	})
}

// TestReadStreamReportsFilterMatchingNothing checks that a filter selecting none
// of the entries present is an error rather than looking like an empty HAR. Left
// silent it would also spin the replay loop.
func TestReadStreamReportsFilterMatchingNothing(t *testing.T) {
	entries := make(chan Entry, 8)

	err := ReadStream(t.Context(), strings.NewReader(filterFixture), entries, ReadOptions{
		Filter: EntryFilter{URL: `nothing-matches-this`},
	})
	if err == nil {
		t.Fatal("ReadStream() error = nil, want non-nil when the filter selects nothing")
	}
	if !strings.Contains(err.Error(), "filter selected none") {
		t.Errorf("ReadStream() error = %q, want it to explain that the filter matched nothing", err)
	}

	// The channel must still be closed, or a consumer ranging it would hang.
	if _, open := <-entries; open {
		t.Error("ReadStream() sent an entry despite the filter selecting nothing")
	}
}

// TestReadStreamFilterSelectsSubset checks that only matching entries are sent.
func TestReadStreamFilterSelectsSubset(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	entries := make(chan Entry, 4)
	go func() {
		_ = ReadStream(ctx, strings.NewReader(filterFixture), entries, ReadOptions{
			Filter: EntryFilter{Method: []string{"POST"}},
		})
	}()

	// The stream replays, so read a couple of passes worth and check every entry.
	for range 3 {
		select {
		case e, open := <-entries:
			if !open {
				t.Fatal("ReadStream() closed the channel early")
			}
			if e.Request.Method != http.MethodPost {
				t.Errorf("ReadStream() sent method %q, want only POST", e.Request.Method)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for a filtered entry")
		}
	}
}

// TestRunFilterLimitsRequests checks that a filtered replay contacts only the
// selected URLs.
func TestRunFilterLimitsRequests(t *testing.T) {
	var mu sync.Mutex
	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	har := twoPathHAR(srv.URL)

	err := Run(t.Context(), strings.NewReader(har), RunOptions{
		NoWait: true,
		Filter: EntryFilter{URL: `/second`},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/second" {
		t.Errorf("Run() requested %v, want only [/second]", paths)
	}
}

// TestRunFilterTallyCountsOnlySelected checks the denominator in the failure
// message. Skipping inside the loop instead of filtering up front would report
// the total number of entries rather than the number attempted.
func TestRunFilterTallyCountsOnlySelected(t *testing.T) {
	// A server that is not listening makes every attempted entry fail.
	har := twoPathHAR("http://127.0.0.1:1")

	err := Run(t.Context(), strings.NewReader(har), RunOptions{
		NoWait: true,
		Filter: EntryFilter{URL: `/second`},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil when the only selected entry fails")
	}
	if !strings.Contains(err.Error(), "1 of 1") {
		t.Errorf("Run() error = %q, want the tally to count only the selected entry", err)
	}
}

// TestRunFailOnStatus checks that an error status counts as a failure only when
// asked, since a recorded 404 is often the expected result.
func TestRunFailOnStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	har := twoPathHAR(srv.URL)

	t.Run("off by default", func(t *testing.T) {
		if err := Run(t.Context(), strings.NewReader(har), RunOptions{NoWait: true}); err != nil {
			t.Errorf("Run() error = %v, want nil when FailOnStatus is unset", err)
		}
	})

	t.Run("on", func(t *testing.T) {
		err := Run(t.Context(), strings.NewReader(har), RunOptions{
			NoWait:       true,
			FailOnStatus: true,
		})
		if err == nil {
			t.Fatal("Run() error = nil, want non-nil with FailOnStatus set")
		}
		if !strings.Contains(err.Error(), "2 of 2") {
			t.Errorf("Run() error = %q, want both entries counted as failures", err)
		}
	})
}

// TestRunFailOnStatusIgnoresSuccess checks that FailOnStatus does not flag a
// response the server was happy with.
func TestRunFailOnStatusIgnoresSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := Run(t.Context(), strings.NewReader(twoPathHAR(srv.URL)), RunOptions{
		NoWait:       true,
		FailOnStatus: true,
	})
	if err != nil {
		t.Errorf("Run() error = %v, want nil for successful responses", err)
	}
}
