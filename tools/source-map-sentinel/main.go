package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alexli18/source-map-sentinel/output"
	"github.com/alexli18/source-map-sentinel/scanner"
)

func main() {
	jsonOut := flag.Bool("json", false, "output findings as JSON")
	failOnFindings := flag.Bool("fail-on-findings", false, "exit with code 1 if findings exist")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sentinel [--json] [--fail-on-findings] <directory>\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	target := flag.Arg(0)
	findings, err := scanner.Scan(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	if *jsonOut {
		if err := output.ReportJSON(os.Stdout, findings); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
			os.Exit(2)
		}
	} else {
		output.Report(os.Stdout, findings)
	}

	if *failOnFindings && len(findings) > 0 {
		os.Exit(1)
	}
}
