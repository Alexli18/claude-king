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
	sb.WriteString("Write your complete findings, results, and analysis as your **final text response**.\n")
	sb.WriteString("Your text response IS the task output — King reads it directly via `get_task_status`.\n")
	sb.WriteString("Do NOT summarize with 'report sent to King' or similar. Include ALL content inline.\n\n")
	sb.WriteString("If you produced output files (reports, builds, data), register them as artifacts:\n")
	sb.WriteString(fmt.Sprintf("```\nkingctl report-done --task %s --artifacts file1 file2\n```\n", t.ID))
	sb.WriteString("(Omit `--artifacts` if there are no files to register.)\n")

	if err := os.WriteFile(filepath.Join(repoPath, "VASSAL.md"), []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write VASSAL.md to %s: %w", repoPath, err)
	}
	return nil
}
