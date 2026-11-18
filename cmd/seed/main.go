// Command seed writes the sample company (DEV-04): `make seed` and `make dev` call it. Every name
// comes from docs/design/screens/sample-data.json, which is synthetic (DEV-08). With --load <org> (the
// local build, `make seed-load`) it loads the company into that organisation of the local database
// and checks the trial balance against the company's own totals. With --new-organisation <name> (the local
// build; the browser journeys, p1-launch-readiness design D6) it first makes that organisation, with an invitation for
// each --invite <name>:<role>,<role>, and prints the organisation's ID and the invitations' codes as JSON: each person
// signs in and accepts theirs, as a person invited does (FR-ADM-01). The codes are one-time and local.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/seed"
)

// loadInto loads a company into an organisation: set by the local build (load_local.go).
var loadInto func(ctx context.Context, org string, c seed.Company) error

// newOrganisation makes an organisation with an invitation for each of invites and loads a company into it: set by the
// local build (load_local.go).
var newOrganisation func(ctx context.Context, now time.Time, name string, invites []string, c seed.Company, scriptedAI string) (Made, error)

// Made is a new organisation and its invitations, as --new-organisation prints them.
type Made struct {
	Organisation string       `json:"organisation"`
	Invitations  []Invitation `json:"invitations"`
}

// Invitation is one person's invitation to a new organisation: the name it was made for and its one-time code.
type Invitation struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// list is a flag given more than once.
type list []string

func (l *list) String() string     { return fmt.Sprint(*l) }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	os.Exit(app.Run("seed", func(ctx context.Context, deps app.Deps, args []string) error {
		fs := flag.NewFlagSet("seed", flag.ContinueOnError)
		vocab := fs.String("vocab", "docs/design/screens/sample-data.json", "the synthetic sample data to draw names from")
		n := fs.Uint64("seed", 1, "the generator seed: the same seed always gives the same company")
		journals := fs.Int("journals", 24, "how many manual journals to generate")
		out := fs.String("out", "", "write the company here (default: standard output)")
		load := fs.String("load", "", "load the company into this organisation (its ID) of the local database instead")
		name := fs.String("new-organisation", "", "make an organisation of this name in the local database, with --invite's, and load the company into it")
		var invites list
		fs.Var(&invites, "invite", "with --new-organisation: <name>:<role>[,<role>…], an invitation to the new organisation (repeatable)")
		scriptedAI := fs.String("scripted-ai", "", "with --new-organisation: routed, the chat agent routed to local mode's scripted model; on, "+
			"also the AI switch and the task type sec.create_task on (p1-launch-readiness design D6)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		v, err := seed.LoadVocabulary(*vocab)
		if err != nil {
			return err
		}
		if *scriptedAI != "" && *scriptedAI != "routed" && *scriptedAI != "on" {
			return fmt.Errorf("--scripted-ai %q is neither routed nor on", *scriptedAI)
		}
		if *name != "" {
			if newOrganisation == nil {
				return fmt.Errorf("--new-organisation is for the local build only (go run -tags local ./cmd/seed)")
			}
			made, err := newOrganisation(ctx, deps.Clock.Now(), *name, invites, seed.Generate(v, *n, *journals), *scriptedAI)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(made)
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
