// internal/fingerprint/contracts.go
package fingerprint

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/alexli18/claude-king/internal/config"
)

// DefaultContracts returns PatternConfig integrity contracts for the project type.
// Contracts use regex to detect errors in vassal output (go vet, eslint, npm test).
// All returned contracts have Source="auto" for easy identification.
func DefaultContracts(pt ProjectType, rootDir string) []config.PatternConfig {
	switch pt {
	case ProjectTypeGo:
		return []config.PatternConfig{
			{
				Name:            "go-vet-error",
				Regex:           `^.*\.go:\d+:\d*:?\s*(vet:|SA\d+:)`,
				Severity:        "error",
				Source:          "auto",
				SummaryTemplate: "go vet error in {vassal}: {match}",
			},
		}
	case ProjectTypeNode:
		return nodeContracts(rootDir)
	case ProjectTypeESP32:
		return []config.PatternConfig{
			{Name: "esp32-panic",         Regex: `(?i)panic:|Guru Meditation Error`, Severity: "critical", Source: "auto"},
			{Name: "esp32-abort",         Regex: `abort\(\)`,                         Severity: "critical", Source: "auto"},
			{Name: "esp32-brownout",      Regex: `(?i)brownout detector`,             Severity: "critical", Source: "auto"},
			{Name: "esp32-stackoverflow", Regex: `(?i)stack overflow`,                Severity: "critical", Source: "auto"},
			{Name: "esp32-error",         Regex: `^E \(\d+\) `,                       Severity: "error",    Source: "auto"},
			{Name: "esp32-warning",       Regex: `^W \(\d+\) `,                       Severity: "warning",  Source: "auto"},
		}
	case ProjectTypeNMEA:
		return []config.PatternConfig{
			{Name: "nmea-invalid-fix", Regex: `\$GP[A-Z]+,[^,]*,V,`, Severity: "warning", Source: "auto"},
			{Name: "nmea-no-signal",   Regex: `\$GPGSA,A,1,`,        Severity: "warning", Source: "auto"},
		}
	case ProjectTypeAT:
		return []config.PatternConfig{
			{Name: "at-error",               Regex: `^(ERROR|\+CME ERROR|\+CMS ERROR)`, Severity: "error",   Source: "auto"},
			{Name: "at-no-carrier",          Regex: `^(NO CARRIER|NO DIALTONE|BUSY)`,   Severity: "warning", Source: "auto"},
			{Name: "at-registration-denied", Regex: `\+CREG: [04]`,                     Severity: "error",   Source: "auto"},
		}
	default:
		return nil
	}
}

// nodeContracts inspects package.json to determine which Node contracts apply.
func nodeContracts(rootDir string) []config.PatternConfig {
	var contracts []config.PatternConfig

	pkg := readPackageJSON(rootDir)

	// npm test — if scripts.test is defined.
	if pkg != nil {
		if scripts, ok := pkg["scripts"].(map[string]any); ok {
			if _, hasTest := scripts["test"]; hasTest {
				contracts = append(contracts, config.PatternConfig{
					Name:            "npm-test-failure",
					Regex:           `(?i)^(FAIL|failing|failed)\b`,
					Severity:        "error",
					Source:          "auto",
					SummaryTemplate: "npm test failure in {vassal}: {match}",
				})
			}
		}

		// eslint — if eslint in devDependencies or dependencies.
		if hasESLint(pkg) {
			contracts = append(contracts, config.PatternConfig{
				Name:            "eslint-error",
				Regex:           `^\s+\d+:\d+\s+error\s+`,
				Severity:        "warning",
				Source:          "auto",
				SummaryTemplate: "eslint error in {vassal}: {match}",
			})
		}
	}

	return contracts
}

func readPackageJSON(rootDir string) map[string]any {
	data, err := os.ReadFile(filepath.Join(rootDir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg
}

func hasESLint(pkg map[string]any) bool {
	for _, key := range []string{"devDependencies", "dependencies"} {
		if deps, ok := pkg[key].(map[string]any); ok {
			if _, found := deps["eslint"]; found {
				return true
			}
		}
	}
	return false
}
