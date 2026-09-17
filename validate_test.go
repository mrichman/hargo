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
			err := Validate(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), "HAR") {
				t.Errorf("error should mention HAR, got %q", err)
			}
		})
	}
}

func TestValidateAcceptsVersion12(t *testing.T) {
	if err := Validate(strings.NewReader(harWith(
		entryJSON("2024-01-01T00:00:01.000Z", "GET", "http://example.com/a")))); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

// Previously an unsupported version returned (false, nil): no error, no
// diagnostic, and the CLI exited 0 while printing nothing at all.
func TestValidateRejectsUnsupportedVersion(t *testing.T) {
	for _, version := range []string{"1.1", "1.0", "2.0", ""} {
		t.Run("version "+version, func(t *testing.T) {
			in := `{"log":{"version":"` + version + `","entries":[]}}`
			err := Validate(strings.NewReader(in))
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil for unsupported version")
			}
			if !strings.Contains(err.Error(), "unsupported HAR version") {
				t.Errorf("error = %q, want it to mention the unsupported version", err)
			}
		})
	}
}

func TestValidateRealHARFixtures(t *testing.T) {
	for _, name := range []string{"testdata/golang.org.har", "testdata/en.wikipedia.org.har"} {
		t.Run(name, func(t *testing.T) {
			if err := Validate(openFixture(t, name)); err != nil {
				t.Fatalf("Validate(%s) error = %v", name, err)
			}
		})
	}
}

func TestValidateSkipsBOM(t *testing.T) {
	in := "\xef\xbb\xbf" + harWith("")
	if err := Validate(strings.NewReader(in)); err != nil {
		t.Fatalf("Validate() error = %v, want nil for a BOM-prefixed HAR", err)
	}
}
