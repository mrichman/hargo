package hargo

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// ManifestEntry represents metadata for a single extracted file,
// tracking its original location and extraction details for audit purposes.
type ManifestEntry struct {
	OriginalURL   string `json:"originalUrl"`
	ExtractedPath string `json:"extractedPath"`
	MimeType      string `json:"mimeType"`
	Size          int    `json:"size"`
	Method        string `json:"method"`
	Status        int    `json:"status"`
}

// Extract extracts response content from .har file to filesystem.
// Creates timestamped output directory and organizes files by domain or MIME type.
// sortByType=true groups files by content type (images/, json/, etc.),
// sortByType=false preserves original domain structure from URLs.
// Returns error if HAR parsing fails or file system operations fail.
func Extract(r *bufio.Reader, sortByType bool) error {
	har, err := Decode(r)
	if err != nil {
		return err
	}

	// Create timestamped output directory to avoid conflicts with previous extractions
	datestring := time.Now().Format("20060102150405")
	outdir := "." + string(filepath.Separator) + "hargo-extract-" + datestring

	err = os.Mkdir(outdir, 0777)
	if err != nil {
		return err
	}

	fmt.Printf("Extracting HAR content to: %s\n", outdir)
	if sortByType {
		fmt.Println("Organizing files by content type...")
	} else {
		fmt.Println("Organizing files by domain...")
	}

	// Track filenames to avoid collisions when multiple entries have same name.
	// filenameCount maps filename -> occurrence count for collision handling.
	// manifest accumulates metadata for all successfully extracted files.
	filenameCount := make(map[string]int)
	var manifest []ManifestEntry

	// Process each HAR entry, extracting response content if present
	for i, entry := range har.Log.Entries {
		if entry.Response.Content.Text == "" {
			log.Debugf("Skipping entry %d: no response content", i)
			continue
		}

		parsedURL, err := url.Parse(entry.Request.URL)
		if err != nil {
			log.Errorf("Failed to parse URL %s: %v", entry.Request.URL, err)
			continue
		}

		var fullPath string
		var filename string

		if sortByType {
			// Organize files into type-based directories (images/, json/, css/, etc.)
			// This mode groups similar content together for easier browsing
			typeDir := getTypeDirectory(entry.Response.Content.MimeType)
			fullTypeDir := filepath.Join(outdir, typeDir)
			err = os.MkdirAll(fullTypeDir, 0777)
			if err != nil {
				log.Errorf("Failed to create type directory %s: %v", fullTypeDir, err)
				continue
			}

			// Smart filename generation extracts meaningful names from URLs
			// and handles collisions by appending sequence numbers
			filename = generateSmartFilename(parsedURL, entry.Response.Content.MimeType, filenameCount)
			fullPath = filepath.Join(fullTypeDir, filename)
		} else {
			// Preserve original domain structure from URLs to maintain site organization.
			// This mode recreates the website's directory structure locally.
			domain := parsedURL.Hostname()
			if domain == "" {
				domain = "unknown"
			}

			domainDir := filepath.Join(outdir, domain)
			err = os.MkdirAll(domainDir, 0777)
			if err != nil {
				log.Errorf("Failed to create domain directory %s: %v", domainDir, err)
				continue
			}

			filename = determineFilename(parsedURL, entry.Response.Content.MimeType)
			urlPath := strings.TrimPrefix(parsedURL.Path, "/")
			if urlPath != "" {
				fullPath = filepath.Join(domainDir, urlPath)
			} else {
				fullPath = filepath.Join(domainDir, filename)
			}
		}

		// Decode response content, handling base64 encoding for binary files.
		// HAR format stores binary content as base64, text content as plain text.
		content := entry.Response.Content.Text
		var decodedContent []byte

		// Check encoding type and decode accordingly
		if entry.Response.Content.Encoding == "base64" {
			decodedContent, err = base64.StdEncoding.DecodeString(content)
			if err != nil {
				log.Errorf("Failed to decode base64 content for %s: %v", entry.Request.URL, err)
				continue
			}
		} else {
			decodedContent = []byte(content)
		}

		// Write decoded content to filesystem with appropriate permissions
		err = os.WriteFile(fullPath, decodedContent, 0644)
		if err != nil {
			log.Errorf("Failed to write file %s: %v", fullPath, err)
			continue
		}

		// Record extraction details in manifest for audit trail
		manifest = append(manifest, ManifestEntry{
			OriginalURL: entry.Request.URL,
			ExtractedPath: fullPath,
			MimeType: entry.Response.Content.MimeType,
			Size: len(decodedContent),
			Method: entry.Request.Method,
			Status: entry.Response.Status,
		})

		fmt.Printf("Extracted %s -> %s [%d bytes]\n", 
			entry.Request.URL, fullPath, len(decodedContent))
	}

	// Write CSV manifest documenting all extracted files with metadata.
	// This provides a complete audit trail of the extraction process.
	manifestPath := filepath.Join(outdir, "extraction_manifest.csv")
	err = writeManifest(manifest, manifestPath)
	if err != nil {
		log.Errorf("Failed to write manifest: %v", err)
	} else {
		fmt.Printf("\nExtraction manifest written to: %s\n", manifestPath)
	}

	return nil
}

// determineFilename extracts filename from URL path or generates sensible default.
// For URLs without filenames (/, /api, etc.), creates appropriate names based on MIME type.
// This ensures every extracted file has a meaningful, recognizable filename.
func determineFilename(parsedURL *url.URL, mimeType string) string {
	filename := path.Base(parsedURL.Path)
	
	// Generate sensible default filename for root paths or empty filenames.
	// Maps common MIME types to conventional file extensions and names.
	if filename == "/" || filename == "" || filename == "." {
		switch {
		case strings.Contains(mimeType, "text/html"):
			filename = "index.html"
		case strings.Contains(mimeType, "application/json"):
			filename = "response.json"
		case strings.Contains(mimeType, "text/css"):
			filename = "style.css"
		case strings.Contains(mimeType, "application/javascript"):
			filename = "script.js"
		case strings.Contains(mimeType, "image/"):
			// Extract image extension from MIME type
			if strings.Contains(mimeType, "png") {
				filename = "image.png"
			} else if strings.Contains(mimeType, "jpeg") {
				filename = "image.jpg"
			} else if strings.Contains(mimeType, "gif") {
				filename = "image.gif"
			} else if strings.Contains(mimeType, "svg") {
				filename = "image.svg"
			} else {
				filename = "image.bin"
			}
		default:
			filename = "response.bin"
		}
	}

	return filename
}

// getTypeDirectory maps MIME types to organized directory names for sortByType mode.
// Groups similar content types together: images/, json/, css/, javascript/, etc.
// Provides a clean, browsable organization of extracted web assets.
func getTypeDirectory(mimeType string) string {
	mimeType = strings.ToLower(mimeType)
	
	switch {
	case strings.Contains(mimeType, "image/"):
		return "images"
	case strings.Contains(mimeType, "application/json") || strings.Contains(mimeType, "text/json"):
		return "json"
	case strings.Contains(mimeType, "text/html"):
		return "html"
	case strings.Contains(mimeType, "text/css"):
		return "css"
	case strings.Contains(mimeType, "javascript") || strings.Contains(mimeType, "application/x-javascript"):
		return "javascript"
	case strings.Contains(mimeType, "font") || strings.Contains(mimeType, "woff"):
		return "fonts"
	case strings.Contains(mimeType, "text/"):
		return "text"
	case strings.Contains(mimeType, "video/"):
		return "videos"
	case strings.Contains(mimeType, "audio/"):
		return "audio"
	default:
		return "other"
	}
}

// generateSmartFilename creates descriptive filenames with collision handling for sortByType mode.
// Extracts meaningful names from URL paths, falls back to content-aware defaults,
// and appends sequence numbers to handle filename collisions across different domains.
func generateSmartFilename(parsedURL *url.URL, mimeType string, filenameCount map[string]int) string {
	var baseName, extension string
	
	// Extract base filename and extension from URL path, preserving original naming
	urlPath := strings.TrimPrefix(parsedURL.Path, "/")
	if urlPath != "" && urlPath != "." {
		baseName = path.Base(urlPath)
		if strings.Contains(baseName, ".") {
			parts := strings.Split(baseName, ".")
			extension = "." + parts[len(parts)-1]
			baseName = strings.Join(parts[:len(parts)-1], ".")
		}
	}
	
	// Fallback to content-aware filename generation when URL provides no useful filename.
	// Uses URL context clues (path segments, query params) to create descriptive names.
	if baseName == "" || baseName == "/" {
		switch {
		case strings.Contains(mimeType, "application/json"):
			if strings.Contains(parsedURL.Path, "posts") || strings.Contains(parsedURL.RawQuery, "posts") {
				baseName = "posts"
			} else if strings.Contains(parsedURL.Path, "api") {
				baseName = "api_response"
			} else {
				baseName = "data"
			}
		case strings.Contains(mimeType, "text/html"):
			baseName = "page"
		case strings.Contains(mimeType, "image/"):
			baseName = "image"
		case strings.Contains(mimeType, "text/css"):
			baseName = "style"
		case strings.Contains(mimeType, "javascript"):
			baseName = "script"
		default:
			baseName = "file"
		}
	}
	
	// Determine extension from MIME type if URL didn't provide one.
	// Ensures files have proper extensions for system recognition.
	if extension == "" {
		extension = getExtensionFromMimeType(mimeType)
	}
	
	// Handle filename collisions by appending sequence numbers.
	// Tracks usage count per filename to ensure uniqueness across all extractions.
	filename := baseName + extension
	if count, exists := filenameCount[filename]; exists {
		filenameCount[filename] = count + 1
		filename = baseName + "_" + strconv.Itoa(count+1) + extension
	} else {
		filenameCount[filename] = 0
	}
	
	return filename
}

// getExtensionFromMimeType maps MIME types to appropriate file extensions.
// Provides comprehensive mapping for web content types to ensure proper file recognition.
// Falls back to .bin for unknown types to prevent extension-less files.
func getExtensionFromMimeType(mimeType string) string {
	mimeType = strings.ToLower(mimeType)
	
	switch {
	case strings.Contains(mimeType, "application/json"):
		return ".json"
	case strings.Contains(mimeType, "text/html"):
		return ".html"
	case strings.Contains(mimeType, "text/css"):
		return ".css"
	case strings.Contains(mimeType, "javascript"):
		return ".js"
	case strings.Contains(mimeType, "image/png"):
		return ".png"
	case strings.Contains(mimeType, "image/jpeg") || strings.Contains(mimeType, "image/jpg"):
		return ".jpg"
	case strings.Contains(mimeType, "image/gif"):
		return ".gif"
	case strings.Contains(mimeType, "image/svg"):
		return ".svg"
	case strings.Contains(mimeType, "image/webp"):
		return ".webp"
	case strings.Contains(mimeType, "text/plain"):
		return ".txt"
	case strings.Contains(mimeType, "application/pdf"):
		return ".pdf"
	case strings.Contains(mimeType, "font/woff2"):
		return ".woff2"
	case strings.Contains(mimeType, "font/woff"):
		return ".woff"
	case strings.Contains(mimeType, "font/ttf"):
		return ".ttf"
	default:
		return ".bin"
	}
}

// writeManifest creates CSV file documenting all extracted files with complete metadata.
// Includes original URLs, extraction paths, content types, sizes, and HTTP details.
// Provides audit trail and enables post-extraction analysis and verification.
func writeManifest(manifest []ManifestEntry, manifestPath string) error {
	file, err := os.Create(manifestPath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write CSV header with descriptive column names for easy parsing
	// Example row: "https://example.com/image.png","./images/image.png","image/png","1024","GET","200"
	header := []string{"Original URL", "Extracted Path", "MIME Type", "Size (bytes)", "HTTP Method", "Status Code"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows with all extraction metadata for each file
	for _, entry := range manifest {
		record := []string{
			entry.OriginalURL,
			entry.ExtractedPath,
			entry.MimeType,
			strconv.Itoa(entry.Size),
			entry.Method,
			strconv.Itoa(entry.Status),
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}