// internal/security/scanner.go
package security

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ScanResult reports whether an artifact should be blocked.
type ScanResult struct {
	Blocked bool
	Reason  string
}

// blockedNames is a set of exact filenames that are always blocked.
var blockedNames = map[string]bool{
	".env":          true,
	"id_rsa":        true,
	"id_ed25519":    true,
	"id_ecdsa":      true,
	"id_dsa":        true,
	".bash_history": true,
	".zsh_history":  true,
	"secrets.json":  true,
}

// blockedExtensions are always blocked regardless of content.
var blockedExtensions = map[string]bool{
	".pem":         true,
	".key":         true,
	".p12":         true,
	".pfx":         true,
	".credentials": true,
}

// blockedFilenamePatterns are prefix-based filename patterns.
// e.g. "credentials." matches credentials.json, credentials.yml, etc.
var blockedFilenamePatterns = []string{
	"credentials.",
}

// textExtensions are eligible for content scanning.
var textExtensions = map[string]bool{
	".json": true,
	".yaml": true,
	".yml":  true,
	".conf": true,
	".txt":  true,
	".sh":   true,
	".toml": true,
	".env":  true,
}

const maxContentScanSize = 1 << 20 // 1 MB

// secretPatterns are compiled regexes for content scanning.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)AWS_ACCESS_KEY_ID\s*[=:]\s*[A-Z0-9]{16,}`),
	regexp.MustCompile(`(?i)AWS_SECRET_ACCESS_KEY\s*[=:]\s*[A-Za-z0-9/+=]{32,}`),
	regexp.MustCompile(`(?i)GITHUB_TOKEN\s*[=:]\s*[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{48}`),
	regexp.MustCompile(`-----BEGIN (RSA|EC|OPENSSH|DSA) PRIVATE KEY-----`),
}

// Scan checks filePath for secrets using a filename blacklist and smart content scan.
// Returns a ScanResult indicating whether the artifact should be blocked.
func Scan(filePath string) ScanResult {
	base := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(base))

	// 1. Exact filename blacklist.
	if blockedNames[base] {
		return ScanResult{Blocked: true, Reason: "filename:blacklisted:" + base}
	}

	// 2. Extension blacklist.
	if blockedExtensions[ext] {
		return ScanResult{Blocked: true, Reason: "filename:blocked-extension:" + ext}
	}

	// 3. Filename prefix patterns (e.g. "credentials.*").
	for _, prefix := range blockedFilenamePatterns {
		if strings.HasPrefix(base, prefix) {
			return ScanResult{Blocked: true, Reason: "filename:blocked-pattern:" + prefix}
		}
	}

	// 4. Filename glob: *.env or .env.*
	if ext == ".env" || strings.HasPrefix(base, ".env.") {
		return ScanResult{Blocked: true, Reason: "filename:env-variant:" + base}
	}

	// 5. Smart content scan (text files ≤ 1MB only).
	if !textExtensions[ext] {
		return ScanResult{Blocked: false}
	}

	info, err := os.Stat(filePath)
	if err != nil || info.Size() > maxContentScanSize {
		return ScanResult{Blocked: false}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return ScanResult{Blocked: false}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, re := range secretPatterns {
			if re.MatchString(line) {
				s := re.String()
				if len(s) > 30 {
					s = s[:30]
				}
				return ScanResult{Blocked: true, Reason: "content:" + s}
			}
		}
	}

	return ScanResult{Blocked: false}
}
