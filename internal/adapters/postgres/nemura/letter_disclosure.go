package nemura

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
)

// A disclosure link (PRD-002 NM-LTR-05): the verification page proves a letter is genuine and shows nothing of the person. When the person wants a recipient to see what the letter says about them, they
// make a link that shows the letter's own facts, for a few days, and revoke it when they like. Revoking it never withdraws the letter or its verification, and withdrawing the verification stops it showing
// the facts. Only a hash of the token is kept; like a delivery link, the token names the organisation so the public page can find the letter.

// DisclosureLifetime is how long a disclosure link works unless the person chooses fewer days or more (up to DisclosureMaxDays).
const (
	DisclosureDefaultDays = 7
	DisclosureMaxDays     = 30
)

var errNoSuchLetter = &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter of yours."}

func ownIssued(ctx context.Context, tx pgx.Tx, member, issued uuid.UUID) error {
	var own bool
	err := tx.QueryRow(ctx, `SELECT coalesce(p.member_id = $2, false) FROM nemura.letter_issued i JOIN nemura.person p ON p.organisation_id = i.organisation_id AND p.id = i.person_id WHERE i.id = $1`, issued, member).Scan(&own)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !own) {
		return errNoSuchLetter
	}
	return err
}

// CreateLetterDisclosure makes a disclosure link for the caller's own issued letter that works for the days chosen (1 to 30). The token is shown once.
func CreateLetterDisclosure(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member, issued uuid.UUID, days int) (uuid.UUID, string, time.Time, error) {
	if days == 0 {
		days = DisclosureDefaultDays
	}
	if days < 1 || days > DisclosureMaxDays {
		return uuid.Nil, "", time.Time{}, &letters.Refusal{Code: letters.CodeInvalid, Message: "A disclosure link works for 1 to 30 days: choose how long the recipient may look."}
	}
	if err := ownIssued(ctx, tx, member, issued); err != nil {
		return uuid.Nil, "", time.Time{}, err
	}
	var state *string
	if err := tx.QueryRow(ctx, `SELECT nemura.letter_verification_state($1, $2)`, org, issued).Scan(&state); err != nil {
		return uuid.Nil, "", time.Time{}, err
	}
	if state == nil || *state != "valid" {
		return uuid.Nil, "", time.Time{}, &letters.Refusal{Code: letters.CodeConflict, Message: "That letter's verification is withdrawn: a link to its facts is not made for a letter that is not verifiable."}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return uuid.Nil, "", time.Time{}, err
	}
	token := org.String() + "." + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte("nemura-letter-disclosure|" + token))
	exp := at.Add(time.Duration(days) * 24 * time.Hour)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.letter_disclosure (id, issued_id, token_hash, expires_at, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, id, issued, sum[:], exp, member, at); err != nil {
		return uuid.Nil, "", time.Time{}, err
	}
	return id, token, exp, Audit(ctx, tx, org, at, user(member), "nemura.letter.disclosure_created", audit.Object{Type: "letter_disclosure", ID: id}, map[string]audit.Change{"letter": {nil, issued.String()}, "expires_at": {nil, exp.UTC().Format(time.RFC3339)}}, "")
}

// LetterDisclosures lists the disclosure links of the caller's own issued letter, newest first, with no token (it was shown once).
func LetterDisclosures(ctx context.Context, tx pgx.Tx, member, issued uuid.UUID) ([]api.LetterShare, error) {
	if err := ownIssued(ctx, tx, member, issued); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, created_at, expires_at, revoked_at FROM nemura.letter_disclosure WHERE issued_id = $1 ORDER BY created_at DESC, id DESC`, issued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.LetterShare{}
	for rows.Next() {
		var id uuid.UUID
		var created, exp time.Time
		var revoked *time.Time
		if err := rows.Scan(&id, &created, &exp, &revoked); err != nil {
			return nil, err
		}
		sh := api.LetterShare{ID: id.String(), CreatedAt: created.UTC().Format(time.RFC3339), ExpiresAt: exp.UTC().Format(time.RFC3339)}
		if revoked != nil {
			sh.RevokedAt = revoked.UTC().Format(time.RFC3339)
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// RevokeLetterDisclosure revokes one of the caller's own disclosure links, once. The letter and its verification stay as they were.
func RevokeLetterDisclosure(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member, id uuid.UUID) error {
	var issued uuid.UUID
	var revoked *time.Time
	err := tx.QueryRow(ctx, `SELECT issued_id, revoked_at FROM nemura.letter_disclosure WHERE id = $1`, id).Scan(&issued, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such link of yours."}
	}
	if err != nil {
		return err
	}
	if ownIssued(ctx, tx, member, issued) != nil {
		return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such link of yours."}
	}
	if revoked != nil {
		return &letters.Refusal{Code: letters.CodeConflict, Message: "That link is revoked already."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.letter_disclosure SET revoked_at = $2 WHERE id = $1`, id, at); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(member), "nemura.letter.disclosure_revoked", audit.Object{Type: "letter_disclosure", ID: id}, map[string]audit.Change{"revoked": {false, true}}, "")
}

// disclosedKeys are the facts a disclosure shows: the letter's own facts about the person, in the catalogue's order. The company, the letter's date and reference (the verification shows them) and
// what the letter is for are left out.
func disclosedFacts(data []byte) ([]api.LetterFact, error) {
	var vals map[string]string
	if err := json.Unmarshal(data, &vals); err != nil {
		return nil, err
	}
	out := []api.LetterFact{}
	for _, f := range letters.Fields {
		if strings.HasPrefix(f.Key, "company.") || strings.HasPrefix(f.Key, "letter.") || f.Key == "request.purpose" {
			continue
		}
		if v, ok := vals[f.Key]; ok && strings.TrimSpace(v) != "" {
			out = append(out, api.LetterFact{Label: f.Label, Value: v})
		}
	}
	return out, nil
}

// LetterDisclosureByToken is what a live token shows with no session: the letter's type, day and company, and, while its verification stands, the facts. A token that is unknown, malformed, expired or
// revoked is not found, all alike. Every look-up of a live token is audited without the facts.
func (s *Store) LetterDisclosureByToken(ctx context.Context, token string, now time.Time) (api.LetterDisclosureView, error) {
	org, ok := LinkOrganisation(token)
	if !ok {
		return api.LetterDisclosureView{}, nil
	}
	sum := sha256.Sum256([]byte("nemura-letter-disclosure|" + token))
	var out api.LetterDisclosureView
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var id, issued uuid.UUID
		var exp time.Time
		var revoked *time.Time
		var typ string
		var on time.Time
		var data []byte
		err := tx.QueryRow(ctx, `SELECT d.id, d.issued_id, d.expires_at, d.revoked_at, i.type_key, i.issued_at, i.data FROM nemura.letter_disclosure d
JOIN nemura.letter_issued i ON i.organisation_id = d.organisation_id AND i.id = d.issued_id WHERE d.token_hash = $1`, sum[:]).Scan(&id, &issued, &exp, &revoked, &typ, &on, &data)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (revoked != nil || !exp.After(now))) {
			return nil
		}
		if err != nil {
			return err
		}
		var state *string
		if err := tx.QueryRow(ctx, `SELECT nemura.letter_verification_state($1, $2)`, org, issued).Scan(&state); err != nil {
			return err
		}
		var company string
		if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT company_name FROM nemura.letter_settings), (SELECT name FROM nemura.organisation WHERE organisation_id = $1))`, org).Scan(&company); err != nil {
			return err
		}
		status := "withdrawn"
		out = api.LetterDisclosureView{Found: true, Type: typ, IssuedOn: on.UTC().Format(time.DateOnly), Company: company, ExpiresAt: exp.UTC().Format(time.RFC3339), Facts: []api.LetterFact{}}
		if state != nil && *state == "valid" {
			status = "valid"
			if out.Facts, err = disclosedFacts(data); err != nil {
				return err
			}
		}
		out.Status = status
		return Audit(ctx, tx, org, now, audit.Actor{Type: "service", ID: "letter-disclosure"}, "nemura.letter.disclosed", audit.Object{Type: "letter_disclosure", ID: id}, map[string]audit.Change{"state": {nil, status}}, "")
	})
	return out, err
}
