//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Auditor's engagement only goes forward, whoever writes (FR-UX-42): an engagement is pending, then active, then closed and is never deleted or edited; a request
// and a sample are added only while it is active; a request is answered, then closed, and keeps its text; a sample never changes; the worker writes none of it; another
// organisation sees none of it; and the database refuses each even from the schema's own owner.
// req: FR-UX-42
// asvs: V4.2.1, V8.2.2
func TestAnEngagementOnlyGoesForwardAndTakesNothingWhileNotActive(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := porttest.NewBooks(t, api, "Engagement Pte Ltd")
	other := porttest.NewBooks(t, api, "Another engagement Pte Ltd")
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	by, auditor, assignee := "user:"+uuid.NewString(), uuid.New(), uuid.New()
	do := func(s storage.Store, org uuid.UUID, f func(k storage.AuditEngagements) error) error {
		return s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u.AuditEngagements()) })
	}
	eng := storage.AuditEngagement{Name: "FY2026", Firm: "Example & Co", AuditorUser: auditor, PeriodFrom: day(1, 1), PeriodTo: day(12, 31), OpensOn: day(11, 1), ClosesOn: day(12, 31), OpenedBy: by, OpenedAt: at}
	if err := do(worker, b.Org, func(k storage.AuditEngagements) error { _, err := k.Open(ctx, eng); return err }); err == nil {
		t.Fatal("the worker opened an engagement")
	}
	var e storage.AuditEngagement
	if err := do(api, b.Org, func(k storage.AuditEngagements) (err error) { e, err = k.Open(ctx, eng); return err }); err != nil || e.Status != storage.EngagementPending {
		t.Fatalf("opening: %+v %v", e, err)
	}
	// Pending: no request, no sample.
	ask := storage.AuditRequest{Engagement: e.ID, Title: "Bank confirmation", Assignee: assignee, RequestedBy: by, RequestedAt: at}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error { _, err := k.Ask(ctx, ask); return err }); storage.CodeOf(err) != storage.CodeAuditEngagement {
		t.Errorf("a request of a pending engagement: %v", err)
	}
	sample := storage.AuditSample{Engagement: e.ID, Rule: []byte(`{"count":1}`), Seed: "s", Population: 0, Items: []byte(`[]`), DrawnBy: by, DrawnAt: at}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error { _, err := k.Draw(ctx, sample); return err }); storage.CodeOf(err) != storage.CodeAuditEngagement {
		t.Errorf("a sample of a pending engagement: %v", err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) (err error) {
		e, err = k.Confirm(ctx, e.ID, by, at.Add(time.Hour))
		return err
	}); err != nil || e.Status != storage.EngagementActive {
		t.Fatalf("confirming: %+v %v", e, err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error { _, err := k.Confirm(ctx, e.ID, by, at); return err }); storage.CodeOf(err) != storage.CodeAuditEngagement {
		t.Errorf("confirming twice: %v", err)
	}
	var r storage.AuditRequest
	var s storage.AuditSample
	if err := do(api, b.Org, func(k storage.AuditEngagements) (err error) {
		if r, err = k.Ask(ctx, ask); err != nil || r.Number != 1 || r.Status != storage.AuditRequestOpen {
			t.Fatalf("asking: %+v %v", r, err)
		}
		if r2, err := k.Ask(ctx, ask); err != nil || r2.Number != 2 {
			t.Fatalf("the next number: %+v %v", r2, err)
		}
		s, err = k.Draw(ctx, sample)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A request is answered (again, until accepted), then closed, and never goes back.
	if err := do(api, b.Org, func(k storage.AuditEngagements) error {
		if _, err := k.CloseRequest(ctx, r.ID, by, at); storage.CodeOf(err) != storage.CodeAuditEngagement {
			t.Errorf("closing an unanswered request: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ev := []uuid.UUID{uuid.New()}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error {
		if _, err := k.Answer(ctx, r.ID, "Attached", ev, by, at); err != nil {
			t.Fatalf("answering: %v", err)
		}
		if got, err := k.Answer(ctx, r.ID, "Attached again", nil, by, at); err != nil || got.Answer != "Attached again" {
			t.Fatalf("answering again: %+v %v", got, err)
		}
		if got, err := k.CloseRequest(ctx, r.ID, by, at); err != nil || got.Status != storage.AuditRequestClosed {
			t.Fatalf("closing: %+v %v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error {
		_, err := k.Answer(ctx, r.ID, "Too late", nil, by, at)
		return err
	}); err == nil {
		t.Error("a closed request was answered")
	}
	// The schema's owner is held to the same rules.
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+b.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DELETE FROM fyniq.auditor_engagement`, `DELETE FROM fyniq.auditor_request`, `DELETE FROM fyniq.auditor_sample`,
		`TRUNCATE fyniq.auditor_sample`, `UPDATE fyniq.auditor_sample SET seed = 'other'`,
		`UPDATE fyniq.auditor_engagement SET name = 'Renamed'`, `UPDATE fyniq.auditor_engagement SET status = 'pending'`,
		`UPDATE fyniq.auditor_request SET title = 'Another item'`, `UPDATE fyniq.auditor_request SET status = 'open'`,
	} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s: the engagement changed", q)
		}
	}
	// Closed: nothing more, and the engagement is never reopened.
	if err := do(api, b.Org, func(k storage.AuditEngagements) (err error) {
		e, err = k.Close(ctx, e.ID, by, at.Add(24*time.Hour))
		return err
	}); err != nil || e.Status != storage.EngagementClosed {
		t.Fatalf("closing the engagement: %+v %v", e, err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error { _, err := k.Draw(ctx, sample); return err }); storage.CodeOf(err) != storage.CodeAuditEngagement {
		t.Errorf("a sample of a closed engagement: %v", err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error { _, err := k.Close(ctx, e.ID, by, at); return err }); storage.CodeOf(err) != storage.CodeAuditEngagement {
		t.Errorf("closing twice: %v", err)
	}
	if err := do(api, b.Org, func(k storage.AuditEngagements) error {
		list, err := k.Samples(ctx, e.ID)
		if err != nil || len(list) != 1 || list[0].ID != s.ID {
			t.Errorf("the samples: %v %v", list, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Another organisation sees none of it, and cannot take the ID.
	if err := do(api, other.Org, func(k storage.AuditEngagements) error {
		if l, err := k.List(ctx); err != nil || len(l) != 0 {
			t.Errorf("another organisation lists %d engagements: %v", len(l), err)
		}
		if _, err := k.Get(ctx, e.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the engagement: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
