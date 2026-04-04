# Multi-AI Vassal Support — Design Document

**Date:** 2026-04-04
**Status:** Approved

## Goal

Allow King kingdoms to use Codex CLI and Gemini CLI as full vassals alongside Claude, enabling mixed kingdoms where a sovereign routes tasks to different AIs based on specialization.

## Architecture

Single `king-vassal` binary receives a new `--executor claude|codex|gemini` flag. The daemon passes the flag when launching vassals based on `type:` in `kingdom.yml`.

```
kingdom.yml          daemon               king-vassal binary
──────────────       ─────────────────    ──────────────────────────────
type: claude    →    launchAIVassal()  →  --executor claude → ClaudeExecutor
type: codex     →         ↑           →  --executor codex  → CodexExecutor
type: gemini    →         ↑           →  --executor gemini → GeminiExecutor
```

`VassalServer` holds `executor AIExecutor` instead of `model string`. `runTask()` calls `executor.RunTask(ctx, prompt, repoPath)` with no knowledge of the underlying AI.

Everything else unchanged: MCP socket, `dispatch_task`, `get_task_status`, `abort_task`, timeouts, VASSAL.md flow.

## Interface & Implementations

**`internal/vassal/executor.go`** — interface + factory:
```go
type AIExecutor interface {
    RunTask(ctx context.Context, prompt, repoPath string) (string, error)
}

func NewExecutor(executorType, model string) AIExecutor
```

**`executor_claude.go`:**
```go
cmd := exec.CommandContext(ctx, "claude",
    "-p", prompt,
    "--dangerously-skip-permissions",
    "--output-format", "text",
    "--mcp-config", `{"mcpServers":{}}`,
    "--strict-mcp-config",
)
cmd.Dir = repoPath
```

**`executor_codex.go`:**
```go
cmd := exec.CommandContext(ctx, "codex", "exec", prompt, "--full-auto")
cmd.Dir = repoPath
```

**`executor_gemini.go`:**
```go
// VASSAL.md content is prepended to prompt (Gemini doesn't auto-read working dir files)
cmd := exec.CommandContext(ctx, "gemini", "-p", fullPrompt)
cmd.Dir = repoPath
```

**VASSAL.md handling:**
- Claude: reads it automatically from working dir
- Codex: reads working dir files automatically
- Gemini: VASSAL.md content prepended to prompt in `RunTask`

## Config Changes

```go
// internal/config/types.go
type VassalConfig struct {
    // ...existing fields...
    Type           string `yaml:"type,omitempty"`           // "shell"|"claude"|"codex"|"gemini"
    Model          string `yaml:"model,omitempty"`          // reused by all AI executors
    Specialization string `yaml:"specialization,omitempty"` // routing hint for sovereign
}
```

**Example mixed kingdom:**
```yaml
vassals:
  - name: claude-architect
    type: claude
    repo_path: backend/
    specialization: "system design, code review, complex refactoring"

  - name: codex-coder
    type: codex
    repo_path: backend/
    model: o4-mini
    specialization: "TypeScript, React, frontend implementation, bug fixes"

  - name: gemini-analyst
    type: gemini
    repo_path: data/
    model: gemini-2.0-flash
    specialization: "data analysis, SQL, Python, large file processing"
```

`list_vassals` returns `specialization` in response — sovereign sees it and routes accordingly.

## Daemon Changes

`launchClaudeVassal` → `launchAIVassal`, passes `--executor` flag:

```go
func (d *Daemon) launchAIVassal(v config.VassalConfig) (*exec.Cmd, error) {
    args := []string{
        "--name", v.Name,
        "--executor", v.TypeOrDefault(), // "claude"|"codex"|"gemini"
        "--repo", v.RepoPath,
        "--king-dir", kingDir,
        "--king-sock", d.sockPath,
        "--timeout", "10",
    }
}
```

Launch condition in `Start()`:
```go
// was: if v.TypeOrDefault() == "claude"
switch v.TypeOrDefault() {
case "claude", "codex", "gemini":
    if err := d.startAIVassal(v); err != nil { ... }
}
```

## Files Changed

| File | Action |
|------|--------|
| `internal/config/types.go` | Add `Specialization` field |
| `internal/vassal/executor.go` | NEW: `AIExecutor` interface + `NewExecutor` factory |
| `internal/vassal/executor_claude.go` | NEW: `ClaudeExecutor` (extracted from `runTask`) |
| `internal/vassal/executor_codex.go` | NEW: `CodexExecutor` |
| `internal/vassal/executor_gemini.go` | NEW: `GeminiExecutor` |
| `internal/vassal/server.go` | Replace `model string` → `executor AIExecutor`; simplify `runTask` |
| `cmd/king-vassal/main.go` | Add `--executor` flag; create executor via `NewExecutor` |
| `internal/daemon/daemon.go` | Rename `launchClaudeVassal` → `launchAIVassal`; expand type switch |
