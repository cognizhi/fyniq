//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A project is a value of the Project dimension with its attributes, members and tasks, each change with its audit event
// under the project; a project on another dimension's value, for a contact that is no customer, or with a member who is
// not an active member of the organisation is refused by the database, and one organisation sees none of another's.
// req: FR-PRJ-01
// req: FR-DIM-01
// req: QA-ENG-05
// asvs: V8.2.2, V13.2.2
func TestAProjectIsADimensionValueWithAttributes(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	api := store(t, "fyniq_api")
	b := books(t, "Projects Pte Ltd")
	other := books(t, "Other projects Pte Ltd")
	now := time.Date(2099, 10, 1, 9, 0, 0, 0, time.UTC)
	priya := joinAs(t, api, b.Org, "priya-"+uuid.NewString()[:8], []string{"employee"}, nil, 1, now)
	stranger := joinAs(t, api, other.Org, "sam-"+uuid.NewString()[:8], []string{"employee"}, nil, 2, now)

	var atlas, eng uuid.UUID
	var acme, vendor uuid.UUID
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"})
		if err != nil {
			return err
		}
		dep, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
		if err != nil {
			return err
		}
		v, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "ATLAS", Name: "Atlas rollout"})
		if err != nil {
			return err
		}
		e, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dep.ID, Code: "ENG", Name: "Engineering"})
		if err != nil {
			return err
		}
		atlas, eng = v.ID, e.ID
		c, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Acme Pte Ltd", Customer: true})
		if err != nil {
			return err
		}
		s, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Supplier Pte Ltd", Vendor: true})
		acme, vendor = c.ID, s.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	minutes, amount := 24000, storage.Decimal("60000.0000")
	in := func(ps ...func(*storage.NewProject)) storage.NewProject {
		n := storage.NewProject{ID: atlas, CustomerID: &acme, Currency: "SGD", Status: storage.ProjectActive, BillingMethod: storage.BillingTimeAndMaterials,
			BudgetMinutes: &minutes, BudgetAmount: &amount}
		for _, p := range ps {
			p(&n)
		}
		return n
	}
	try := func(n storage.NewProject) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Projects().Create(ctx, n)
			return err
		})
	}

	// Refused: a department value, a customer who is a vendor, a budget finer than the currency, a closed one's task later.
	for name, n := range map[string]storage.NewProject{
		"a value of another dimension": in(func(n *storage.NewProject) { n.ID = eng }),
		"a vendor as the customer":     in(func(n *storage.NewProject) { n.CustomerID = &vendor }),
		"a budget finer than a cent":   in(func(n *storage.NewProject) { d := storage.Decimal("1.2345"); n.BudgetAmount = &d }),
		"an unknown status":            in(func(n *storage.NewProject) { n.Status = "done" }),
	} {
		if err := try(n); err == nil {
			t.Errorf("%s: the project was made", name)
		}
	}
	if err := try(in(func(n *storage.NewProject) { n.ID = eng })); storage.CodeOf(err) != storage.CodeProject {
		t.Errorf("a project on a department value: %v", err)
	}
	if err := try(in()); err != nil {
		t.Fatalf("a project: %v", err)
	}

	read := func(org uuid.UUID) (ps []storage.Project) {
		t.Helper()
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			ps, err = u.Projects().List(ctx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ps
	}
	ps := read(b.Org)
	if len(ps) != 1 || !ps[0].Configured || ps[0].Code != "ATLAS" || ps[0].Currency != "SGD" || ps[0].Version != 1 ||
		ps[0].BudgetAmount == nil || *ps[0].BudgetAmount != "60000.0000" || ps[0].BudgetMinutes == nil || *ps[0].BudgetMinutes != minutes {
		t.Fatalf("the project: %+v", ps)
	}
	if o := read(other.Org); len(o) != 0 {
		t.Fatalf("another organisation sees %d projects", len(o))
	}

	// A change at a stale version is refused; at the version it moves to the next, and the currency stays.
	change := func(version int) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Projects().Change(ctx, atlas, version, storage.ProjectChange{CustomerID: &acme, Status: storage.ProjectOnHold,
				BillingMethod: storage.BillingTimeAndMaterials})
			return err
		})
	}
	if err := change(7); storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("a change at another version: %v", err)
	}
	if err := change(1); err != nil {
		t.Fatalf("a change: %v", err)
	}
	if p := read(b.Org)[0]; p.Version != 2 || p.Status != storage.ProjectOnHold || p.BudgetMinutes != nil || p.BudgetAmount != nil {
		t.Errorf("after the change: %+v", p)
	}

	// People: a member and a manager; a person of another organisation is refused; a person put out of use is kept.
	set := func(org uuid.UUID, ps ...storage.ProjectPerson) error {
		return api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return u.Projects().SetMembers(ctx, atlas, ps) })
	}
	if err := set(b.Org, storage.ProjectPerson{UserID: stranger.UserID}); err == nil {
		t.Error("a member of another organisation was put on the project")
	}
	if err := set(b.Org, storage.ProjectPerson{UserID: priya.UserID, Manager: true}); err != nil {
		t.Fatal(err)
	}
	if err := set(b.Org); err != nil {
		t.Fatal(err)
	}
	var ms []storage.ProjectMember
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ms, err = u.Projects().Members(ctx, atlas)
		return err
	}); err != nil || len(ms) != 1 || ms[0].Active || ms[0].Manager {
		t.Fatalf("a person put out of use is kept, not a manager now: %+v %v", ms, err)
	}

	// Tasks: one live task of a name, whatever the case; none on a closed project.
	task := func(name string) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Projects().CreateTask(ctx, atlas, name, true)
			return err
		})
	}
	if err := task("Data migration"); err != nil {
		t.Fatal(err)
	}
	if err := task("data MIGRATION"); err == nil {
		t.Error("two live tasks of one name")
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Projects().Change(ctx, atlas, 2, storage.ProjectChange{CustomerID: &acme, Status: storage.ProjectClosed, BillingMethod: storage.BillingTimeAndMaterials})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := task("Handover"); storage.CodeOf(err) != storage.CodeProject {
		t.Errorf("a task on a closed project: %v", err)
	}
	tr := trail(t, b.Org)
	for _, want := range []string{`"action":"project.created"`, `"action":"project.changed"`, `"action":"project.people_changed"`, `"action":"project.task_added"`} {
		if !strings.Contains(tr, want) {
			t.Errorf("the audit trail lacks %s", want)
		}
	}
	// Another organisation's project is not found by ID.
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Projects().Get(ctx, atlas)
		return err
	}); err != ports.ErrNotFound {
		t.Errorf("another organisation reads the project: %v", err)
	}
}

// A project is never deleted and keeps its value, its currency and its creation, by any role; a member and a task are
// never deleted; the application role has no right to delete or to change what is fixed (FR-PRJ-01; FY600, FY601).
// req: FR-PRJ-01
// req: QA-ENG-05
// asvs: V13.2.2
func TestAttackAProjectIsNeverDeletedAndKeepsItsCurrency(t *testing.T) {
	ctx := context.Background()
	apiCtx := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	api := store(t, "fyniq_api")
	b := books(t, "Attack projects Pte Ltd")
	var value uuid.UUID
	if err := api.Do(apiCtx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"})
		if err != nil {
			return err
		}
		v, err := u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: d.ID, Code: "ATLAS", Name: "Atlas"})
		if err != nil {
			return err
		}
		value = v.ID
		if _, err := u.Projects().Create(ctx, storage.NewProject{ID: v.ID, Currency: "SGD", Status: storage.ProjectActive, BillingMethod: storage.BillingInternal}); err != nil {
			return err
		}
		_, err = u.Projects().CreateTask(ctx, v.ID, "Build", true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, role, sql, code string }{
		{"deleting a project as the owner", "fyniq_migrator", `DELETE FROM fyniq.project`, "FY600"},
		{"changing a project's currency", "fyniq_migrator", `UPDATE fyniq.project SET currency = 'USD', minor_units = 2, version = version + 1`, "FY600"},
		{"changing a project's value", "fyniq_migrator", `UPDATE fyniq.project SET id = gen_random_uuid(), version = version + 1`, "FY600"},
		{"skipping a version", "fyniq_migrator", `UPDATE fyniq.project SET version = version + 2`, "FY600"},
		{"deleting a task as the owner", "fyniq_migrator", `DELETE FROM fyniq.project_task`, "FY601"},
		{"deleting a project as the application", "fyniq_api", `DELETE FROM fyniq.project`, "42501"},
		{"changing a currency as the application", "fyniq_api", `UPDATE fyniq.project SET currency = 'USD'`, "42501"},
		{"deleting a task as the application", "fyniq_api", `DELETE FROM fyniq.project_task`, "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, c.sql); err != nil {
				return err
			}
			return auditRaw(ctx, tx, "project.changed", "project", value)
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %v, want %s", c.name, err, c.code)
		}
	}
}
