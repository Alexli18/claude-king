# Design: Quality of Life v2 — Zero-Config Onboarding, Secret Scanner, Auto-Integrity

**Date:** 2026-04-02
**Status:** Approved
**Approach:** B — Separation of Concerns (three new internal packages)

---

## Overview

Three interconnected features that improve developer experience and security:

1. **Zero-Config Onboarding** — `king-vassal` discovers daemon socket via git-style traversal and self-registers without flags
2. **Secret Scanner** — `Ledger.Register` blocks artifacts containing secrets before DB write
3. **Auto-Integrity** — Project fingerprinting adds appropriate integrity contracts automatically

---

## Architecture

### New Packages

```
internal/
  discovery/
    discovery.go       # FindKingdomSocket: git-style traversal
  fingerprint/
    fingerprint.go     # Fingerprint: project type detection
    contracts.go       # DefaultContracts: integrity patterns per project type
  security/
    scanner.go         # Scan: filename blacklist + smart content regex
```

### Modified Files

- `cmd/king-vassal/main.go` — zero-config mode (optional flags, auto-discovery fallback)
- `internal/artifacts/ledger.go` — call `security.Scan` before artifact registration
- `internal/vassal/server.go` — pass project type from fingerprint to daemon on registration
- `internal/daemon/` — store project type, inject auto-integrity contracts on vassal register

---

## Feature 1: Zero-Config Onboarding

### Socket Discovery (`internal/discovery/discovery.go`)

```go
var ErrNoKingdom = errors.New("no Kingdom found: run king-daemon init first")

// FindKingdomSocket walks from startDir up to / looking for .king/daemon.sock.
// Returns the socket path and the kingdom root directory.
func FindKingdomSocket(startDir string) (socketPath, rootDir string, err error)
```

- Walks parent directories until `.king/daemon.sock` is found or `/` is reached
- Returns `ErrNoKingdom` if not found — clear error message for the user

### `king-vassal` Zero-Config Mode

All three flags (`--name`, `--repo`, `--king-sock`) become optional:
- If `--king-sock` is absent → call `discovery.FindKingdomSocket(cwd)`
- If `--name` is absent → `filepath.Base(cwd)`
- If `--repo` is absent → `cwd`

Error behavior: if socket not found, print `"No Kingdom found. Run king-daemon init first"` and exit 1.

### Project Fingerprinting (`internal/fingerprint/fingerprint.go`)

```go
type ProjectType string

const (
    ProjectTypeGo       ProjectType = "go"
    ProjectTypeNode     ProjectType = "node"
    ProjectTypeHardware ProjectType = "hardware"
    ProjectTypeUnknown  ProjectType = "unknown"
)

// Fingerprint detects the project type by inspecting rootDir.
func Fingerprint(rootDir string) ProjectType
```

Detection rules (first match wins):
1. `go.mod` exists → `Go`
2. `package.json` exists → `Node`
3. `*.ino` or (`*.c` + `Makefile`) exists → `Hardware`
4. Otherwise → `Unknown`

Fingerprint is called immediately after socket discovery. Result is sent to daemon during vassal registration.

---

## Feature 2: Secret Scanner

### Package `internal/security/scanner.go`

```go
type ScanResult struct {
    Blocked bool
    Reason  string // e.g. "filename:blacklisted:.env" or "content:aws_key"
}

// Scan checks filePath for secrets via filename blacklist and smart content scan.
func Scan(filePath string) ScanResult
```

### Filename Blacklist (checked without reading file content)

Blocked filenames/patterns:
- `.env`, `*.env`, `.env.*`
- `*.pem`, `*.key`, `*.p12`, `*.pfx`
- `id_rsa`, `id_ed25519`, `id_ecdsa`, `id_dsa`
- `*.history`, `.bash_history`, `.zsh_history`
- `secrets.json`, `credentials.*`, `*.credentials`

### Smart Content Scan

Applied only to:
- Text file extensions: `.json`, `.yaml`, `.yml`, `.conf`, `.txt`, `.sh`, `.toml`, `.env`
- File size ≤ 1MB

Regex patterns:
```
AWS_ACCESS_KEY_ID\s*[=:]\s*[A-Z0-9]{16,}
AWS_SECRET_ACCESS_KEY\s*[=:]\s*[A-Za-z0-9/+=]{32,}
GITHUB_TOKEN\s*[=:]\s*[A-Za-z0-9_]{20,}
ghp_[A-Za-z0-9]{36}
ghs_[A-Za-z0-9]{36}
sk-[A-Za-z0-9]{48}
-----BEGIN (RSA|EC|OPENSSH|DSA) PRIVATE KEY-----
```

### Fail-Fast Integration in `Ledger.Register`

```go
func (l *Ledger) Register(name, filePath, producerID, mimeType string) (*store.Artifact, error) {
    // Secret scan before ANY other processing
    if result := security.Scan(filePath); result.Blocked {
        return nil, fmt.Errorf("INVALID_SECURITY: %s", result.Reason)
    }
    // ... existing logic
}
```

Artifact is never written to DB if scan fails.

---

## Feature 3: Auto-Integrity

### Package `internal/fingerprint/contracts.go`

```go
// DefaultContracts returns integrity PatternConfigs for the detected project type.
// Checks rootDir for actual presence of scripts (e.g., package.json scripts.test).
func DefaultContracts(pt ProjectType, rootDir string) []config.PatternConfig
```

Rules:
- `Go` → `go vet ./...` (severity: error)
- `Node` + `scripts.test` in package.json → `npm test` (severity: error)
- `Node` + `eslint` in devDependencies → `npx eslint .` (severity: warning)
- `Hardware` / `Unknown` → no contracts

### Daemon Integration

When daemon handles vassal auto-registration:
1. Calls `fingerprint.Fingerprint(repoPath)` → stores `project_type` on vassal record
2. Calls `fingerprint.DefaultContracts(projectType, repoPath)`
3. Merges auto-contracts into `KingdomConfig.Patterns`, tagging each with `source: auto`
4. Never overwrites patterns already present (name deduplication)

Auto-contracts are processed by the existing Pattern evaluation system — no new execution pipeline required.

---

## Data Flow

```
king-vassal (zero-config)
  → discovery.FindKingdomSocket(cwd)         # find .king/daemon.sock
  → fingerprint.Fingerprint(cwd)             # detect Go/Node/Hardware
  → connect to daemon socket
  → register_vassal(name, repo, projectType)
      daemon: store project_type on vassal
      daemon: DefaultContracts → append to KingdomConfig.Patterns (source: auto)

king-vassal (artifact via MCP tool)
  → Ledger.Register(name, filePath, ...)
      security.Scan(filePath)                 # check before DB write
      → blocked? return INVALID_SECURITY
      → ok? checksum + DB write
```

---

## Error Handling

| Scenario | Behavior |
|---|---|
| `.king/daemon.sock` not found | Exit 1: "No Kingdom found. Run king-daemon init first" |
| Daemon socket found but not listening | `dial unix: connection refused` — propagated as-is |
| Artifact blocked by scanner | `INVALID_SECURITY: <reason>` — artifact not stored |
| Binary artifact exceeds 1MB | Skip content scan, check filename only |
| `go.mod` and `package.json` both present | Go takes priority (first-match wins) |

---

## Testing

- `discovery`: table-driven tests with temp directory trees
- `fingerprint`: test each ProjectType detection with fixture dirs
- `security`: test each blacklist entry and each regex pattern
- `Ledger.Register`: integration test — verify blocked artifacts return error and are absent from DB
- `king-vassal main`: test zero-config flag fallback logic
