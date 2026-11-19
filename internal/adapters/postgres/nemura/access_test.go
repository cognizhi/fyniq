//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Need to know: a restricted field is masked by default, shown in full only with the right, always in full to the person it
// belongs to, hidden otherwise; every read is audited, a refusal too, and the value never enters the event (NM-SEC-02, NM-PPL-02).
// req: NM-SEC-02
// asvs: V8.2.1, V16.2.5, V14.2.6
func TestARestrictedFieldIsReleasedByNeedToKnowAndEveryReadIsAudited(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	at := today.Add(14 * time.Hour)
	const nric = "S1234567D"
	read := func(by uuid.UUID, held []authz.Role, capability string) (string, error) {
		var shown string
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			shown, err = nemura.ReadRestricted(ctx, tx, c.org, at, by, held, capability, c.pay, "nric", nric)
			var refused *nemura.RestrictedRefusal
			if errors.As(err, &refused) {
				return nil // the refusal's event commits; the caller answers the person
			}
			return err
		})
		return shown, err
	}
	for _, tc := range []struct {
		name       string
		by         uuid.UUID
		held       []authz.Role
		capability string
		want       string
		level      string
	}{
		{"the Payroll administrator, for statutory filings", c.hr, []authz.Role{authz.PayrollAdmin}, "NRIC or FIN", nric, "full"},
		{"the HR administrator", c.hr, []authz.Role{authz.HRAdmin}, "NRIC or FIN", nric, "full"},
		{"the HR officer sees the shape", c.hr, []authz.Role{authz.HROfficer}, "NRIC or FIN", "*****567D", "masked"},
		{"a line manager sees none", c.hr, []authz.Role{authz.LineManager}, "NRIC or FIN", "", "hidden"},
		{"the person's own", c.pay, []authz.Role{authz.Employee}, "NRIC or FIN", nric, "full"},
		{"payroll sees no medical note", c.hr, []authz.Role{authz.PayrollAdmin}, "Medical certificates and notes", "", "hidden"},
		{"the AI, alone, sees none", c.hr, []authz.Role{authz.AI}, "NRIC or FIN", "", "hidden"},
		{"an unknown role sees none", c.hr, []authz.Role{"superuser"}, "NRIC or FIN", "", "hidden"},
	} {
		got, err := read(tc.by, tc.held, tc.capability)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v (want %q)", tc.name, got, err, tc.want)
		}
		if tc.level == "hidden" && access.Of(tc.held, tc.capability, tc.by == c.pay) != access.Hidden {
			t.Errorf("%s: the level is not hidden", tc.name)
		}
	}
	// Every read is in the trail, the refusals too, each with its level, and the value is nowhere in it.
	super := connect(t, db.SuperURL())
	var reads, refusals, leaks int
	if err := super.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action = 'nemura.restricted.read'), count(*) FILTER (WHERE action = 'nemura.restricted.refused'),
count(*) FILTER (WHERE body LIKE '%S1234567D%' OR body LIKE '%567D%') FROM nemura.audit_event WHERE organisation_id = $1`, c.org).Scan(&reads, &refusals, &leaks); err != nil {
		t.Fatal(err)
	}
	if reads != 4 || refusals != 4 || leaks != 0 {
		t.Errorf("audited reads %d (want 4), refusals %d (want 4), events holding the value %d (want 0)", reads, refusals, leaks)
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}

// Row-level scope: a policy that asks "is it mine, or my report's?" matches only what the role may read, so a missed check in the
// API cannot read more. The reporting line is effective-dated and the scope follows it; unset, nobody matches (NM-ROLE-03, NM-SEC-02).
// req: NM-ROLE-03
// asvs: V8.2.2, V8.3.1
func TestTheLineManagerSeesOnlyTheirReportsThroughRowLevelSecurity(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	at := today.Add(15 * time.Hour)
	// A table that stands for any people record: its policy is the scope functions' use.
	super := connect(t, db.SuperURL())
	if _, err := super.Exec(ctx, `
CREATE TABLE IF NOT EXISTS nemura.scope_probe (organisation_id uuid NOT NULL DEFAULT nemura.current_org(), id uuid NOT NULL DEFAULT uuidv7(), subject uuid NOT NULL, note text NOT NULL);
ALTER TABLE nemura.scope_probe ENABLE ROW LEVEL SECURITY; ALTER TABLE nemura.scope_probe FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS scope ON nemura.scope_probe;
CREATE POLICY scope ON nemura.scope_probe USING (organisation_id = nemura.current_org() AND
  (subject = nemura.current_person() OR nemura.is_manager_of(subject) OR nemura.has_role('hr_officer')))
  WITH CHECK (organisation_id = nemura.current_org());
GRANT SELECT, INSERT ON nemura.scope_probe TO nemura_api;`); err != nil {
		t.Fatal(err)
	}
	// Three people: a manager, their report and a stranger; and an HR officer.
	var manager, report, stranger, hrOfficer, line uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if manager, err = nemura.AddMember(ctx, tx, c.org, at, actor, "Mina Manager"); err != nil {
			return err
		}
		if report, err = nemura.AddMember(ctx, tx, c.org, at, actor, "Rey Report"); err != nil {
			return err
		}
		if stranger, err = nemura.AddMember(ctx, tx, c.org, at, actor, "Sam Stranger"); err != nil {
			return err
		}
		if hrOfficer, err = nemura.AddMember(ctx, tx, c.org, at, actor, "Hugo Officer"); err != nil {
			return err
		}
		if _, err = nemura.GrantRole(ctx, tx, c.org, at, c.owner, hrOfficer, authz.HROfficer, today, nil); err != nil {
			return err
		}
		if line, err = nemura.AddReportingLine(ctx, tx, c.org, at, c.hr, report, manager, later); err != nil {
			return err
		}
		for _, s := range []uuid.UUID{manager, report, stranger} {
			if _, err := tx.Exec(ctx, "INSERT INTO nemura.scope_probe (subject, note) VALUES ($1, $2)", s, "note of "+s.String()); err != nil {
				return err
			}
		}
		return nil
	})
	seen := func(person uuid.UUID, on time.Time) []uuid.UUID {
		var out []uuid.UUID
		err := store(t, "nemura_api").InOrgAs(ctx, c.org, person, on, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT subject FROM nemura.scope_probe ORDER BY note")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var s uuid.UUID
				if err := rows.Scan(&s); err != nil {
					return err
				}
				out = append(out, s)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	has := func(l []uuid.UUID, ids ...uuid.UUID) bool {
		for _, id := range ids {
			found := false
			for _, x := range l {
				found = found || x == id
			}
			if !found {
				return false
			}
		}
		return true
	}
	onLine := later.AddDate(0, 0, 1)
	if got := seen(manager, onLine); !has(got, manager, report) || has(got, stranger) || len(got) != 2 {
		t.Errorf("the manager sees %d rows: their own and their report's only", len(got))
	}
	if got := seen(report, onLine); len(got) != 1 || got[0] != report {
		t.Errorf("a report sees %d rows: their own only", len(got))
	}
	if got := seen(stranger, onLine); len(got) != 1 || got[0] != stranger {
		t.Errorf("a stranger sees %d rows: their own only", len(got))
	}
	if got := seen(hrOfficer, onLine); len(got) != 3 {
		t.Errorf("the HR officer, by role, sees %d rows, want 3", len(got))
	}
	// Dated: before the line begins the manager has no reports; once it ends they have none again.
	if got := seen(manager, today); len(got) != 1 {
		t.Errorf("before the reporting line begins the manager sees %d rows", len(got))
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndReportingLine(ctx, tx, c.org, at, c.hr, line, later.AddDate(0, 0, 10))
	})
	if got := seen(manager, later.AddDate(0, 0, 11)); len(got) != 1 {
		t.Errorf("after the line ends the manager sees %d rows", len(got))
	}
	// With no acting person nobody matches: the pool's plain unit of work sees nothing of this table.
	var n int
	if err := store(t, "nemura_api").InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.scope_probe").Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("with no acting person %d rows match, %v", n, err)
	}
	if err := store(t, "nemura_api").InOrgAs(ctx, c.org, uuid.Nil, at, func(context.Context, pgx.Tx) error { return nil }); err == nil || !strings.Contains(err.Error(), "no person") {
		t.Errorf("work for no person: %v", err)
	}
	// A line is history: ended once, never edited or deleted.
	if _, err := super.Exec(ctx, "DELETE FROM nemura.reporting_line WHERE id = $1", line); err == nil || !strings.Contains(err.Error(), "NM081") {
		t.Errorf("deleting a reporting line: %v", err)
	}
	_, _ = super.Exec(ctx, "DROP TABLE nemura.scope_probe")
}
