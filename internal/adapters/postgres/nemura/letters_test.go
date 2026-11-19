//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func letterCode(err error) string {
	var r *letters.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

var confirmation = []letters.Block{
	{Kind: letters.Letterhead, Text: "{{company.name}}\nUEN {{company.uen}}\n{{company.address}}"},
	{Kind: letters.Heading, Text: "Employment confirmation"},
	{Kind: letters.Paragraph, Text: "This confirms that {{person.legal_name}} has been employed as {{job.title}} since {{employment.start}}. Dated {{letter.date}}. Purpose: {{request.purpose}}."},
	{Kind: letters.Signature, Text: "For {{company.name}}\nReference {{letter.reference}}"},
}

// Letters are issued in two steps with evidence: the company sets its letterhead and switches a type on; a template is a draft until another person approves it and is then
// an immutable numbered version; a request fills the published version from the record; an HR officer who is not the person checks it, which draws the PDF and hashes it; the
// signatory confirms that hash and the letter is issued once as evidence nothing changes; the stored bytes are checked against the hash whenever they are read, and a changed
// byte is never served or signed (NM-LTR-01, 02, 03, 07).
// req: NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-07
// asvs: V8.2.1, V8.3.1, V13.2.1
func TestALetterIsIssuedInTwoStepsAndTheEvidenceCannotChange(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	dir := t.TempDir()
	blobs, err := local.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	render := letters.Local{}
	// Nothing happens before the letterhead, the type and a published template exist.
	ask := func(person uuid.UUID, by uuid.UUID, purpose string) (uuid.UUID, time.Time, error) {
		var id uuid.UUID
		var due time.Time
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, due, err = nemura.RequestLetter(ctx, tx, lt.c.org, now, by, nemura.LetterAsk{Person: person, Type: "employment_confirmation", Purpose: purpose})
			return err
		})
		return id, due, err
	}
	if _, _, err := ask(lt.ben, lt.benLogin, "bank loan"); letterCode(err) != letters.CodeConflict {
		t.Fatalf("a type that is not switched on: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.SetLetterSettings(ctx, tx, lt.c.org, now, lt.c.hr, nemura.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place, Singapore 048616"}); err != nil {
			return err
		}
		return nemura.SetLetterType(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", true, 3)
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetLetterType(ctx, tx, lt.c.org, now, lt.c.hr, "salary_certificate", true, 3)
	}); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a type that needs the salary: %v", err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetLetterType(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", true, 99)
	}); letterCode(err) != letters.CodeInvalid {
		t.Errorf("due days out of range: %v", err)
	}
	if _, _, err := ask(lt.ben, lt.benLogin, "bank loan"); letterCode(err) != letters.CodeConflict || !strings.Contains(err.Error(), "no published version") {
		t.Fatalf("no published template: %v", err)
	}
	// A bad template names every problem; a good one is a draft that its author cannot publish; another person publishes it as version 1.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SaveTemplateDraft(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", []letters.Block{{Kind: letters.Paragraph, Text: "{{pay.basic_salary}}"}}, "")
		return err
	}); letterCode(err) != letters.CodeInvalid || !strings.Contains(err.Error(), "signature") {
		t.Errorf("a bad template: %v", err)
	}
	var draft uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		draft, err = nemura.SaveTemplateDraft(ctx, tx, lt.c.org, now, lt.c.hr, "employment_confirmation", confirmation, "First version.")
		return err
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.PublishTemplate(ctx, tx, lt.c.org, now, lt.c.hr, draft)
		return err
	}); letterCode(err) != letters.CodeRole {
		t.Errorf("the author approving their own template: %v", err)
	}
	var version int
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		version, err = nemura.PublishTemplate(ctx, tx, lt.c.org, now.Add(time.Hour), lt.c.owner, draft)
		return err
	})
	if version != 1 {
		t.Fatalf("version %d", version)
	}
	su := connect(t, db.SuperURL())
	if _, err := su.Exec(ctx, "UPDATE nemura.letter_template_version SET blocks = '[{\"kind\":\"paragraph\",\"text\":\"x\"}]'::jsonb WHERE state = 'published'"); err == nil {
		t.Error("a published template was edited")
	}
	// Request: the facts are filled and checked at once; a missing fact names itself.
	id, due, err := ask(lt.ben, lt.benLogin, "bank loan")
	if err != nil || due.Format(time.DateOnly) != "2026-11-25" {
		t.Fatalf("request: %v %s", err, due.Format(time.DateOnly))
	}
	// HR check: not the person's own, a reference and a hash.
	check := func(by uuid.UUID) (string, [32]byte, error) {
		var ref string
		var sum [32]byte
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			ref, sum, err = nemura.CheckLetter(ctx, tx, lt.c.org, now.Add(2*time.Hour), by, id, render, blobs)
			return err
		})
		return ref, sum, err
	}
	if _, _, err := check(lt.benLogin); letterCode(err) != letters.CodeRole {
		t.Errorf("checking one's own letter: %v", err)
	}
	ref, sum, err := check(lt.c.owner)
	if err != nil || ref != "EC-2026-0001" {
		t.Fatalf("check: %s %v", ref, err)
	}
	if _, _, err := check(lt.c.owner); letterCode(err) != letters.CodeConflict {
		t.Errorf("checking twice: %v", err)
	}
	// Signing: the wrong hash is refused; the right one issues the letter once.
	sign := func(by uuid.UUID, h [32]byte) (uuid.UUID, error) {
		var issued uuid.UUID
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			issued, err = nemura.SignLetter(ctx, tx, lt.c.org, now.Add(3*time.Hour), by, id, h, now.Add(3*time.Hour-time.Minute), "role:owner", blobs)
			return err
		})
		return issued, err
	}
	if _, err := sign(lt.c.owner, sha256.Sum256([]byte("another letter"))); letterCode(err) != letters.CodeConflict {
		t.Errorf("a hash that is not the checked letter's: %v", err)
	}
	// A byte changed in the store: it is neither signed nor served.
	var blobPath string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, id.String()+".pdf") {
			blobPath = p
		}
		return nil
	})
	if blobPath == "" {
		t.Fatal("the checked letter is not in the store")
	}
	original, _ := os.ReadFile(blobPath)
	tampered := bytes.Replace(original, []byte("Ben Lim"), []byte("Bon Lim"), 1)
	if bytes.Equal(original, tampered) {
		t.Fatal("nothing to tamper with")
	}
	_ = os.Chmod(blobPath, 0o600)
	_ = os.WriteFile(blobPath, tampered, 0o600)
	if _, err := sign(lt.c.owner, sum); letterCode(err) != letters.CodeConflict || !strings.Contains(err.Error(), "no longer matches") {
		t.Errorf("signing tampered bytes: %v", err)
	}
	_ = os.WriteFile(blobPath, original, 0o600)
	issued, err := sign(lt.c.owner, sum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sign(lt.c.owner, sum); letterCode(err) != letters.CodeConflict {
		t.Errorf("signing twice: %v", err)
	}
	// The evidence: the hash of the bytes, the template, the signatory and the envelope; nothing about it changes.
	var gotSum []byte
	var authority, envelope string
	if err := su.QueryRow(ctx, "SELECT sha256, authority, envelope::text FROM nemura.letter_issued WHERE id = $1", issued).Scan(&gotSum, &authority, &envelope); err != nil || !bytes.Equal(gotSum, sum[:]) || authority != "role:owner" || !strings.Contains(envelope, "EC-2026-0001") {
		t.Errorf("the evidence: %x %s %s (%v)", gotSum, authority, envelope, err)
	}
	for _, q := range []string{"UPDATE nemura.letter_issued SET sha256 = decode(repeat('00', 32), 'hex')", "DELETE FROM nemura.letter_issued", "UPDATE nemura.letter_request SET render_sha256 = decode(repeat('00', 32), 'hex')", "DELETE FROM nemura.letter_request", "UPDATE nemura.letter_request SET purpose = 'x'", "UPDATE nemura.letter_request SET state = 'requested'"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
	// Reading: the bytes are the ones issued; a changed byte is not served.
	read := func() (string, []byte, error) {
		var r string
		var pdf []byte
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			r, pdf, err = nemura.ReadLetter(ctx, tx, blobs, issued)
			return err
		})
		return r, pdf, err
	}
	r, pdf, err := read()
	if err != nil || r != ref || !bytes.Contains(pdf, []byte("Ben Lim has been employed as Engineer since 5 January 2026")) || !bytes.Contains(pdf, []byte("bank loan")) || sha256.Sum256(pdf) != sum {
		t.Fatalf("the issued letter: %s %v", r, err)
	}
	_ = os.WriteFile(blobPath, tampered, 0o600)
	if _, _, err := read(); letterCode(err) != letters.CodeConflict {
		t.Errorf("serving tampered bytes: %v", err)
	}
	_ = os.WriteFile(blobPath, original, 0o600)
	// A link: the token is shown once, only its hash is kept, it delivers the one letter and it expires.
	var token string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		token, _, err = nemura.CreateLetterLink(ctx, tx, lt.c.org, now.Add(4*time.Hour), lt.benLogin, issued)
		return err
	})
	var stored int
	hash := sha256.Sum256([]byte(token))
	if err := su.QueryRow(ctx, "SELECT count(*) FROM nemura.letter_link WHERE token_hash = $1", hash[:]).Scan(&stored); err != nil || stored != 1 {
		t.Errorf("only the token's hash is stored: %d (%v)", stored, err)
	}
	if err := su.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%' || $1 || '%'", strings.SplitN(token, ".", 2)[1]).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("the token is in an audit event: %d (%v)", stored, err)
	}
	org, ok := nemura.LinkOrganisation(token)
	if !ok || org != lt.c.org {
		t.Fatalf("the organisation of the token: %v %v", org, ok)
	}
	byLink := func(tok string, at time.Time) (string, error) {
		var r string
		err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			r, _, err = nemura.LetterByLink(ctx, tx, blobs, tok, at)
			return err
		})
		return r, err
	}
	if r, err := byLink(token, now.Add(5*time.Hour)); err != nil || r != ref {
		t.Errorf("the link: %s %v", r, err)
	}
	if _, err := byLink(token, now.Add(4*time.Hour+nemura.LinkLifetime+time.Second)); letterCode(err) != letters.CodeNotFound {
		t.Errorf("an expired link: %v", err)
	}
	if _, err := byLink(lt.c.org.String()+".not-a-token", now); letterCode(err) != letters.CodeNotFound {
		t.Errorf("a wrong token: %v", err)
	}
	// Scopes: the person reads their own request with its reference and hash, a stranger none, the people team all.
	list := func(s nemura.LetterScope) []nemura.LetterRow {
		var rows []nemura.LetterRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = nemura.LetterRequests(ctx, tx, s, "")
			return err
		})
		return rows
	}
	if rows := list(nemura.LetterScope{Member: lt.benLogin}); len(rows) != 1 || rows[0].State != "issued" || rows[0].Reference != ref || rows[0].Issued == nil || *rows[0].Issued != issued {
		t.Errorf("own: %+v", rows)
	}
	if len(list(nemura.LetterScope{Member: lt.c.pay})) != 0 || len(list(nemura.LetterScope{Member: lt.c.owner, All: true})) != 1 {
		t.Error("scopes")
	}
	// A second letter takes the next reference; a refusal needs a reason; the person may withdraw their own.
	id2, _, err := ask(lt.ben, lt.benLogin, "a visa")
	if err != nil {
		t.Fatal(err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RefuseLetter(ctx, tx, lt.c.org, now, lt.c.owner, id2, "", false)
	}); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RefuseLetter(ctx, tx, lt.c.org, now, lt.benLogin, id2, "", true)
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RefuseLetter(ctx, tx, lt.c.org, now, lt.c.owner, id2, "too late", false)
	}); letterCode(err) != letters.CodeConflict {
		t.Errorf("refusing a withdrawn request: %v", err)
	}
}

// The letters service lists the catalogue, asks for a letter for the signed-in person, checks and signs through the same rules, serves the issued letter to the person and the
// people team only, makes a link that needs no session, and refuses to check or sign without a renderer or a file store (NM-LTR-01, NM-LTR-03, NM-LTR-07).
// req: NM-LTR-03, NM-LTR-07
func TestTheLettersServiceAsksChecksSignsAndDelivers(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.LettersService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Blobs: blobs, Renderer: letters.Local{}}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	stranger := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	if err := svc.SetSettings(ctx, ben, api.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetType(ctx, ben, "employment_confirmation", true, 3); err != nil {
		t.Fatal(err)
	}
	blocks := []api.LetterBlock{{Kind: "letterhead", Text: "{{company.name}}"}, {Kind: "paragraph", Text: "{{person.legal_name}} works as {{job.title}}. {{letter.reference}}"}, {Kind: "signature", Text: "For {{company.name}}"}}
	draft, err := svc.SaveDraft(ctx, ben, "employment_confirmation", blocks, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, ana, uuid.MustParse(draft)); err != nil {
		t.Fatal(err)
	}
	types, err := svc.Types(ctx, ben)
	if err != nil || len(types) != len(letters.Types) || types[0].Key != "employment_confirmation" || !types[0].Enabled || types[0].Version != 1 || types[len(types)-1].Needs == "" {
		t.Fatalf("types: %+v %v", types, err)
	}
	if v, err := svc.Templates(ctx, ben, "employment_confirmation"); err != nil || len(v) != 1 || v[0].State != "published" || len(v[0].Blocks) != 3 {
		t.Errorf("templates: %+v %v", v, err)
	}
	if _, err := svc.Templates(ctx, ben, "nonsense"); letterCode(err) != letters.CodeNotFound {
		t.Errorf("an unknown type: %v", err)
	}
	if _, _, err := svc.Request(ctx, stranger, "employment_confirmation", "x"); personRefusal(err) != people.CodeNotFound {
		t.Errorf("a login with no person: %v", err)
	}
	id, due, err := svc.Request(ctx, ben, "employment_confirmation", "a bank loan")
	if err != nil || due != "2026-11-25" {
		t.Fatalf("request: %s %s %v", id, due, err)
	}
	rid := uuid.MustParse(id)
	if list, _ := svc.List(ctx, stranger, false, ""); len(list) != 0 {
		t.Errorf("a stranger lists %d", len(list))
	}
	if list, _ := svc.List(ctx, ana, true, "requested"); len(list) != 1 || list[0].Person != "Ben Lim" {
		t.Errorf("the people team's list: %+v", list)
	}
	if _, _, err := svc.Draft(ctx, ana, rid); letterCode(err) != letters.CodeNotFound {
		t.Errorf("a draft before the check: %v", err)
	}
	ref, sha, err := svc.Check(ctx, ana, rid)
	if err != nil || ref != "EC-2026-0001" || len(sha) != 64 {
		t.Fatalf("check: %s %s %v", ref, sha, err)
	}
	if r, pdf, err := svc.Draft(ctx, ana, rid); err != nil || r != ref || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("draft: %s %v", r, err)
	}
	if _, err := svc.Sign(ctx, ana, rid, "not hex"); letterCode(err) != letters.CodeInvalid {
		t.Errorf("a bad hash: %v", err)
	}
	if _, _, err := svc.File(ctx, ben, rid, false); letterCode(err) != letters.CodeNotFound {
		t.Errorf("a letter before it is issued: %v", err)
	}
	issued, err := svc.Sign(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}, rid, sha)
	if err != nil || issued == "" {
		t.Fatalf("sign: %v", err)
	}
	if r, pdf, err := svc.File(ctx, ben, rid, false); err != nil || r != ref || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("the person's copy: %v", err)
	}
	if _, _, err := svc.File(ctx, stranger, rid, false); letterCode(err) != letters.CodeNotFound {
		t.Errorf("a stranger's copy: %v", err)
	}
	token, exp, err := svc.Link(ctx, ben, rid, false)
	if err != nil || exp == "" {
		t.Fatalf("link: %v", err)
	}
	if r, pdf, err := svc.ByLink(ctx, token); err != nil || r != ref || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("by link: %v", err)
	}
	for _, bad := range []string{"", "x", lt.c.org.String() + ".nope", uuid.NewString() + ".nope"} {
		if _, _, err := svc.ByLink(ctx, bad); letterCode(err) != letters.CodeNotFound {
			t.Errorf("a bad token %q: %v", bad, err)
		}
	}
	// Withdrawing is the person's own; and without a renderer or a store nothing is checked or read.
	id2, _, err := svc.Request(ctx, ben, "employment_confirmation", "a visa")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Refuse(ctx, ana, uuid.MustParse(id2), "", true); letterCode(err) != letters.CodeNotFound {
		t.Errorf("withdrawing someone else's request: %v", err)
	}
	if err := svc.Refuse(ctx, ben, uuid.MustParse(id2), "", true); err != nil {
		t.Errorf("withdrawing one's own: %v", err)
	}
	bare := nemura.LettersService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}
	if _, _, err := bare.Check(ctx, ana, rid); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("checking with no renderer: %v", err)
	}
	if _, _, err := bare.File(ctx, ben, rid, false); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("reading with no store: %v", err)
	}
}
