package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexli18/claude-king/internal/vassal"
)

func main() {
	name := flag.String("name", "", "vassal name (required)")
	repoPath := flag.String("repo", "", "path to vassal repo (required)")
	kingDir := flag.String("king-dir", ".king", "path to .king directory")
	kingSocket := flag.String("king-sock", "", "path to king daemon socket (required)")
	timeoutMin := flag.Int("timeout", 10, "task timeout in minutes")
	flag.Parse()

	if *name == "" || *repoPath == "" || *kingSocket == "" {
		fmt.Fprintln(os.Stderr, "error: --name, --repo, and --king-sock are required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "king-vassal", "name", *name)

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
	case <-ctx.Done():
	}
}
