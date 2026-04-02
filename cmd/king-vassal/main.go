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
