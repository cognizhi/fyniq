//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SOD-5 is the database's too (FY267; PRD-001 rev 0.179): in Standard mode Ann, who decided a line of DBS current in
// October, signs off its October reconciliation only with the approved override that holds exactly it, asked for by
// her with the reason it keeps; a pending override, another reason, or someone else's sign-off under her override are
// refused. In Founder mode, before the switch, the reconciliation of the USD account needs no override.
// req: SOD-5
// asvs: V8.2.2
func TestAttackAReconciliationKeepsSOD5(t *testing.T) {
	ctx := context.Background()
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	a := newBankedBooks(t, "Attack SOD-5 Pte Ltd", now)
	st := store(t, "fyniq_api")
	ann, ben := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now)
	as := func(who uuid.UUID) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who.String()}})
	}
	one := []storage.ApprovalStep{{Quorum: 1}}

	// Ann excludes a line of each account in October; Ben prepares both reconciliations.
	usdLines := importUSD(t, st, a, now)
	for _, l := range []storage.BankLine{a.lines[2], usdLines[0]} {
		if err := st.Do(as(ann), a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Banking().SetLineStatus(ctx, l.ID, l.Version, storage.LineExcluded, now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func(m storage.MoneyAccount) storage.BankReconciliation {
		var rec storage.BankReconciliation
		if err := st.Do(as(ben), a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			rec, err = u.BankMatching().PrepareReconciliation(ctx, storage.NewBankReconciliation{MoneyAccountID: m.ID, PeriodID: a.Oct.ID,
				Currency: m.Currency, StatementBalance: "0.00", BookBalance: "0.00", UnreconciledTotal: "0.00", Difference: "0.00", At: now})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	sign := func(who uuid.UUID, rec storage.BankReconciliation, override *storage.ApprovalRequest, reason string) error {
		return st.Do(as(who), a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			if override == nil {
				_, err := u.BankMatching().SignReconciliation(ctx, rec.ID, rec.Version, now, now)
				return err
			}
			_, err := u.BankMatching().SignReconciliationOverridden(ctx, rec.ID, rec.Version, "user:"+who.String(), now, now, override.ID, reason)
			return err
		})
	}

	// Founder mode: no override needed.
	if err := sign(ann, prepare(a.usd), nil, ""); err != nil {
		t.Fatalf("Ann signing off in Founder mode: %v", err)
	}

	// Standard mode.
	sw := request(t, st, a.Org, ann, "team_mode", "organisation", "team_mode.change", `{"execute":{"mode":"standard","version":1}}`, one, now)
	if err := signs(st, a.Org, sw, 1, ben, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, sw, "approved", now); err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.TeamModes().Add(ctx, storage.NewTeamMode{Version: 1, Mode: "standard", RequestID: sw.ID, CreatedBy: ann, CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rec := prepare(a.sgd)
	wantState(t, "a sign-off by the one who decided a line, with no override", sign(ann, rec, nil, ""), "FY267")
	const reason = "Ben is on leave"
	override := request(t, st, a.Org, ann, "bank_reconciliation", rec.ID.String(), "bank_reconciliation.sign",
		`{"execute":{"reason":"`+reason+`"}}`, one, now)
	wantState(t, "a sign-off under a pending override", sign(ann, rec, &override, reason), "FY267")
	if err := signs(st, a.Org, override, 1, ben, now); err != nil {
		t.Fatal(err)
	}
	if err := decides(st, a.Org, override, "approved", now); err != nil {
		t.Fatal(err)
	}
	wantState(t, "a sign-off under the override with another reason", sign(ann, rec, &override, "Another reason"), "FY267")
	carl := founder(t, st, a.Org, 3, now)
	wantState(t, "someone else's sign-off under Ann's override", sign(carl, rec, &override, reason), "FY267")
	if err := sign(ann, rec, &override, reason); err != nil {
		t.Fatalf("Ann's sign-off under her approved override: %v", err)
	}
}

// importUSD imports an October statement of one USD line to a's Wise account.
func importUSD(t *testing.T, st interface {
	Do(context.Context, uuid.UUID, func(context.Context, storage.UnitOfWork) error) error
}, a bankedBooks, now time.Time) []storage.BankLine {
	t.Helper()
	var lines []storage.BankLine
	if err := st.Do(context.Background(), a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: a.usd.ID, Source: storage.MoneySourceFile,
			Format: storage.FormatCSV, ParserVersion: "csv@1", PeriodFrom: a.Oct.StartsOn, PeriodTo: a.Oct.EndsOn, Currency: "USD",
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: a.Oct.StartsOn.AddDate(0, 0, 6), Amount: "-5.00", Fingerprint: fingerprint("usd" + a.Org.String())}},
			At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return lines
}
