package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/source-map-sentinel/scanner"
)

func TestFindMapFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "app.js.map"), []byte(`{"version":3}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(`console.log("hi")`), 0644); err != nil {
		t.Fatal(err)
	}

	findings, err := scanner.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Type != scanner.FindingMapFile {
		t.Errorf("expected FindingMapFile, got %s", findings[0].Type)
	}
}

func TestDetectSourceMappingURL(t *testing.T) {
	dir := t.TempDir()

	content := "(function(){ /* bundled code */ })()\n//# sourceMappingURL=app.js.map\n"
	if err := os.WriteFile(filepath.Join(dir, "bundle.js"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	findings, err := scanner.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Type != scanner.FindingSourceMappingURL {
		t.Errorf("expected FindingSourceMappingURL, got %s", findings[0].Type)
	}
	if findings[0].Line != 2 {
		t.Errorf("expected line 2, got %d", findings[0].Line)
	}
	if !strings.Contains(findings[0].Text, "sourceMappingURL") {
		t.Errorf("expected Text to contain sourceMappingURL, got %q", findings[0].Text)
	}
}

func TestMapFileWithSourceMappingURLContent(t *testing.T) {
	dir := t.TempDir()

	// .map file that also contains a sourceMappingURL reference
	content := `{"version":3,"sourceMappingURL=chained.map"}`
	if err := os.WriteFile(filepath.Join(dir, "app.js.map"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	findings, err := scanner.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (map file only), got %d", len(findings))
	}
	if findings[0].Type != scanner.FindingMapFile {
		t.Errorf("expected FindingMapFile, got %s", findings[0].Type)
	}
}

func TestCleanDirectory(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main`), 0644); err != nil {
		t.Fatal(err)
	}

	findings, err := scanner.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings in clean directory, got %d", len(findings))
	}
}
