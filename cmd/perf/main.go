// Command perf is the performance harness (QA-PERF-01; p1-operations-dr design D7): `make perf` runs it against the
// running local stack. It grows a synthetic organisation's ledger by --lines through the posting engine, times each of
// NFR-PERF's budgets through the API, writes .data/perf/report.json and report.md, and fails when a gated budget is
// missed. The local build only: it posts into the local database and signs its calls with the local dev token.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

// Options are the run's flags.
type Options struct {
	API      string
	Org      string
	Lines    int
	Readers  int
	Out      string
	SkipGrow bool
	Only     string
}

// run runs the harness: set by the local build (perf_local.go).
var run func(ctx context.Context, deps app.Deps, o Options) error

func main() {
	os.Exit(app.Run("perf", func(ctx context.Context, deps app.Deps, args []string) error {
		fs := flag.NewFlagSet("perf", flag.ContinueOnError)
		var o Options
		fs.StringVar(&o.API, "api", "http://localhost:8080/api", "the running stack's API, through its one origin")
		fs.StringVar(&o.Org, "org", "", "the organisation to grow and measure (default: the installation's own)")
		fs.IntVar(&o.Lines, "lines", 1_000_000, "how many ledger lines to add before measuring (QA-PERF-01: 1 M weekly, 10 M per minor release)")
		fs.IntVar(&o.Readers, "readers", 100, "how many read at once (NFR-SCALE-01)")
		fs.StringVar(&o.Out, "out", ".data/perf", "where the report goes")
		fs.BoolVar(&o.SkipGrow, "skip-grow", false, "measure the ledger as it is, adding nothing")
		fs.StringVar(&o.Only, "only", "", "measure only these budgets, comma-separated: a requirement such as NFR-PERF-02, or reads, reports, posting, import, export")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if run == nil {
			return fmt.Errorf("perf is for the local build only (go run -tags local ./cmd/perf, or make perf)")
		}
		return run(ctx, deps, o)
	}))
}
