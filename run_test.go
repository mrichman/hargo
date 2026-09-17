package hargo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder captures the requests an httptest server receives.
type recorder struct {
	mu       sync.Mutex
	paths    []string
	cookies  []string
	methods  []string
	handlerF func(w http.ResponseWriter, r *http.Request)
}

func (rec *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.methods = append(rec.methods, r.Method)
		rec.cookies = append(rec.cookies, r.Header.Get("Cookie"))
		rec.mu.Unlock()

		if rec.handlerF != nil {
			rec.handlerF(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

func (rec *recorder) seen() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]string, len(rec.paths))
	copy(out, rec.paths)
	return out
}

func TestRunExecutesAllEntriesInOrder(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// 1ms apart so Decode's sort is deterministic without a slow sleep.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/first") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/second") + "," +
			entryJSON("2024-01-01T00:00:00.003Z", "GET", srv.URL+"/third"))

	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"/first", "/second", "/third"}
	got := rec.seen()
	if len(got) != len(want) {
		t.Fatalf("got %d requests %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("request %d = %q, want %q", i, got[i], w)
		}
	}
}

func TestRunEmptyEntries(t *testing.T) {
	if err := Run(t.Context(), strings.NewReader(harWith("")), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Errorf("Run() error = %v, want nil for an empty entry list", err)
	}
}

func TestRunMalformedJSONReturnsError(t *testing.T) {
	if err := Run(t.Context(), strings.NewReader(`{"log":{ BROKEN`), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err == nil {
		t.Error("Run() error = nil, want non-nil for malformed JSON")
	}
}

// Regression: EntryToRequest returned (nil, nil) for an unparseable URL and
// Run then dereferenced req.URL, panicking. A bad entry must be skipped and
// the remaining entries must still run.
func TestRunSkipsUnparseableEntryAndContinues(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "://not a url") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/good") + "," +
			entryJSON("2024-01-01T00:00:00.003Z", "BAD METHOD", srv.URL+"/badmethod") + "," +
			entryJSON("2024-01-01T00:00:00.004Z", "GET", srv.URL+"/alsogood"))

	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err == nil {
		t.Fatal("Run() error = nil, want non-nil: two entries could not be built")
	} else if !strings.Contains(err.Error(), "2 of 4 entries failed") {
		t.Errorf("Run() error = %q, want it to report 2 of 4 failures", err)
	}

	want := []string{"/good", "/alsogood"}
	got := rec.seen()
	if len(got) != len(want) {
		t.Fatalf("got %d requests %v, want %v", len(got), got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("request %d = %q, want %q", i, got[i], w)
		}
	}
}

// A transport-level failure on one entry must not panic or abort the replay.
func TestRunContinuesAfterTransportError(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// Port 0 on localhost is never listening, so this entry fails to connect.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://127.0.0.1:0/dead") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", srv.URL+"/alive"))

	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err == nil {
		t.Fatal("Run() error = nil, want non-nil: one entry was unreachable")
	} else if !strings.Contains(err.Error(), "1 of 2 entries failed") {
		t.Errorf("Run() error = %q, want it to report 1 of 2 failures", err)
	}

	// The reachable entry must still have been replayed.
	if got := rec.seen(); len(got) != 1 || got[0] != "/alive" {
		t.Errorf("got %v, want [/alive]", got)
	}
}

func TestRunNon2xxIsNotAnError(t *testing.T) {
	rec := &recorder{handlerF: func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", srv.URL+"/missing"))
	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Errorf("Run() error = %v, want nil (a 404 is a valid response)", err)
	}
	if got := rec.seen(); len(got) != 1 {
		t.Errorf("got %v, want one request", got)
	}
}

func TestRunHonoursIgnoreHARCookies(t *testing.T) {
	tests := []struct {
		name             string
		ignoreHARCookies bool
		wantCookie       bool
	}{
		{name: "cookies sent", ignoreHARCookies: false, wantCookie: true},
		{name: "cookies ignored", ignoreHARCookies: true, wantCookie: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			srv := httptest.NewServer(rec.handler())
			defer srv.Close()

			har := `{"log":{"version":"1.2","entries":[{
				"startedDateTime":"2024-01-01T00:00:00.001Z",
				"request":{"method":"GET","url":"` + srv.URL + `/c","httpVersion":"HTTP/1.1",
				"cookies":[{"name":"session","value":"abc123"}]}}]}}`

			if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: tt.ignoreHARCookies, InsecureSkipVerify: true}); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.cookies) != 1 {
				t.Fatalf("got %d requests, want 1", len(rec.cookies))
			}
			hasCookie := strings.Contains(rec.cookies[0], "session=abc123")
			if hasCookie != tt.wantCookie {
				t.Errorf("cookie present = %v, want %v (header %q)",
					hasCookie, tt.wantCookie, rec.cookies[0])
			}
		})
	}
}

func TestRunSendsPostBody(t *testing.T) {
	var gotBody string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		mu.Lock()
		gotBody = string(buf)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"POST","url":"` + srv.URL + `/submit","httpVersion":"HTTP/1.1",
		"postData":{"mimeType":"application/json","text":"{\"k\":\"v\"}"}}}]}}`

	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if want := `{"k":"v"}`; gotBody != want {
		t.Errorf("body = %q, want %q", gotBody, want)
	}
}

// WebSocket entries are dropped by Decode, so Run must never try to dial them.
func TestRunSkipsWebSocketEntries(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	har := harWith(
		entryJSON("2024-01-01T00:00:00.001Z", "GET", "ws://127.0.0.1:1/sock") + "," +
			entryJSON("2024-01-01T00:00:00.002Z", "GET", "wss://127.0.0.1:1/sock2") + "," +
			entryJSON("2024-01-01T00:00:00.003Z", "GET", srv.URL+"/http"))

	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "/http" {
		t.Errorf("got %v, want [/http]", got)
	}
}

func TestRunOptionsDelayBefore(t *testing.T) {
	tests := []struct {
		name string
		opts RunOptions
		gap  time.Duration
		want time.Duration
	}{
		{
			name: "zero value replays in real time",
			opts: RunOptions{},
			gap:  2 * time.Second,
			want: 2 * time.Second,
		},
		{
			name: "speed 1 is real time",
			opts: RunOptions{Speed: 1},
			gap:  2 * time.Second,
			want: 2 * time.Second,
		},
		{
			name: "speed 2 halves the delay",
			opts: RunOptions{Speed: 2},
			gap:  2 * time.Second,
			want: time.Second,
		},
		{
			name: "speed 0.5 doubles the delay",
			opts: RunOptions{Speed: 0.5},
			gap:  time.Second,
			want: 2 * time.Second,
		},
		{
			name: "NoWait removes the delay",
			opts: RunOptions{NoWait: true},
			gap:  time.Hour,
			want: 0,
		},
		{
			name: "NoWait beats Speed and MaxDelay",
			opts: RunOptions{NoWait: true, Speed: 0.1, MaxDelay: time.Minute},
			gap:  time.Hour,
			want: 0,
		},
		{
			name: "MaxDelay caps a long gap",
			opts: RunOptions{MaxDelay: 2 * time.Second},
			gap:  10 * time.Minute,
			want: 2 * time.Second,
		},
		{
			name: "MaxDelay does not extend a short gap",
			opts: RunOptions{MaxDelay: time.Minute},
			gap:  time.Second,
			want: time.Second,
		},
		{
			name: "MaxDelay applies after Speed",
			opts: RunOptions{Speed: 10, MaxDelay: 5 * time.Second},
			gap:  20 * time.Second,
			want: 2 * time.Second,
		},
		{
			name: "negative gap yields no delay",
			opts: RunOptions{},
			gap:  -time.Second,
			want: 0,
		},
		{
			name: "zero gap yields no delay",
			opts: RunOptions{},
			gap:  0,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.delayBefore(tt.gap); got != tt.want {
				t.Errorf("delayBefore(%v) = %v, want %v", tt.gap, got, tt.want)
			}
		})
	}
}

func TestRunWithOptionsRejectsNegativeSpeed(t *testing.T) {
	har := harWith(entryJSON("2024-01-01T00:00:00.001Z", "GET", "http://example.com/"))

	err := Run(t.Context(), strings.NewReader(har), RunOptions{Speed: -1})
	if err == nil {
		t.Fatal("Run(t.Context(), ) error = nil, want non-nil for a negative speed")
	}
	if !strings.Contains(err.Error(), "speed") {
		t.Errorf("error = %q, want it to mention speed", err)
	}
}

// Regression: replay used the recorded wall-clock gaps with no way to compress
// them, so a HAR spanning minutes took minutes.
func TestRunWithOptionsNoWaitSkipsRecordedDelays(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// Three entries a full second apart: ~2s of sleeping in real time.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.000Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:00:01.000Z", "GET", srv.URL+"/b") + "," +
			entryJSON("2024-01-01T00:00:02.000Z", "GET", srv.URL+"/c"))

	start := time.Now()
	err := Run(t.Context(), strings.NewReader(har), RunOptions{
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
		NoWait:             true,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run(t.Context(), ) error = %v", err)
	}
	if got := len(rec.seen()); got != 3 {
		t.Errorf("got %d requests, want 3", got)
	}
	// Generous bound: the point is that it is nowhere near the recorded 2s.
	if elapsed > 500*time.Millisecond {
		t.Errorf("NoWait replay took %v, expected it to skip the recorded ~2s", elapsed)
	}
}

func TestRunWithOptionsMaxDelayCapsLongGaps(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// A five-minute idle gap, capped to 50ms.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.000Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:05:00.000Z", "GET", srv.URL+"/b"))

	start := time.Now()
	err := Run(t.Context(), strings.NewReader(har), RunOptions{
		IgnoreHARCookies:   true,
		InsecureSkipVerify: true,
		MaxDelay:           50 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run(t.Context(), ) error = %v", err)
	}
	if got := len(rec.seen()); got != 2 {
		t.Errorf("got %d requests, want 2", got)
	}
	if elapsed > 2*time.Second {
		t.Errorf("replay took %v, expected the 5m gap to be capped", elapsed)
	}
}

// Run must remain a real-time replay, so the existing behaviour is preserved
// for library callers that have not opted in.
func TestRunStillHonoursRecordedDelays(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// 120ms apart; real-time replay must take at least that long.
	har := harWith(
		entryJSON("2024-01-01T00:00:00.000Z", "GET", srv.URL+"/a") + "," +
			entryJSON("2024-01-01T00:00:00.120Z", "GET", srv.URL+"/b"))

	start := time.Now()
	if err := Run(t.Context(), strings.NewReader(har), RunOptions{IgnoreHARCookies: true, InsecureSkipVerify: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("Run took %v, expected it to honour the recorded ~120ms gap", elapsed)
	}
}
