package events

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alexli18/claude-king/internal/config"
)

// CompiledPattern is a pre-compiled event detection pattern.
type CompiledPattern struct {
	Name            string
	Regex           *regexp.Regexp
	Severity        string
	Source          string // vassal name filter, empty = match all
	SummaryTemplate string
}

// CompilePatterns compiles PatternConfig entries into CompiledPatterns.
func CompilePatterns(patterns []config.PatternConfig) ([]*CompiledPattern, error) {
	compiled := make([]*CompiledPattern, 0, len(patterns))
	for i, p := range patterns {
		if p.Regex == "" {
			return nil, fmt.Errorf("pattern %d (%q): empty regex", i, p.Name)
		}
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("pattern %d (%q): invalid regex: %w", i, p.Name, err)
		}
		compiled = append(compiled, &CompiledPattern{
			Name:            p.Name,
			Regex:           re,
			Severity:        p.Severity,
			Source:          p.Source,
			SummaryTemplate: p.SummaryTemplate,
		})
	}
	return compiled, nil
}

// Match tests a line against a compiled pattern.
// Returns a summary string if matched, empty string if not.
func (cp *CompiledPattern) Match(line, vassalName string) (summary string, matched bool) {
	// Check source filter: if set, only match for that vassal.
	if cp.Source != "" && cp.Source != vassalName {
		return "", false
	}

	matches := cp.Regex.FindStringSubmatch(line)
	if matches == nil {
		return "", false
	}

	// Generate summary from template or fall back to full match.
	if cp.SummaryTemplate == "" {
		return matches[0], true
	}

	summary = cp.SummaryTemplate

	// Replace {match} with the full match.
	summary = strings.ReplaceAll(summary, "{match}", matches[0])

	// Replace {vassal} with the vassal name.
	summary = strings.ReplaceAll(summary, "{vassal}", vassalName)

	// Replace {group.N} with capture group N.
	for i := 1; i < len(matches); i++ {
		placeholder := fmt.Sprintf("{group.%d}", i)
		summary = strings.ReplaceAll(summary, placeholder, matches[i])
	}

	return summary, true
}
