//go:build local

package banking_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// day is a calendar date in 2026.
func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

// Rules are checked when saved, every problem at once; they are kept in their order and change one version at a time;
// a back-test counts the lines held that a rule would have matched; and what a rule suggests posts as a person's
// categorisation does.
// req: FR-BNK-05
// req: FR-BNK-06
func TestRulesAreCheckedSavedAndBackTested(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	w := start(t)
	var rent, receivables storage.Account
	var supplier storage.Contact
	var lines []storage.BankLine
	if err := w.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: day(10, 1), EndsOn: day(10, 31)}); err != nil {
			return err
		}
		if rent, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "6100", Name: "Cloud hosting", Type: storage.Expense}); err != nil {
			return err
		}
		if receivables, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Accounts receivable", Type: storage.Asset,
			Role: storage.AccountRole(ledger.RoleAccountsReceivable)}); err != nil {
			return err
		}
		if supplier, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Amazon Web Services EMEA SARL", Vendor: true}); err != nil {
			return err
		}
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: w.sgd.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: day(10, 1), PeriodTo: day(10, 4), Currency: "SGD", At: now, Lines: []storage.NewBankLine{
				{Position: 1, BookingDate: day(10, 1), Amount: "-120.50", Description: "GIRO AWS EMEA", Fingerprint: strings.Repeat("1", 64)},
				{Position: 2, BookingDate: day(10, 2), Amount: "-99.00", Description: "giro aws emea", Fingerprint: strings.Repeat("2", 64)},
				{Position: 3, BookingDate: day(10, 3), Amount: "-50.00", Description: "GOOGLE CLOUD", Fingerprint: strings.Repeat("3", 64)},
				{Position: 4, BookingDate: day(10, 4), Amount: "1000.00", Description: "ACME ROBOTICS INV-0001", Fingerprint: strings.Repeat("4", 64)},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	do := func(fn func(ctx context.Context, u storage.UnitOfWork) error) error {
		return w.store.Do(ctx, w.org, fn)
	}

	// Every problem at once.
	stranger := uuid.New()
	err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := banking.CreateRule(ctx, u, banking.RuleRequest{Name: " ", Position: 0, When: banking.Conditions{DescriptionRegex: `(a)\1`},
			Then: banking.Action{AccountID: w.sgd.AccountID, GSTCode: "ZZ", ContactID: &stranger, Reference: strings.Repeat("r", banking.MaxReference+1)}, At: now})
		return err
	})
	var e *banking.Error
	if !errors.As(err, &e) || e.Code != banking.CodeRule {
		t.Fatalf("a rule with every problem: %v", err)
	}
	for _, want := range []string{"the name has 0 characters", "the position 0", "description_regex", "a money account", `"ZZ" is not a GST code`,
		"contact does not exist", "the reference has 141 characters"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("the refusal does not say %q: %s", want, e.Message)
		}
	}
	for name, a := range map[string]banking.Action{"receivables": {AccountID: receivables.ID}, "no account": {AccountID: uuid.New()}} {
		if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := banking.CreateRule(ctx, u, banking.RuleRequest{Name: name, Position: 1, When: banking.Conditions{Direction: banking.DirectionIn}, Then: a, At: now})
			return err
		}); !errors.As(err, &e) || e.Code != banking.CodeRule {
			t.Errorf("an action to %s: %v", name, err)
		}
	}

	// Two rules, kept in their order; a stale change is refused.
	var aws, cloud banking.Rule
	if err := do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if cloud, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: "Small cloud bills", Position: 2, Active: true, At: now,
			When: banking.Conditions{Direction: banking.DirectionOut, AmountMax: "100"}, Then: banking.Action{AccountID: rent.ID, GSTCode: "TX"}}); err != nil {
			return err
		}
		aws, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: "AWS", Position: 1, Active: true, At: now,
			When: banking.Conditions{DescriptionContains: "aws"}, Then: banking.Action{AccountID: rent.ID, ContactID: &supplier.ID, GSTCode: "OP", Reference: "AWS"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if aws.AutoApply || aws.Version != 1 {
		t.Errorf("a new rule never auto-applies: %+v", aws.BankRule)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		rules, bad, err := banking.Rules(ctx, u)
		if err != nil || len(bad) > 0 || len(rules) != 2 || rules[0].ID != aws.ID || rules[1].ID != cloud.ID {
			t.Errorf("the rules in order: %+v %v %v", rules, bad, err)
		}
		for want, l := range map[string]storage.BankLine{"AWS": lines[1], "Small cloud bills": lines[2], "": lines[3]} {
			r, ok := banking.First(rules, l)
			if got := map[bool]string{true: r.Name}[ok]; got != want {
				t.Errorf("%s: %q, want %q", l.Description, got, want)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := banking.ChangeRule(ctx, u, aws.ID, aws.Version+1, banking.RuleRequest{Name: "AWS", Position: 1, Active: true, At: now,
			When: aws.When, Then: aws.Then})
		return err
	}); storage.CodeOf(err) != storage.CodeVersion {
		t.Errorf("a stale change: %v", err)
	}

	// The back-test: "this rule would have matched 2 lines", the latest first.
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		bt, err := banking.RunBackTest(ctx, u, aws.When)
		if err == nil && (bt.Examined != 4 || bt.Matched != 2 || bt.ByStatus[storage.LineUnreconciled] != 2 || len(bt.Sample) != 2 ||
			bt.Sample[0].ID != lines[1].ID) {
			t.Errorf("the back-test: %+v", bt)
		}
		if _, err := banking.RunBackTest(ctx, u, banking.Conditions{}); !errors.As(err, &e) {
			t.Errorf("a back-test of no conditions: %v", err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// What the rule suggests posts: Dr 6100 120.50, Cr 1010 120.50 (out of scope, OP), the line categorised.
	got, err := journal.BankTransactions{Store: w.store}.Categorise(ctx, w.org, journal.Categorisation{Draft: aws.Draft(lines[0]), At: now})
	if err != nil || got.Line.Status != storage.LineCategorised || len(got.Entry.Lines) != 2 {
		t.Fatalf("the rule's coding posted: %+v %v", got, err)
	}
	if err := do(func(ctx context.Context, u storage.UnitOfWork) error {
		bt, err := banking.RunBackTest(ctx, u, aws.When)
		if err == nil && (bt.ByStatus[storage.LineCategorised] != 1 || bt.ByStatus[storage.LineUnreconciled] != 1) {
			t.Errorf("the back-test by status: %+v", bt.ByStatus)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
