package hargo

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTestHAR creates a minimal HAR structure for testing
func createTestHAR() string {
	harData := Har{
		Log: Log{
			Entries: []Entry{
				{
					Request: Request{
						Method: "GET",
						URL:    "https://example.com/image.png",
					},
					Response: Response{
						Status: 200,
						Content: Content{
							MimeType: "image/png",
							Text:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==",
							Encoding: "base64",
						},
					},
				},
				{
					Request: Request{
						Method: "GET",
						URL:    "https://example.com/data.json",
					},
					Response: Response{
						Status: 200,
						Content: Content{
							MimeType: "application/json",
							Text:     `{"test": "data"}`,
						},
					},
				},
				{
					Request: Request{
						Method: "GET",
						URL:    "https://example.com/",
					},
					Response: Response{
						Status: 200,
						Content: Content{
							MimeType: "text/html",
							Text:     "<html><body>Test</body></html>",
						},
					},
				},
			},
		},
	}

	jsonData, _ := json.Marshal(harData)
	return string(jsonData)
}

// createEmptyHAR creates a HAR with no entries
func createEmptyHAR() string {
	harData := Har{
		Log: Log{
			Entries: []Entry{},
		},
	}
	jsonData, _ := json.Marshal(harData)
	return string(jsonData)
}

// cleanupExtractDirs removes any test extraction directories
func cleanupExtractDirs() {
	matches, _ := filepath.Glob("./hargo-extract-*")
	for _, match := range matches {
		os.RemoveAll(match)
	}
}

func TestExtractWithTypeOrganization(t *testing.T) {
	defer cleanupExtractDirs()

	testHAR := createTestHAR()
	reader := bufio.NewReader(strings.NewReader(testHAR))

	err := Extract(reader, true) // sortByType = true
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find the created extraction directory
	matches, err := filepath.Glob("./hargo-extract-*")
	if err != nil || len(matches) == 0 {
		t.Fatal("No extraction directory created")
	}

	extractDir := matches[0]

	// Verify type-based directories exist
	imageDir := filepath.Join(extractDir, "images")
	if _, err := os.Stat(imageDir); os.IsNotExist(err) {
		t.Error("Images directory not created")
	}

	jsonDir := filepath.Join(extractDir, "json")
	if _, err := os.Stat(jsonDir); os.IsNotExist(err) {
		t.Error("JSON directory not created")
	}

	htmlDir := filepath.Join(extractDir, "html")
	if _, err := os.Stat(htmlDir); os.IsNotExist(err) {
		t.Error("HTML directory not created")
	}

	// Verify manifest exists
	manifestPath := filepath.Join(extractDir, "extraction_manifest.csv")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("Manifest file not created")
	}
}

func TestExtractWithDomainOrganization(t *testing.T) {
	defer cleanupExtractDirs()

	testHAR := createTestHAR()
	reader := bufio.NewReader(strings.NewReader(testHAR))

	err := Extract(reader, false) // sortByType = false
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find the created extraction directory
	matches, err := filepath.Glob("./hargo-extract-*")
	if err != nil || len(matches) == 0 {
		t.Fatal("No extraction directory created")
	}

	extractDir := matches[0]

	// Verify domain-based directory exists
	domainDir := filepath.Join(extractDir, "example.com")
	if _, err := os.Stat(domainDir); os.IsNotExist(err) {
		t.Error("Domain directory not created")
	}

	// Verify manifest exists
	manifestPath := filepath.Join(extractDir, "extraction_manifest.csv")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("Manifest file not created")
	}
}

func TestExtractEmptyHAR(t *testing.T) {
	defer cleanupExtractDirs()

	emptyHAR := createEmptyHAR()
	reader := bufio.NewReader(strings.NewReader(emptyHAR))

	err := Extract(reader, false)
	if err != nil {
		t.Fatalf("Extract should handle empty HAR: %v", err)
	}

	// Should still create directory and manifest even with no content
	matches, err := filepath.Glob("./hargo-extract-*")
	if err != nil || len(matches) == 0 {
		t.Fatal("No extraction directory created for empty HAR")
	}

	extractDir := matches[0]
	manifestPath := filepath.Join(extractDir, "extraction_manifest.csv")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("Manifest file not created for empty HAR")
	}
}

func TestDetermineFilename(t *testing.T) {
	tests := []struct {
		url      string
		mimeType string
		expected string
	}{
		{"https://example.com/image.png", "image/png", "image.png"},
		{"https://example.com/", "text/html", "index.html"},
		{"https://example.com/api", "application/json", "api"}, // Uses URL path filename
		{"https://example.com/style", "text/css", "style"}, // Uses URL path filename  
		{"https://example.com/script", "application/javascript", "script"}, // Uses URL path filename
		{"https://example.com", "text/html", "index.html"}, // Root path gets default
	}

	for _, test := range tests {
		url := parseURL(t, test.url)
		result := determineFilename(url, test.mimeType)
		if result != test.expected {
			t.Errorf("determineFilename(%s, %s) = %s, expected %s", 
				test.url, test.mimeType, result, test.expected)
		}
	}
}

func TestGetTypeDirectory(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/png", "images"},
		{"application/json", "json"},
		{"text/html", "html"},
		{"text/css", "css"},
		{"application/javascript", "javascript"},
		{"font/woff2", "fonts"},
		{"video/mp4", "videos"},
		{"audio/mp3", "audio"},
		{"unknown/type", "other"},
	}

	for _, test := range tests {
		result := getTypeDirectory(test.mimeType)
		if result != test.expected {
			t.Errorf("getTypeDirectory(%s) = %s, expected %s", 
				test.mimeType, result, test.expected)
		}
	}
}

func TestGetExtensionFromMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"text/html", ".html"},
		{"application/json", ".json"},
		{"text/css", ".css"},
		{"application/javascript", ".js"},
		{"unknown/type", ".bin"},
	}

	for _, test := range tests {
		result := getExtensionFromMimeType(test.mimeType)
		if result != test.expected {
			t.Errorf("getExtensionFromMimeType(%s) = %s, expected %s", 
				test.mimeType, result, test.expected)
		}
	}
}

// Helper function to parse URL for testing
func parseURL(t *testing.T, urlStr string) *url.URL {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("Failed to parse URL %s: %v", urlStr, err)
	}
	return parsedURL
}