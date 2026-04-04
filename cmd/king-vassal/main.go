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

	"github.com/alexli18/claude-king/internal/daemon"
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
	model := flag.String("model", "", "claude model to use for task execution (empty = claude default)")
	stdio := flag.Bool("stdio", false, "serve MCP over stdio (for Claude Code .mcp.json)")
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
		kingdoms, discErr := discovery.FindAllKingdomSockets(cwd)
		if discErr != nil {
			fmt.Fprintln(os.Stderr, "error: No Kingdom found. Run king up first.")
			os.Exit(1)
		}
		switch len(kingdoms) {
		case 1:
			*kingSocket = kingdoms[0].SocketPath
		default:
			if *stdio {
				fmt.Fprintf(os.Stderr, "error: multiple kingdoms found, use --king-sock to specify one\n")
				for _, k := range kingdoms {
					fmt.Fprintf(os.Stderr, "  %s (%s)\n", k.Name, k.RootDir)
				}
				os.Exit(1)
			}
			// Interactive selection — only available when stdin is a terminal.
			fi, _ := os.Stdin.Stat()
			if (fi.Mode() & os.ModeCharDevice) == 0 {
				fmt.Fprintln(os.Stderr, "error: multiple kingdoms found but stdin is not a terminal, use --king-sock")
				os.Exit(1)
			}
			fmt.Println("Found multiple kingdoms:")
			for i, k := range kingdoms {
				fmt.Printf("  %d. %-20s (%s)\n", i+1, k.Name, k.RootDir)
			}
			fmt.Printf("\nSelect kingdom [1-%d]: ", len(kingdoms))
			var choice int
			if _, err := fmt.Scan(&choice); err != nil || choice < 1 || choice > len(kingdoms) {
				fmt.Fprintln(os.Stderr, "error: invalid selection")
				os.Exit(1)
			}
			*kingSocket = kingdoms[choice-1].SocketPath
		}
	}

	// Fingerprint the project and log the type (used by daemon for Auto-Integrity).
	pt := fingerprint.Fingerprint(*repoPath)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With(
		"component", "king-vassal",
		"name", *name,
		"project_type", string(pt),
	)

	srv := vassal.NewVassalServer(*name, *repoPath, *kingDir, *kingSocket, *timeoutMin, *model, logger)

	// Register with King daemon (best-effort, non-fatal, async to avoid deadlock during daemon startup).
	if *kingSocket != "" {
		go func() {
			client, err := daemon.NewClientFromSocket(*kingSocket)
			if err != nil {
				logger.Warn("vassal registration failed: cannot connect to king daemon",
					"error", err,
					"socket", *kingSocket,
				)
				return
			}
			if _, err := client.Call("vassal.register", map[string]interface{}{
				"name":      *name,
				"repo_path": *repoPath,
				"socket":    filepath.Join(*kingDir, "vassals", *name+".sock"),
				"pid":       os.Getpid(),
			}); err != nil {
				logger.Warn("vassal registration RPC call failed",
					"error", err,
					"vassal", *name,
				)
			}
			client.Close()
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if *stdio {
		if err := srv.StartStdio(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
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
}
