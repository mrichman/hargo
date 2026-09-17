package hargo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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

	if err := Run(NewReader(strings.NewReader(har)), true, true); err != nil {
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
	if err := Run(NewReader(strings.NewReader(harWith(""))), true, true); err != nil {
		t.Errorf("Run() error = %v, want nil for an empty entry list", err)
	}
}

func TestRunMalformedJSONReturnsError(t *testing.T) {
	if err := Run(NewReader(strings.NewReader(`{"log":{ BROKEN`)), true, true); err == nil {
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

	if err := Run(NewReader(strings.NewReader(har)), true, true); err == nil {
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

	if err := Run(NewReader(strings.NewReader(har)), true, true); err == nil {
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
	if err := Run(NewReader(strings.NewReader(har)), true, true); err != nil {
		t.Errorf("Run() error = %v, want nil (a 404 is a valid response)", err)
	}
	if got := rec.seen(); len(got) != 1 {
		t.Errorf("got %v, want one request", got)
	}
}

func TestRunHonoursIgnoreHarCookies(t *testing.T) {
	tests := []struct {
		name             string
		ignoreHarCookies bool
		wantCookie       bool
	}{
		{name: "cookies sent", ignoreHarCookies: false, wantCookie: true},
		{name: "cookies ignored", ignoreHarCookies: true, wantCookie: false},
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

			if err := Run(NewReader(strings.NewReader(har)), tt.ignoreHarCookies, true); err != nil {
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

	if err := Run(NewReader(strings.NewReader(har)), true, true); err != nil {
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

	if err := Run(NewReader(strings.NewReader(har)), true, true); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := rec.seen(); len(got) != 1 || got[0] != "/http" {
		t.Errorf("got %v, want [/http]", got)
	}
}
