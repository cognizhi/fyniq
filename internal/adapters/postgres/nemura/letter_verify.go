package nemura

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
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
	return s.lookupVerification(ctx, `SELECT organisation_id, letter_id, state, type_key, issued_on, company FROM nemura.letter_verification_lookup($1)`, verificationHash(reference, code), "reference", now)
}

// VerifyLetterFile looks a document up by the SHA-256 of its exact bytes, which the recipient's browser works out so that the file never leaves their device (NM-LTR-05). It answers what a look-up by
// reference and code answers, and a document that is not an issued letter, or has been changed by a byte, is not found.
func (s *Store) VerifyLetterFile(ctx context.Context, sha256 []byte, now time.Time) (LetterVerification, bool, error) {
	if len(sha256) != 32 {
		return LetterVerification{}, false, nil
	}
	return s.lookupVerification(ctx, `SELECT organisation_id, letter_id, state, type_key, issued_on, company FROM nemura.letter_verification_lookup_content($1)`, sha256, "file", now)
}

func (s *Store) lookupVerification(ctx context.Context, query string, arg []byte, by string, now time.Time) (LetterVerification, bool, error) {
	var v LetterVerification
	var state string
	err := s.pool.QueryRow(ctx, query, arg).Scan(&v.Organisation, &v.LetterID, &state, &v.Type, &v.IssuedOn, &v.Company)
	if errors.Is(err, pgx.ErrNoRows) {
		return LetterVerification{}, false, nil
	}
	if err != nil {
		return LetterVerification{}, false, err
	}
	v.Valid, v.Withdrawn = state == "valid", state == "withdrawn"
	err = s.InOrg(ctx, v.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return Audit(ctx, tx, v.Organisation, now, audit.Actor{Type: "service", ID: "letter-verification"}, "nemura.letter.verified", audit.Object{Type: "letter_issued", ID: v.LetterID},
			map[string]audit.Change{"state": {nil, state}, "by": {nil, by}}, "")
	})
	return v, true, err
}

// PublicRate counts one try against a limit that every API instance shares, held in the database: it reports whether the try is within `limit` tries for the key in the fixed window of
// `window` that holds `now`. What is counted is hashed, so a reference or a token is never stored (NM-LTR-05, migration 000062).
func (s *Store) PublicRate(ctx context.Context, scope, key string, window time.Duration, limit int, now time.Time) (bool, error) {
	sum := sha256.Sum256([]byte(scope + "|" + strings.ToUpper(strings.TrimSpace(key))))
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT nemura.public_rate_hit($1, $2, $3, $4, $5)`, scope, sum[:], int(window.Seconds()), limit, now).Scan(&ok)
	return ok, err
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
	return verificationView(v, ok, err)
}

// VerifyFile looks a document up by the SHA-256 of its bytes (64 hexadecimal characters) and shows the same.
func (c LetterVerifyService) VerifyFile(ctx context.Context, sha256hex string) (api.LetterVerificationView, error) {
	sum, err := hex.DecodeString(sha256hex)
	if err != nil {
		return api.LetterVerificationView{}, nil
	}
	v, ok, err := c.Store.VerifyLetterFile(ctx, sum, c.Clock.Now())
	return verificationView(v, ok, err)
}

// Allow counts a try of a public look-up against the limit every instance shares.
func (c LetterVerifyService) Allow(ctx context.Context, scope, key string, window time.Duration, limit int) (bool, error) {
	return c.Store.PublicRate(ctx, scope, key, window, limit, c.Clock.Now())
}

func verificationView(v LetterVerification, ok bool, err error) (api.LetterVerificationView, error) {
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

// Share makes a disclosure link for the caller's own letter.
func (c LetterVerifyService) Share(ctx context.Context, s api.Session, letter uuid.UUID, days int) (string, string, string, error) {
	var id uuid.UUID
	var token string
	var exp time.Time
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, token, exp, err = CreateLetterDisclosure(ctx, tx, s.Organisation, c.Clock.Now(), s.Member, letter, days)
		return err
	})
	return id.String(), token, exp.UTC().Format(time.RFC3339), err
}

// Shares lists the disclosure links of the caller's own letter.
func (c LetterVerifyService) Shares(ctx context.Context, s api.Session, letter uuid.UUID) ([]api.LetterShare, error) {
	var out []api.LetterShare
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = LetterDisclosures(ctx, tx, s.Member, letter)
		return err
	})
	return out, err
}

// Unshare revokes one of the caller's own disclosure links.
func (c LetterVerifyService) Unshare(ctx context.Context, s api.Session, id uuid.UUID) error {
	return c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return RevokeLetterDisclosure(ctx, tx, s.Organisation, c.Clock.Now(), s.Member, id)
	})
}

// Disclose shows what a live disclosure token shows with no session.
func (c LetterVerifyService) Disclose(ctx context.Context, token string) (api.LetterDisclosureView, error) {
	return c.Store.LetterDisclosureByToken(ctx, token, c.Clock.Now())
}
