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

	localadapter "github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/ai"
	appclock "github.com/cognizhi/fyniq/internal/clock"
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
	newOrganisation = func(ctx context.Context, now time.Time, name string, invites []string, c seed.Company, scriptedAI string) (Made, error) {
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
		if scriptedAI != "" {
			if err := store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
				return scripted(ctx, u, now, scriptedAI == "on")
			}); err != nil {
				return Made{}, fmt.Errorf("the scripted AI: %w", err)
			}
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

// scripted sets an organisation up for the AI journeys (p1-launch-readiness design D6): a connection to local mode's
// scripted model, which runs inside the API under its own identity at no fee, its one model listed, probed, allowed and qualified
// for the chat agent, and the chat agent's route to it; with on, also the AI switch on from today and the task type
// sec.create_task on, which a UAT script instead asks for and a second person approves. The approvals stand in, as the
// API tests' do: these journeys test the assistant and a proposal's approval, and the set-up's own dual control is
// tested where it is built (p1-ai-providers). Its qualification is the scripted model's own scripts, never an eval suite.
func scripted(ctx context.Context, u storage.UnitOfWork, now time.Time, on bool) error {
	const by = "system:seed"
	approval := uuid.Must(uuid.NewV7())
	entry, err := u.Compliance().SaveInventoryEntry(ctx, storage.InventoryEntry{What: "The journeys' questions to the scripted model",
		Why: "The AI journeys", Where: "Inside the local API", Who: "The journeys' people", Retention: "None", Transfers: "None", State: "open",
		SavedBy: by, SavedAt: now, Version: 1})
	if err != nil {
		return err
	}
	handling := `{"inventory_id":"` + entry.EntryID.String() + `","handling":[{"model":"*","endpoint":"scripted.localhost","feature":"*",` +
		`"processed_in":["SG"],"training":"no","evidence":{"kind":"source","url":"https://github.com/cognizhi/fyniq/blob/main/internal/adapters/local/scripted.go",` +
		`"title":"The scripted model answers inside the local API","checked_on":"` + now.Format(time.DateOnly) + `"}}]}`
	c, err := u.AIConnections().Create(ctx, storage.AIConnection{Name: "Scripted (journeys)", Connector: "scripted", Auth: "workload-identity", Configuration: []byte(`{}`),
		Host: "scripted.localhost", Billing: "subscription", Quota: []byte(`{"amount":100000,"unit":"requests","window":"day","fee":"0.00","currency":"SGD"}`),
		DataHandling: []byte(handling), CreatedBy: by, CreatedAt: now}, approval)
	if err != nil {
		return err
	}
	model := localadapter.ScriptedModel
	if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "refresh", Outcome: "complete", Pages: 1, Added: 1,
		ReadBy: by, ReadAt: now}, []storage.AIModel{{ConnectionID: c.ID, Model: model, FirstListed: now, LastListed: now, Listed: true}}); err != nil {
		return err
	}
	if _, err := u.AIModels().Probe(ctx, c.ID, model, true, false, "answered", now); err != nil {
		return err
	}
	if _, err := u.AIModels().Allow(ctx, c.ID, model, true, &approval); err != nil {
		return err
	}
	if _, err := u.AIQualifications().Record(ctx, storage.AIQualification{Agent: "chat", ConnectionID: c.ID, Model: model, Version: model,
		Suite: "scripted-journeys", SuiteVersion: 1, Threshold: "1", Cases: 2, Passed: 2, Result: "pass", Run: uuid.Must(uuid.NewV7()), RanBy: by, RanAt: now}); err != nil {
		return err
	}
	c.State, c.ChangedBy, c.ChangedAt = "enabled", by, now
	if _, err := u.AIConnections().Step(ctx, c, storage.AIConnectionEnabled, &approval); err != nil {
		return err
	}
	if _, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: by,
		MadeAt: now, Steps: []storage.AIRouteStep{{ConnectionID: c.ID, Model: model}}}); err != nil {
		return err
	}
	if !on {
		return nil
	}
	y, m, d := now.In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if _, err := u.Settings().Record(ctx, ai.Switch, "false", "true", from, 1); err != nil {
		return err
	}
	_, err = u.Settings().Record(ctx, "ai.task_types", "", "sec.create_task", from, 1)
	return err
}
