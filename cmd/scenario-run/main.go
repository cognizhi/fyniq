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
	"io"
	"os"

	"github.com/cognizhi/fyniq/internal/differential"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the command: the exit status for the arguments, with the result on stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: scenario-run <scenario.json>")
		return 2
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "scenario-run:", err)
		return 1
	}
	s, err := differential.ParseScenario(b)
	var invalid *differential.InvalidError
	if errors.As(err, &invalid) {
		fmt.Fprintln(stderr, "scenario-run:", err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "scenario-run:", err)
		return 1
	}
	res, err := differential.RunEngine(s)
	if err != nil {
		fmt.Fprintln(stderr, "scenario-run:", err)
		return 1
	}
	out := json.NewEncoder(stdout)
	out.SetIndent("", " ")
	if err := out.Encode(res); err != nil {
		fmt.Fprintln(stderr, "scenario-run:", err)
		return 1
	}
	return 0
}
