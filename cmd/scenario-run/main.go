//go:build local

// Command scenario-run posts a differential-test scenario through the Go engine and writes the
// result as JSON (p0-python-oracle-differential, QA-ENG-02). It is test tooling: local build only,
// never in an image.
//
//	scenario-run <scenario.json>     the result on standard output
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/differential"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scenario-run <scenario.json>")
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "scenario-run:", err)
		os.Exit(1)
	}
	var s differential.Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Fprintf(os.Stderr, "scenario-run: %s is not a scenario: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	res, err := differential.RunEngine(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scenario-run:", err)
		os.Exit(1)
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", " ")
	if err := out.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, "scenario-run:", err)
		os.Exit(1)
	}
}
