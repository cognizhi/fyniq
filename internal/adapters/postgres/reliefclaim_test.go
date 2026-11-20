//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/reliefclaim"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// GST bad debt relief (FR-AR-11, RND-BDR; the posting rule bad_debt_relief@1), worked by hand: INV-0001 of S$10.00 on 14 Oct 2026 at 9% owes S$10.90 with S$0.90 of output tax; S$10.00 of it
// written off as a bad debt on 31 Oct 2026 claims 0.90 x 10.00 / 10.90 = 0.8257 = S$0.83 on or after 14 Oct 2027 (twelve months from the supply), Dr 2200, Cr the bad-debt account; the claim replays
// (ENG-06); the database holds two backstops whatever the application does: a write-off is claimed once while a claim of it lives (FYT01), and a write-off with a live claim is not reversed (FYT02), and
// another organisation's write-off cannot be claimed (tenancy).
// req: FR-AR-11
// req: RND-BDR
// req: ENG-06
// req: INV-07
// asvs: V4.1.3, V8.2.1
func TestGSTBadDebtReliefIsClaimedOnceAndNotOverARecordedWriteOff(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "tax-agent"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Relief Pte Ltd")
	invoices, writeOffs, claims := journal.Invoices{Store: s}, journal.WriteOffs{Store: s}, journal.ReliefClaims{Store: s}
	sgd := money.MustLookup("SGD")
	var badDebts storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if badDebts, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "6470", Name: "Bad debts", Type: storage.Expense}); err != nil {
			return err
		}
		if _, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2027, 10, 31, 0, 0, 0, 0, time.UTC)}); err != nil {
			return err
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	inv, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(10, 14), Currency: sgd, Lines: []invoice.Line{invoiceLine(t, b, "1", "10.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, inv.ID); err != nil {
		t.Fatal(err)
	}
	ten, _ := money.Parse("10.00", sgd)
	wo, err := writeOffs.Create(ctx, b.Org, writeoff.Draft{InvoiceID: inv.ID, Date: porttest.Day(10, 31), Amount: ten, Reason: writeoff.ReasonBadDebt, AccountID: badDebts.ID, Note: "The customer went into liquidation"})
	if err != nil {
		t.Fatal(err)
	}
	// a claim of a draft write-off is refused by the rules; so is one before the twelve months or after today
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	today := day(2027, 10, 20)
	if _, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 14)}, today); !hasReliefCode(err, reliefclaim.CodeWriteOff) {
		t.Fatalf("a claim of a draft write-off: %v", err)
	}
	if _, err := writeOffs.Post(ctx, b.Org, wo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 13)}, today); !hasReliefCode(err, reliefclaim.CodeNotYet) {
		t.Fatalf("before the twelve months: %v", err)
	}
	if _, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 21)}, today); !hasReliefCode(err, reliefclaim.CodeFuture) {
		t.Fatalf("after today: %v", err)
	}

	posted, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 14), Note: "Winding-up order 18 Sep"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, posted.Entry); got != "2200 0.83 SGD = 0.83; 6470 -0.83 SGD = -0.83" || posted.Entry.RuleVersion != "bad_debt_relief@1" {
		t.Fatalf("the claim: %s", got)
	}
	if _, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 15)}, today); !hasReliefCode(err, reliefclaim.CodeClaimed) {
		t.Errorf("a second claim: %v", err)
	}

	// the database: a second live claim and a reversed write-off are refused whatever the application does
	other, err := claimDocument(ctx, s, b.Org, wo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, other, storage.DocumentPosted)
	}); err == nil || !strings.Contains(err.Error(), "claimed already") {
		t.Errorf("a second live claim at the database: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, wo.ID, storage.DocumentReversed)
	}); err == nil || !strings.Contains(err.Error(), "bad debt relief claimed") {
		t.Errorf("reversing a write-off with a live claim at the database: %v", err)
	}
	if _, err := writeOffs.Void(ctx, b.Org, wo.ID, day(2027, 10, 20), "Reinstated"); err == nil {
		t.Error("voiding a write-off with a live claim")
	}
	// tenancy: another organisation cannot claim this write-off
	rival := newSalesBooks(t, ctx, s, "Rival Pte Ltd")
	foreign, err := claimDocument(ctx, s, rival.Org, wo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, rival.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, foreign, storage.DocumentPosted)
	}); err == nil || !strings.Contains(err.Error(), "not") {
		t.Errorf("a claim of another organisation's write-off: %v", err)
	}

	// the replay reproduces the claim (ENG-06); a void reverses it and frees the write-off
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) > 0 {
			t.Errorf("the replay: %+v", rep.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := claims.Void(ctx, b.Org, posted.Claim.ID, day(2027, 10, 20), "The debtor paid"); err != nil {
		t.Fatal(err)
	}
	if _, err := claims.Claim(ctx, b.Org, reliefclaim.Request{WriteOffID: wo.ID, Date: day(2027, 10, 20)}, today); err != nil {
		t.Errorf("a claim after the void: %v", err)
	}
}

func hasReliefCode(err error, code string) bool {
	var re *reliefclaim.Error
	return errors.As(err, &re) && re.Code == code
}

// claimDocument saves, as a draft, a claim of a write-off the way an attacker with the API role would: a document of the type, naming it.
func claimDocument(ctx context.Context, s storage.Store, org, writeOff uuid.UUID) (uuid.UUID, error) {
	b, _ := json.Marshal(map[string]string{"write_off_id": writeOff.String(), "date": "2027-10-14"})
	var id uuid.UUID
	err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBadDebtRelief, Payload: b})
		id = d.ID
		return err
	})
	return id, err
}
