package hargo

import (
	"strings"
	"testing"
)

// Regression: Validate used to call os.Exit(-2) on a decode error, which made
// the failure path untestable and killed any process that used the library.
func TestValidateMalformedInputReturnsErrorNotExit(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "truncated object", input: `{"log":{"version":"1.2"`},
		{name: "not json at all", input: `this is not json`},
		{name: "empty input", input: ``},
		{name: "syntax error mid-document", input: `{"log":{ THIS IS NOT JSON }}`},
		{name: "wrong type for version", input: `{"log":{"version":123,"entries":[]}}`},
		{name: "entries is not an array", input: `{"log":{"version":"1.2","entries":"nope"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Validate(NewReader(strings.NewReader(tt.input)))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if ok {
				t.Error("Validate() ok = true, want false")
			}
			if !strings.Contains(err.Error(), "HAR") {
				t.Errorf("error should mention HAR, got %q", err)
			}
		})
	}
}

func TestValidateAcceptsVersion12(t *testing.T) {
	ok, err := Validate(NewReader(strings.NewReader(harWith(
		entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/a")))))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !ok {
		t.Error("Validate() ok = false, want true")
	}
}

// Previously an unsupported version returned (false, nil): no error, no
// diagnostic, and the CLI exited 0 while printing nothing at all.
func TestValidateRejectsUnsupportedVersion(t *testing.T) {
	for _, version := range []string{"1.1", "1.0", "2.0", ""} {
		t.Run("version "+version, func(t *testing.T) {
			in := `{"log":{"version":"` + version + `","entries":[]}}`
			ok, err := Validate(NewReader(strings.NewReader(in)))
			if ok {
				t.Error("Validate() ok = true, want false")
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil for unsupported version")
			}
			if !strings.Contains(err.Error(), "unsupported HAR version") {
				t.Errorf("error = %q, want it to mention the unsupported version", err)
			}
		})
	}
}

func TestValidateRealHarFixtures(t *testing.T) {
	for _, name := range []string{"test/golang.org.har", "test/en.wikipedia.org.har"} {
		t.Run(name, func(t *testing.T) {
			ok, err := Validate(NewReader(openFixture(t, name)))
			if err != nil {
				t.Fatalf("Validate(%s) error = %v", name, err)
			}
			if !ok {
				t.Errorf("Validate(%s) ok = false, want true", name)
			}
		})
	}
}

func TestValidateSkipsBOM(t *testing.T) {
	in := "\xef\xbb\xbf" + harWith("")
	ok, err := Validate(NewReader(strings.NewReader(in)))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !ok {
		t.Error("Validate() ok = false, want true for a BOM-prefixed HAR")
	}
}
