// internal/security/scanner_codes_test.go
package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScan_ErrorCodes_Structured(t *testing.T) {
	loreStrings := []string{
		"🛡️", "Royal Guard", "Inquisitor", "Court Mage", "Herald",
		"INVALID_SECURITY", "blacklisted", "blocked",
	}

	tmp := t.TempDir()
	envFile := filepath.Join(tmp, ".env")
	if err := os.WriteFile(envFile, []byte("SECRET=abc"), 0600); err != nil {
		t.Fatal(err)
	}

	result := Scan(envFile)
	if !result.Blocked {
		t.Fatal("expected .env to be blocked")
	}

	for _, lore := range loreStrings {
		if strings.Contains(result.Reason, lore) {
			t.Errorf("Reason must not contain lore string %q, got: %q", lore, result.Reason)
		}
	}

	validReasons := map[string]bool{
		"FILENAME_BLACKLISTED":  true,
		"EXTENSION_BLOCKED":     true,
		"AWS_KEY_DETECTED":      true,
		"GITHUB_TOKEN_DETECTED": true,
		"PRIVATE_KEY_DETECTED":  true,
		"SCANNER_REJECTED":      true,
	}
	if !validReasons[result.Reason] {
		t.Errorf("unexpected reason code %q — must be one of: %v", result.Reason, validReasons)
	}
}
