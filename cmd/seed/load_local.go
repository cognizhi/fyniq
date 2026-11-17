//go:build local

package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// local is the local database, as the API's role.
func local(ctx context.Context) (*postgres.Store, error) {
	url := cmp.Or(os.Getenv("FYNIQ_DATABASE_URL"),
		fmt.Sprintf("postgres://fyniq_api:%s@localhost:%s/fyniq?sslmode=disable", postgres.LocalPassword, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432")))
	return postgres.Open(ctx, url)
}

func init() {
	newOrganisation = func(ctx context.Context, now time.Time, name string, invites []string, c seed.Company) (Made, error) {
		type invite struct {
			name  string
			roles []string
		}
		var is []invite
		for _, i := range invites {
			who, roles, ok := strings.Cut(i, ":")
			if !ok || strings.TrimSpace(who) == "" || roles == "" {
				return Made{}, fmt.Errorf("--invite %q is not <name>:<role>[,<role>…]", i)
			}
			is = append(is, invite{strings.TrimSpace(who), strings.Split(roles, ",")})
		}
		store, err := local(ctx)
		if err != nil {
			return Made{}, err
		}
		defer store.Close()
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &seedload.Actor, Reason: "a browser journey's organisation (p1-launch-readiness design D6)"})
		org, err := store.CreateOrganisation(ctx, storage.NewOrganisation{Name: name, BaseCurrency: "SGD"})
		if err != nil {
			return Made{}, err
		}
		made := Made{Organisation: org.ID.String(), Invitations: []Invitation{}}
		if err := store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, i := range is {
				code, hash := setupcode.New()
				if _, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: i.name, Roles: i.roles,
					ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}); err != nil {
					return err
				}
				made.Invitations = append(made.Invitations, Invitation{Name: i.name, Code: code})
			}
			return nil
		}); err != nil {
			return Made{}, err
		}
		if _, err := seedload.Load(ctx, store, org.ID, c); err != nil {
			return Made{}, err
		}
		return made, nil
	}
	loadInto = func(ctx context.Context, org string, c seed.Company) error {
		id, err := uuid.Parse(org)
		if err != nil {
			return fmt.Errorf("ORG %q is not an organisation ID: find yours in GET /api/v1/session (organisation_id)", org)
		}
		store, err := local(ctx)
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
