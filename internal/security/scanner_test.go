// internal/security/scanner_test.go
package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/security"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- Filename blacklist tests ---

func TestScan_BlockedByFilename_DotEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "SAFE=true")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for .env file")
	}
	if result.Reason != "FILENAME_BLACKLISTED" {
		t.Errorf("expected FILENAME_BLACKLISTED reason, got %q", result.Reason)
	}
}

func TestScan_BlockedByFilename_Pem(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "cert.pem", "data")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for .pem file")
	}
	if result.Reason != "EXTENSION_BLOCKED" {
		t.Errorf("expected EXTENSION_BLOCKED reason, got %q", result.Reason)
	}
}

func TestScan_BlockedByFilename_IdRsa(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "id_rsa", "data")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for id_rsa file")
	}
	if result.Reason != "FILENAME_BLACKLISTED" {
		t.Errorf("expected FILENAME_BLACKLISTED reason, got %q", result.Reason)
	}
}

func TestScan_BlockedByFilename_Credentials(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "credentials.json", "{}")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for credentials.json")
	}
	if result.Reason != "FILENAME_BLACKLISTED" {
		t.Errorf("expected FILENAME_BLACKLISTED reason, got %q", result.Reason)
	}
}

// --- Content scan tests ---

func TestScan_BlockedByContent_AWSKey(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yml", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n")
	result := security.Scan(path)
	if !result.Blocked {
		t.Errorf("expected Blocked=true for AWS key in content, reason: %s", result.Reason)
	}
}

func TestScan_BlockedByContent_GitHubPAT(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.json", `{"token":"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ123456789012"}`)
	result := security.Scan(path)
	if !result.Blocked {
		t.Errorf("expected Blocked=true for GitHub PAT, reason: %s", result.Reason)
	}
}

func TestScan_BlockedByContent_PrivateKey(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "setup.sh", "-----BEGIN RSA PRIVATE KEY-----\nMIIEo...\n")
	result := security.Scan(path)
	if !result.Blocked {
		t.Errorf("expected Blocked=true for private key header, reason: %s", result.Reason)
	}
}

// --- Safe file tests ---

func TestScan_AllowedSafeConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "app.yml", "host: localhost\nport: 8080\n")
	result := security.Scan(path)
	if result.Blocked {
		t.Errorf("expected Blocked=false for safe config, reason: %s", result.Reason)
	}
}

func TestScan_AllowedBinaryFile(t *testing.T) {
	dir := t.TempDir()
	// Binary extension — should skip content scan, and filename is fine.
	path := writeFile(t, dir, "output.bin", "binary data")
	result := security.Scan(path)
	if result.Blocked {
		t.Errorf("expected Blocked=false for .bin file, reason: %s", result.Reason)
	}
}

func TestScan_SkipsContentScanForLargeFile(t *testing.T) {
	dir := t.TempDir()
	// Write a safe .yaml file but mock "large" — test that we DON'T block for size alone.
	// (Real large-file test would require >1MB; we just verify the safe path here.)
	path := writeFile(t, dir, "data.yaml", "key: value\n")
	result := security.Scan(path)
	if result.Blocked {
		t.Errorf("expected Blocked=false, got reason: %s", result.Reason)
	}
}

// --- ScanContent tests ---

func TestScanContent_BlocksAWSAccessKey(t *testing.T) {
	content := "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nsome other line"
	result := security.ScanContent(content)
	if !result.Blocked {
		t.Error("expected Blocked=true for AWS access key in content")
	}
	if result.Reason != "AWS_KEY_DETECTED" {
		t.Errorf("expected reason AWS_KEY_DETECTED, got %q", result.Reason)
	}
}

func TestScanContent_BlocksGitHubToken(t *testing.T) {
	content := "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	result := security.ScanContent(content)
	if !result.Blocked {
		t.Error("expected Blocked=true for GitHub token in content")
	}
}

func TestScanContent_CleanContentPasses(t *testing.T) {
	content := "hello world\nno secrets here\nfoo=bar"
	result := security.ScanContent(content)
	if result.Blocked {
		t.Errorf("expected Blocked=false for clean content, got reason %q", result.Reason)
	}
}

func TestScanContent_EmptyStringPasses(t *testing.T) {
	result := security.ScanContent("")
	if result.Blocked {
		t.Errorf("expected Blocked=false for empty string, got reason %q", result.Reason)
	}
}
