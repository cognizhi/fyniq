//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// claimBooks are Books with the claim rule's system accounts, three expense accounts, the starter-like categories, a
// founder who claims, another who approves, and a user who is not a member.
type claimBooks struct {
	porttest.Books
	Payable, Overseas, Meals, Other storage.Account
	Travel, Dining, Abroad, Mileage storage.ClaimCategory
	Claimant, Approver, Stranger    uuid.UUID
}

func newClaimBooks(t *testing.T, ctx context.Context, s *postgres.Store, name string) claimBooks {
	t.Helper()
	b := claimBooks{Books: porttest.NewBooks(t, s, name)}
	now := porttest.Day(10, 1)
	b.Claimant, b.Approver = founder(t, s, b.Org, 1, now), founder(t, s, b.Org, 2, now)
	if err := conn(t, "fyniq_migrator").QueryRow(ctx, "INSERT INTO fyniq.app_user (display_name) VALUES ('Not A Member') RETURNING user_id").
		Scan(&b.Stranger); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, a := range []struct {
			to *storage.Account
			n  storage.NewAccount
		}{
			{&b.Payable, storage.NewAccount{Code: "2150", Name: "Expense claims payable", Type: storage.Liability,
				Role: storage.AccountRole(ledger.RoleExpenseClaimsPayable)}},
			{new(storage.Account), storage.NewAccount{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)}},
			{new(storage.Account), storage.NewAccount{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense,
				Role: storage.AccountRole(ledger.RoleFXRounding)}},
			{&b.Overseas, storage.NewAccount{Code: "6520", Name: "Overseas travel", Type: storage.Expense}},
			{&b.Meals, storage.NewAccount{Code: "6530", Name: "Meals & entertainment", Type: storage.Expense}},
			{&b.Other, storage.NewAccount{Code: "6531", Name: "Staff welfare", Type: storage.Expense}},
		} {
			if *a.to, err = u.Accounts().Create(ctx, a.n); err != nil {
				return err
			}
		}
		for _, c := range []struct {
			to *storage.ClaimCategory
			n  storage.NewClaimCategory
		}{
			{&b.Travel, storage.NewClaimCategory{Name: "Local travel", AccountID: b.Rent.ID}},
			{&b.Dining, storage.NewClaimCategory{Name: "Meals & entertainment", AccountID: b.Meals.ID}},
			{&b.Abroad, storage.NewClaimCategory{Name: "Overseas travel", AccountID: b.Overseas.ID}},
			{&b.Mileage, storage.NewClaimCategory{Name: "Mileage", AccountID: b.Rent.ID, Mileage: true}},
		} {
			c.n.Currency, c.n.Active, c.n.At = "SGD", true, now
			if *c.to, err = u.ClaimCategories().Create(ctx, c.n); err != nil {
				return err
			}
		}
		if _, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: now, GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: now}); err != nil {
			return err
		}
		_, err = u.FXRates().Enter(ctx, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "IDR", On: porttest.Day(10, 23), Rate: "0.0000850",
			Quote: "0.0000850", QuoteForm: "base-per-unit", Note: "MAS"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// FR-AP-07 and the posting rule "Posting an expense claim" (p1-expense-claims task 1.2): a member's claim is drafted
// with its number, checked and posted through the posting service in one transaction with its entry. UC-EXP-01's
// S$27.30 taxi shows no GST; a S$109.00 meal on a simplified tax invoice claims its S$9.00 GST, the organisation being
// GST-registered; an IDR 1,500,000.00 hotel takes the cascade's rate on its date, S$127.50. The company owes S$263.80
// on 2150. It posts only under an approved request its claimant asked for and did not sign (FY233). Every problem comes
// back at once; a posted claim does not change; the replay reproduces the entry after its category moves to another
// account.
// req: FR-AP-07
// req: ledger/posting-rules Posting an expense claim
// req: purchases/expense-claims Out-of-pocket receipt claim
// req: FR-AP-20
// req: FR-AP-17
// req: ENG-06
// req: QA-ENG-05
// asvs: V8.2.2
func TestExpenseClaimsPostThroughThePostingService(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "mei-ling"}})
	s := store(t, "fyniq_api")
	b := newClaimBooks(t, ctx, s, "Claims posting Pte Ltd")
	claims := journal.Claims{Store: s}
	sgd, idr := money.MustLookup("SGD"), money.MustLookup("IDR")
	oct24, oct26 := porttest.Day(10, 24), porttest.Day(10, 26) // the simplified-invoice limit is in sg-gst@2026.2 from 23 October
	taxi := claim.Line{Date: oct24, Merchant: "ComfortDelGro", CategoryID: b.Travel.ID, Description: "Taxi to Acme", Amount: sgdOf(t, "27.30"),
		GSTCode: "NR", GST: money.Zero(sgd)}
	meal := claim.Line{Date: oct24, Merchant: "Din Tai Fung", CategoryID: b.Dining.ID, Description: "Lunch with Acme", Amount: sgdOf(t, "109.00"),
		GSTCode: "TX", GST: sgdOf(t, "9.00"), Document: bill.DocSimplified, Attendees: "Mei Ling Tan; Tan Wei, Acme"}
	hotel := claim.Line{Date: porttest.Day(10, 23), Merchant: "Hotel Indonesia", CategoryID: b.Abroad.ID, Amount: moneyOf(t, "1500000.00", "IDR"),
		GSTCode: "OP", GST: money.Zero(idr)}
	draft := claim.Draft{ClaimantID: b.Claimant, Date: oct26, Title: "Acme visit", Lines: []claim.Line{taxi, meal, hotel}}

	doc, err := claims.Create(ctx, b.Org, draft)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Number != "CLM-0001" || doc.Status != storage.DocumentDraft {
		t.Fatalf("a claim is numbered when it is saved: %+v", doc)
	}
	c, err := claims.Check(ctx, b.Org, doc.ID)
	if err != nil || len(c.Problems) > 0 || c.Owed.String() != "263.80" {
		t.Fatalf("the check: %+v %v", c.Problems, err)
	}
	if got := []string{c.Prepared.InputTax[0].Reason, c.Prepared.InputTax[1].Reason}; got[0] != bill.ReasonNoGST || got[1] != bill.ReasonSimplified {
		t.Errorf("input tax decided as %v", got)
	}
	// Posted without an approval, or under one its claimant signed, the claim is refused at commit (FY233).
	if _, err := claims.Post(ctx, b.Org, doc.ID); storage.CodeOf(err) != storage.CodeClaim {
		t.Fatalf("posting without an approval: %v", err)
	}
	selfSigned := request(t, s, b.Org, b.Approver, "expense_claim", doc.ID.String(), "expense_claim.post", `{"execute":{}}`,
		[]storage.ApprovalStep{{Quorum: 1}}, oct26)
	if err := signs(s, b.Org, selfSigned, 1, b.Claimant, oct26); err != nil {
		t.Fatal(err)
	}
	if err := decides(s, b.Org, selfSigned, "approved", oct26); err != nil {
		t.Fatal(err)
	}
	if _, err := claims.Post(ctx, b.Org, doc.ID); storage.CodeOf(err) != storage.CodeClaim {
		t.Fatalf("posting under the claimant's own signature: %v", err)
	}
	approved(t, s, b, doc.ID, oct26)
	e, err := claims.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, e); got != "6100 27.3 SGD = 27.3; 6530 100 SGD = 100; 6520 1500000 IDR = 127.5; 2200 9 SGD = 9; 2150 -263.8 SGD = -263.8" {
		t.Errorf("the entry: %s", got)
	}
	if e.Source.Type != ledger.SourceExpenseClaim || e.RuleVersion != ledger.RuleExpenseClaim || !e.PostingDate.Equal(oct26) {
		t.Errorf("the entry's source: %+v", e)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = u.Documents().Get(ctx, doc.ID)
		return err
	}); err != nil || doc.Number != "CLM-0001" || doc.Status != storage.DocumentPosted {
		t.Fatalf("the posted claim: %+v %v", doc, err)
	}
	if _, err := claims.Post(ctx, b.Org, doc.ID); !claimCode(err, claim.CodeNotDraft) {
		t.Errorf("posting twice: %v", err)
	}
	if _, err := claims.Revise(ctx, b.Org, doc.ID, draft); !claimCode(err, claim.CodeNotDraft) {
		t.Errorf("revising a posted claim: %v", err)
	}

	// The category moves to another account: the replay still reproduces what the claim posted (ENG-06).
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.ClaimCategories().Change(ctx, b.Dining.ID, b.Dining.Version, storage.NewClaimCategory{Name: b.Dining.Name, AccountID: b.Other.ID,
			Active: true, At: oct26})
		if err != nil {
			return err
		}
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Paid back by hand from DBS (FR-AP-07): Dr 2150 · Cr DBS, an allocation to the claim; voided, a reversing allocation.
	var dbs storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1020", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pays := journal.ClaimReimbursements{Store: s}
	back, err := pays.Record(ctx, b.Org, claim.Reimbursement{ClaimantID: b.Claimant, Date: oct26, AccountID: dbs.ID, Reference: "FAST 1",
		Claims: []claim.Part{{ClaimID: doc.ID, Amount: sgdOf(t, "263.80")}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, back.Entry); got != "2150 263.8 SGD = 263.8; 1020 -263.8 SGD = -263.8" || back.Reimbursement.Number != "RB-0001" || len(back.Allocations) != 1 {
		t.Errorf("the reimbursement: %s %+v", got, back.Reimbursement)
	}
	if _, err := pays.Record(ctx, b.Org, claim.Reimbursement{ClaimantID: b.Claimant, Date: oct26, AccountID: dbs.ID,
		Claims: []claim.Part{{ClaimID: doc.ID, Amount: sgdOf(t, "0.01")}}}); !hasProblem(err, claim.CodeOverpaid) {
		t.Errorf("paying back more than is owed: %v", err)
	}
	if v, err := pays.Void(ctx, b.Org, back.Reimbursement.ID, oct26, "Paid to the wrong account"); err != nil || v.Reimbursement.Status != storage.DocumentReversed ||
		len(v.Allocations) != 1 || v.Allocations[0].Amount != "-263.8000" && v.Allocations[0].Amount != "-263.80" {
		t.Fatalf("voiding it: %+v %v", v, err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) > 0 {
			t.Errorf("the replay after the reimbursement: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A reimbursement settles expense claims, and nothing else (FY171).
	wantState(t, "a reimbursement settling a reimbursement", asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount, base_currency,
base_minor_units, base_amount, realised_fx, written_by, written_at) VALUES ($1, 1, $1, '2026-10-26', 'SGD', 2, 1, 'SGD', 2, 1, 0, 'user:x', now())`,
			back.Reimbursement.ID)
		return err
	}), "FY171")

	// A returned claim is a draft its claimant revises: each version is kept (UC-EXP-04's history).
	second, err := claims.Create(ctx, b.Org, claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{taxi}})
	if err != nil || second.Number != "CLM-0002" {
		t.Fatalf("a second claim: %+v %v", second, err)
	}
	if second, err = claims.Revise(ctx, b.Org, second.ID, claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{taxi, meal}}); err != nil ||
		second.Version != 2 {
		t.Fatalf("revising a draft: %+v %v", second, err)
	}

	// Every problem at once (ENG-22): no claimant, a stranger, the mileage category on a receipt, a receipt after the
	// claim's date with no merchant, a GST code that is not for purchases, an IDR receipt with no rate on its date.
	late := taxi
	late.Date, late.Merchant = porttest.Day(10, 28), " "
	onMileage := taxi
	onMileage.CategoryID = b.Mileage.ID
	sales := taxi
	sales.GSTCode = "SR"
	noRate := hotel
	noRate.Date = porttest.Day(10, 24)
	for _, x := range []struct {
		what  string
		d     claim.Draft
		field string
		code  string
	}{
		{"no claimant", claim.Draft{Date: oct26, Lines: []claim.Line{taxi}}, "claimant_id", claim.CodeClaimant},
		{"a stranger", claim.Draft{ClaimantID: b.Stranger, Date: oct26, Lines: []claim.Line{taxi}}, "claimant_id", claim.CodeClaimant},
		{"the mileage category", claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{onMileage}}, "lines[1].category_id", claim.CodeCategory},
		{"after the claim", claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{late}}, "lines[1].date", claim.CodeReceipt},
		{"no merchant", claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{late}}, "lines[1].merchant", claim.CodeReceipt},
		{"a sales code", claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{sales}}, "lines[1].gst_code", claim.CodeGSTCode},
		{"no rate", claim.Draft{ClaimantID: b.Claimant, Date: oct26, Lines: []claim.Line{noRate}}, "lines[1].fx_rate", "FX-01"},
		{"no lines", claim.Draft{ClaimantID: b.Claimant, Date: oct26}, "lines", ledger.CodeTooFewLines},
		{"no date", claim.Draft{ClaimantID: b.Claimant, Lines: []claim.Line{taxi}}, "date", ledger.CodeNoDate},
	} {
		d, err := claims.Create(ctx, b.Org, x.d)
		if err != nil {
			t.Fatalf("%s: %v", x.what, err)
		}
		c, err := claims.Check(ctx, b.Org, d.ID)
		if err != nil {
			t.Fatalf("%s: %v", x.what, err)
		}
		found := false
		for _, p := range c.Problems {
			found = found || p.Field == x.field && p.Code == x.code
		}
		if !found {
			t.Errorf("%s: want %s on %s, got %+v", x.what, x.code, x.field, c.Problems)
		}
		if _, err := claims.Post(ctx, b.Org, d.ID); err == nil {
			t.Errorf("%s: posted", x.what)
		}
	}
	if _, err := claims.Create(ctx, b.Org, claim.Draft{ClaimantID: b.Claimant, Lines: []claim.Line{{Document: "receipt", Amount: sgdOf(t, "1.00"),
		GST: money.Zero(sgd)}}}); !claimCode(err, claim.CodeReceipt) {
		t.Errorf("a document Fyniq cannot read: %v", err)
	}
}

// approved approves posting claim id as the approval engine does: its claimant asks, and the other founder signs.
func approved(t *testing.T, s *postgres.Store, b claimBooks, id uuid.UUID, now time.Time) {
	t.Helper()
	req := request(t, s, b.Org, b.Claimant, "expense_claim", id.String(), "expense_claim.post", `{"execute":{}}`, []storage.ApprovalStep{{Quorum: 1}}, now)
	if err := signs(s, b.Org, req, 1, b.Approver, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(s, b.Org, req, "approved", now); err != nil {
		t.Fatal(err)
	}
}

func claimCode(err error, code string) bool {
	var ce *claim.Error
	return errors.As(err, &ce) && ce.Code == code
}
