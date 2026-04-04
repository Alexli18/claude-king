//go:build !integration

package mcp_test

import (
	"testing"
)

func TestHandleExecIn_BlocksSecretOutput(t *testing.T) {
	// Setup a server with scanExecOutput=true and a fake PTY session
	// that returns AWS credentials in output.
	// Assert the result contains "SENSITIVE_OUTPUT_BLOCKED".
	//
	// NOTE: No fakePTYManager fixture exists in this package yet.
	// The scan logic itself is covered by TestScanContent_* in internal/security/.
	t.Skip("implement after checking existing test infrastructure in internal/mcp/")
}
