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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
)

// issuedConfirmation is a signed employment confirmation for the team's Ben, with the hash of the exact PDF.
func issuedConfirmation(t *testing.T, lt *leaveTeam, now time.Time) (issued uuid.UUID, sum [32]byte) {
	t.Helper()
	blobs, _ := local.NewDir(t.TempDir())
	var draft, id uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.SetLetterSettings(ctx, tx, lt.c.org, now, lt.c.hr, nemura.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place, Singapore 048616"}); err != nil {
			return err
		}
		if err := nemura.SetLetterType(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", true, 3); err != nil {
			return err
		}
		var err error
		draft, err = nemura.SaveTemplateDraft(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", confirmation, "First version.")
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PublishTemplate(ctx, tx, lt.c.org, now.Add(time.Hour), lt.c.owner, draft)
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, _, err = nemura.RequestLetter(ctx, tx, lt.c.org, now, lt.benLogin, pack.Version{}, nemura.LetterAsk{Person: lt.ben, Type: "employment_confirmation", Purpose: "a private visa matter"})
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		_, sum, err = nemura.CheckLetter(ctx, tx, lt.c.org, now.Add(2*time.Hour), lt.c.owner, id, letters.Local{}, blobs)
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		issued, err = nemura.SignLetter(ctx, tx, lt.c.org, now.Add(3*time.Hour), lt.c.owner, id, sum, now.Add(3*time.Hour-time.Minute), "role:owner", blobs)
		return err
	})
	return issued, sum
}

func pgErr(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// A recipient who holds the document looks it up by its hash and learns what the reference and code would tell them, and a document changed by a byte or never issued is not found; the person the
// letter is about makes a link that shows the letter's own facts for a few days, and only they can; the link is read with no session, answers alike for what it does not know, never shows the company's
// letterhead, the reference or the purpose, stops at its expiry or its revocation (which never withdraws the letter or its verification), and stops showing facts once the person withdraws the verification;
// every look-up is audited without the facts (NM-LTR-05).
// asvs: V2.3.1, V8.2.1, V8.3.1, V13.2.1
func TestADocumentIsVerifiedByItsHashAndAPersonSharesTheirLettersFactsThroughARevocableLink(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	api := store(t, "nemura_api")
	issued, sum := issuedConfirmation(t, lt, now)

	v, ok, err := api.VerifyLetterFile(ctx, sum[:], now.Add(4*time.Hour))
	if err != nil || !ok || !v.Valid || v.Type != "employment_confirmation" || v.Company != "Acme Pte. Ltd." {
		t.Fatalf("the issued document by its hash: %+v %v %v", v, ok, err)
	}
	changed := sum
	changed[31] ^= 1
	for name, h := range map[string][]byte{"a document changed by a byte": changed[:], "a hash that is too short": sum[:5], "nothing": nil} {
		if _, ok, _ := api.VerifyLetterFile(ctx, h, now); ok {
			t.Errorf("%s was found", name)
		}
	}

	// Only the person the letter is about makes, lists or revokes a link; an out-of-range life is refused.
	share := func(by uuid.UUID, days int) (uuid.UUID, string, time.Time, error) {
		var id uuid.UUID
		var token string
		var exp time.Time
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, token, exp, err = nemura.CreateLetterDisclosure(ctx, tx, lt.c.org, now.Add(5*time.Hour), by, issued, days)
			return err
		})
		return id, token, exp, err
	}
	if _, _, _, err := share(lt.c.owner, 7); letterCode(err) != letters.CodeNotFound {
		t.Errorf("another member making a link: %v", err)
	}
	for _, days := range []int{-1, 31} {
		if _, _, _, err := share(lt.benLogin, days); letterCode(err) != letters.CodeInvalid {
			t.Errorf("%d days: %v", days, err)
		}
	}
	link1, token1, exp1, err := share(lt.benLogin, 0)
	if err != nil || exp1 != now.Add(5*time.Hour).Add(7*24*time.Hour) {
		t.Fatalf("the default life is seven days: %v %v", exp1, err)
	}
	link2, token2, _, err := share(lt.benLogin, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.LetterDisclosures(ctx, tx, lt.c.owner, issued)
		return err
	}); letterCode(err) != letters.CodeNotFound {
		t.Errorf("another member listing the links: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		rows, err := nemura.LetterDisclosures(ctx, tx, lt.benLogin, issued)
		if err != nil || len(rows) != 2 || rows[0].ID != link2.String() || rows[0].RevokedAt != "" {
			t.Errorf("the person's links: %+v %v", rows, err)
		}
		return err
	})

	read := func(token string, at time.Time) (found bool, status string, facts map[string]string, company string) {
		t.Helper()
		out, err := api.LetterDisclosureByToken(ctx, token, at)
		if err != nil {
			t.Fatal(err)
		}
		facts = map[string]string{}
		for _, f := range out.Facts {
			facts[f.Label] = f.Value
		}
		return out.Found, out.Status, facts, out.Company
	}
	found, status, facts, company := read(token1, now.Add(6*time.Hour))
	if !found || status != "valid" || company != "Acme Pte. Ltd." || facts["Legal name"] == "" || facts["Job title"] == "" || facts["Start date"] == "" {
		t.Fatalf("a live link: %v %s %v", found, status, facts)
	}
	for label := range facts { // the company, the reference, the date and the purpose are not the person's facts
		if strings.Contains(label, "Company") || strings.Contains(label, "Reference") || strings.Contains(label, "Date of the letter") || strings.Contains(label, "for") {
			t.Errorf("the link shows %q", label)
		}
	}
	for name, tok := range map[string]string{"a wrong token": token1[:len(token1)-3] + "AAA", "another organisation's prefix": uuid.New().String() + token1[strings.Index(token1, "."):], "no dot": "nodot", "empty": ""} {
		if f, _, _, _ := read(tok, now.Add(6*time.Hour)); f {
			t.Errorf("%s was found", name)
		}
	}
	// It expires, and a revoked link stops at once; the other link and the letter's verification are untouched.
	if f, _, _, _ := read(token2, now.Add(5*time.Hour).Add(25*time.Hour)); f {
		t.Error("an expired link was found")
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RevokeLetterDisclosure(ctx, tx, lt.c.org, now.Add(7*time.Hour), lt.c.owner, link1)
	}); letterCode(err) != letters.CodeNotFound {
		t.Errorf("another member revoking: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RevokeLetterDisclosure(ctx, tx, lt.c.org, now.Add(7*time.Hour), lt.benLogin, link1)
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RevokeLetterDisclosure(ctx, tx, lt.c.org, now.Add(8*time.Hour), lt.benLogin, link1)
	}); letterCode(err) != letters.CodeConflict {
		t.Errorf("revoking twice: %v", err)
	}
	if f, _, _, _ := read(token1, now.Add(8*time.Hour)); f {
		t.Error("a revoked link was found")
	}
	if f, s, _, _ := read(token2, now.Add(8*time.Hour)); !f || s != "valid" {
		t.Errorf("the other link: %v %s", f, s)
	}
	if v, ok, _ := api.VerifyLetterFile(ctx, sum[:], now.Add(9*time.Hour)); !ok || !v.Valid {
		t.Error("revoking a link withdrew the letter's verification")
	}
	// Withdrawing the verification stops the facts, and no new link is made for a letter that cannot be verified.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawLetterVerification(ctx, tx, lt.c.org, now.Add(10*time.Hour), lt.benLogin, issued)
	})
	if f, s, facts, _ := read(token2, now.Add(11*time.Hour)); !f || s != "withdrawn" || len(facts) != 0 {
		t.Errorf("a link to a withdrawn letter: %v %s %v", f, s, facts)
	}
	if _, _, _, err := share(lt.benLogin, 3); letterCode(err) != letters.CodeConflict {
		t.Errorf("a link for a withdrawn letter: %v", err)
	}
	// Every look-up of a live link is audited, with the state and no fact; so is the file look-up.
	if err := store(t, "nemura_worker").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		var n, leaked int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.letter.disclosed%'`).Scan(&n); err != nil {
			return err
		}
		if n != 3 {
			t.Errorf("%d link look-ups audited, want 3", n)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text ~ '(disclosed|disclosure_)' AND (body::text LIKE '%Ben%' OR body::text LIKE '%visa%' OR body::text LIKE '%Engineer%' OR body::text LIKE '%'||$1||'%')`, token2[strings.Index(token2, ".")+1:]).Scan(&leaked); err != nil {
			return err
		}
		if leaked != 0 {
			t.Errorf("%d audit events carry a fact or a token", leaked)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The database: a link only ever changes by being revoked, once; no role deletes it; a life over thirty days is refused; no role edits another column.
	run := func(role, want, sql string, args ...any) {
		t.Helper()
		err := store(t, role).InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if pgErr(err) != want {
			t.Errorf("%s %s: want %s, got %v", role, sql, want, err)
		}
	}
	run("nemura_api", "42501", `DELETE FROM nemura.letter_disclosure`)
	run("nemura_api", "42501", `UPDATE nemura.letter_disclosure SET expires_at = expires_at + interval '1 day'`)
	run("nemura_api", "42501", `UPDATE nemura.letter_disclosure SET token_hash = '\x00'::bytea`)
	run("nemura_worker", "42501", `UPDATE nemura.letter_disclosure SET revoked_at = now()`)
	run("nemura_api", "NM207", `UPDATE nemura.letter_disclosure SET revoked_at = now() WHERE id = $1`, link1) // already revoked
	run("nemura_api", "23514", `INSERT INTO nemura.letter_disclosure (issued_id, token_hash, expires_at, created_by, created_at) VALUES ($1, sha256('x'), now() + interval '31 days', $2, now())`, issued, lt.benLogin)
	run("nemura_worker", "42501", `INSERT INTO nemura.letter_disclosure (issued_id, token_hash, expires_at, created_by, created_at) VALUES ($1, sha256('y'), now() + interval '1 day', $2, now())`, issued, lt.benLogin)
	run("nemura_api", "NM206", `SELECT nemura.letter_verification_state($1, $2)`, uuid.New(), issued)
	run("nemura_worker", "42501", `SELECT nemura.letter_verification_lookup_content('\x00'::bytea)`)
	run("nemura_readonly", "42501", `SELECT nemura.letter_verification_state($1, $2)`, lt.c.org, issued)
}

// The public look-ups are limited by a count the database keeps, so two API instances (two stores) add to the same limit; a window is the caller's clock's, a new window starts afresh, what is counted is
// stored as a hash, and no role reads the counter or runs its function but the API's (NM-LTR-05).
// asvs: V2.3.1, V13.2.1
func TestThePublicLimitIsSharedByEveryInstance(t *testing.T) {
	ctx := context.Background()
	a, b := store(t, "nemura_api"), store(t, "nemura_api") // two instances
	now := d("2099-03-01").Add(10 * time.Hour)
	key := "EC-2099-" + uuid.NewString()[:8]
	for i := 0; i < 10; i++ {
		inst := a
		if i%2 == 1 {
			inst = b
		}
		ok, err := inst.PublicRate(ctx, "letter-ref", key, time.Hour, 10, now.Add(time.Duration(i)*time.Minute))
		if err != nil || !ok {
			t.Fatalf("try %d: %v %v", i+1, ok, err)
		}
	}
	for _, inst := range []*nemura.Store{a, b} {
		if ok, err := inst.PublicRate(ctx, "letter-ref", " "+strings.ToLower(key)+" ", time.Hour, 10, now.Add(11*time.Minute)); err != nil || ok {
			t.Errorf("the eleventh try on either instance: %v %v", ok, err)
		}
	}
	if ok, _ := a.PublicRate(ctx, "letter-ref", key+"-other", time.Hour, 10, now); !ok {
		t.Error("another key shares the limit")
	}
	if ok, _ := b.PublicRate(ctx, "letter-ref", key, time.Hour, 10, now.Add(time.Hour)); !ok {
		t.Error("the next window did not start afresh")
	}
	// The counter holds a hash, never the reference; no role but the API's runs the function and none reads or writes the table.
	for _, role := range []string{"nemura_api", "nemura_worker", "nemura_readonly"} {
		err := store(t, role).InOrg(ctx, uuid.New(), func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT * FROM nemura.public_rate`)
			return err
		})
		if pgErr(err) != "42501" {
			t.Errorf("%s reading the counter: %v", role, err)
		}
	}
	for _, role := range []string{"nemura_worker", "nemura_readonly"} {
		_, err := store(t, role).PublicRate(ctx, "letter-ref", key, time.Hour, 10, now)
		if pgErr(err) != "42501" {
			t.Errorf("%s running the counter: %v", role, err)
		}
	}
	for name, c := range map[string]struct {
		scope  string
		window time.Duration
		limit  int
	}{"a bad scope": {"Bad Scope!", time.Hour, 10}, "a window of nothing": {"letter-ref", 0, 10}, "no limit": {"letter-ref", time.Hour, 0}, "a window over a day": {"letter-ref", 48 * time.Hour, 10}} {
		if _, err := a.PublicRate(ctx, c.scope, key, c.window, c.limit, now); pgErr(err) != "NM208" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
