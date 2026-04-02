package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alexli18/claude-king/internal/daemon"
	"github.com/alexli18/claude-king/internal/tui"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "up":
		cmdUp()
	case "down":
		cmdDown()
	case "mcp":
		cmdMCP()
	case "dashboard":
		cmdDashboard()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: king <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  up [--detach]  Start the Kingdom daemon")
	fmt.Fprintln(os.Stderr, "  down    Stop the Kingdom daemon")
	fmt.Fprintln(os.Stderr, "  mcp     Start MCP server on stdio (for Claude Code)")
	fmt.Fprintln(os.Stderr, "  dashboard  Open the TUI dashboard")
}

func cmdUp() {
	// Parse --detach and --daemon flags from arguments.
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
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	d, err := daemon.NewDaemon(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if daemonMode {
		// Daemon mode: block until context is cancelled.
		// When "king down" sends the shutdown RPC, the daemon's "shutdown"
		// handler calls d.cancel() (see daemon.go registerRealHandlers /
		// registerStubHandlers). That cancel func is the same one bound to
		// ctx here, so <-ctx.Done() unblocks and the defer cancel() / Stop()
		// sequence below runs correctly.
		<-ctx.Done()
	} else {
		fmt.Println("Kingdom is running. Press Ctrl+C to stop.")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			fmt.Println("\nShutting down...")
		case <-ctx.Done():
		}
	}

	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "error stopping: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Kingdom stopped.")
}

func cmdUpDetach() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Pre-check for UX only: avoids spawning a child that will immediately
	// fail. Not a hard guard — the daemon's own Start() re-checks internally.
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
		logFile.Close()
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

	pid := cmd.Process.Pid // capture BEFORE Release() zeroes it

	// Release parent's reference to the child process so it doesn't become a
	// zombie if the poll loop times out. The daemon is now fully detached.
	if err := cmd.Process.Release(); err != nil {
		// Non-fatal: log but continue — the daemon is running.
		fmt.Fprintf(os.Stderr, "warning: could not release daemon process: %v\n", err)
	}

	// Poll for socket file — daemon signals readiness by creating it.
	sockPath := daemon.SocketPathForRoot(rootDir)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			fmt.Printf("Kingdom started (pid: %d)\n", pid)
			fmt.Printf("Logs: %s\n", logPath)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Fprintf(os.Stderr, "error: daemon (pid: %d) did not start within 1s (check %s)\n", pid, logPath)
	os.Exit(1)
}

func cmdDown() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	running, err := daemon.IsRunning(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if !running {
		fmt.Println("No Kingdom is running in this directory.")
		return
	}

	// Connect to the daemon socket and send shutdown command.
	client, err := daemon.NewClient(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	_, err = client.Call("shutdown", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Shutdown signal sent.")
}

func cmdMCP() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	d, err := daemon.NewDaemon(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Start MCP server on stdio.
	if err := d.MCPServer().Start(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
	}

	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "error stopping: %v\n", err)
	}
}

func cmdDashboard() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	running, err := daemon.IsRunning(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if !running {
		fmt.Fprintln(os.Stderr, "No Kingdom is running in this directory.")
		fmt.Fprintln(os.Stderr, "Start one with: king up")
		os.Exit(1)
	}

	client, err := daemon.NewClient(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to daemon: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	p := tui.NewApp(client)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dashboard error: %v\n", err)
		os.Exit(1)
	}
}
