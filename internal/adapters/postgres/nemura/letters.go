package nemura

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/ports"
)

func letterError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM160":
			return &letters.Refusal{Code: letters.CodeConflict, Message: "A published template version is immutable: change it with a new version, which the HR administrator approves."}
		case "NM161":
			return &letters.Refusal{Code: letters.CodeConflict, Message: "A letter request keeps its words, its data and the checked letter, and moves one way; an issued letter never changes."}
		case "NM162":
			return &letters.Refusal{Code: letters.CodeRole, Message: "Nobody checks their own letter: another member of the people team does."}
		case "23514":
			if pg.ConstraintName == "letter_template_version_not_own_approval" {
				return &letters.Refusal{Code: letters.CodeRole, Message: "Nobody approves the template they wrote: another HR administrator or the Owner does."}
			}
		}
	}
	return err
}

// LetterSettings is the letterhead every letter carries: the company's name, UEN and address (NM-LTR-02's merge fields).
type LetterSettings struct{ Name, UEN, Address string }

// SetLetterSettings sets the letterhead, with its audit event.
func SetLetterSettings(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, s LetterSettings) error {
	for _, v := range []string{s.Name, s.UEN, s.Address} {
		if strings.TrimSpace(v) == "" {
			return &letters.Refusal{Code: letters.CodeInvalid, Message: "The company name, UEN and address are all required: a letterhead names the company, its registration and where it is."}
		}
	}
	if len(s.Name) > 200 || len(s.UEN) > 30 || len(s.Address) > 400 {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "The name is at most 200 characters, the UEN 30 and the address 400: shorten them."}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.letter_settings (company_name, uen, address, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organisation_id) DO UPDATE SET company_name = EXCLUDED.company_name, uen = EXCLUDED.uen, address = EXCLUDED.address, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		s.Name, s.UEN, s.Address, by, at); err != nil {
		return letterError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.letter_settings.set", audit.Object{Type: "letter_settings", ID: org}, map[string]audit.Change{"company_name": {nil, s.Name}, "uen": {nil, s.UEN}}, "")
}

// LetterSettingsOf is the letterhead, or false when none is set.
func LetterSettingsOf(ctx context.Context, tx pgx.Tx) (LetterSettings, bool, error) {
	var s LetterSettings
	err := tx.QueryRow(ctx, "SELECT company_name, uen, address FROM nemura.letter_settings").Scan(&s.Name, &s.UEN, &s.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

// SetLetterType switches a type of the catalogue on or off for the company and sets its due days (NM-LTR-01). A type that needs the pay structure or the terms cannot be
// switched on until they exist.
func SetLetterType(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, key string, enabled bool, dueDays int) error {
	t, ok := letters.TypeOf(key)
	if !ok {
		return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter type in the catalogue."}
	}
	if enabled && t.Needs != "" {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: fmt.Sprintf("The %s letter needs the person's %s, which Nemura does not hold yet: it can be switched on once it does.", strings.ToLower(t.Title), t.Needs)}
	}
	if dueDays < 1 || dueDays > 30 {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "The due days are 1 to 30 working days: choose how fast the letter is issued."}
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO nemura.letter_type (type_key, enabled, due_days, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organisation_id, type_key) DO UPDATE SET enabled = EXCLUDED.enabled, due_days = EXCLUDED.due_days, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at RETURNING id`, key, enabled, dueDays, by, at).Scan(&id); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.letter_type.set", audit.Object{Type: "letter_type", ID: id}, map[string]audit.Change{"type": {nil, key}, "enabled": {nil, enabled}, "due_days": {nil, dueDays}}, "")
}

// LetterTypeRow is a type of the catalogue with the company's setting on it.
type LetterTypeRow struct {
	Type    letters.Type
	Enabled bool
	DueDays int
	Version int // the published template version, 0 when none
}

// LetterTypes lists the catalogue with each type's company setting and its published template version.
func LetterTypes(ctx context.Context, tx pgx.Tx) ([]LetterTypeRow, error) {
	set := map[string][2]int{}
	rows, err := tx.Query(ctx, "SELECT type_key, enabled, due_days FROM nemura.letter_type")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var e bool
		var d int
		if err := rows.Scan(&k, &e, &d); err != nil {
			rows.Close()
			return nil, err
		}
		on := 0
		if e {
			on = 1
		}
		set[k] = [2]int{on, d}
	}
	rows.Close()
	ver := map[string]int{}
	vr, err := tx.Query(ctx, "SELECT type_key, max(version) FROM nemura.letter_template_version WHERE state = 'published' GROUP BY 1")
	if err != nil {
		return nil, err
	}
	for vr.Next() {
		var k string
		var v int
		if err := vr.Scan(&k, &v); err != nil {
			vr.Close()
			return nil, err
		}
		ver[k] = v
	}
	vr.Close()
	var out []LetterTypeRow
	for _, t := range letters.Types {
		r := LetterTypeRow{Type: t, DueDays: t.DueDays, Version: ver[t.Key]}
		if s, ok := set[t.Key]; ok {
			r.Enabled, r.DueDays = s[0] == 1, s[1]
		}
		out = append(out, r)
	}
	return out, nil
}

// SaveTemplateDraft saves a draft of a type's template, after the template is checked against the type: it replaces the type's open draft, or starts one. Every problem
// is reported at once (NM-LTR-02).
func SaveTemplateDraft(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, key string, blocks []letters.Block, note string) (uuid.UUID, error) {
	t, ok := letters.TypeOf(key)
	if !ok {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter type in the catalogue."}
	}
	if problems := letters.Check(t, blocks); len(problems) > 0 {
		var msgs []string
		for _, p := range problems {
			msgs = append(msgs, p.Message)
		}
		return uuid.Nil, &letters.Refusal{Code: letters.CodeInvalid, Message: strings.Join(msgs, " ")}
	}
	if len(note) > 500 {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeInvalid, Message: "The note is at most 500 characters."}
	}
	body, _ := json.Marshal(blocks)
	var id uuid.UUID
	err := tx.QueryRow(ctx, "UPDATE nemura.letter_template_version SET blocks = $2::jsonb, note = $3 WHERE type_key = $1 AND state = 'draft' RETURNING id", key, string(body), orNil(note)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id = uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.letter_template_version (id, type_key, blocks, note, created_by, created_at) VALUES ($1, $2, $3::jsonb, $4, $5, $6)", id, key, string(body), orNil(note), by, at); err != nil {
			return uuid.Nil, letterError(err)
		}
	} else if err != nil {
		return uuid.Nil, letterError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.letter_template.draft_saved", audit.Object{Type: "letter_template_version", ID: id}, map[string]audit.Change{"type": {nil, key}, "blocks": {nil, len(blocks)}}, "")
}

// PublishTemplate publishes a draft as the next numbered version, once the HR administrator approves it: the approver is never the author (the database refuses it), and the
// version before is superseded. A published version is immutable (NM-LTR-02).
func PublishTemplate(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, draft uuid.UUID) (int, error) {
	var key string
	if err := tx.QueryRow(ctx, "SELECT type_key FROM nemura.letter_template_version WHERE id = $1 AND state = 'draft'", draft).Scan(&key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such draft waiting to be published: it is published already, or there is none."}
		}
		return 0, err
	}
	var next int
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM nemura.letter_template_version WHERE type_key = $1", key).Scan(&next); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.letter_template_version SET state = 'published', version = $2, approved_by = $3, approved_at = $4 WHERE id = $1", draft, next, by, at); err != nil {
		return 0, letterError(err)
	}
	var older []uuid.UUID
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.letter_template_version WHERE type_key = $1 AND state = 'published' AND id <> $2", key, draft)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		older = append(older, id)
	}
	rows.Close()
	for _, id := range older {
		if _, err := tx.Exec(ctx, "UPDATE nemura.letter_template_version SET state = 'superseded' WHERE id = $1", id); err != nil {
			return 0, err
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.letter_template.superseded", audit.Object{Type: "letter_template_version", ID: id}, map[string]audit.Change{"state": {"published", "superseded"}}, ""); err != nil {
			return 0, err
		}
	}
	if err := endDelegationsOf(ctx, tx, org, at, by, key); err != nil {
		return 0, err
	}
	return next, Audit(ctx, tx, org, at, user(by), "nemura.letter_template.published", audit.Object{Type: "letter_template_version", ID: draft}, map[string]audit.Change{"type": {nil, key}, "version": {nil, next}}, "")
}

// TemplateRow is a template version as it is listed.
type TemplateRow struct {
	ID        uuid.UUID
	Type      string
	Version   int
	State     string
	Blocks    []letters.Block
	Note      string
	CreatedAt time.Time
}

// TemplateVersions lists a type's versions, newest first, the open draft on top.
func TemplateVersions(ctx context.Context, tx pgx.Tx, key string) ([]TemplateRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, type_key, coalesce(version, 0), state, blocks, coalesce(note, ''), created_at FROM nemura.letter_template_version WHERE type_key = $1
ORDER BY (state = 'draft') DESC, version DESC NULLS LAST, created_at DESC`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TemplateRow
	for rows.Next() {
		var r TemplateRow
		var body []byte
		if err := rows.Scan(&r.ID, &r.Type, &r.Version, &r.State, &body, &r.Note, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &r.Blocks); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// workingDaysAfter is the day this many working days after a day, Monday to Friday.
func workingDaysAfter(d time.Time, n int) time.Time {
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			n--
		}
	}
	return d
}

// LetterAsk is a request for a letter (NM-LTR-03).
type LetterAsk struct {
	Person  uuid.UUID
	Type    string
	Purpose string
}

// RequestLetter makes a request: it fills the published template of an enabled type from the person's facts and the letterhead, refusing a fact the record does not hold, and
// writes the values once. It returns the request and the day it is due.
func RequestLetter(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a LetterAsk) (uuid.UUID, time.Time, error) {
	t, ok := letters.TypeOf(a.Type)
	if !ok {
		return uuid.Nil, time.Time{}, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter type."}
	}
	if p := strings.TrimSpace(a.Purpose); p == "" || len(p) > 300 {
		return uuid.Nil, time.Time{}, &letters.Refusal{Code: letters.CodeInvalid, Message: "Say what the letter is for, in up to 300 characters: for a bank, a landlord or a visa."}
	}
	types, err := LetterTypes(ctx, tx)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	var row LetterTypeRow
	for _, r := range types {
		if r.Type.Key == a.Type {
			row = r
		}
	}
	if !row.Enabled {
		return uuid.Nil, time.Time{}, &letters.Refusal{Code: letters.CodeConflict, Message: fmt.Sprintf("The company does not issue %s letters yet: ask the people team to switch the type on.", strings.ToLower(t.Title))}
	}
	if row.Version == 0 {
		return uuid.Nil, time.Time{}, &letters.Refusal{Code: letters.CodeConflict, Message: fmt.Sprintf("The %s template has no published version yet: the HR administrator publishes one first.", strings.ToLower(t.Title))}
	}
	var tid uuid.UUID
	var body []byte
	if err := tx.QueryRow(ctx, "SELECT id, blocks FROM nemura.letter_template_version WHERE type_key = $1 AND state = 'published'", a.Type).Scan(&tid, &body); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	var blocks []letters.Block
	if err := json.Unmarshal(body, &blocks); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	vals, err := letterValues(ctx, tx, a.Person, at, strings.TrimSpace(a.Purpose))
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	if t.Pay {
		kv, err := ketValues(ctx, tx, v, a.Person, at)
		if err != nil {
			return uuid.Nil, time.Time{}, err
		}
		for k, x := range kv {
			vals[k] = x
		}
	}
	probe := maps(vals)
	probe["letter.reference"] = "—"
	if _, err := letters.Fill(blocks, probe); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	data, _ := json.Marshal(vals)
	due := workingDaysAfter(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), row.DueDays)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.letter_request (id, person_id, type_key, template_id, purpose, data, due_on, requested_by, requested_at) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)`,
		id, a.Person, a.Type, tid, strings.TrimSpace(a.Purpose), string(data), day(due), by, at); err != nil {
		return uuid.Nil, time.Time{}, letterError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.letter.requested", audit.Object{Type: "letter_request", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()}, "type": {nil, a.Type}, "due_on": {nil, day(due)}}, ""); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return id, due, notice(ctx, tx, "letter_requested", a.Person, id, "letter-requested")
}

func maps(v letters.Values) letters.Values {
	out := letters.Values{}
	for k, x := range v {
		out[k] = x
	}
	return out
}

// letterValues are the facts a letter is filled from: the record on the day and the letterhead. A fact the record does not hold is left out, so filling names it.
func letterValues(ctx context.Context, tx pgx.Tx, person uuid.UUID, at time.Time, purpose string) (letters.Values, error) {
	p, err := PersonOn(ctx, tx, person, at)
	if err != nil {
		return nil, err
	}
	s, ok, err := LetterSettingsOf(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The letterhead is not set: the HR administrator sets the company's name, UEN and address first."}
	}
	v := letters.Values{"person.legal_name": p.LegalName, "person.preferred_name": p.PreferredName, "job.title": p.Title, "job.department": p.Department, "employment.type": p.Type,
		"employment.start": day(p.Start), "company.name": s.Name, "company.uen": s.UEN, "company.address": s.Address, "letter.date": day(at), "request.purpose": purpose}
	if p.LeavingDate != nil {
		v["employment.end"] = day(*p.LeavingDate)
	}
	if p.ProbationEnd != nil {
		v["employment.probation_end"] = day(*p.ProbationEnd)
	}
	return v, nil
}

// CheckLetter is the HR officer's check (NM-LTR-03): the request is filled and drawn into a PDF, which is kept and hashed, and a reference is given; the signatory then signs
// exactly those bytes. Nobody checks their own letter. A refusal needs a reason.
func CheckLetter(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, r letters.Renderer, blobs ports.BlobStore) (string, [32]byte, error) {
	var sum [32]byte
	if blobs == nil {
		return "", sum, &letters.Refusal{Code: "NM_UNAVAILABLE", Message: "This deployment has no file store for letters yet: ask whoever runs Nemura."}
	}
	var key string
	var tid uuid.UUID
	var data, body []byte
	var state string
	err := tx.QueryRow(ctx, "SELECT q.type_key, q.template_id, q.data, v.blocks, q.state FROM nemura.letter_request q JOIN nemura.letter_template_version v ON v.organisation_id = q.organisation_id AND v.id = q.template_id WHERE q.id = $1 FOR UPDATE OF q", request).
		Scan(&key, &tid, &data, &body, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", sum, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter request."}
	}
	if err != nil {
		return "", sum, err
	}
	if state != "requested" {
		return "", sum, &letters.Refusal{Code: letters.CodeConflict, Message: "That request is " + state + ": only a request that has not been checked can be."}
	}
	var own bool
	if err := tx.QueryRow(ctx, "SELECT coalesce(p.member_id = $2, false) FROM nemura.letter_request q JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id WHERE q.id = $1", request, by).Scan(&own); err != nil {
		return "", sum, err
	}
	if own {
		return "", sum, &letters.Refusal{Code: letters.CodeRole, Message: "Nobody checks their own letter: another member of the people team does."}
	}
	var blocks []letters.Block
	var vals letters.Values
	if err := json.Unmarshal(body, &blocks); err != nil {
		return "", sum, err
	}
	if err := json.Unmarshal(data, &vals); err != nil {
		return "", sum, err
	}
	// The reference is numbered within the organisation and year; the unique constraint backs the lock.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "letter-ref:"+org.String()+at.Format("2006")); err != nil {
		return "", sum, err
	}
	var n int
	prefix := letters.Reference(at.Year(), key, 0)
	prefix = prefix[:len(prefix)-4]
	if err := tx.QueryRow(ctx, "SELECT count(*) + 1 FROM nemura.letter_request WHERE reference LIKE $1", prefix+"%").Scan(&n); err != nil {
		return "", sum, err
	}
	ref := letters.Reference(at.Year(), key, n)
	vals["letter.reference"] = ref
	filled, err := letters.Fill(blocks, vals)
	if err != nil {
		return "", sum, err
	}
	pdf, err := r.Render(ctx, letters.Document{Blocks: filled.Blocks, Footer: "Reference " + ref})
	if err != nil {
		var u letters.ErrUnsupported
		if errors.As(err, &u) {
			return "", sum, &letters.Refusal{Code: letters.CodeInvalid, Message: err.Error()}
		}
		return "", sum, err
	}
	sum = sha256.Sum256(pdf)
	blob := "letters/" + org.String() + "/" + request.String() + ".pdf"
	if err := blobs.Create(ctx, blob, bytes.NewReader(pdf)); errors.Is(err, ports.ErrExists) {
		// A check that did not commit left its bytes: they are the same letter only if they hash the same.
		rc, gerr := blobs.Get(ctx, blob)
		if gerr != nil {
			return "", sum, gerr
		}
		old, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			return "", sum, rerr
		}
		if sha256.Sum256(old) != sum {
			return "", sum, &letters.Refusal{Code: letters.CodeConflict, Message: "A different letter is already stored for that request: it is not replaced, and the change is for the security team to look into."}
		}
	} else if err != nil {
		return "", sum, err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.letter_request SET state = 'checked', reference = $2, render_blob = $3, render_sha256 = $4, checked_by = $5, checked_at = $6 WHERE id = $1", request, ref, blob, sum[:], by, at); err != nil {
		return "", sum, letterError(err)
	}
	return ref, sum, Audit(ctx, tx, org, at, user(by), "nemura.letter.checked", audit.Object{Type: "letter_request", ID: request}, map[string]audit.Change{"reference": {nil, ref}, "sha256": {nil, hex.EncodeToString(sum[:])}, "template": {nil, tid.String()}}, "")
}

// RefuseLetter refuses a request that has not been issued, with a reason the person is told; an HR officer or the signatory does it, and the person may withdraw their own.
func RefuseLetter(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, reason string, withdraw bool) error {
	state := "refused"
	if withdraw {
		state = "withdrawn"
	} else if r := strings.TrimSpace(reason); r == "" || len(r) > 500 {
		return &letters.Refusal{Code: letters.CodeInvalid, Message: "A refusal needs a reason of up to 500 characters: tell the person why."}
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.letter_request SET state = $2, decision_reason = $3, decided_at = $4 WHERE id = $1 AND state IN ('requested', 'checked')", request, state, orNil(strings.TrimSpace(reason)), at)
	if err != nil {
		return letterError(err)
	}
	if tag.RowsAffected() != 1 {
		return &letters.Refusal{Code: letters.CodeConflict, Message: "That request is issued, refused or withdrawn already, or there is no such request."}
	}
	var person uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT person_id FROM nemura.letter_request WHERE id = $1", request).Scan(&person); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.letter."+state, audit.Object{Type: "letter_request", ID: request}, map[string]audit.Change{"state": {nil, state}}, reason); err != nil {
		return err
	}
	return notice(ctx, tx, "letter_"+state, person, request, "letter-"+state)
}

// SignLetter issues a checked letter (NM-LTR-03, NM-LTR-07): the signatory has seen the hash of the exact bytes and confirmed with a passkey in the last five minutes
// (stepUp says so; the API checks it), the hash they confirm must be the checked letter's, and the bytes in the store must still hash to it. The letter is written once as
// evidence: the hash, the template version, the data, the signatory, their authority and the envelope the passkey confirmed.
func SignLetter(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, confirmed [32]byte, stepUpAt time.Time, authority string, blobs ports.BlobStore) (uuid.UUID, error) {
	if blobs == nil {
		return uuid.Nil, &letters.Refusal{Code: "NM_UNAVAILABLE", Message: "This deployment has no file store for letters yet: ask whoever runs Nemura."}
	}
	var person, tid uuid.UUID
	var key, state string
	var refP, blobP *string
	var sum, data []byte
	err := tx.QueryRow(ctx, "SELECT person_id, type_key, template_id, reference, render_blob, render_sha256, data, state FROM nemura.letter_request WHERE id = $1 FOR UPDATE", request).Scan(&person, &key, &tid, &refP, &blobP, &sum, &data, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter request."}
	}
	if err != nil {
		return uuid.Nil, err
	}
	if state != "checked" || refP == nil || blobP == nil {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "That request is " + state + ": only a checked letter is signed."}
	}
	ref, blob := *refP, *blobP
	if !bytes.Equal(sum, confirmed[:]) {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The hash you confirmed is not the checked letter's: open the letter again and confirm the one shown."}
	}
	rc, err := blobs.Get(ctx, blob)
	if err != nil {
		return uuid.Nil, err
	}
	pdf, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return uuid.Nil, err
	}
	if got := sha256.Sum256(pdf); !bytes.Equal(got[:], sum) {
		return uuid.Nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The stored letter no longer matches its hash: it is not signed, and the change is for the security team to look into."}
	}
	env := map[string]any{"request": request.String(), "reference": ref, "sha256": hex.EncodeToString(sum), "template": tid.String(), "signed_at": at.UTC().Format(time.RFC3339)}
	if stepUpAt.IsZero() {
		env["signed_under"] = authority // no passkey: a delegation the signatory made gave the right
	} else {
		env["step_up_at"] = stepUpAt.UTC().Format(time.RFC3339)
	}
	envelope, _ := json.Marshal(env)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.letter_issued (id, request_id, person_id, type_key, reference, blob_key, sha256, template_id, data, signatory, authority, envelope, issued_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12::jsonb, $13)`, id, request, person, key, ref, blob, sum, tid, string(data), by, authority, string(envelope), at); err != nil {
		return uuid.Nil, letterError(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.letter_request SET state = 'issued', decided_at = $2 WHERE id = $1", request, at); err != nil {
		return uuid.Nil, letterError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.letter.issued", audit.Object{Type: "letter_issued", ID: id}, map[string]audit.Change{"reference": {nil, ref}, "sha256": {nil, hex.EncodeToString(sum)}, "authority": {nil, authority}}, ""); err != nil {
		return uuid.Nil, err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.letter.request_issued", audit.Object{Type: "letter_request", ID: request}, map[string]audit.Change{"state": {"checked", "issued"}}, ""); err != nil {
		return uuid.Nil, err
	}
	return id, notice(ctx, tx, "letter_issued", person, request, "letter-issued")
}

// LetterRow is a request as it is listed, with the issued letter when there is one.
type LetterRow struct {
	ID, Person                       uuid.UUID
	PersonName, Type, Purpose, State string
	Due                              time.Time
	RequestedAt                      time.Time
	Reference                        string
	SHA256                           string
	Reason                           string
	Issued                           *uuid.UUID
	IssuedAt                         *time.Time
}

// LetterScope is whose requests a caller reads: their own, or everyone's for the people team and the signatory.
type LetterScope struct {
	Member uuid.UUID
	All    bool
}

// LetterRequests lists the requests a scope may read, those still open first by due day.
func LetterRequests(ctx context.Context, tx pgx.Tx, s LetterScope, state string) ([]LetterRow, error) {
	rows, err := tx.Query(ctx, `SELECT q.id, q.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), q.type_key, q.purpose, q.state, q.due_on, q.requested_at, coalesce(q.reference, ''),
 coalesce(encode(q.render_sha256, 'hex'), ''), coalesce(q.decision_reason, ''), i.id, i.issued_at
FROM nemura.letter_request q JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id
LEFT JOIN nemura.letter_issued i ON i.organisation_id = q.organisation_id AND i.request_id = q.id
WHERE ($2 OR p.member_id = $1) AND ($3 = '' OR q.state = $3) ORDER BY (q.state IN ('requested', 'checked')) DESC, q.due_on, q.id`, s.Member, s.All, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LetterRow
	for rows.Next() {
		var r LetterRow
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Type, &r.Purpose, &r.State, &r.Due, &r.RequestedAt, &r.Reference, &r.SHA256, &r.Reason, &r.Issued, &r.IssuedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReadLetter returns an issued letter's bytes after checking them against the hash kept as evidence: a blob that no longer matches is refused, never served.
func ReadLetter(ctx context.Context, tx pgx.Tx, blobs ports.BlobStore, issued uuid.UUID) (string, []byte, error) {
	if blobs == nil {
		return "", nil, &letters.Refusal{Code: "NM_UNAVAILABLE", Message: "This deployment has no file store for letters yet: ask whoever runs Nemura."}
	}
	var ref, blob string
	var sum []byte
	if err := tx.QueryRow(ctx, "SELECT reference, blob_key, sha256 FROM nemura.letter_issued WHERE id = $1", issued).Scan(&ref, &blob, &sum); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter in your view."}
		}
		return "", nil, err
	}
	rc, err := blobs.Get(ctx, blob)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = rc.Close() }()
	pdf, err := io.ReadAll(rc)
	if err != nil {
		return "", nil, err
	}
	if got := sha256.Sum256(pdf); !bytes.Equal(got[:], sum) {
		return "", nil, &letters.Refusal{Code: letters.CodeConflict, Message: "The stored letter no longer matches the hash kept as its evidence: it is not served, and the change is for the security team to look into."}
	}
	return ref, pdf, nil
}

// LinkLifetime is how long a delivery link works.
const LinkLifetime = 7 * 24 * time.Hour

// CreateLetterLink makes an expiring delivery link for an issued letter: the token is shown once, and only its hash is kept (NM-LTR-03). The token names the organisation, so
// the page that takes it can find the letter without a session.
func CreateLetterLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, issued uuid.UUID) (string, time.Time, error) {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.letter_issued WHERE id = $1)", issued).Scan(&exists); err != nil || !exists {
		return "", time.Time{}, &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter in your view."}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := org.String() + "." + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	exp := at.Add(LinkLifetime)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.letter_link (id, issued_id, token_hash, expires_at, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)", id, issued, sum[:], exp, by, at); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, Audit(ctx, tx, org, at, user(by), "nemura.letter.link_created", audit.Object{Type: "letter_link", ID: id}, map[string]audit.Change{"letter": {nil, issued.String()}, "expires_at": {nil, exp.UTC().Format(time.RFC3339)}}, "")
}

// LinkOrganisation reads the organisation a token names, so the unauthenticated delivery can open the right tenant; a malformed token names none.
func LinkOrganisation(token string) (uuid.UUID, bool) {
	org, _, ok := strings.Cut(token, ".")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(org)
	return id, err == nil
}

// LetterByLink returns the letter a live token delivers, or a refusal that says only that the link is not valid: unknown, wrong, and expired look the same.
func LetterByLink(ctx context.Context, tx pgx.Tx, blobs ports.BlobStore, token string, now time.Time) (string, []byte, error) {
	sum := sha256.Sum256([]byte(token))
	var issued uuid.UUID
	var exp time.Time
	err := tx.QueryRow(ctx, "SELECT issued_id, expires_at FROM nemura.letter_link WHERE token_hash = $1", sum[:]).Scan(&issued, &exp)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !exp.After(now)) {
		return "", nil, &letters.Refusal{Code: letters.CodeNotFound, Message: "This link is not valid: it may have expired. Ask for a new one in My requests."}
	}
	if err != nil {
		return "", nil, err
	}
	return ReadLetter(ctx, tx, blobs, issued)
}
