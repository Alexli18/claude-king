package vassal

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
)

// claudeExecutor runs tasks using the Claude Code CLI ("claude -p ...").
type claudeExecutor struct {
	model string
}

// claudeJSONResponse is the structure returned by claude --output-format json.
type claudeJSONResponse struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}

// RunTask runs claude headless with the given prompt in repoPath.
// Returns stdout, stderr, and any process error.
func (e *claudeExecutor) RunTask(ctx context.Context, prompt, repoPath string) ([]byte, []byte, error) {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "json",
		"--mcp-config", `{"mcpServers":{}}`,
		"--strict-mcp-config",
	}
	if e.model != "" {
		args = append(args, "--model", e.model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = repoPath

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	rawOut, err := cmd.Output()
	if err != nil {
		return rawOut, stderrBuf.Bytes(), err
	}

	// Extract the result field from JSON response.
	var resp claudeJSONResponse
	if json.Unmarshal(rawOut, &resp) == nil && resp.Result != "" {
		return []byte(resp.Result), stderrBuf.Bytes(), nil
	}

	// Fallback: if result is empty (Claude used tools only), return the raw JSON
	// so the caller still gets useful content.
	if len(rawOut) > 0 {
		return rawOut, stderrBuf.Bytes(), nil
	}

	return []byte("Task completed (no text output from Claude CLI)"), stderrBuf.Bytes(), nil
}
