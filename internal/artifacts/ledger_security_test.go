// internal/artifacts/ledger_security_test.go
package artifacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/artifacts"
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/store"
)

func newTestLedger(t *testing.T) (*artifacts.Ledger, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	kingdomID := "test-kingdom-id"
	err = s.CreateKingdom(store.Kingdom{
		ID:       kingdomID,
		Name:     "test",
		RootPath: dir,
		Status:   "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	return artifacts.NewLedger(s, kingdomID), dir
}

func TestRegister_BlockedBySecretScanner_DotEnv(t *testing.T) {
	ledger, dir := newTestLedger(t)

	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET=mysecret"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("my-env", envPath, "", "")
	if err == nil {
		t.Fatal("expected error for .env file, got nil")
	}
	if !strings.Contains(err.Error(), "FILE_BLOCKED") {
		t.Errorf("expected FILE_BLOCKED in error, got: %v", err)
	}
}

func TestRegister_BlockedBySecretScanner_AWSKey(t *testing.T) {
	ledger, dir := newTestLedger(t)

	configPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(configPath, []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("config", configPath, "", "")
	if err == nil {
		t.Fatal("expected error for file with AWS key, got nil")
	}
	if !strings.Contains(err.Error(), "FILE_BLOCKED") {
		t.Errorf("expected FILE_BLOCKED in error, got: %v", err)
	}
}

func TestRegister_AllowedSafeFile(t *testing.T) {
	ledger, dir := newTestLedger(t)

	safePath := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(safePath, []byte("All tests passed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	art, err := ledger.Register("report", safePath, "", "")
	if err != nil {
		t.Fatalf("unexpected error for safe file: %v", err)
	}
	if art.Name != "report" {
		t.Errorf("got name %q, want %q", art.Name, "report")
	}
}

// newTestStore creates an in-memory test store with a kingdom pre-created.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	err = s.CreateKingdom(store.Kingdom{
		ID:       "kingdom-1",
		Name:     "test",
		RootPath: dir,
		Status:   "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRegister_ExternalScanner_Blocked(t *testing.T) {
	// Create a fake scanner that always exits 1
	tmp := t.TempDir()
	fakeScanner := filepath.Join(tmp, "fakescanner")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(fakeScanner, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	s := newTestStore(t)
	settings := config.Settings{SecurityScanner: fakeScanner}
	ledger := artifacts.NewLedgerWithSettings(s, "kingdom-1", settings)

	benign := filepath.Join(tmp, "safe.bin")
	if err := os.WriteFile(benign, []byte("safe content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("safe-artifact", benign, "", "application/octet-stream")
	if err == nil {
		t.Fatal("expected error when external scanner exits 1")
	}
	if !strings.Contains(err.Error(), "SCANNER_REJECTED") {
		t.Errorf("expected SCANNER_REJECTED in error, got: %v", err)
	}
}

func TestRegister_ExternalScanner_NotFound_FailOpen(t *testing.T) {
	tmp := t.TempDir()
	s := newTestStore(t)
	settings := config.Settings{SecurityScanner: "/nonexistent/scanner-king-test"}
	ledger := artifacts.NewLedgerWithSettings(s, "kingdom-1", settings)

	benign := filepath.Join(tmp, "safe.bin")
	if err := os.WriteFile(benign, []byte("safe content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("safe-artifact-nf", benign, "", "application/octet-stream")
	if err != nil {
		t.Fatalf("expected fail-open when scanner not found, got: %v", err)
	}
}

func TestRegister_ExternalScanner_Timeout_FailOpen(t *testing.T) {
	tmp := t.TempDir()
	fakeScanner := filepath.Join(tmp, "slow-scanner")
	// Scanner that sleeps much longer than the 5s timeout
	script := "#!/bin/sh\nsleep 60\n"
	if err := os.WriteFile(fakeScanner, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	s := newTestStore(t)
	settings := config.Settings{SecurityScanner: fakeScanner}
	ledger := artifacts.NewLedgerWithSettings(s, "kingdom-1", settings)

	benign := filepath.Join(tmp, "safe.bin")
	if err := os.WriteFile(benign, []byte("safe content"), 0644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err := ledger.Register("safe-artifact-timeout", benign, "", "application/octet-stream")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected fail-open on timeout, got: %v", err)
	}
	if elapsed > 7*time.Second {
		t.Errorf("scanner took too long: %v (expected ~5s timeout)", elapsed)
	}
}
