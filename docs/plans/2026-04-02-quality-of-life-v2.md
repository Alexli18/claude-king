# Quality of Life v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Zero-Config Onboarding, Secret Scanner, and Auto-Integrity to the Claude King orchestrator.

**Architecture:** Three new internal packages (`discovery`, `fingerprint`, `security`) implement the logic. `king-vassal` gains optional flags with auto-discovery fallback. `Ledger.Register` gains a security guard. The daemon injects integrity patterns on vassal registration.

**Tech Stack:** Go 1.22+, `path/filepath`, `bufio`, `regexp`, `gopkg.in/yaml.v3`, existing `internal/config`, `internal/artifacts`, `internal/daemon` packages. Module: `github.com/alexli18/claude-king`.

---

## Context

- Socket path: `SocketPathForRoot(rootDir)` → `.king/king-<sha256[:8]hex>.sock` (NOT a fixed name)
- `PatternConfig` has a `Source` field (`yaml:"source,omitempty"`) — use `"auto"` for auto-injected contracts
- `king-vassal` currently requires `--name`, `--repo`, `--king-sock` (all become optional)
- Module: `github.com/alexli18/claude-king`

---

## Task 1: `internal/discovery` — Socket Traversal

**Files:**
- Create: `internal/discovery/discovery.go`
- Create: `internal/discovery/discovery_test.go`

### Step 1: Write the failing test

```go
// internal/discovery/discovery_test.go
package discovery_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/discovery"
)

func TestFindKingdomSocket_Found(t *testing.T) {
	// Build a temp tree: /tmp/root/.king/king-aabbccdd.sock
	root := t.TempDir()
	kingDir := filepath.Join(root, ".king")
	if err := os.MkdirAll(kingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(kingDir, "king-aabbccdd.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	// Search from a subdirectory.
	subDir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gotSock, gotRoot, err := discovery.FindKingdomSocket(subDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSock != sockPath {
		t.Errorf("socket: got %q, want %q", gotSock, sockPath)
	}
	if gotRoot != root {
		t.Errorf("root: got %q, want %q", gotRoot, root)
	}
}

func TestFindKingdomSocket_NotFound(t *testing.T) {
	root := t.TempDir()
	_, _, err := discovery.FindKingdomSocket(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != discovery.ErrNoKingdom {
		t.Errorf("expected ErrNoKingdom, got %v", err)
	}
}

func TestFindKingdomSocket_CurrentDir(t *testing.T) {
	// Socket in the start dir itself.
	root := t.TempDir()
	kingDir := filepath.Join(root, ".king")
	if err := os.MkdirAll(kingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(kingDir, "king-deadbeef.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	gotSock, gotRoot, err := discovery.FindKingdomSocket(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSock != sockPath {
		t.Errorf("socket: got %q, want %q", gotSock, sockPath)
	}
	if gotRoot != root {
		t.Errorf("root: got %q, want %q", gotRoot, root)
	}
}
```

### Step 2: Run test to verify it fails

```bash
cd /Users/alex/Desktop/Claude_King
go test ./internal/discovery/... -v
```
Expected: `cannot find package` or `no Go files`

### Step 3: Write implementation

```go
// internal/discovery/discovery.go
package discovery

import (
	"errors"
	"path/filepath"

	"github.com/alexli18/claude-king/internal/daemon"
)

// ErrNoKingdom is returned when no .king socket is found in the directory tree.
var ErrNoKingdom = errors.New("no Kingdom found: run king-daemon init first")

// FindKingdomSocket walks from startDir up to / looking for a king daemon socket
// in a .king subdirectory. It uses daemon.SocketPathForRoot to compute the expected
// socket path for each candidate root directory.
//
// Returns the socket path and the kingdom root directory on success.
func FindKingdomSocket(startDir string) (socketPath, rootDir string, err error) {
	dir := startDir
	for {
		sockPath := daemon.SocketPathForRoot(dir)
		if fileExists(sockPath) {
			return sockPath, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", "", ErrNoKingdom
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

Wait — `daemon.SocketPathForRoot` is in package `daemon`, which would create an import cycle if discovery imports daemon. Instead, duplicate the socket path logic in discovery (or extract it to a shared sub-package).

**Correct implementation** (no import cycle — inline the hash logic):

```go
// internal/discovery/discovery.go
package discovery

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoKingdom is returned when no king daemon socket is found in the directory tree.
var ErrNoKingdom = errors.New("no Kingdom found: run king-daemon init first")

// socketPathForRoot mirrors daemon.SocketPathForRoot without importing daemon.
// The socket path is deterministic: .king/king-<sha256[:8]hex>.sock.
func socketPathForRoot(rootDir string) string {
	h := sha256.Sum256([]byte(rootDir))
	return filepath.Join(rootDir, ".king", fmt.Sprintf("king-%x.sock", h[:8]))
}

// FindKingdomSocket walks from startDir up to / looking for a king daemon socket.
// Returns (socketPath, kingdomRootDir, nil) on success, or ErrNoKingdom if not found.
func FindKingdomSocket(startDir string) (socketPath, rootDir string, err error) {
	dir := startDir
	for {
		sockPath := socketPathForRoot(dir)
		if _, statErr := os.Stat(sockPath); statErr == nil {
			return sockPath, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", ErrNoKingdom
		}
		dir = parent
	}
}
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/discovery/... -v
```
Expected: all 3 tests PASS

### Step 5: Commit

```bash
git add internal/discovery/discovery.go internal/discovery/discovery_test.go
git commit -m "feat: add internal/discovery — git-style kingdom socket traversal"
```

---

## Task 2: `internal/fingerprint` — Project Type Detection

**Files:**
- Create: `internal/fingerprint/fingerprint.go`
- Create: `internal/fingerprint/fingerprint_test.go`

### Step 1: Write the failing test

```go
// internal/fingerprint/fingerprint_test.go
package fingerprint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/fingerprint"
)

func mkFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprint_Go(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "go.mod")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeGo {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeGo)
	}
}

func TestFingerprint_Node(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "package.json")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeNode {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeNode)
	}
}

func TestFingerprint_GoTakesPriorityOverNode(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "go.mod")
	mkFile(t, dir, "package.json")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeGo {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeGo)
	}
}

func TestFingerprint_Hardware_Ino(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "sketch.ino")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeHardware {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeHardware)
	}
}

func TestFingerprint_Hardware_CMakefile(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "main.c")
	mkFile(t, dir, "Makefile")
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeHardware {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeHardware)
	}
}

func TestFingerprint_Unknown(t *testing.T) {
	dir := t.TempDir()
	if got := fingerprint.Fingerprint(dir); got != fingerprint.ProjectTypeUnknown {
		t.Errorf("got %q, want %q", got, fingerprint.ProjectTypeUnknown)
	}
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/fingerprint/... -v
```
Expected: `cannot find package`

### Step 3: Write implementation

```go
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
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/fingerprint/... -v
```
Expected: all 6 tests PASS

### Step 5: Commit

```bash
git add internal/fingerprint/fingerprint.go internal/fingerprint/fingerprint_test.go
git commit -m "feat: add internal/fingerprint — project type detection (Go/Node/Hardware)"
```

---

## Task 3: `internal/security` — Secret Scanner

**Files:**
- Create: `internal/security/scanner.go`
- Create: `internal/security/scanner_test.go`

### Step 1: Write the failing test

```go
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
}

func TestScan_BlockedByFilename_Pem(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "cert.pem", "data")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for .pem file")
	}
}

func TestScan_BlockedByFilename_IdRsa(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "id_rsa", "data")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for id_rsa file")
	}
}

func TestScan_BlockedByFilename_Credentials(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "credentials.json", "{}")
	result := security.Scan(path)
	if !result.Blocked {
		t.Error("expected Blocked=true for credentials.json")
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
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/security/... -v
```
Expected: `cannot find package`

### Step 3: Write implementation

```go
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
	".env":            true,
	"id_rsa":          true,
	"id_ed25519":      true,
	"id_ecdsa":        true,
	"id_dsa":          true,
	".bash_history":   true,
	".zsh_history":    true,
	"secrets.json":    true,
}

// blockedExtensions are always blocked regardless of content.
var blockedExtensions = map[string]bool{
	".pem":         true,
	".key":         true,
	".p12":         true,
	".pfx":         true,
	".credentials": true,
}

// blockedPrefixPatterns are prefix-based filename patterns.
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
				return ScanResult{Blocked: true, Reason: "content:" + re.String()[:30]}
			}
		}
	}

	return ScanResult{Blocked: false}
}
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/security/... -v
```
Expected: all 9 tests PASS

### Step 5: Commit

```bash
git add internal/security/scanner.go internal/security/scanner_test.go
git commit -m "feat: add internal/security — secret scanner with filename blacklist and content regex"
```

---

## Task 4: Update `cmd/king-vassal/main.go` — Zero-Config Mode

**Files:**
- Modify: `cmd/king-vassal/main.go`

### Step 1: Read current main.go

Read `cmd/king-vassal/main.go` (lines 1-52). Note the required flags: `--name`, `--repo`, `--king-sock`.

### Step 2: Write the updated main.go

Replace the entire file:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/alexli18/claude-king/internal/discovery"
	"github.com/alexli18/claude-king/internal/fingerprint"
	"github.com/alexli18/claude-king/internal/vassal"
)

func main() {
	name := flag.String("name", "", "vassal name (default: current directory name)")
	repoPath := flag.String("repo", "", "path to vassal repo (default: current directory)")
	kingDir := flag.String("king-dir", ".king", "path to .king directory")
	kingSocket := flag.String("king-sock", "", "path to king daemon socket (auto-discovered if omitted)")
	timeoutMin := flag.Int("timeout", 10, "task timeout in minutes")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine current directory: %v\n", err)
		os.Exit(1)
	}

	// Zero-config: fill missing flags from environment.
	if *repoPath == "" {
		*repoPath = cwd
	}
	if *name == "" {
		*name = filepath.Base(*repoPath)
	}
	if *kingSocket == "" {
		sockPath, _, discErr := discovery.FindKingdomSocket(cwd)
		if discErr != nil {
			fmt.Fprintln(os.Stderr, "error: No Kingdom found. Run king-daemon init first.")
			os.Exit(1)
		}
		*kingSocket = sockPath
	}

	// Fingerprint the project and log the type (used by daemon for Auto-Integrity).
	pt := fingerprint.Fingerprint(*repoPath)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With(
		"component", "king-vassal",
		"name", *name,
		"project_type", string(pt),
	)

	srv := vassal.NewVassalServer(*name, *repoPath, *kingDir, *kingSocket, *timeoutMin, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	sockPath, err := srv.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting vassal server: %v\n", err)
		os.Exit(1)
	}
	logger.Info("vassal MCP server started", "socket", sockPath)

	select {
	case <-sigCh:
		logger.Info("shutting down")
		cancel()
	case <-ctx.Done():
	}
}
```

### Step 3: Build to verify no compile errors

```bash
go build ./cmd/king-vassal/...
```
Expected: no errors, binary produced

### Step 4: Test zero-config manually (if a kingdom is running)

If a daemon is running in a test directory:
```bash
cd /tmp/some-project
king-vassal  # should auto-discover and start without flags
```

### Step 5: Commit

```bash
git add cmd/king-vassal/main.go
git commit -m "feat: king-vassal zero-config mode — auto-discover socket, name from cwd"
```

---

## Task 5: `internal/fingerprint/contracts.go` — Auto-Integrity Patterns

**Files:**
- Create: `internal/fingerprint/contracts.go`
- Create: `internal/fingerprint/contracts_test.go`

### Step 1: Write the failing test

```go
// internal/fingerprint/contracts_test.go
package fingerprint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/fingerprint"
)

func TestDefaultContracts_Go(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeGo, dir)
	if len(contracts) == 0 {
		t.Fatal("expected at least one contract for Go projects")
	}
	found := false
	for _, c := range contracts {
		if c.Name == "go-vet-error" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected go-vet-error contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_NodeWithTest(t *testing.T) {
	dir := t.TempDir()
	// Write a package.json with a test script.
	pkg := map[string]any{
		"scripts": map[string]any{"test": "jest"},
	}
	data, _ := json.Marshal(pkg)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644)

	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeNode, dir)
	found := false
	for _, c := range contracts {
		if c.Name == "npm-test-failure" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected npm-test-failure contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_NodeWithEslint(t *testing.T) {
	dir := t.TempDir()
	pkg := map[string]any{
		"devDependencies": map[string]any{"eslint": "^8.0.0"},
	}
	data, _ := json.Marshal(pkg)
	_ = os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644)

	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeNode, dir)
	found := false
	for _, c := range contracts {
		if c.Name == "eslint-error" && c.Source == "auto" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected eslint-error contract, got: %+v", contracts)
	}
}

func TestDefaultContracts_Unknown(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeUnknown, dir)
	if len(contracts) != 0 {
		t.Errorf("expected 0 contracts for Unknown, got %d", len(contracts))
	}
}

func TestDefaultContracts_Hardware(t *testing.T) {
	dir := t.TempDir()
	contracts := fingerprint.DefaultContracts(fingerprint.ProjectTypeHardware, dir)
	if len(contracts) != 0 {
		t.Errorf("expected 0 contracts for Hardware, got %d", len(contracts))
	}
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/fingerprint/... -v -run TestDefaultContracts
```
Expected: `undefined: fingerprint.DefaultContracts`

### Step 3: Write implementation

```go
// internal/fingerprint/contracts.go
package fingerprint

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/alexli18/claude-king/internal/config"
)

// DefaultContracts returns PatternConfig integrity contracts for the project type.
// Contracts use regex to detect errors in vassal output (go vet, eslint, npm test).
// All returned contracts have Source="auto" for easy identification.
func DefaultContracts(pt ProjectType, rootDir string) []config.PatternConfig {
	switch pt {
	case ProjectTypeGo:
		return []config.PatternConfig{
			{
				Name:            "go-vet-error",
				Regex:           `^.*\.go:\d+:\d*:?\s*(vet:|SA\d+:)`,
				Severity:        "error",
				Source:          "auto",
				SummaryTemplate: "go vet error in {vassal}: {match}",
			},
		}
	case ProjectTypeNode:
		return nodeContracts(rootDir)
	default:
		return nil
	}
}

// nodeContracts inspects package.json to determine which Node contracts apply.
func nodeContracts(rootDir string) []config.PatternConfig {
	var contracts []config.PatternConfig

	pkg := readPackageJSON(rootDir)

	// npm test — if scripts.test is defined.
	if pkg != nil {
		if scripts, ok := pkg["scripts"].(map[string]any); ok {
			if _, hasTest := scripts["test"]; hasTest {
				contracts = append(contracts, config.PatternConfig{
					Name:            "npm-test-failure",
					Regex:           `(?i)^(FAIL|failing|failed)\b`,
					Severity:        "error",
					Source:          "auto",
					SummaryTemplate: "npm test failure in {vassal}: {match}",
				})
			}
		}

		// eslint — if eslint in devDependencies or dependencies.
		if hasESLint(pkg) {
			contracts = append(contracts, config.PatternConfig{
				Name:            "eslint-error",
				Regex:           `^\s+\d+:\d+\s+error\s+`,
				Severity:        "warning",
				Source:          "auto",
				SummaryTemplate: "eslint error in {vassal}: {match}",
			})
		}
	}

	return contracts
}

func readPackageJSON(rootDir string) map[string]any {
	data, err := os.ReadFile(filepath.Join(rootDir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg
}

func hasESLint(pkg map[string]any) bool {
	for _, key := range []string{"devDependencies", "dependencies"} {
		if deps, ok := pkg[key].(map[string]any); ok {
			if _, found := deps["eslint"]; found {
				return true
			}
		}
	}
	return false
}
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/fingerprint/... -v
```
Expected: all 9 tests PASS (6 from Task 2 + 5 from this task... wait, Task 2 has 6, this has 5: total 11)

Actually run all fingerprint tests:
```bash
go test ./internal/fingerprint/... -v
```
Expected: all fingerprint tests PASS

### Step 5: Commit

```bash
git add internal/fingerprint/contracts.go internal/fingerprint/contracts_test.go
git commit -m "feat: add fingerprint.DefaultContracts — auto-integrity patterns per project type"
```

---

## Task 6: Integrate Secret Scanner into `Ledger.Register`

**Files:**
- Modify: `internal/artifacts/ledger.go`
- Create: `internal/artifacts/ledger_security_test.go`

### Step 1: Write the failing test

```go
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

	// We need a kingdom ID — create one directly in the store.
	// Use store methods to insert a test kingdom.
	kingdomID := "test-kingdom-id"
	// Insert minimal kingdom record:
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

	// Create a .env file.
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET=mysecret"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("my-env", envPath, "producer-1", "")
	if err == nil {
		t.Fatal("expected error for .env file, got nil")
	}
	if !strings.Contains(err.Error(), "INVALID_SECURITY") {
		t.Errorf("expected INVALID_SECURITY in error, got: %v", err)
	}
}

func TestRegister_BlockedBySecretScanner_AWSKey(t *testing.T) {
	ledger, dir := newTestLedger(t)

	configPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(configPath, []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ledger.Register("config", configPath, "producer-1", "")
	if err == nil {
		t.Fatal("expected error for file with AWS key, got nil")
	}
	if !strings.Contains(err.Error(), "INVALID_SECURITY") {
		t.Errorf("expected INVALID_SECURITY in error, got: %v", err)
	}
}

func TestRegister_AllowedSafeFile(t *testing.T) {
	ledger, dir := newTestLedger(t)

	safePath := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(safePath, []byte("All tests passed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	art, err := ledger.Register("report", safePath, "producer-1", "")
	if err != nil {
		t.Fatalf("unexpected error for safe file: %v", err)
	}
	if art.Name != "report" {
		t.Errorf("got name %q, want %q", art.Name, "report")
	}
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/artifacts/... -v -run TestRegister_Blocked
```
Expected: PASS (scanner not yet integrated — blocked files will be registered without error)

Actually since the scanner is not integrated, these tests will FAIL because `err == nil` when it should return an error. Good — they fail correctly.

### Step 3: Modify `internal/artifacts/ledger.go`

Add the import and the scan call at the top of `Register`:

**Add import** — add `"github.com/alexli18/claude-king/internal/security"` to the import block.

**Add scan call** — insert after the file existence check (`os.Stat`) and before MIME detection:

```go
// Secret scan: block artifacts containing secrets (INVALID_SECURITY).
if result := security.Scan(filePath); result.Blocked {
    return nil, fmt.Errorf("INVALID_SECURITY: %s", result.Reason)
}
```

The modified `Register` function beginning becomes:

```go
func (l *Ledger) Register(name, filePath, producerID, mimeType string) (*store.Artifact, error) {
	// Validate file exists.
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("artifact file not found: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("artifact path is a directory, not a file: %s", filePath)
	}

	// Secret scan: block artifacts containing secrets (INVALID_SECURITY).
	if result := security.Scan(filePath); result.Blocked {
		return nil, fmt.Errorf("INVALID_SECURITY: %s", result.Reason)
	}

	// Auto-detect MIME type ... (rest unchanged)
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/artifacts/... -v
```
Expected: all artifact tests PASS

### Step 5: Commit

```bash
git add internal/artifacts/ledger.go internal/artifacts/ledger_security_test.go
git commit -m "feat: integrate secret scanner into Ledger.Register — blocks INVALID_SECURITY artifacts"
```

---

## Task 7: Daemon Auto-Integrity — Inject Contracts on Vassal Registration

**Files:**
- Modify: `internal/daemon/daemon.go` (in `startClaudeVassal` or `startVassals`)

### Step 1: Understand the integration point

The `startVassals()` function in `daemon.go` (lines 350-422) handles vassal startup. For each vassal with `repo_path`, it already loads `vassal.json`. We add fingerprinting + contract injection here.

Also `startClaudeVassal` (line 424+) handles claude-type vassals. Both paths need fingerprinting.

For this task, we add a helper method `injectAutoContracts(repoPath string)` that:
1. Calls `fingerprint.Fingerprint(repoPath)`
2. Calls `fingerprint.DefaultContracts(pt, repoPath)`
3. Merges contracts into `d.config.Patterns` (deduplicated by name)

### Step 2: Write the failing test

Add a unit test for the helper (test the merging logic in isolation by extracting it):

```go
// internal/daemon/auto_integrity_test.go
package daemon_test

import (
	"testing"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/daemon"
)

func TestMergeAutoContracts_NoOverwrite(t *testing.T) {
	existing := []config.PatternConfig{
		{Name: "my-pattern", Regex: `error`, Severity: "error"},
	}
	auto := []config.PatternConfig{
		{Name: "go-vet-error", Regex: `\.go:\d+`, Severity: "error", Source: "auto"},
		{Name: "my-pattern", Regex: `duplicate`, Severity: "warning", Source: "auto"}, // duplicate
	}

	merged := daemon.MergeAutoContracts(existing, auto)

	if len(merged) != 2 {
		t.Errorf("expected 2 patterns, got %d: %+v", len(merged), merged)
	}
	// Existing "my-pattern" must not be overwritten.
	for _, p := range merged {
		if p.Name == "my-pattern" && p.Regex == "duplicate" {
			t.Error("existing pattern was overwritten by auto contract")
		}
	}
}

func TestMergeAutoContracts_AddsNew(t *testing.T) {
	existing := []config.PatternConfig{}
	auto := []config.PatternConfig{
		{Name: "go-vet-error", Regex: `\.go:\d+`, Severity: "error", Source: "auto"},
	}

	merged := daemon.MergeAutoContracts(existing, auto)
	if len(merged) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(merged))
	}
}
```

### Step 3: Run test to verify it fails

```bash
go test ./internal/daemon/... -v -run TestMergeAutoContracts
```
Expected: `undefined: daemon.MergeAutoContracts`

### Step 4: Add `MergeAutoContracts` and `injectAutoContracts` to daemon

Add a new file to keep daemon.go clean:

```go
// internal/daemon/auto_integrity.go
package daemon

import (
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/fingerprint"
)

// MergeAutoContracts merges auto-generated contracts into existing patterns.
// Existing patterns are never overwritten (deduplication by name).
// Exported for testing.
func MergeAutoContracts(existing, auto []config.PatternConfig) []config.PatternConfig {
	names := make(map[string]bool, len(existing))
	for _, p := range existing {
		names[p.Name] = true
	}
	result := make([]config.PatternConfig, len(existing))
	copy(result, existing)
	for _, p := range auto {
		if !names[p.Name] {
			result = append(result, p)
			names[p.Name] = true
		}
	}
	return result
}

// injectAutoContracts fingerprints repoPath and merges integrity contracts
// into d.config.Patterns. Safe to call multiple times (idempotent via dedup).
func (d *Daemon) injectAutoContracts(repoPath string) {
	pt := fingerprint.Fingerprint(repoPath)
	contracts := fingerprint.DefaultContracts(pt, repoPath)
	if len(contracts) == 0 {
		return
	}
	d.config.Patterns = MergeAutoContracts(d.config.Patterns, contracts)
	d.logger.Info("auto-integrity contracts injected",
		"repo", repoPath,
		"project_type", string(pt),
		"contracts", len(contracts),
	)
	// Refresh the sieve with updated patterns.
	if d.sieve != nil {
		d.sieve.SetPatterns(d.config.Patterns)
	}
}
```

### Step 5: Call `injectAutoContracts` in `startVassals` and `startClaudeVassal`

**In `startVassals`** (line ~401, after the manifest loading block), add:
```go
// Auto-Integrity: inject contracts based on project type.
if vc.RepoPath != "" {
    repoPath := vc.RepoPath
    if !filepath.IsAbs(repoPath) {
        repoPath = filepath.Join(d.rootDir, repoPath)
    }
    d.injectAutoContracts(repoPath)
}
```

**In `startClaudeVassal`**: read the function to find where `repoPath` is resolved, then add `d.injectAutoContracts(repoPath)` after resolution.

### Step 6: Check if `sieve.SetPatterns` exists

```bash
grep -n "SetPatterns\|func.*Sieve" /Users/alex/Desktop/Claude_King/internal/events/*.go
```

If `SetPatterns` does not exist on `Sieve`, remove the `d.sieve.SetPatterns` call from `injectAutoContracts` — patterns are loaded once at startup in this implementation.

### Step 7: Run all tests

```bash
go test ./... -v 2>&1 | tail -30
```
Expected: all tests PASS

### Step 8: Commit

```bash
git add internal/daemon/auto_integrity.go internal/daemon/auto_integrity_test.go internal/daemon/daemon.go
git commit -m "feat: daemon auto-integrity — inject go-vet/eslint/npm-test contracts on vassal registration"
```

---

## Task 8: Final Validation

### Step 1: Build all binaries

```bash
go build ./...
```
Expected: no errors

### Step 2: Run all tests

```bash
go test ./... -count=1
```
Expected: all PASS

### Step 3: Vet

```bash
go vet ./...
```
Expected: no output (clean)

### Step 4: Final commit if needed

```bash
git status
# If any uncommitted changes remain:
git add -A
git commit -m "chore: quality-of-life-v2 final cleanup"
```

---

## Summary of New Files

| File | Purpose |
|---|---|
| `internal/discovery/discovery.go` | Git-style socket traversal |
| `internal/discovery/discovery_test.go` | Tests for discovery |
| `internal/fingerprint/fingerprint.go` | Project type detection |
| `internal/fingerprint/fingerprint_test.go` | Tests for fingerprint |
| `internal/fingerprint/contracts.go` | Auto-integrity pattern generation |
| `internal/fingerprint/contracts_test.go` | Tests for contracts |
| `internal/security/scanner.go` | Secret scanner |
| `internal/security/scanner_test.go` | Tests for scanner |
| `internal/daemon/auto_integrity.go` | MergeAutoContracts + injectAutoContracts |
| `internal/daemon/auto_integrity_test.go` | Tests for merging logic |

## Modified Files

| File | Change |
|---|---|
| `cmd/king-vassal/main.go` | Zero-config mode: optional flags + auto-discovery |
| `internal/artifacts/ledger.go` | Secret scan guard in Register |
| `internal/daemon/daemon.go` | Call injectAutoContracts in startVassals/startClaudeVassal |
