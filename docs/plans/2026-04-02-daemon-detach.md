# Daemon Detach Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `king up --detach` flag so the daemon runs in the background, freeing the terminal for `kingctl` and other tools.

**Architecture:** `king up --detach` re-execs itself as `king up --daemon` via `os/exec`, detaches from the terminal session via `Setsid`, redirects output to `.king/daemon.log`, then polls for the socket file before reporting success. The `--daemon` flag is an internal mode that disables signal handling (daemon shuts down only via `shutdown` RPC).

**Tech Stack:** Go 1.22+, `os/exec`, `syscall.SysProcAttr`, `cmd/king/main.go` only.

---

### Task 1: Add `--detach` / `--daemon` flag parsing to `cmdUp()`

**Files:**
- Modify: `cmd/king/main.go:46-84`

**Step 1: Add flag parsing at the top of `cmdUp()`**

Replace the current `cmdUp()` body start with:

```go
func cmdUp() {
	// Parse flags: --detach (user-facing) and --daemon (internal, set by --detach re-exec)
	detach := false
	daemonMode := false
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--detach":
			detach = true
		case "--daemon":
			daemonMode = true
		}
	}

	if detach {
		cmdUpDetach()
		return
	}

	rootDir, err := os.Getwd()
	// ... rest of existing code unchanged ...
```

For `--daemon` mode, remove the "Press Ctrl+C" message and signal handling — the daemon should only stop via `king down`. Replace the signal-wait block:

```go
	if daemonMode {
		// Daemon mode: block until context is cancelled by shutdown RPC.
		<-ctx.Done()
	} else {
		fmt.Println("Kingdom is running. Press Ctrl+C to stop.")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-sigCh:
			fmt.Println("\nShutting down...")
		case <-ctx.Done():
		}
	}
```

**Step 2: Verify it compiles**

```bash
go build ./cmd/king/
```
Expected: no errors.

**Step 3: Commit**

```bash
git add cmd/king/main.go
git commit -m "feat: add --detach/--daemon flag parsing to cmdUp"
```

---

### Task 2: Implement `cmdUpDetach()`

**Files:**
- Modify: `cmd/king/main.go` (add new function after `cmdUp`)

**Step 1: Add the function**

```go
func cmdUpDetach() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Check if already running.
	running, err := daemon.IsRunning(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if running {
		fmt.Println("Kingdom is already running.")
		return
	}

	// Prepare log file at .king/daemon.log
	kingDir := filepath.Join(rootDir, ".king")
	if err := os.MkdirAll(kingDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating .king dir: %v\n", err)
		os.Exit(1)
	}
	logPath := filepath.Join(kingDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening log file: %v\n", err)
		os.Exit(1)
	}

	// Re-exec self with --daemon flag, detached from terminal.
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving executable: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(exe, "up", "--daemon")
	cmd.Dir = rootDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		fmt.Fprintf(os.Stderr, "error starting daemon: %v\n", err)
		os.Exit(1)
	}
	logFile.Close()

	pid := cmd.Process.Pid

	// Poll for socket file — daemon signals readiness by creating it.
	sockPath := daemon.SocketPathForRoot(rootDir)
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if _, err := os.Stat(sockPath); err == nil {
			fmt.Printf("Kingdom started (pid: %d)\n", pid)
			fmt.Printf("Logs: %s\n", logPath)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "error: daemon did not start within 1s (check %s)\n", logPath)
	os.Exit(1)
}
```

**Step 2: Add missing imports to `cmd/king/main.go`**

Add to the import block:
```go
"os/exec"
"path/filepath"
"time"

"github.com/alexli18/claude-king/internal/daemon"
```

(Note: `daemon` import may already be present — check and deduplicate.)

**Step 3: Export `SocketPathForRoot` in `internal/daemon/daemon.go`**

The function `socketPathForRoot` is currently unexported. Rename it:

```go
// SocketPathForRoot returns a unique socket path based on the root directory hash.
func SocketPathForRoot(rootDir string) string {
	h := sha256.Sum256([]byte(rootDir))
	return filepath.Join(rootDir, kingDirName, fmt.Sprintf("king-%x.sock", h[:8]))
}
```

And update all internal callers (`pidPathForRoot` and `NewDaemon`) to use `SocketPathForRoot`.

**Step 4: Verify it compiles**

```bash
go build ./cmd/king/ && go build ./cmd/kingctl/
```
Expected: no errors.

**Step 5: Commit**

```bash
git add cmd/king/main.go internal/daemon/daemon.go
git commit -m "feat: implement king up --detach via re-exec with Setsid"
```

---

### Task 3: Manual end-to-end test

**Step 1: Build fresh binaries**

```bash
go build -o king ./cmd/king && go build -o kingctl ./cmd/kingctl
```

**Step 2: Test `--detach` mode**

```bash
./king up --detach
```
Expected output:
```
Kingdom started (pid: XXXXX)
Logs: /path/to/project/.king/daemon.log
```

**Step 3: Verify daemon is running**

```bash
./kingctl status
```
Expected:
```
Kingdom: Claude_King (running)
Vassals:
  shell  running  ...
```

**Step 4: Stop daemon**

```bash
./king down
```
Expected: `Shutdown signal sent.`

**Step 5: Verify daemon stopped**

```bash
./kingctl status
```
Expected: `error: cannot connect to daemon` (or similar "not running" message).

**Step 6: Test foreground mode still works**

```bash
./king up
# In another terminal:
./kingctl status
# Back in first terminal: Ctrl+C
```
Expected: foreground mode unchanged.

**Step 7: Commit test confirmation (no code changes needed)**

```bash
git commit --allow-empty -m "test: manually verified king up --detach end-to-end"
```

---

## Summary of changes

| File | Change |
|---|---|
| `cmd/king/main.go` | Parse `--detach`/`--daemon`, add `cmdUpDetach()`, add imports |
| `internal/daemon/daemon.go` | Export `socketPathForRoot` → `SocketPathForRoot` |
