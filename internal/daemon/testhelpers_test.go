package daemon

import (
	"log/slog"
	"testing"
)

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}
