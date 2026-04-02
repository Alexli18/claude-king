// internal/artifacts/ledger_security_test.go
package artifacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/artifacts"
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
