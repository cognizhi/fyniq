// Command evals runs the AI eval suites (PRD-001 §16.2, AI-78, CTL-AI-05): `make evals` runs it. Each suite under
// evals/ is checked for a sound harness (its own expected answers pass and an empty answer fails), then run against
// every model whose answers are recorded in evals/cassettes, in replay mode by default, offline and with no credential
// (DEV-02). FYNIQ_AI_LIVE=1 runs one model live on the suites' synthetic text and records its answers for the next
// replay. It writes .data/evals/report.json, the control's evidence, and fails on a harness that is not sound, a
// recorded run below its threshold, or a recording that is not whole. The local build only (DEV-07).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// Options are the run's flags.
type Options struct {
	Suites    string
	Cassettes string
	Out       string
	Model     string
}

// run runs the suites: set by the local build (evals_local.go).
var run func(ctx context.Context, o Options) error

func main() {
	fs := flag.NewFlagSet("evals", flag.ExitOnError)
	var o Options
	fs.StringVar(&o.Suites, "suites", "evals", "where the suites are, one directory each with its suite.json")
	fs.StringVar(&o.Cassettes, "cassettes", "evals/cassettes", "where the recorded answers are")
	fs.StringVar(&o.Out, "out", ".data/evals", "where the report goes")
	fs.StringVar(&o.Model, "model", "", "the model to run live with FYNIQ_AI_LIVE=1; replay runs every model recorded")
	_ = fs.Parse(os.Args[1:])
	if run == nil {
		fmt.Fprintln(os.Stderr, "✖ evals: the runner is in the local build only: run make evals")
		os.Exit(2)
	}
	if err := run(context.Background(), o); err != nil {
		fmt.Fprintln(os.Stderr, "✖ evals:", err)
		os.Exit(1)
	}
}
