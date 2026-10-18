// Command gentestdata regenerates the committed golden migration report
// from a Statuspage export fixture. In-repo (not /tmp) so it can import
// internal packages and double as the CI drift gate:
//
//	go run ./cmd/gentestdata testdata/fixtures/statuspage/sample-export.json \
//	  > testdata/golden/statuspage-report.json
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jonathanngiroux-star/outpage/internal/migrate/statuspage"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gentestdata <statuspage-export.json>")
		os.Exit(2)
	}
	exp, err := statuspage.Parse(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gentestdata: %v\n", err)
		os.Exit(1)
	}
	b, err := json.MarshalIndent(statuspage.Analyze(exp), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gentestdata: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(b)
	fmt.Println()
}
