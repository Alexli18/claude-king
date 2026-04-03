// Package scanner detects source map files and sourceMappingURL references
// that indicate accidental source exposure in packaged distributions.
package scanner

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// FindingType categorizes what was detected.
type FindingType string

const (
	// FindingMapFile is a *.map file found in the scan target.
	FindingMapFile FindingType = "map_file"
	// FindingSourceMappingURL is a sourceMappingURL reference in a text file.
	FindingSourceMappingURL FindingType = "source_mapping_url"
)

// Finding describes a single detected issue.
type Finding struct {
	Type FindingType `json:"type"`
	Path string      `json:"path"`
	Line int         `json:"line,omitempty"` // 0 for file-level findings
	Text string      `json:"text,omitempty"` // matched line text
}

// Scan walks root recursively and returns all findings.
func Scan(root string) ([]Finding, error) {
	var findings []Finding

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable entries and continue walking.
			return nil
		}
		if info.IsDir() {
			return nil
		}

		// Check for .map extension.
		if strings.HasSuffix(strings.ToLower(info.Name()), ".map") {
			findings = append(findings, Finding{
				Type: FindingMapFile,
				Path: path,
			})
			// .map files are reported as FindingMapFile only; content is not scanned
			// (chained source maps are out of scope for this tool).
			return nil
		}

		// Check text files for sourceMappingURL references.
		if isTextFile(info.Name()) {
			lineFindings, err := scanFileForMappingURL(path)
			if err != nil {
				// Skip unreadable files silently.
				return nil
			}
			findings = append(findings, lineFindings...)
		}

		return nil
	})

	return findings, err
}

// isTextFile returns true for file extensions eligible for content scanning.
func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".js", ".css", ".ts", ".mjs", ".cjs", ".jsx", ".tsx", ".html", ".htm":
		return true
	}
	return false
}

// scanFileForMappingURL opens path and checks each line for sourceMappingURL.
func scanFileForMappingURL(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	lineNum := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lineNum++
		line := sc.Text()
		if strings.Contains(line, "sourceMappingURL=") {
			findings = append(findings, Finding{
				Type: FindingSourceMappingURL,
				Path: path,
				Line: lineNum,
				Text: strings.TrimSpace(line),
			})
		}
	}
	return findings, sc.Err()
}
