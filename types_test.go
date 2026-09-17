package hargo

import (
	"encoding/json"
	"strings"
	"testing"
)

// Regression: Entry timings were tagged "pageTimings", but the HAR spec names the
// entry field "timings" ("pageTimings" belongs to a page). Every real HAR
// therefore decoded entry timings as all zeros.
//
// The wrong tag also masked a second defect: the fields were int, and HAR timings
// are fractional, so correcting the tag alone made both fixtures fail to decode.
func TestEntryTimingsDecodeFromRealFixture(t *testing.T) {
	har, err := Decode(openFixture(t, "testdata/golang.org.har"))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(har.Log.Entries) == 0 {
		t.Fatal("no entries decoded")
	}

	e := har.Log.Entries[0]
	if e.Timings == (Timings{}) {
		t.Fatal("Entry.Timings is the zero value; the timings object did not decode")
	}

	// The fixture records fractional milliseconds, which an int field cannot hold.
	if e.Timings.Blocked <= 0 {
		t.Errorf("Timings.Blocked = %v, want the recorded positive value", e.Timings.Blocked)
	}
	if e.Timings.Blocked == float64(int(e.Timings.Blocked)) {
		t.Errorf("Timings.Blocked = %v, want a fractional value from the fixture", e.Timings.Blocked)
	}
	if e.Timings.Wait <= 0 {
		t.Errorf("Timings.Wait = %v, want the recorded positive value", e.Timings.Wait)
	}
}

func TestFractionalTimingsDecode(t *testing.T) {
	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"time":2.7129996047616007,
		"request":{"method":"GET","url":"http://example.com/a"},
		"timings":{"blocked":2.7129996047616007,"dns":-1,"connect":-1,
		"send":0.44700000000000273,"wait":73.4600002156198,"receive":2.93899979442358,"ssl":31.277}}]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	e := got.Log.Entries[0]
	if want := 2.7129996047616007; e.Timings.Blocked != want {
		t.Errorf("Timings.Blocked = %v, want %v", e.Timings.Blocked, want)
	}
	if want := 2.7129996047616007; e.Time != want {
		t.Errorf("Entry.Time = %v, want %v (float32 would lose precision)", e.Time, want)
	}
	if e.Timings.DNS != -1 {
		t.Errorf("Timings.DNS = %v, want -1 preserved as not-applicable", e.Timings.DNS)
	}
}

// Page timings are fractional too, and were tagged "pageTiming" rather than the
// spec's "pageTimings".
func TestPageTimingsDecode(t *testing.T) {
	har := `{"log":{"version":"1.2",
		"pages":[{"startedDateTime":"2024-01-01T00:00:00.001Z","id":"page_1","title":"t",
		"pageTimings":{"onContentLoad":171.71799996867776,"onLoad":268.4079999662936}}],
		"entries":[]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(got.Log.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(got.Log.Pages))
	}

	pt := got.Log.Pages[0].PageTimings
	if want := 171.71799996867776; pt.OnContentLoad != want {
		t.Errorf("PageTimings.OnContentLoad = %v, want %v", pt.OnContentLoad, want)
	}
	if want := 268.4079999662936; pt.OnLoad != want {
		t.Errorf("PageTimings.OnLoad = %v, want %v", pt.OnLoad, want)
	}
}

// Regression: Cookie.Comment was declared bool, so a spec-legal string comment
// made Decode and Validate reject the whole file.
func TestCookieCommentIsAString(t *testing.T) {
	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"GET","url":"http://example.com/a",
		"cookies":[{"name":"a","value":"b","comment":"why this cookie exists"}]}}]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v, want a string cookie comment accepted", err)
	}
	if err := Validate(strings.NewReader(har)); err != nil {
		t.Fatalf("Validate() error = %v, want a string cookie comment accepted", err)
	}

	cookies := got.Log.Entries[0].Request.Cookies
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	if want := "why this cookie exists"; cookies[0].Comment != want {
		t.Errorf("Cookie.Comment = %q, want %q", cookies[0].Comment, want)
	}
}

// Regression: the request header size was tagged "headerSize"; the spec (and both
// fixtures) use "headersSize", matching the response field.
func TestRequestHeadersSizeDecodes(t *testing.T) {
	har := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-01T00:00:00.001Z",
		"request":{"method":"GET","url":"http://example.com/a","headersSize":517,"bodySize":0}}]}}`

	got, err := Decode(strings.NewReader(har))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if want := 517; got.Log.Entries[0].Request.HeadersSize != want {
		t.Errorf("Request.HeadersSize = %d, want %d", got.Log.Entries[0].Request.HeadersSize, want)
	}
}

// The struct tags must match the keys the fixtures actually contain, so that a
// future rename cannot silently go back to decoding nothing.
func TestFixtureKeysMatchStructTags(t *testing.T) {
	var raw struct {
		Log struct {
			Entries []map[string]json.RawMessage `json:"entries"`
			Pages   []map[string]json.RawMessage `json:"pages"`
		} `json:"log"`
	}

	if err := json.NewDecoder(openFixture(t, "testdata/golang.org.har")).Decode(&raw); err != nil {
		t.Fatalf("raw decode: %v", err)
	}
	if len(raw.Log.Entries) == 0 {
		t.Fatal("fixture has no entries")
	}

	if _, ok := raw.Log.Entries[0]["timings"]; !ok {
		t.Error(`fixture entry has no "timings" key; Entry.Timings tag would be dead`)
	}
	if _, ok := raw.Log.Entries[0]["pageTimings"]; ok {
		t.Error(`fixture entry unexpectedly has "pageTimings"; that key belongs to a page`)
	}
	if len(raw.Log.Pages) > 0 {
		if _, ok := raw.Log.Pages[0]["pageTimings"]; !ok {
			t.Error(`fixture page has no "pageTimings" key`)
		}
	}
}
