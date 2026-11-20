//go:build local

package nemura_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

// An issued letter carries a reference and a code, and a recipient who enters both learns only whether it is valid, its type, the day it was issued and the company: nothing of the person or the
// words. A wrong code, another reference and a letter not issued yet all answer the same way; the employee withdraws the verification, which never withdraws the letter; every look-up of a found letter is
// audited; and no role reads or changes the directory itself (NM-LTR-05).
// part of NM-LTR-05 (the page and its rate limit are tested with the API)
// asvs: V2.3.1, V8.2.1, V8.3.1, V13.2.1
func TestALetterIsVerifiedByItsReferenceAndCodeAndShowsOnlyItsOwnFacts(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	dir := t.TempDir()
	blobs, _ := local.NewDir(dir)
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.SetLetterSettings(ctx, tx, lt.c.org, now, lt.c.hr, nemura.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place, Singapore 048616"}); err != nil {
			return err
		}
		return nemura.SetLetterType(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", true, 3)
	})
	var draft, id uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
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
		id, _, err = nemura.RequestLetter(ctx, tx, lt.c.org, now, lt.benLogin, pack.Version{}, nemura.LetterAsk{Person: lt.ben, Type: "employment_confirmation", Purpose: "bank loan"})
		return err
	})
	var ref string
	var sum [32]byte
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ref, sum, err = nemura.CheckLetter(ctx, tx, lt.c.org, now.Add(2*time.Hour), lt.c.owner, id, letters.Local{}, blobs)
		return err
	})
	// The code is printed on the checked letter, in the footer, and nowhere else.
	var pdf []byte
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.Contains(p, id.String()) {
			pdf, _ = os.ReadFile(p)
		}
		return nil
	})
	m := regexp.MustCompile(`verification code ([A-Z2-7]{4}(?:-[A-Z2-7]{4}){3})`).FindSubmatch(pdf)
	if m == nil {
		t.Fatalf("the letter carries no verification code: %q", pdf)
	}
	code := string(m[1])
	verify := func(r, c string) (nemura.LetterVerification, bool) {
		v, ok, err := store(t, "nemura_api").VerifyLetter(ctx, r, c, now.Add(5*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return v, ok
	}
	// Checked is not issued: it cannot be verified yet.
	if _, ok := verify(ref, code); ok {
		t.Fatal("a letter that is checked and not signed was verified")
	}
	var issued uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		issued, err = nemura.SignLetter(ctx, tx, lt.c.org, now.Add(3*time.Hour), lt.c.owner, id, sum, now.Add(3*time.Hour-time.Minute), "role:owner", blobs)
		return err
	})
	v, ok := verify(ref, code)
	if !ok || !v.Valid || v.Withdrawn || v.Type != "employment_confirmation" || v.Company != "Acme Pte. Ltd." || v.IssuedOn.Format(time.DateOnly) != "2026-11-20" {
		t.Fatalf("a valid letter: %+v %v", v, ok)
	}
	// Case, dashes and spaces in what the recipient types do not matter.
	if _, ok := verify(" "+ref+" ", strings.ToLower(strings.ReplaceAll(code, "-", " "))); !ok {
		t.Error("typing the code in lower case without dashes did not verify")
	}
	for name, c := range map[string][2]string{"a wrong code": {ref, "AAAA-AAAA-AAAA-AAAA"}, "another reference": {"EC-2026-0099", code}, "nothing": {"", ""}} {
		if _, ok := verify(c[0], c[1]); ok {
			t.Errorf("%s verified", name)
		}
	}
	// Every look-up of a found letter is audited by the service, with the state shown and no name.
	if err := store(t, "nemura_worker").InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.letter.verified%'`).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("%d look-ups audited, want 2", n)
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.letter.verified%' AND (body::text LIKE '%Ben%' OR body::text LIKE '%Acme%')`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	// Only the person withdraws it, once, and the letter itself stays issued.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawLetterVerification(ctx, tx, lt.c.org, now, lt.c.owner, issued)
	}); letterCode(err) != letters.CodeNotFound {
		t.Errorf("someone else withdrawing it: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawLetterVerification(ctx, tx, lt.c.org, now.Add(6*time.Hour), lt.benLogin, issued)
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawLetterVerification(ctx, tx, lt.c.org, now, lt.benLogin, issued)
	}); letterCode(err) != letters.CodeConflict {
		t.Errorf("withdrawing twice: %v", err)
	}
	if v, ok := verify(ref, code); !ok || v.Valid || !v.Withdrawn {
		t.Errorf("a withdrawn letter: %+v %v", v, ok)
	}
	var state string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT state FROM nemura.letter_request WHERE id = $1`, id).Scan(&state)
	})
	if state != "issued" {
		t.Errorf("withdrawing the verification changed the letter: %s", state)
	}
	// The directory is the database's: no role reads or writes it, and the functions refuse another organisation's rows.
	pgCode := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	run := func(s *nemura.Store, want, sql string, args ...any) {
		t.Helper()
		err := s.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if pgCode(err) != want {
			t.Errorf("%s: want %s, got %v", sql, want, err)
		}
	}
	for _, role := range []string{"nemura_api", "nemura_worker", "nemura_readonly"} {
		run(store(t, role), "42501", `SELECT * FROM nemura.letter_verification`)
		run(store(t, role), "42501", `DELETE FROM nemura.letter_verification`)
	}
	run(store(t, "nemura_worker"), "42501", `SELECT nemura.letter_verification_lookup('\x00'::bytea)`)
	run(store(t, "nemura_api"), "NM206", `SELECT nemura.letter_verification_register($1, $2, 'x', '\x00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff'::bytea)`, uuid.New(), uuid.New())
	run(store(t, "nemura_api"), "NM206", `SELECT nemura.letter_verification_withdraw($1, $2)`, uuid.New(), issued)
}
