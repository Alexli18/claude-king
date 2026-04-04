package vassal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// geminiExecutor runs tasks using the Google Gemini CLI ("gemini -p ...").
type geminiExecutor struct {
	model string
}

// RunTask runs gemini headless with the given prompt.
// Unlike Claude Code, Gemini does not auto-read VASSAL.md from the working
// directory, so this executor prepends its content to the prompt if it exists.
func (e *geminiExecutor) RunTask(ctx context.Context, prompt, repoPath string) ([]byte, []byte, error) {
	fullPrompt := e.buildPrompt(prompt, repoPath)

	args := []string{"-p", fullPrompt}
	if e.model != "" {
		args = append(args, "--model", e.model)
	}
	cmd := exec.CommandContext(ctx, "gemini", args...)
	cmd.Dir = repoPath

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	out, err := cmd.Output()
	return out, stderrBuf.Bytes(), err
}

// buildPrompt prepends VASSAL.md content to the prompt if the file exists.
func (e *geminiExecutor) buildPrompt(prompt, repoPath string) string {
	vassalMDPath := filepath.Join(repoPath, "VASSAL.md")
	data, err := os.ReadFile(vassalMDPath)
	if err != nil {
		return prompt // VASSAL.md absent — use prompt as-is
	}
	return fmt.Sprintf("%s\n\n---\n\n%s", string(data), prompt)
}
