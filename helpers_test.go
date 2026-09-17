package hargo

import (
	"os"
	"path/filepath"
	"testing"
)

// openFixture opens a .har file from the repository's test/ directory and
// registers it for cleanup.
func openFixture(t *testing.T, rel string) *os.File {
	t.Helper()

	f, err := os.Open(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("opening fixture %s: %v", rel, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
