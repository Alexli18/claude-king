package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexli18/claude-king/internal/daemon"
	"github.com/alexli18/claude-king/internal/registry"
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
	case "list":
		cmdList()
	case "status":
		cmdStatus()
	case "prompt-info":
		cmdPromptInfo()
	case "doctor":
		cmdDoctor()
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
	fmt.Fprintln(os.Stderr, "  down [--force] Stop the Kingdom daemon (--force kills zombie processes)")
	fmt.Fprintln(os.Stderr, "  mcp            Start MCP server on stdio (for Claude Code)")
	fmt.Fprintln(os.Stderr, "  dashboard      Open the TUI dashboard")
	fmt.Fprintln(os.Stderr, "  list           List all registered kingdoms")
	fmt.Fprintln(os.Stderr, "  status         Show current kingdom status and ID")
	fmt.Fprintln(os.Stderr, "  prompt-info    Output kingdom name:id8 for shell prompt (safe for PS1)")
	fmt.Fprintln(os.Stderr, "  doctor         Check kingdom health")
}

// registryPath returns the path to the global King P2P registry file.
// Creates the ~/.king directory if it does not exist.
func registryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".king")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "registry.json")
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
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
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

	reg := registry.NewRegistry(registryPath())
	sockPath := daemon.SocketPathForRoot(rootDir)
	_ = reg.Register(rootDir, registry.Entry{
		Socket: sockPath,
		PID:    os.Getpid(), // foreground: daemon IS this process
		Name:   filepath.Base(rootDir),
	})
	defer func() {
		_ = reg.Unregister(rootDir)
	}()

	if daemonMode {
		// Daemon mode: block until either a signal or a remote shutdown RPC.
		// SIGTERM/SIGINT handling is required here because `kill <pid>` or
		// `pkill king` would otherwise terminate the process without calling
		// d.Stop(), leaving king-vassal child processes running as zombies.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
		case <-d.Done():
		}
	} else {
		fmt.Println("Kingdom is running. Press Ctrl+C to stop.")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			fmt.Println("\nShutting down...")
		case <-d.Done():
			// Daemon was shut down remotely via king down.
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
			reg := registry.NewRegistry(registryPath())
			_ = reg.Register(rootDir, registry.Entry{
				Socket: sockPath,
				PID:    pid,
				Name:   filepath.Base(rootDir),
			})
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

	// --force: kill all king / king-vassal processes and clean up files,
	// even when no daemon is reachable (handles zombie processes from crashes).
	force := len(os.Args) > 2 && os.Args[2] == "--force"
	if force {
		cmdDownForce(rootDir)
		return
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

	// Wait for daemon to fully stop (socket disappears)
	sockPath := daemon.SocketPathForRoot(rootDir)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	reg := registry.NewRegistry(registryPath())
	_ = reg.Unregister(rootDir)
}

// cmdDownForce kills all king daemon and king-vassal processes for this
// rootDir, then cleans up stale socket/PID files. Use when the daemon is
// unreachable but child processes are still running (zombie scenario).
func cmdDownForce(rootDir string) {
	kingDir := filepath.Join(rootDir, ".king")

	// Read all king-*.pid files and kill the processes.
	pidFiles, _ := filepath.Glob(filepath.Join(kingDir, "king-*.pid"))
	for _, pf := range pidFiles {
		data, err := os.ReadFile(pf)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err == nil {
			if err := proc.Signal(syscall.SIGTERM); err == nil {
				fmt.Printf("Sent SIGTERM to daemon PID %d\n", pid)
			}
		}
		_ = os.Remove(pf)
	}

	// Give processes a moment to exit, then clean up sockets.
	time.Sleep(500 * time.Millisecond)
	sockFiles, _ := filepath.Glob(filepath.Join(kingDir, "king-*.sock"))
	for _, sf := range sockFiles {
		_ = os.Remove(sf)
		fmt.Printf("Removed socket %s\n", sf)
	}
	// Clean vassal sockets.
	vassalSocks, _ := filepath.Glob(filepath.Join(kingDir, "vassals", "*.sock"))
	for _, sf := range vassalSocks {
		_ = os.Remove(sf)
	}

	reg := registry.NewRegistry(registryPath())
	_ = reg.Unregister(rootDir)

	fmt.Println("Force shutdown complete.")
	fmt.Println("Note: king-vassal child processes were managed by the daemon.")
	fmt.Println("If any remain, run: pkill -f 'king-vassal --name'")
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
		if !strings.Contains(err.Error(), "kingdom already running") {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		// Daemon already running — attach to existing state for MCP-only mode.
		if err := d.Attach(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	// Start MCP server on stdio.
	if err := d.MCPServer().Start(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
	}

	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "error stopping: %v\n", err)
	}
}

func cmdList() {
	reg := registry.NewRegistry(registryPath())
	entries, err := reg.ListAlive()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading registry: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No kingdoms registered.")
		return
	}

	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	fmt.Printf("%-45s  %-12s  %-8s  %s\n", "KINGDOM", "STATUS", "PID", "ID")

	for _, path := range paths {
		e := entries[path]
		status := "running"
		if !e.Reachable {
			status = "unreachable"
		}

		shortID := "--------"
		if e.Reachable {
			idClient, err := daemon.NewClient(path)
			if err == nil {
				if raw, err := idClient.Call("kingdom.status", nil); err == nil {
					var st struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(raw, &st) == nil && len(st.ID) >= 8 {
						shortID = st.ID[:8]
					}
				}
				idClient.Close()
			}
		}

		fmt.Printf("%-45s  %-12s  %-8d  %s\n", path, status, e.PID, shortID)

		if !e.Reachable {
			fmt.Println("  vassals: (kingdom unreachable)")
			continue
		}

		// Query vassals from the daemon.
		client, err := daemon.NewClient(path)
		if err != nil {
			fmt.Println("  vassals: (none)")
			continue
		}
		raw, err := client.Call("vassal.list", nil)
		client.Close()
		if err != nil {
			fmt.Println("  vassals: (none)")
			continue
		}
		var resp struct {
			Vassals []struct {
				Name          string `json:"name"`
				RepoPath      string `json:"repo_path"`
				Alive         bool   `json:"alive"`
				Delegated     bool   `json:"delegated"`
				ControllerPID int    `json:"controller_pid"`
				HeartbeatAgeS int    `json:"heartbeat_age_s"`
			} `json:"vassals"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Vassals) == 0 {
			fmt.Println("  vassals: (none)")
			continue
		}
		fmt.Println("  vassals:")
		fmt.Printf("  %-20s %-12s %s\n", "VASSAL", "STATUS", "CONTROLLER")
		for _, v := range resp.Vassals {
			status := "running"
			if !v.Alive {
				status = "dead"
			}
			controller := "daemon"
			if v.Delegated {
				status = "DELEGATED"
				controller = fmt.Sprintf("Claude[PID %d] (%ds ago)", v.ControllerPID, v.HeartbeatAgeS)
			}
			fmt.Printf("    %-20s %-12s %s\n", v.Name, status, controller)
		}
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

func cmdStatus() {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	client, err := daemon.NewClient(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no kingdom running here: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	raw, err := client.Call("kingdom.status", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Root   string `json:"root"`
		PID    int    `json:"pid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Kingdom: %s\n", resp.Name)
	fmt.Printf("ID:      %s\n", resp.ID)
	fmt.Printf("Root:    %s\n", resp.Root)
	fmt.Printf("Status:  %s\n", resp.Status)
	fmt.Printf("PID:     %d\n", resp.PID)
}

func cmdPromptInfo() {
	rootDir, err := os.Getwd()
	if err != nil {
		os.Exit(0) // silent — safe for prompt
	}
	client, err := daemon.NewClient(rootDir)
	if err != nil {
		os.Exit(0) // no kingdom, silent
	}
	defer client.Close()

	raw, err := client.Call("kingdom.status", nil)
	if err != nil {
		os.Exit(0)
	}
	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		os.Exit(0)
	}
	shortID := resp.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	fmt.Printf("👑 %s:%s\n", resp.Name, shortID)
}

func cmdDoctor() {
	allOK := true
	check := func(label string, pass bool, hint string) {
		if pass {
			fmt.Printf("  ✓ %s\n", label)
		} else {
			fmt.Printf("  ✗ %s\n    hint: %s\n", label, hint)
			allOK = false
		}
	}

	fmt.Println("King Doctor — checking kingdom health...")
	fmt.Println()

	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// 1. .king directory exists
	kingDir := filepath.Join(rootDir, ".king")
	_, err = os.Stat(kingDir)
	check(".king directory exists", err == nil, "run 'king up' to initialize the kingdom")

	// 2. kingdom.yml exists
	cfgPath := filepath.Join(kingDir, "kingdom.yml")
	_, err = os.Stat(cfgPath)
	check("kingdom.yml found", err == nil, "create .king/kingdom.yml — see README for examples")

	// 3. Daemon socket exists
	sockPath := daemon.SocketPathForRoot(rootDir)
	_, err = os.Stat(sockPath)
	socketExists := err == nil
	check("daemon socket exists", socketExists, "run 'king up' to start the daemon")

	// 4. Daemon responds
	if socketExists {
		client, connErr := daemon.NewClient(rootDir)
		responds := connErr == nil
		check("daemon responds to connections", responds, "socket exists but daemon is not responding — try 'king down --force && king up'")
		if responds {
			client.Close()
		}
	} else {
		check("daemon responds to connections", false, "start the daemon first with 'king up'")
	}

	// 5. king-vassal in PATH
	_, kvErr := exec.LookPath("king-vassal")
	check("king-vassal binary in PATH", kvErr == nil, "run 'make install' or 'make install-user' to install binaries")

	// 6. kingctl in PATH
	_, kctlErr := exec.LookPath("kingctl")
	check("kingctl binary in PATH", kctlErr == nil, "run 'make install' or 'make install-user' to install binaries")

	fmt.Println()
	if allOK {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("Some checks failed — see hints above.")
		os.Exit(1)
	}
}
