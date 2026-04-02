# Design: Daemon Detach Mode

**Date**: 2026-04-02
**Status**: Approved

## Problem

`king up` blocks the terminal, making it impossible to use `kingctl` or other tools in the same session.

## Solution

Add `--detach` flag to `king up` using Approach A: re-exec via `os/exec`.

## Command Behavior

| Command | Behavior |
|---|---|
| `king up` | Foreground: daemon runs in current terminal with logs (existing behavior) |
| `king up --detach` | Fork daemon to background, wait for socket, print PID and exit |
| `king down` | Send shutdown via socket (existing behavior, unchanged) |

## Implementation

### File: `cmd/king/main.go` only (~40 lines added)

1. `cmdUp()` parses `--detach` flag from `os.Args[2:]`
2. If `--detach`:
   - Launch `os/exec.Command(os.Executable(), "up", "--daemon")`
   - Set `SysProcAttr{Setsid: true}` to detach from terminal session
   - Redirect stdout/stderr to `.king/daemon.log`
   - Call `cmd.Start()` (not `Wait`)
   - Poll for socket file (50ms × 20 iterations = 1s timeout)
   - Print `Kingdom started (pid: XXXX)` and exit 0
   - On timeout: print error, exit 1
3. If `--daemon` (internal flag, not for users):
   - Run daemon as now but without signal handling
   - Daemon lives until shutdown RPC received via socket
4. No flags: existing foreground behavior unchanged

## Files Changed

- `cmd/king/main.go` — only file modified
