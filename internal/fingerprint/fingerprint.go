// internal/fingerprint/fingerprint.go
package fingerprint

import (
	"os"
	"path/filepath"
)

// ProjectType represents the detected type of a project.
type ProjectType string

const (
	ProjectTypeGo       ProjectType = "go"
	ProjectTypeNode     ProjectType = "node"
	ProjectTypeHardware ProjectType = "hardware"
	ProjectTypeUnknown  ProjectType = "unknown"
	ProjectTypeESP32    ProjectType = "esp32"
	ProjectTypeNMEA     ProjectType = "nmea"
	ProjectTypeAT       ProjectType = "at"
)

// Fingerprint detects the project type by inspecting rootDir.
// Detection is first-match: Go > Node > Hardware > Unknown.
func Fingerprint(rootDir string) ProjectType {
	if fileExists(filepath.Join(rootDir, "go.mod")) {
		return ProjectTypeGo
	}
	if fileExists(filepath.Join(rootDir, "package.json")) {
		return ProjectTypeNode
	}
	if hasHardwareIndicators(rootDir) {
		return ProjectTypeHardware
	}
	return ProjectTypeUnknown
}

func hasHardwareIndicators(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	hasC := false
	hasMakefile := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext == ".ino" {
			return true
		}
		if ext == ".c" {
			hasC = true
		}
		if name == "Makefile" {
			hasMakefile = true
		}
	}
	return hasC && hasMakefile
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
