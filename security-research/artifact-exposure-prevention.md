# Artifact Exposure Prevention

## Summary

Claude King scans all artifacts submitted to the Ledger for secrets before storage, using a combination of filename blocklists, extension blocklists, filename prefix patterns, glob patterns, and regex-based content scanning. This is a meaningful safeguard, but it covers only the artifact storage path and not command arguments, event payloads, or `exec_in` output.

## Scope

- Artifact secret scanning
- Ledger storage path
- Content scanning coverage and limits

## Relevant Components

- `internal/security/scanner.go` — `Scan(filePath string) ScanResult`: the core scanning function; checks filename blocklist, extension blocklist, prefix patterns, glob patterns, and regex content patterns
- `internal/artifacts/ledger.go` — `Ledger.Register()`: calls `security.Scan()` at line 58 before accepting an artifact; also supports an optional external scanner binary configured via `Settings.SecurityScanner`
- `internal/mcp/tools.go` — `handleExecIn()`: returns PTY output to caller without scanning; `handleDispatchTask()`: forwards task strings to vassals without scanning

## Risk Description

The scanner covers artifacts stored in the Ledger. It does not scan:

- Command strings passed to `exec_in`
- PTY output returned from `exec_in`
- Event payloads emitted to the event bus
- Task descriptions dispatched to vassals via `dispatch_task`

An AI model could receive a secret (e.g. an AWS key) in `exec_in` output and forward it in a subsequent `register_artifact` call — but the Ledger scan would catch it there. However, if the secret is embedded in a task description or event payload, it bypasses the scanner entirely.

## Existing Safeguards

**Filename blocklist** (`blockedNames` in `internal/security/scanner.go`):
- `.env`, `id_rsa`, `id_ed25519`, `id_ecdsa`, `id_dsa`, `.bash_history`, `.zsh_history`, `secrets.json`

**Extension blocklist** (`blockedExtensions`):
- `.pem`, `.key`, `.p12`, `.pfx`, `.credentials`

**Filename prefix patterns** (`blockedFilenamePatterns`):
- `credentials.` — matches `credentials.json`, `credentials.yml`, etc.

**Filename glob patterns**:
- `*.env` (by extension check) and `.env.*` (by prefix check)

**Content scanning** (regex patterns applied to text files with eligible extensions, limited to files ≤ 1MB):
- AWS access key ID: `(?i)AWS_ACCESS_KEY_ID\s*[=:]\s*[A-Z0-9]{16,}`
- AWS secret access key: `(?i)AWS_SECRET_ACCESS_KEY\s*[=:]\s*[A-Za-z0-9/+=]{32,}`
- GitHub token env var: `(?i)GITHUB_TOKEN\s*[=:]\s*[A-Za-z0-9_]{20,}`
- GitHub personal access token: `ghp_[A-Za-z0-9]{36}`
- GitHub server-to-server token: `ghs_[A-Za-z0-9]{36}`
- OpenAI API key: `sk-[A-Za-z0-9]{48}`
- PEM private key headers: `-----BEGIN (RSA|EC|OPENSSH|DSA) PRIVATE KEY-----`

**External scanner plugin**: `Ledger.Register()` supports an optional external binary (`Settings.SecurityScanner`); fails-open on timeout (5 second limit) or missing binary, so it does not create a DoS vector.

**Size and type limits**: content scanning is skipped for non-text extensions and files >1MB (`maxContentScanSize = 1 << 20`), preventing DoS via large artifact submission.

## Gaps

- `exec_in` output is not scanned. A command like `cat ~/.aws/credentials` returns secret content directly to the caller via `handleExecIn` in `internal/mcp/tools.go`.
- Task descriptions dispatched via `dispatch_task` are not scanned.
- The scanner regex for `GITHUB_TOKEN` env var (`[A-Za-z0-9_]{20,}`) may produce false positives on long identifiers that happen to be assigned to that variable name.
- No test coverage for `maxContentScanSize` boundary behavior (files exactly at 1MB).
- The external scanner fails-open on timeout and on binary-not-found. This is the correct production behavior, but operators should be aware that a misconfigured or unavailable external scanner does not block artifacts.

## Recommendations

1. **Scan exec_in output** before returning it to the caller. Reuse `security.Scan()` on a temp file or extend the scanner to accept `io.Reader`.
2. **Add integration tests** that verify secrets in `exec_in` output trigger a warning or are redacted.
3. **Consider scanning task descriptions** for the highest-sensitivity patterns (AWS keys, private keys) in `handleDispatchTask` — a lightweight string check without the full file scanner overhead.
4. **Document scanner limitations** in the README Security section so operators understand what is and is not covered, including the fail-open behavior of the external scanner.
