//go:build local

// Command payrollrun works a differential-test pay run through Nemura's payroll engine and writes the result as JSON
// (p1-nemura-payroll 1.5, QA-ENG-02). It is test tooling: local build only, never in an image. Like the oracle, it exits with status 2
// for a document that is not a valid run.
//
//	payrollrun <run.json>     the fyniq-payroll-run-result@1 on standard output
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cognizhi/fyniq/internal/nemura/payroll/differential"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the command: the exit status for the arguments, with the result on stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: payrollrun <run.json>")
		return 2
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "payrollrun:", err)
		return 1
	}
	doc, err := differential.Parse(b)
	var bad *differential.InvalidError
	if errors.As(err, &bad) {
		fmt.Fprintln(stderr, "payrollrun:", err)
		return 2
	}
	res, err := differential.Run(doc)
	if errors.As(err, &bad) {
		fmt.Fprintln(stderr, "payrollrun:", err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "payrollrun:", err)
		return 1
	}
	out := json.NewEncoder(stdout)
	out.SetIndent("", " ")
	if err := out.Encode(res); err != nil {
		fmt.Fprintln(stderr, "payrollrun:", err)
		return 1
	}
	return 0
}
