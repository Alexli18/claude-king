package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	fmt.Fprintln(os.Stderr, "error: --detach not yet implemented")
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
