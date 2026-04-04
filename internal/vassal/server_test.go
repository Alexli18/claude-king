package vassal_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/vassal"
)

func TestStartStdio_RespondsToInitialize(t *testing.T) {
	exec, _ := vassal.NewExecutor("claude", "")
	srv := vassal.NewVassalServer("test", t.TempDir(), ".king", "", 1, exec, slog.Default())

	initMsg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := strings.NewReader(initMsg)
	var out strings.Builder

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.StartStdio(ctx, in, &out)
	}()

	// Give it time to process the request before cancelling
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-errCh

	if !strings.Contains(out.String(), `"protocolVersion"`) {
		t.Errorf("expected MCP initialize response, got: %s", out.String())
	}
}
