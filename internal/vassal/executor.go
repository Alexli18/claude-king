package vassal

import (
	"context"
	"fmt"
)

// AIExecutor runs a task prompt in a specific AI tool and returns stdout and stderr.
type AIExecutor interface {
	RunTask(ctx context.Context, prompt, repoPath string) (stdout, stderr []byte, err error)
}

// NewExecutor creates an AIExecutor for the given type and optional model.
// executorType must be "claude", "codex", "gemini", or "" (defaults to "claude").
func NewExecutor(executorType, model string) (AIExecutor, error) {
	switch executorType {
	case "claude", "":
		return &claudeExecutor{model: model}, nil
	case "codex":
		return &codexExecutor{model: model}, nil
	case "gemini":
		return &geminiExecutor{model: model}, nil
	default:
		return nil, fmt.Errorf("unknown executor type %q: must be claude, codex, or gemini", executorType)
	}
}
