// Command seed writes the sample company (DEV-04): `make seed` and `make dev` call it. Every name
// comes from docs/design/screens/sample-data.json, which is synthetic (DEV-08). With --load <org> (the
// local build, `make seed-load`) it loads the company into that organisation of the local database
// and checks the trial balance against the company's own totals.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/seed"
)

// loadInto loads a company into an organisation: set by the local build (load_local.go).
var loadInto func(ctx context.Context, org string, c seed.Company) error

func main() {
	os.Exit(app.Run("seed", func(ctx context.Context, _ app.Deps, args []string) error {
		fs := flag.NewFlagSet("seed", flag.ContinueOnError)
		vocab := fs.String("vocab", "docs/design/screens/sample-data.json", "the synthetic sample data to draw names from")
		n := fs.Uint64("seed", 1, "the generator seed: the same seed always gives the same company")
		journals := fs.Int("journals", 24, "how many manual journals to generate")
		out := fs.String("out", "", "write the company here (default: standard output)")
		load := fs.String("load", "", "load the company into this organisation (its ID) of the local database instead")
		if err := fs.Parse(args); err != nil {
			return err
		}
		v, err := seed.LoadVocabulary(*vocab)
		if err != nil {
			return err
		}
		if *load != "" {
			if loadInto == nil {
				return fmt.Errorf("--load is for the local build only (go run -tags local ./cmd/seed)")
			}
			return loadInto(ctx, *load, seed.Generate(v, *n, *journals))
		}
		b, err := json.MarshalIndent(seed.Generate(v, *n, *journals), "", " ")
		if err != nil {
			return err
		}
		b = append(b, '\n')
		if *out == "" {
			_, err = os.Stdout.Write(b)
			return err
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "seed: wrote the sample company (seed %d) to %s\n", *n, *out)
		return nil
	}))
}
