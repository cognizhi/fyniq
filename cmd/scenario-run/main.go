//go:build local

// Command scenario-run posts a differential-test scenario through the Go engine and writes the
// result as JSON (p0-python-oracle-differential, QA-ENG-02). It is test tooling: local build only,
// never in an image. Like the oracle, it exits with status 2 for an invalid scenario.
//
//	scenario-run <scenario.json>     the result on standard output
package main

import (
	"encoding/json"
	"errors"
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
	s, err := differential.ParseScenario(b)
	var invalid *differential.InvalidError
	if errors.As(err, &invalid) {
		fmt.Fprintln(os.Stderr, "scenario-run:", err)
		os.Exit(2)
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
