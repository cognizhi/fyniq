//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/perf"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/version"
)

func init() {
	run = func(ctx context.Context, deps app.Deps, o Options) error {
		url := cmp.Or(os.Getenv("FYNIQ_DATABASE_URL"),
			fmt.Sprintf("postgres://fyniq_api:%s@localhost:%s/fyniq?sslmode=disable", postgres.LocalPassword, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432")))
		store, err := postgres.Open(ctx, url, postgres.WithClock(deps.Clock))
		if err != nil {
			return err
		}
		defer store.Close()
		org, err := uuid.Parse(o.Org)
		if o.Org == "" {
			org, err = theCompany(ctx, store)
		}
		if err != nil {
			return fmt.Errorf("the organisation to measure: %w", err)
		}
		r := perf.Report{At: deps.Clock.Now(), Version: version.Version, Lines: o.Lines}
		if !o.SkipGrow {
			fmt.Fprintf(os.Stderr, "perf: growing %s by %d ledger lines through the posting engine\n", org, o.Lines)
			if r.Grown, err = (perf.Grower{Store: store, Org: org, Clock: deps.Clock, Lines: o.Lines, PerJournal: 50, Seed: 1, Log: os.Stderr}).Grow(ctx); err != nil {
				return err
			}
		} else if r.Grown, err = (perf.Grower{Store: store, Org: org, Clock: deps.Clock, PerJournal: 50}).Grow(ctx); err != nil {
			return err
		}
		// An Owner and Accountant of the organisation (Appendix D: the Accountant creates and posts journals and imports
		// statements), as the local issuer signs one, stepped up (FR-ADM-07); one token per client.
		issuer := local.NewDevTokens([]byte(local.DevTokenKey), deps.Clock)
		token := func(client string) (string, error) {
			return issuer.Issue(authn.Principal{Subject: "perf", Client: client, Class: authn.ClassUser, Organisation: org,
				Roles: []authz.Role{authz.Owner, authz.Accountant}, StepUpAt: deps.Clock.Now()}, 6*time.Hour)
		}
		client := (&egress.Guard{Local: true}).Bounded(16<<20, 10*time.Minute).For(egress.HostOf(o.API))
		r.Results = perf.Measurer{API: o.API, Token: token, HTTP: client, Clock: deps.Clock, Readers: o.Readers, Lines: o.Lines, Log: os.Stderr}.Run(ctx, r.Grown)
		r.Settle()
		if err := perf.Write(o.Out, r); err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, r.Markdown())
		if !r.Passed {
			return fmt.Errorf("a gated budget was missed: see %s/report.md", o.Out)
		}
		return nil
	}
}

// theCompany is the one organisation, besides the installation's own, that keeps books: the sample company the smoke
// test loads (make seed-load).
func theCompany(ctx context.Context, store *postgres.Store) (uuid.UUID, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	inst, _ := store.InstallationOrganisation(ctx)
	var found []uuid.UUID
	for _, org := range orgs {
		if org == inst {
			continue
		}
		var n int
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			accounts, err := u.Accounts().List(ctx)
			n = len(accounts)
			return err
		}); err != nil {
			return uuid.Nil, err
		}
		if n > 0 {
			found = append(found, org)
		}
	}
	if len(found) != 1 {
		return uuid.Nil, fmt.Errorf("%d organisations keep books: name one with ORG=<organisation id> (GET /api/v1/session), or load the sample company with make seed-load", len(found))
	}
	return found[0], nil
}
