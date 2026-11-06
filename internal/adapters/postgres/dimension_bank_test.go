//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 and FR-DIM-02 on bank lines (rev 0.171; slice 3d): Rent requiring Department, a rule applied by itself
// (FR-BNK-14) tags its line with the rule's Engineering, and one with no value posts its line without a department, as
// the system's postings do, for Appendix C.2 #60 to flag; a person's categorisation of a part on Rent without one is
// refused (DIM-05) and with Engineering posts; a rule naming a value out of use is refused; every entry replays.
// req: FR-DIM-01
// req: FR-DIM-02
// req: FR-BNK-14
// req: FR-BNK-05
func TestBankLinePartsCarryTheirDimensionValues(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Dimension bank Pte Ltd")
	var dbs storage.MoneyAccount
	var eng, old storage.DimensionValue
	var aws, zoom banking.Rule
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		if err != nil {
			return err
		}
		if dbs, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: "DBS current",
			Currency: "SGD", At: now}); err != nil {
			return err
		}
		dept, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
		if err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		if old, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "OLD", Name: "Old team"}); err != nil {
			return err
		}
		if _, err = u.Dimensions().ChangeValue(ctx, old.ID, old.Version, storage.DimensionChange{Name: old.Name, Active: false}); err != nil {
			return err
		}
		if aws, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: "AWS", Position: 1, Active: true, At: now,
			When: banking.Conditions{DescriptionContains: "aws", Direction: banking.DirectionOut},
			Then: banking.Action{AccountID: b.Rent.ID, GSTCode: "OP", Values: []uuid.UUID{eng.ID}}}); err != nil {
			return err
		}
		if zoom, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: "Zoom", Position: 2, Active: true, At: now,
			When: banking.Conditions{DescriptionContains: "zoom", Direction: banking.DirectionOut}, Then: banking.Action{AccountID: b.Rent.ID, GSTCode: "OP"}}); err != nil {
			return err
		}
		ra, err := u.Accounts().Get(ctx, b.Rent.ID)
		if err != nil {
			return err
		}
		c := storage.AccountChange{Name: ra.Name, ParentID: ra.ParentID, Group: ra.Group, Active: ra.Active, AccountAttributes: ra.AccountAttributes}
		c.RequiredDimensions = []uuid.UUID{dept.ID}
		_, err = u.Accounts().Update(ctx, ra.ID, ra.Version, c)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := banking.CreateRule(ctx, u, banking.RuleRequest{Name: "Old", Position: 3, Active: true, At: now,
			When: banking.Conditions{DescriptionContains: "old"}, Then: banking.Action{AccountID: b.Rent.ID, GSTCode: "OP", Values: []uuid.UUID{old.ID}}})
		return err
	}); err == nil {
		t.Error("a rule naming a value out of use was saved")
	}
	autoApplied(t, s, b.Org, aws, now)
	autoAppliedBy(t, s, b.Org, zoom, now, 3)

	line := func(n int, amount, desc string) storage.NewBankLine {
		return storage.NewBankLine{Position: n, BookingDate: porttest.Day(10, 21), Amount: storage.Decimal(amount), Description: desc,
			Fingerprint: fingerprint(fmt.Sprintf("dim-bank-%d", n))}
	}
	lines := importLines(t, b.Org, dbs, now, line(1, "-100.00", "AWS EMEA"), line(2, "-50.00", "ZOOM VIDEO"), line(3, "-20.00", "COFFEE"))
	run, err := banking.AutoReconcile(context.Background(), journal.BankTransactions{Store: s}, b.Org, nil, now)
	if err != nil || len(run.Reconciled) != 2 {
		t.Fatalf("the rules applied by themselves: %+v %v", run, err)
	}

	txns := journal.BankTransactions{Store: s}
	coffee := banktransaction.Draft{LineID: lines[2].ID, Parts: []banktransaction.Part{{AccountID: b.Rent.ID, GSTCode: "OP"}}}
	var probs ledger.Problems
	if _, err := txns.Categorise(ctx, b.Org, journal.Categorisation{Draft: coffee, Basis: storage.BasisPerson, At: now}); !errors.As(err, &probs) ||
		!probs.Has(ledger.CodeDimensionRequired) {
		t.Errorf("a person's part with no department: %v", err)
	}
	coffee.Parts[0].Values = []uuid.UUID{eng.ID}
	if _, err := txns.Categorise(ctx, b.Org, journal.Categorisation{Draft: coffee, Basis: storage.BasisPerson, At: now}); err != nil {
		t.Fatal(err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rows, err := balance.BySet(ctx, u, money.MustLookup("SGD"), b.Oct.ID)
		if err != nil {
			return err
		}
		var got []string
		for _, r := range rows {
			if r.AccountID == b.Rent.ID {
				got = append(got, fmt.Sprintf("%v %s", r.Values, r.Base))
			}
		}
		if want := []string{"[] 50.00", fmt.Sprintf("[%s] 120.00", eng.ID)}; !slices.Equal(got, want) {
			t.Errorf("Rent by set: %q, want %q", got, want)
		}
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) > 0 {
			t.Errorf("replay: %+v", rep.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// autoAppliedBy is autoApplied with founders from the first'th on, for a second rule of an organisation.
func autoAppliedBy(t *testing.T, st storage.Store, org uuid.UUID, r banking.Rule, now time.Time, first byte) {
	t.Helper()
	pg := store(t, "fyniq_api")
	requester, signer := founder(t, pg, org, first, now), founder(t, pg, org, first+1, now)
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
