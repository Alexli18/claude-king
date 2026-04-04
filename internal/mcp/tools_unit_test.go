package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// isPathAllowed (unexported — tested via white-box)
// ---------------------------------------------------------------------------

func TestIsPathAllowed_ExactRoot(t *testing.T) {
	if !isPathAllowed("/tmp/kingdom", "/tmp/kingdom") {
		t.Error("exact root should be allowed")
	}
}

func TestIsPathAllowed_SubPath(t *testing.T) {
	if !isPathAllowed("/tmp/kingdom/subdir/file.txt", "/tmp/kingdom") {
		t.Error("sub-path should be allowed")
	}
}

func TestIsPathAllowed_Sibling_Rejected(t *testing.T) {
	if isPathAllowed("/tmp/kingdomextra/file.txt", "/tmp/kingdom") {
		t.Error("sibling dir with same prefix should be rejected")
	}
}

func TestIsPathAllowed_Parent_Rejected(t *testing.T) {
	if isPathAllowed("/tmp", "/tmp/kingdom") {
		t.Error("parent dir should be rejected")
	}
}

func TestIsPathAllowed_Unrelated_Rejected(t *testing.T) {
	if isPathAllowed("/etc/passwd", "/tmp/kingdom") {
		t.Error("unrelated path should be rejected")
	}
}

func TestIsPathAllowed_TrailingSlash(t *testing.T) {
	// filepath.Clean removes trailing slash, so these should be equivalent
	if !isPathAllowed("/tmp/kingdom/", "/tmp/kingdom") {
		t.Error("path with trailing slash should be allowed (after clean)")
	}
}

// ---------------------------------------------------------------------------
// readFileLines (unexported — tested via white-box)
// ---------------------------------------------------------------------------

func TestReadFileLines_Basic(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644)

	content, lines, truncated, err := readFileLines(f, 10)
	if err != nil {
		t.Fatalf("readFileLines: %v", err)
	}
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
	if truncated {
		t.Error("expected not truncated")
	}
	if !strings.Contains(content, "line1") || !strings.Contains(content, "line3") {
		t.Errorf("expected all lines in content, got %q", content)
	}
}

func TestReadFileLines_Truncated(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := range 10 {
		b.WriteString("line\n")
		_ = i
	}
	_ = os.WriteFile(f, []byte(b.String()), 0644)

	_, lines, truncated, err := readFileLines(f, 5)
	if err != nil {
		t.Fatalf("readFileLines: %v", err)
	}
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
	if !truncated {
		t.Error("expected truncated=true")
	}
}

func TestReadFileLines_Empty(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.txt")
	_ = os.WriteFile(f, []byte(""), 0644)

	content, lines, truncated, err := readFileLines(f, 100)
	if err != nil {
		t.Fatalf("readFileLines: %v", err)
	}
	if lines != 0 {
		t.Errorf("expected 0 lines, got %d", lines)
	}
	if truncated {
		t.Error("expected not truncated for empty file")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestReadFileLines_NotFound(t *testing.T) {
	_, _, _, err := readFileLines("/nonexistent/file.txt", 10)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadFileLines_SingleLine(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "single.txt")
	_ = os.WriteFile(f, []byte("only line"), 0644)

	content, lines, truncated, err := readFileLines(f, 10)
	if err != nil {
		t.Fatalf("readFileLines: %v", err)
	}
	if lines != 1 {
		t.Errorf("expected 1 line, got %d", lines)
	}
	if truncated {
		t.Error("expected not truncated")
	}
	if content != "only line" {
		t.Errorf("expected 'only line', got %q", content)
	}
}

func TestReadFileLines_ExactLimit(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "exact.txt")
	_ = os.WriteFile(f, []byte("a\nb\nc"), 0644)

	_, lines, truncated, err := readFileLines(f, 3)
	if err != nil {
		t.Fatalf("readFileLines: %v", err)
	}
	if lines != 3 {
		t.Errorf("expected 3 lines, got %d", lines)
	}
	if truncated {
		t.Error("expected not truncated when exactly at limit")
	}
}
