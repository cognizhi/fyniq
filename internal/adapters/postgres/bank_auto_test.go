//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// autoApplied makes a rule apply by itself under an approved request, as the approval engine does (FY222).
func autoApplied(t *testing.T, st storage.Store, org uuid.UUID, r banking.Rule, now time.Time) {
	t.Helper()
	pg := store(t, "fyniq_api")
	requester, signer := founder(t, pg, org, 1, now), founder(t, pg, org, 2, now)
	req := request(t, pg, org, requester, "bank_rule", r.ID.String(), "bank_rule.auto_apply", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	if err := signs(pg, org, req, 1, signer, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(pg, org, req, "approved", now); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BankMatching().SetRuleAutoApply(ctx, r.ID, r.Version, req.ID, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// FR-BNK-14 (p1-bank-import-reconcile task 5.1, design D8; the owner's decisions of 25 Oct 2026, rev 0.142): the system
// principal reconciles, without a click and within the limits of S$500 a line and S$2,000 per rule a day,
//   - S$300.00 naming INV-0001 from ACME ANALYTICS PTE LTD: Acme's one open invoice of that amount (a receipt in full);
//   - four of five S$450.00 lines an approved auto-apply rule meets: the fifth would take the rule past S$2,000;
//   - the S$400.00 reaching savings: the missing side of a transfer in transit;
//
// and leaves in the queue a S$900.00 line (above S$500), a S$200.00 line naming INV-0003 from GLOBEX (not Acme's
// counterparty), and a S$100.00 line naming INV-0004 and INV-0005 (two open invoices). What it did is undone while the
// period is open: the receipt voided and INV-0001 open again; the transfer undone, its lines back in the queue and free
// to be recorded again; nothing is undone once the period is closed. Each week the worker logs a sample of what still
// stands in the review, once each, and may log nothing else.
// req: FR-BNK-14
// req: FR-AUT-02
// req: FM-3
// asvs: V2.3.2
func TestAutoReconcileWithinItsLimits(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Auto Reconcile Pte Ltd")
	var dbs, savings storage.MoneyAccount
	var rule banking.Rule
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, x := range []struct {
			code, name string
			to         *storage.MoneyAccount
		}{{"1010", "DBS current", &dbs}, {"1030", "DBS savings", &savings}} {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: x.code, Name: x.name, Type: storage.Asset,
				AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
			if err != nil {
				return err
			}
			if *x.to, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: x.name,
				Currency: "SGD", At: now}); err != nil {
				return err
			}
		}
		rule, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: "AWS", Position: 1, Active: true, At: now,
			When: banking.Conditions{DescriptionContains: "aws", Direction: banking.DirectionOut}, Then: banking.Action{AccountID: b.Rent.ID, GSTCode: "OP"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	autoApplied(t, s, b.Org, rule, now)
	invoices := journal.Invoices{Store: s}
	issue := func(price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", "SGD")}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		doc, _ = getDoc(t, s, b.Org, doc.ID)
		return doc
	}
	inv1, _, _, _, _ := issue("300.00"), issue("900.00"), issue("200.00"), issue("100.00"), issue("100.00")
	acme := "ACME ANALYTICS PTE LTD"
	line := func(n int, day int, amount, ref, party, desc string) storage.NewBankLine {
		return storage.NewBankLine{Position: n, BookingDate: porttest.Day(10, day), Amount: storage.Decimal(amount), Reference: ref, Counterparty: party,
			Description: desc, Fingerprint: fingerprint("auto" + string(rune('a'+n)))}
	}
	cur := importLines(t, b.Org, dbs, now,
		line(1, 20, "300.00", "INV-0001", acme, "GIRO"), line(2, 20, "900.00", "INV-0002", acme, "GIRO"),
		line(3, 20, "200.00", "INV-0003", "GLOBEX", "GIRO"), line(4, 20, "100.00", "INV-0004 INV-0005", acme, "GIRO"),
		line(5, 21, "-450.00", "", "", "AWS EMEA 1"), line(6, 21, "-450.00", "", "", "AWS EMEA 2"), line(7, 21, "-450.00", "", "", "AWS EMEA 3"),
		line(8, 21, "-450.00", "", "", "AWS EMEA 4"), line(9, 21, "-450.00", "", "", "AWS EMEA 5"), line(10, 22, "-400.00", "", "", "TO SAVINGS"))
	in := importLines(t, b.Org, savings, now, line(11, 22, "400.00", "", "", "FROM CURRENT"))
	txns := journal.BankTransactions{Store: s}
	received := moneyOf(t, "400.00", "SGD")
	if _, err := txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{FromLineID: &cur[9].ID, ToAccountID: savings.ID, Received: &received, At: now}); err != nil {
		t.Fatal(err)
	}

	run, err := banking.AutoReconcile(context.Background(), s, b.Org, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]string{}
	for _, r := range run.Reconciled {
		got[r.Line.ID] = r.Basis
		if r.Match.MadeBy != banking.AutoActor.String() || r.Match.Basis != r.Basis {
			t.Errorf("the match of %s: %+v", r.Line.Description, r.Match)
		}
	}
	for _, l := range run.Left {
		got[l.Line.ID] = "left: " + l.Why
	}
	for id, want := range map[uuid.UUID]string{cur[0].ID: storage.BasisExact, cur[1].ID: "left: " + banking.LeftLineCap, cur[2].ID: "left: " + banking.LeftNoMatch,
		cur[3].ID: "left: " + banking.LeftAmbiguous, cur[4].ID: storage.BasisRule, cur[5].ID: storage.BasisRule, cur[6].ID: storage.BasisRule,
		cur[7].ID: storage.BasisRule, cur[8].ID: "left: " + banking.LeftRuleCap, in[0].ID: storage.BasisTransfer} {
		if got[id] != want {
			t.Errorf("line %s: %q, want %q", id, got[id], want)
		}
	}
	if len(run.Reconciled) != 6 {
		t.Fatalf("reconciled %d", len(run.Reconciled))
	}
	if got := openOn(t, s, b.Org, b.AR.ID, inv1.ID, true); got != "closed" {
		t.Fatalf("INV-0001 after the exact match: %s", got)
	}
	if again, err := banking.AutoReconcile(context.Background(), s, b.Org, nil, now); err != nil || len(again.Reconciled) != 0 {
		t.Fatalf("a second run: %+v %v", again.Reconciled, err)
	}

	// The weekly sample: what stands, once each; the worker logs nothing else.
	worker := store(t, "fyniq_worker")
	week := porttest.Day(10, 19)
	if n, err := banking.Sample(context.Background(), worker, b.Org, week, porttest.Day(10, 26)); err != nil || n != 6 {
		t.Fatalf("the sample: %d %v", n, err)
	}
	if n, err := banking.Sample(context.Background(), worker, b.Org, week, porttest.Day(10, 26)); err != nil || n != 0 {
		t.Fatalf("the sample again: %d %v", n, err)
	}
	if err := banking.SampleAll(context.Background(), worker, porttest.Day(10, 26).Add(time.Hour)); err != nil {
		t.Errorf("every organisation's sample: %v", err)
	}
	err = asOrg(context.Background(), conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO fyniq.review_log_entry (kind, object_type, object_key, summary, logged_at)
			VALUES ('auto_reconciled', 'bank_line', $1, 'forged', now())`, cur[9].ID.String())
		return err
	})
	wantState(t, "the worker logging a line a person matched", err, "FY201")

	// Undone while the period is open.
	if err := banking.UndoAuto(ctx, s, b.Org, cur[0].ID, "Paid by another customer", now); err != nil {
		t.Fatal(err)
	}
	if got := openOn(t, s, b.Org, b.AR.ID, inv1.ID, true); got != "300.00 SGD" || lineOf(t, b.Org, cur[0].ID).Status != storage.LineUnreconciled {
		t.Fatalf("INV-0001 after the undo: %s", got)
	}
	var e *banking.Error
	if err := banking.UndoAuto(ctx, s, b.Org, cur[9].ID, "A person's transfer", now); !errors.As(err, &e) || e.Code != banking.CodeNotAuto {
		t.Errorf("an undo of a person's match: %v", err)
	}
	if err := banking.UndoAuto(ctx, s, b.Org, in[0].ID, "Not a transfer", now); err != nil {
		t.Fatal(err)
	}
	for _, l := range []storage.BankLine{cur[9], in[0]} {
		if got := lineOf(t, b.Org, l.ID); got.Status != storage.LineUnreconciled {
			t.Errorf("%s after the transfer's undo: %s", l.Description, got.Status)
		}
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		transit, err := u.BankMatching().InTransit(ctx)
		if err == nil && len(transit) != 0 {
			t.Errorf("an undone transfer in transit: %+v", transit)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := txns.RecordTransfer(ctx, b.Org, journal.TransferRequest{FromLineID: &cur[9].ID, ToLineID: &in[0].ID, At: now}); err != nil {
		t.Fatalf("the lines of an undone transfer recorded again: %v", err)
	}
	// Once October is closed, what the system did in it stands.
	if err := s.Do(ctx, b.Org, closePeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if err := banking.UndoAuto(ctx, s, b.Org, cur[4].ID, "Too late", now); err == nil || lineOf(t, b.Org, cur[4].ID).Status != storage.LineCategorised {
		t.Errorf("an undo in a closed period: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
