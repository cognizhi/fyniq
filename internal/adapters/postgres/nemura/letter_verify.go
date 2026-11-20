package nemura

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Verifying an issued letter (PRD-002 NM-LTR-05): a recipient enters the reference and the code printed on the letter and learns only whether it is valid or withdrawn, its type, the day it was issued and
// the company. The code is 80 random bits made when the letter is checked and printed on it; the database keeps only a hash of the reference and the code (migration 000061).

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newVerificationCode is 80 random bits as sixteen letters and digits in four groups, such as K7QX-2M4D-9WTB-HP3R.
func newVerificationCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := codeEncoding.EncodeToString(b)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16], nil
}

// verificationHash is what the directory keeps: the reference and the code with case, dashes and spaces ignored, so a recipient's typing does not matter.
func verificationHash(reference, code string) []byte {
	norm := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	sum := sha256.Sum256([]byte("nemura-letter-verification|" + strings.TrimSpace(reference) + "|" + norm))
	return sum[:]
}

// LetterVerification is the answer to a look-up: what the page may show, and nothing of the person or the letter's words.
type LetterVerification struct {
	Valid        bool // false once the employee withdraws it
	Withdrawn    bool
	Type         string
	IssuedOn     time.Time
	Company      string
	LetterID     uuid.UUID
	Organisation uuid.UUID
}

// VerifyLetter looks a reference and code up with no sign-in. A wrong code, an unknown reference and a letter not issued yet all answer false the same way. A found letter's look-up is audited in its
// organisation, by the service, with the state shown and nothing else.
func (s *Store) VerifyLetter(ctx context.Context, reference, code string, now time.Time) (LetterVerification, bool, error) {
	var v LetterVerification
	var state string
	err := s.pool.QueryRow(ctx, `SELECT organisation_id, letter_id, state, type_key, issued_on, company FROM nemura.letter_verification_lookup($1)`, verificationHash(reference, code)).
		Scan(&v.Organisation, &v.LetterID, &state, &v.Type, &v.IssuedOn, &v.Company)
	if errors.Is(err, pgx.ErrNoRows) {
		return LetterVerification{}, false, nil
	}
	if err != nil {
		return LetterVerification{}, false, err
	}
	v.Valid, v.Withdrawn = state == "valid", state == "withdrawn"
	err = s.InOrg(ctx, v.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return Audit(ctx, tx, v.Organisation, now, audit.Actor{Type: "service", ID: "letter-verification"}, "nemura.letter.verified", audit.Object{Type: "letter_issued", ID: v.LetterID},
			map[string]audit.Change{"state": {nil, state}}, "")
	})
	return v, true, err
}

// WithdrawLetterVerification is the employee's revocation of their own letter's verification: a recipient who looks it up is told it is withdrawn. It never withdraws the letter, which stays as issued.
func WithdrawLetterVerification(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member, letter uuid.UUID) error {
	var own bool
	err := tx.QueryRow(ctx, `SELECT coalesce(p.member_id = $2, false) FROM nemura.letter_issued i JOIN nemura.person p ON p.organisation_id = i.organisation_id AND p.id = i.person_id WHERE i.id = $1`, letter, member).Scan(&own)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !own) {
		return &letters.Refusal{Code: letters.CodeNotFound, Message: "There is no such letter of yours."}
	}
	if err != nil {
		return err
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT nemura.letter_verification_withdraw($1, $2)`, org, letter).Scan(&done); err != nil {
		return err
	}
	if !done {
		return &letters.Refusal{Code: letters.CodeConflict, Message: "That letter's verification is withdrawn already, or it has none."}
	}
	return Audit(ctx, tx, org, at, user(member), "nemura.letter.verification_withdrawn", audit.Object{Type: "letter_issued", ID: letter}, nil, "")
}

// LetterVerifyService is the API's side of the verification: the public look-up and the employee's withdrawal.
type LetterVerifyService struct {
	Store *Store
	Clock ports.Clock
}

// Verify looks the reference and code up and shows only what NM-LTR-05 allows.
func (c LetterVerifyService) Verify(ctx context.Context, reference, code string) (api.LetterVerificationView, error) {
	v, ok, err := c.Store.VerifyLetter(ctx, reference, code, c.Clock.Now())
	if err != nil || !ok {
		return api.LetterVerificationView{}, err
	}
	status, typ, on, company := "valid", v.Type, v.IssuedOn.Format(time.DateOnly), v.Company
	if v.Withdrawn {
		status = "withdrawn"
	}
	return api.LetterVerificationView{Found: true, Status: &status, Type: &typ, IssuedOn: &on, Company: &company}, nil
}

// Withdraw withdraws the verification of the caller's own letter.
func (c LetterVerifyService) Withdraw(ctx context.Context, s api.Session, letter uuid.UUID) error {
	return c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return WithdrawLetterVerification(ctx, tx, s.Organisation, c.Clock.Now(), s.Member, letter)
	})
}
