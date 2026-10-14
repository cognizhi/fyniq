// Command seed writes the sample company (DEV-04): `make seed` and `make dev` call it. Every name
// comes from docs/design/screens/sample-data.json, which is synthetic (DEV-08).
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

func main() {
	os.Exit(app.Run("seed", func(_ context.Context, _ app.Deps, args []string) error {
		fs := flag.NewFlagSet("seed", flag.ContinueOnError)
		vocab := fs.String("vocab", "docs/design/screens/sample-data.json", "the synthetic sample data to draw names from")
		n := fs.Uint64("seed", 1, "the generator seed: the same seed always gives the same company")
		journals := fs.Int("journals", 24, "how many manual journals to generate")
		out := fs.String("out", "", "write the company here (default: standard output)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		v, err := seed.LoadVocabulary(*vocab)
		if err != nil {
			return err
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
