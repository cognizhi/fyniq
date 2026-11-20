//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A board pack and an investor update (FR-RPT-06, FR-EQ-23; p2-reporting-planning): a month has one draft at a time; a draft changes only as its
// next edit; publishing freezes it with its figures and nothing about it changes again, and a correction is the next version that supersedes it;
// an update is built from a published pack and sent once, to people fixed when it is sent; a read is recorded once, for a recipient only;
// nothing is deleted; the worker holds no write; and another organisation sees none of it.
// req: FR-RPT-06
// req: FR-EQ-23
// asvs: V4.2.1
func TestABoardPackIsFrozenWhenPublishedAndAnUpdateGoesOnceToItsRecipients(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Board pack Pte Ltd")
	other := porttest.NewBooks(t, api, "Another pack Pte Ltd")
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	by := "user:" + uuid.NewString()
	reader := uuid.New()
	do := func(s storage.Store, org uuid.UUID, f func(u storage.UnitOfWork) error) error {
		return s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u) })
	}

	if err := do(worker, books.Org, func(u storage.UnitOfWork) error { _, err := u.BoardPacks().CreateDraft(ctx, month, by, at); return err }); err == nil {
		t.Fatal("the worker made a board pack")
	}
	var draft storage.BoardPack
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		draft, err = u.BoardPacks().CreateDraft(ctx, month, by, at)
		return err
	}); err != nil ||
		draft.Version != 1 || draft.Status != storage.PackDraft || draft.Edit != 1 {
		t.Fatalf("the draft: %+v, %v", draft, err)
	}
	// One draft per month at a time.
	err := do(api, books.Org, func(u storage.UnitOfWork) error { _, err := u.BoardPacks().CreateDraft(ctx, month, by, at); return err })
	var se *storage.Error
	if !errors.As(err, &se) || se.Code != storage.CodeBoardPackDraft && se.Code != storage.CodeBoardPack {
		t.Fatalf("a second draft of the month: %v", err)
	}
	// The texts change as the next edit; a stale edit is refused.
	var edited storage.BoardPack
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		edited, err = u.BoardPacks().EditDraft(ctx, draft.ID, "Revenue up", "Hosting rose", "Two introductions", by, at.Add(time.Hour), 1)
		return err
	}); err != nil || edited.Edit != 2 || edited.Highlights != "Revenue up" {
		t.Fatalf("the edit: %+v, %v", edited, err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.BoardPacks().EditDraft(ctx, draft.ID, "x", "y", "z", by, at, 1)
		return err
	}); !errors.As(err, &se) || se.Code != storage.CodeVersion {
		t.Fatalf("a stale edit: %v", err)
	}
	// An update is built from a published pack only: the database refuses it from a draft... the draft is not yet sent.
	var upd storage.InvestorUpdate
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		upd, err = u.InvestorUpdates().CreateDraft(ctx, draft.ID, storage.UpdateMonthly, "October update", by, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.InvestorUpdates().Send(ctx, upd.ID, []uuid.UUID{reader}, by, at, 1)
		return err
	}); err == nil {
		t.Fatal("an update was sent from an unpublished pack")
	}
	// Publishing freezes the pack with its figures; the worker cannot publish; a published pack takes no edit and no second publication.
	figs := json.RawMessage(`{"pl":{"revision":42},"period_status":"open"}`)
	if err := do(worker, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.BoardPacks().Publish(ctx, draft.ID, figs, 42, by, at, 2)
		return err
	}); err == nil {
		t.Fatal("the worker published a board pack")
	}
	var pub storage.BoardPack
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		pub, err = u.BoardPacks().Publish(ctx, draft.ID, figs, 42, by, at.Add(2*time.Hour), 2)
		return err
	}); err != nil || pub.Status != storage.PackPublished || pub.LedgerRevision != 42 || pub.PublishedAt == nil || len(pub.Figures) == 0 {
		t.Fatalf("publishing: %+v, %v", pub, err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.BoardPacks().EditDraft(ctx, draft.ID, "changed", "changed", "changed", by, at, 3)
		return err
	}); err == nil {
		t.Fatal("a published pack was edited")
	}
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+books.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM fyniq.board_pack WHERE id = $1`, `UPDATE fyniq.board_pack SET highlights = 'x', edit = edit + 1 WHERE id = $1`} {
		if _, err := su.Exec(ctx, q, draft.ID); err == nil {
			t.Errorf("%s: a published pack changed", q)
		}
	}
	// A correction is the next version, which supersedes the first.
	var v2 storage.BoardPack
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		v2, err = u.BoardPacks().CreateDraft(ctx, month, by, at.Add(24*time.Hour))
		return err
	}); err != nil ||
		v2.Version != 2 || v2.Supersedes == nil || *v2.Supersedes != draft.ID {
		t.Fatalf("the correction: %+v, %v", v2, err)
	}
	// The update, made while its pack was a draft, is sent once the pack is published, to recipients fixed then.
	var sent storage.InvestorUpdate
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		if upd, err = u.InvestorUpdates().EditDraft(ctx, upd.ID, "October update", "Revenue up", "Introductions", by, at, 1); err != nil {
			return err
		}
		sent, err = u.InvestorUpdates().Send(ctx, upd.ID, []uuid.UUID{reader}, by, at, 2)
		return err
	}); err != nil || sent.Status != storage.UpdateSent || len(sent.Recipients) != 1 || sent.SentAt == nil {
		t.Fatalf("sending: %+v, %v", sent, err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.InvestorUpdates().EditDraft(ctx, upd.ID, "x", "y", "z", by, at, 3)
		return err
	}); err == nil {
		t.Fatal("a sent update was edited")
	}
	// A read is recorded for a recipient only: the database refuses a stranger's.
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.InvestorUpdates().MarkRead(ctx, upd.ID, uuid.New(), at)
		return err
	}); err == nil {
		t.Fatal("a read was recorded for someone the update was not sent to")
	}
	var first, second bool
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		if first, err = u.InvestorUpdates().MarkRead(ctx, upd.ID, reader, at.Add(time.Hour)); err != nil {
			return err
		}
		second, err = u.InvestorUpdates().MarkRead(ctx, upd.ID, reader, at.Add(2*time.Hour))
		return err
	}); err != nil || !first || second {
		t.Fatalf("a read is recorded once: %v %v %v", first, second, err)
	}
	if _, err := su.Exec(ctx, `UPDATE fyniq.investor_update_read SET read_at = now() WHERE update_id = $1`, upd.ID); err == nil {
		t.Error("a recorded read was changed")
	}
	// Another organisation sees none of it.
	if err := do(api, other.Org, func(u storage.UnitOfWork) error {
		if _, err := u.BoardPacks().Get(ctx, draft.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the pack: %v", err)
		}
		if _, err := u.InvestorUpdates().Get(ctx, upd.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the update: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A spend alert and a dashboard layout (FR-KPI-06, FR-VIZ-09; p2-reporting-planning): only the worker raises an alert, once for a category and
// month, and nobody changes or deletes one; a layout changes only as its next version, one per role and per person, never deleted; and another
// organisation sees none of it.
// req: FR-KPI-06
// req: FR-VIZ-09
// asvs: V4.2.1
func TestAnAlertIsRaisedOnceByTheWorkerAndALayoutKeepsItsVersions(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Alert Pte Ltd")
	other := porttest.NewBooks(t, api, "Another alert Pte Ltd")
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	do := func(s storage.Store, org uuid.UUID, f func(u storage.UnitOfWork) error) error {
		return s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u) })
	}
	alert := storage.KPIAlert{Kind: storage.AlertSpendAnomaly, Category: "Hosting", Month: month, Currency: "SGD", Spend: "1000.00", Mean: "500.00", HistoryMonths: 8, LedgerRevision: 42, RaisedAt: at}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error { _, _, err := u.KPIAlerts().Raise(ctx, alert); return err }); err == nil {
		t.Fatal("the API raised an alert")
	}
	var first, second bool
	if err := do(worker, books.Org, func(u storage.UnitOfWork) (err error) {
		if _, first, err = u.KPIAlerts().Raise(ctx, alert); err != nil {
			return err
		}
		_, second, err = u.KPIAlerts().Raise(ctx, alert)
		return err
	}); err != nil || !first || second {
		t.Fatalf("raised once: %v %v %v", first, second, err)
	}
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+books.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE fyniq.kpi_alert SET spend = 1`, `DELETE FROM fyniq.kpi_alert`} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s: a raised alert changed", q)
		}
	}
	// A layout: first version, then the next; one per role; the worker saves none; a stale version is refused.
	tiles := json.RawMessage(`[{"tile":"cash","x":0,"y":0,"w":4,"h":1}]`)
	me := uuid.New()
	by := "user:" + me.String()
	if err := do(worker, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.DashboardLayouts().SaveUser(ctx, me, tiles, by, at, 0)
		return err
	}); err == nil {
		t.Fatal("the worker saved a layout")
	}
	var v1, v2 storage.DashboardLayout
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		if v1, err = u.DashboardLayouts().SaveUser(ctx, me, tiles, by, at, 0); err != nil {
			return err
		}
		v2, err = u.DashboardLayouts().SaveUser(ctx, me, json.RawMessage(`[]`), by, at.Add(time.Hour), 1)
		return err
	}); err != nil || v1.Version != 1 || v2.Version != 2 || v2.ID != v1.ID {
		t.Fatalf("the layout's versions: %+v %+v %v", v1, v2, err)
	}
	var se *storage.Error
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.DashboardLayouts().SaveUser(ctx, me, tiles, by, at, 1)
		return err
	}); !errors.As(err, &se) || se.Code != storage.CodeVersion {
		t.Fatalf("a stale layout: %v", err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.DashboardLayouts().SaveUser(ctx, me, tiles, by, at, 0) // a second first version of the same person
		return err
	}); err == nil {
		t.Fatal("a second layout of one person was saved")
	}
	if _, err := su.Exec(ctx, `DELETE FROM fyniq.dashboard_layout`); err == nil {
		t.Error("a layout was deleted")
	}
	if err := do(api, other.Org, func(u storage.UnitOfWork) error {
		if _, err := u.DashboardLayouts().OfUser(ctx, me); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation read the layout: %v", err)
		}
		l, err := u.KPIAlerts().List(ctx)
		if err != nil || len(l) != 0 {
			t.Errorf("another organisation lists %d alerts: %v", len(l), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// An Owner's cap table grant (FR-EQ-24): the API grants one live grant a person, ends it once with a reason, and nothing else changes or deletes it; the
// worker holds no write; another organisation sees none.
// req: FR-EQ-24
// asvs: V4.2.1
func TestACapTableGrantIsLiveOnceAndEndsOnce(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	books := porttest.NewBooks(t, api, "Grant Pte Ltd")
	other := porttest.NewBooks(t, api, "Another grant Pte Ltd")
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	user, by := uuid.New(), "user:"+uuid.NewString()
	do := func(s storage.Store, org uuid.UUID, f func(u storage.UnitOfWork) error) error {
		return s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u) })
	}
	if err := do(worker, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.CapTableGrants().Grant(ctx, user, "Board seat", by, at)
		return err
	}); err == nil {
		t.Fatal("the worker granted")
	}
	var g storage.CapTableGrant
	if err := do(api, books.Org, func(u storage.UnitOfWork) (err error) {
		g, err = u.CapTableGrants().Grant(ctx, user, "Board seat", by, at)
		return err
	}); err != nil || g.RevokedAt != nil {
		t.Fatalf("the grant: %+v %v", g, err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.CapTableGrants().Grant(ctx, user, "Again", by, at)
		return err
	}); err == nil {
		t.Fatal("a second live grant")
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.CapTableGrants().Revoke(ctx, g.ID, "Seat ended", by, at.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		_, err := u.CapTableGrants().Revoke(ctx, g.ID, "Again", by, at.Add(2*time.Hour))
		return err
	}); err == nil {
		t.Fatal("a grant was revoked twice")
	}
	if err := do(api, books.Org, func(u storage.UnitOfWork) error {
		if _, err := u.CapTableGrants().LiveOf(ctx, user); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("a revoked grant is live: %v", err)
		}
		_, err := u.CapTableGrants().Grant(ctx, user, "A new seat", by, at.Add(3*time.Hour))
		return err
	}); err != nil {
		t.Fatalf("granting again after the revocation: %v", err)
	}
	su := conn(t, "fyniq")
	if _, err := su.Exec(ctx, `SET app.org_id = '`+books.Org.String()+`'`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM fyniq.cap_table_grant`, `UPDATE fyniq.cap_table_grant SET reason = 'changed'`} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s: a grant changed", q)
		}
	}
	if err := do(api, other.Org, func(u storage.UnitOfWork) error {
		if l, err := u.CapTableGrants().List(ctx); err != nil || len(l) != 0 {
			t.Errorf("another organisation lists %d grants: %v", len(l), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A board pack's PDF is kept as evidence only under an approved board_pack_pdf.keep request that its requester did not sign alone: posting a draft kept-PDF document with no request at all, or
// reversing one, is refused at commit (FR-RPT-06, FR-DOC-13, CTL-FIN-07; FYR12).
// req: FR-RPT-06
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackAKeptBoardPackPDFIsPostedOnlyByApproval(t *testing.T) {
	ctx := context.Background()
	books := porttest.NewBooks(t, store(t, "fyniq_api"), "Kept pack Pte Ltd")
	payload := []byte(`{"pack_id":"` + uuid.NewString() + `","pack_version":1,"month":"2026-10","date":"2026-11-04","sha256":"00","file_name":"board-pack-2026-10-v1.pdf"}`)
	err := store(t, "fyniq_api").Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBoardPackPDF, Payload: payload})
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	})
	if storage.CodeOf(err) != storage.CodeBoardPackPDF {
		t.Errorf("a kept PDF posted with no approved request: %v", err)
	}
	// Left a draft it is harmless, and the worker holds no right to make one.
	err = store(t, "fyniq_worker").Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBoardPackPDF, Payload: payload})
		return err
	})
	if err == nil {
		t.Error("the worker made a kept-PDF document")
	}
}

// A recorded run of an allocation rule, and a month of a share grant's expense, are appended once and never reversed: the record of what a rule proposed stays as it was
// (FR-DIM-03, FR-EQ-13; FYR14).
// req: FR-DIM-03
// req: QA-ENG-05
// asvs: V4.2.1
func TestAttackARecordedRunIsNeverReversed(t *testing.T) {
	ctx := context.Background()
	books := porttest.NewBooks(t, store(t, "fyniq_api"), "Recorded run Pte Ltd")
	for _, kind := range []storage.DocumentType{storage.DocumentAllocationRun, storage.DocumentShareGrantMonth} {
		var id uuid.UUID
		if err := store(t, "fyniq_api").Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := u.Documents().Create(ctx, storage.NewDocument{Type: kind, Payload: []byte(`{"date":"2026-10-31"}`)})
			id = d.ID
			if err != nil {
				return err
			}
			return u.Documents().SetStatus(ctx, d.ID, storage.DocumentPosted)
		}); err != nil {
			t.Fatalf("%s recorded: %v", kind, err)
		}
		err := store(t, "fyniq_api").Do(ctx, books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Documents().SetStatus(ctx, id, storage.DocumentReversed)
		})
		if storage.CodeOf(err) != storage.CodeProposalRun {
			t.Errorf("a recorded %s reversed: %v", kind, err)
		}
	}
}
