//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-04 with FR-EQ-01: a director's account marked revalue, as the standard chart marks 2500 and 2120, holding
// amounts in another currency, is revalued person by person, each line naming whose it is, as every line on it must
// (FY250). Revalued as one balance, its line named no one and the database refused the whole month's revaluation.
// USD 1,000.00 lent by Mei Lin at 1.35 (S$1,350.00) and USD 200.00 by Arjun at 1.34 (S$268.00), at the 31 October rate
// 1.33, are S$1,330.00 and S$266.00: the company owes Mei Lin 20.00 and Arjun 2.00 less, gains of 20.00 and 2.00 that
// reduce the credits, so 2500 takes 20.00 and 2.00 each naming its person and 7040 −22.00.
// req: FR-GL-04
// req: FR-EQ-01
// req: INV-13
func TestADirectorsAccountIsRevaluedPerPerson(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "Director revaluation Pte Ltd")
	var loan storage.Account
	var mei, arjun storage.Contact
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if loan, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2500", Name: "Loan from director / shareholder", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleDirectorLoan), AccountAttributes: storage.AccountAttributes{Revalue: true}}); err != nil {
			return err
		}
		if _, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "7040", Name: "Unrealised FX gain or loss", Type: storage.Revenue,
			Role: storage.RoleUnrealisedFX}); err != nil {
			return err
		}
		if _, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 31), Rate: "1.33", Quote: "1.33",
			QuoteForm: "base-per-unit"}); err != nil {
			return err
		}
		if mei, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Mei Lin Tan", Director: true, Shareholder: true, Country: "SG"}); err != nil {
			return err
		}
		arjun, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Arjun Rao", Director: true, Shareholder: true, Country: "SG"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lend := func(day int, who storage.Contact, usd, rate, sgd string) {
		t.Helper()
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			id := uuid.NewString()
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, day), PostingIdentity: id, RuleVersion: "test@1",
				Source: storage.Source{Type: "manual_journal", ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: b.Bank.ID, Currency: "SGD", Amount: storage.Decimal(sgd), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(sgd)},
					{AccountID: loan.ID, Currency: "USD", Amount: storage.Decimal("-" + usd), FXRate: storage.Decimal(rate), FXRateSource: "document",
						BaseAmount: storage.Decimal("-" + sgd), PartyID: &who.ID},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	lend(10, mei, "1000.00", "1.35", "1350.00")
	lend(12, arjun, "200.00", "1.34", "268.00")

	var r fx.Run
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = journal.PostRevaluation(ctx, u, b.Oct.ID, porttest.Day(11, 1))
		return err
	}); err != nil {
		t.Fatalf("the month's revaluation: %v", err)
	}
	if r.Entry == nil {
		t.Fatal("the run posted nothing")
	}
	got := map[string]string{}
	for _, l := range r.Entry.Lines {
		who := "-"
		if l.PartyID != nil {
			who = l.PartyID.String()
		}
		got[l.AccountID.String()+" "+who] = string(l.BaseAmount)
	}
	for key, want := range map[string]string{
		loan.ID.String() + " " + mei.ID.String():   "20.0000",
		loan.ID.String() + " " + arjun.ID.String(): "2.0000",
	} {
		if got[key] != want {
			t.Errorf("%s: %q, want %s; the entry %v", key, got[key], want, got)
		}
	}
}
