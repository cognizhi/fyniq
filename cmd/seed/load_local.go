//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
)

func init() {
	loadInto = func(ctx context.Context, org string, c seed.Company) error {
		id, err := uuid.Parse(org)
		if err != nil {
			return fmt.Errorf("ORG %q is not an organisation ID: find yours in GET /api/v1/session (organisation_id)", org)
		}
		url := cmp.Or(os.Getenv("FYNIQ_DATABASE_URL"),
			fmt.Sprintf("postgres://fyniq_api:%s@localhost:%s/fyniq?sslmode=disable", postgres.LocalPassword, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432")))
		store, err := postgres.Open(ctx, url)
		if err != nil {
			return err
		}
		defer store.Close()
		r, err := seedload.Load(ctx, store, id, c)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "seed: loaded the sample company (seed %d) into %s: %d accounts, %d periods, %d journals posted, the first reversed; the trial balance matches the seed's own totals\n",
			c.Seed, id, r.Accounts, r.Periods, r.Journals)
		return nil
	}
}
