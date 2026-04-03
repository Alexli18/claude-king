// Package output formats scan findings for terminal or machine consumption.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/alexli18/source-map-sentinel/scanner"
)

// Report writes findings to w in human-readable format.
func Report(w io.Writer, findings []scanner.Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "No findings.")
		return
	}
	fmt.Fprintf(w, "Found %d issue(s):\n\n", len(findings))
	for _, f := range findings {
		switch f.Type {
		case scanner.FindingMapFile:
			fmt.Fprintf(w, "  [MAP FILE]           %s\n", f.Path)
		case scanner.FindingSourceMappingURL:
			fmt.Fprintf(w, "  [SOURCE_MAPPING_URL] %s:%d\n    %s\n", f.Path, f.Line, f.Text)
		}
	}
}

// ReportJSON writes findings to w as a JSON array.
func ReportJSON(w io.Writer, findings []scanner.Finding) error {
	if findings == nil {
		findings = []scanner.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}
