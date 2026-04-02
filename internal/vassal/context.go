package vassal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactRef is a resolved artifact passed to a vassal for context.
type ArtifactRef struct {
	Name     string
	FilePath string
}

// WriteVassalMD generates VASSAL.md in repoPath with task context.
// Claude Code reads this file to understand its role and assigned task.
func WriteVassalMD(repoPath, vassalName string, t *Task, artifacts []ArtifactRef) error {
	if t == nil {
		return fmt.Errorf("WriteVassalMD: task must not be nil")
	}
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# You are a Vassal: %s\n\n", vassalName))
	sb.WriteString("Your King has assigned you a task. Work autonomously, then report completion.\n\n")
	sb.WriteString("## Task\n\n")
	sb.WriteString(t.Task + "\n\n")

	if len(artifacts) > 0 {
		sb.WriteString("## Available artifacts from other vassals\n\n")
		for _, a := range artifacts {
			sb.WriteString(fmt.Sprintf("- **%s** → `%s`\n", a.Name, a.FilePath))
		}
		sb.WriteString("\n")
	}

	if notes, ok := t.Context["notes"].(string); ok && notes != "" {
		sb.WriteString("## Notes from King\n\n")
		sb.WriteString(notes + "\n\n")
	}

	sb.WriteString("## When done\n\n")
	sb.WriteString(fmt.Sprintf("Run: `kingctl report-done --task %s`\n", t.ID))
	sb.WriteString("(This signals King that the task is complete.)\n")

	if err := os.WriteFile(filepath.Join(repoPath, "VASSAL.md"), []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write VASSAL.md to %s: %w", repoPath, err)
	}
	return nil
}
