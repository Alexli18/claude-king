package vassal

import (
	"bytes"
	"context"
	"os/exec"
)

// claudeExecutor runs tasks using the Claude Code CLI ("claude -p ...").
type claudeExecutor struct {
	model string
}

// RunTask runs claude headless with the given prompt in repoPath.
// Returns stdout, stderr, and any process error.
func (e *claudeExecutor) RunTask(ctx context.Context, prompt, repoPath string) ([]byte, []byte, error) {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "text",
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
	out, err := cmd.Output()
	return out, stderrBuf.Bytes(), err
}
