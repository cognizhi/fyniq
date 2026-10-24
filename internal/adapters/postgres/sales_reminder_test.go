//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-08 (p1-sales task 7.1): a reminder is decided once per document and step, only of an issued invoice or a sent
// quote, a sent one naming a send of that document, and never changes or goes whoever tries (FY203); a customer group's
// schedule has at most 6 days, its name is unique, and it is never deleted; a customer joins one of the organisation's
// groups; the worker decides nothing and keeps no group.
// req: FR-AR-08
func TestARemindersStepIsDecidedOnce(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "clerk"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Reminders Pte Ltd")
	sgd := money.MustLookup("SGD")
	api := journal.Invoices{Store: s}
	draft, err := api.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 1), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "100.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	record := func(n storage.NewSalesReminder) (d storage.SalesReminder, err error) {
		err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err = u.SalesReminders().Record(ctx, n)
			return err
		})
		return d, err
	}
	if _, err := record(storage.NewSalesReminder{DocumentID: draft.ID, Step: 7, Outcome: storage.ReminderSkipped}); storage.CodeOf(err) != storage.CodeReminder {
		t.Fatalf("a draft's reminder: %v", err)
	}
	if _, err := api.Post(ctx, b.Org, draft.ID); err != nil {
		t.Fatal(err)
	}
	d, err := record(storage.NewSalesReminder{DocumentID: draft.ID, Step: 7, Outcome: storage.ReminderSkipped, Reason: "Paid by cheque"})
	if err != nil || d.DecidedBy != "user:clerk" || d.Reason != "Paid by cheque" || d.SendID != nil {
		t.Fatalf("a skipped reminder: %+v %v", d, err)
	}
	if _, err := record(storage.NewSalesReminder{DocumentID: draft.ID, Step: 7, Outcome: storage.ReminderSkipped}); storage.CodeOf(err) != storage.CodeReminder {
		t.Fatalf("the step decided again: %v", err)
	}
	if _, err := record(storage.NewSalesReminder{DocumentID: draft.ID, Step: 0, Outcome: storage.ReminderSkipped}); err != nil {
		t.Fatalf("another step: %v", err)
	}

	// Customer groups, and a customer in one.
	var g storage.CustomerGroup
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if g, err = u.CustomerGroups().Create(ctx, "Retainer", []int{-3, 0, 7}); err != nil {
			return err
		}
		if g, err = u.CustomerGroups().Update(ctx, g.ID, g.Version, "Retainer clients", []int{0, 14}, true); err != nil {
			return err
		}
		c, err := u.Contacts().Get(ctx, b.Customer.ID)
		if err != nil {
			return err
		}
		c.CustomerGroupID = &g.ID
		if c, err = u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active); err != nil || c.CustomerGroupID == nil || *c.CustomerGroupID != g.ID {
			t.Errorf("the customer's group: %+v %v", c, err)
		}
		ds, err := u.SalesReminders().Of(ctx, []uuid.UUID{draft.ID})
		if steps := []int{}; err == nil {
			for _, x := range ds[draft.ID] {
				steps = append(steps, x.Step)
			}
			if !slices.Equal(steps, []int{0, 7}) {
				t.Errorf("the decisions: %v", steps)
			}
		}
		return err
	}); err != nil || g.Version != 2 || !slices.Equal(g.ReminderDays, []int{0, 14}) {
		t.Fatalf("a customer group: %+v %v", g, err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.CustomerGroups().Create(ctx, "retainer clients", nil)
		return err
	}); storage.CodeOf(err) != storage.CodeDuplicate {
		t.Fatalf("a name in use: %v", err)
	}

	doc, decision, group := draft.ID.String(), d.ID.String(), g.ID.String()
	insert := "INSERT INTO fyniq.sales_reminder (document_id, step, outcome, send_id, decided_by) VALUES ('" + doc + "', 30, "
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "UPDATE fyniq.sales_reminder SET reason = 'x' WHERE id = '" + decision + "'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.sales_reminder WHERE id = '" + decision + "'", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.sales_reminder SET reason = 'x' WHERE id = '" + decision + "'", "FY203"},
		{"fyniq_migrator", "DELETE FROM fyniq.sales_reminder WHERE id = '" + decision + "'", "FY203"},
		{"fyniq_worker", insert + "'skipped', NULL, 'user:x')", "42501"},
		// Sent names a send, of that document; skipped none.
		{"fyniq_api", insert + "'sent', NULL, 'user:x')", "23514"},
		{"fyniq_api", insert + "'sent', '" + uuid.NewString() + "', 'user:x')", "FY203"},
		{"fyniq_api", insert + "'skipped', '" + uuid.NewString() + "', 'user:x')", "FY203"},
		{"fyniq_api", "INSERT INTO fyniq.sales_reminder (document_id, step, outcome, decided_by) VALUES ('" + doc + "', 400, 'skipped', 'user:x')", "23514"},
		// A group is never deleted, keeps at most 6 days, and is the API's alone.
		{"fyniq_api", "DELETE FROM fyniq.customer_group WHERE id = '" + group + "'", "42501"},
		{"fyniq_worker", "UPDATE fyniq.customer_group SET name = 'x' WHERE id = '" + group + "'", "42501"},
		{"fyniq_api", "INSERT INTO fyniq.customer_group (name, reminder_days) VALUES ('Too many', '{1,2,3,4,5,6,7}')", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.customer_group (name, reminder_days) VALUES ('Too late', '{400}')", "23514"},
		{"fyniq_api", "UPDATE fyniq.contact SET customer_group_id = '" + uuid.NewString() + "' WHERE id = '" + b.Customer.ID.String() + "'", "23503"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
