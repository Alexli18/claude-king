package vassal

import (
	"bytes"
	"context"
	"os/exec"
)

// codexExecutor runs tasks using the OpenAI Codex CLI ("codex exec ...").
type codexExecutor struct {
	model string
}

// RunTask runs codex in non-interactive exec mode with the given prompt.
// Codex automatically reads files in the working directory for context.
func (e *codexExecutor) RunTask(ctx context.Context, prompt, repoPath string) ([]byte, []byte, error) {
	args := []string{"exec", prompt, "--full-auto"}
	if e.model != "" {
		args = append(args, "--model", e.model)
	}
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = repoPath

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()
	return out, stderrBuf.Bytes(), err
}
