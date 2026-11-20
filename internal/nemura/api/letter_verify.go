package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// Verifying an issued letter (PRD-002 NM-LTR-05): anyone with the reference and the code printed on a letter learns whether it is valid or withdrawn, its type, the day it was issued and the company. It shows
// nothing of the person or the letter's words, and it is rate-limited: only so many tries for one reference and in all, so a code cannot be guessed.

// LetterVerificationView is what a look-up shows.
type LetterVerificationView struct {
	Found    bool    `json:"found" doc:"False for a wrong code, an unknown reference and a letter not issued yet, all alike." example:"true"`
	Status   *string `json:"status,omitempty" enum:"valid,withdrawn" doc:"valid: the company issued this letter and it stands; withdrawn: the verification was withdrawn, so do not rely on the letter." example:"valid"`
	Type     *string `json:"type,omitempty" doc:"The kind of letter." example:"employment_confirmation"`
	IssuedOn *string `json:"issued_on,omitempty" format:"date" doc:"The day it was issued." example:"2026-11-20"`
	Company  *string `json:"company,omitempty" doc:"The company that issued it." example:"Acme Pte. Ltd."`
}

// LetterVerify is the look-up and the employee's withdrawal.
type LetterVerify interface {
	Verify(ctx context.Context, reference, code string) (LetterVerificationView, error)
	Withdraw(ctx context.Context, s Session, letter uuid.UUID) error
}

type letterVerifyInput struct {
	Body struct {
		Reference string `json:"reference" minLength:"3" maxLength:"60" doc:"The reference printed on the letter." example:"EC-2026-0001"`
		Code      string `json:"code" minLength:"8" maxLength:"40" doc:"The verification code printed on the letter." example:"K7QX-2M4D-9WTB-HP3R"`
	}
}
type letterVerifyOutput struct{ Body LetterVerificationView }
type letterWithdrawInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The issued letter." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
}
type letterWithdrawOutput struct {
	Body struct {
		Withdrawn bool `json:"withdrawn" doc:"True once the letter's verification is withdrawn." example:"true"`
	}
}

// verifyLimit allows 10 tries a reference an hour and 300 in all a minute; a look-up past either is refused without looking.
type verifyLimit struct {
	mu    sync.Mutex
	byRef map[string][]time.Time
	all   []time.Time
}

func (l *verifyLimit) allow(ref string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byRef == nil {
		l.byRef = map[string][]time.Time{}
	}
	keep := func(ts []time.Time, since time.Time) []time.Time {
		out := ts[:0]
		for _, t := range ts {
			if t.After(since) {
				out = append(out, t)
			}
		}
		return out
	}
	l.all = keep(l.all, now.Add(-time.Minute))
	key := strings.ToUpper(strings.TrimSpace(ref))
	l.byRef[key] = keep(l.byRef[key], now.Add(-time.Hour))
	if len(l.all) >= 300 || len(l.byRef[key]) >= 10 {
		return false
	}
	l.all, l.byRef[key] = append(l.all, now), append(l.byRef[key], now)
	if len(l.byRef) > 10000 { // a flood of references cannot grow it without end
		l.byRef = map[string][]time.Time{key: l.byRef[key]}
	}
	return true
}

var verifyLimiter verifyLimit

func init() {
	IdempotencyExempt["POST /v1/letter-verification"] = "a look-up changes nothing the caller can see (it is audited and counted against the rate limit)"
	IdempotencyExempt["POST /v1/me/letters/{id}/verification/withdrawal"] = "a verification is withdrawn once: a second withdrawal is refused (NM_LETTER_CONFLICT)"
}

func (a *API) registerLetterVerify() {
	huma.Register(a.API, huma.Operation{
		OperationID: "verifyLetter", Method: http.MethodPost, Path: "/v1/letter-verification", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Verify an issued letter",
		Description: "PRD-002 NM-LTR-05. With no sign-in: the reference and the verification code printed on a letter. The answer is whether the company issued it and it stands or was withdrawn, its type, the day it was issued and the company, " +
			"and nothing of the person or the words. A wrong code, an unknown reference and a letter not issued yet answer alike. At most 10 tries for a reference in an hour and 300 in all in a minute (NM_RATE_LIMITED); every look-up of a " +
			"found letter is audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *letterVerifyInput) (*letterVerifyOutput, error) {
		if a.d.LetterVerify == nil {
			return nil, Err(CodeUnavailable, "Letter verification is not set up in this deployment.")
		}
		if !verifyLimiter.allow(in.Body.Reference, a.d.Clock.Now()) {
			return nil, Err("NM_RATE_LIMITED", "Too many tries for this reference or in all just now. Wait a while and try again with the reference and code exactly as printed.")
		}
		v, err := a.d.LetterVerify.Verify(ctx, in.Body.Reference, in.Body.Code)
		if err != nil {
			return nil, huma.Error500InternalServerError("The letter could not be looked up.")
		}
		return &letterVerifyOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawLetterVerification", Method: http.MethodPost, Path: "/v1/me/letters/{id}/verification/withdrawal", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Withdraw the verification of your own letter",
		Description: "The person the letter is about withdraws its verification: a recipient who looks it up is told it is withdrawn. It never withdraws the letter, which stays as issued. Audited. It is on the never-list for the AI and MCP surfaces.",
		Errors:      []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *letterWithdrawInput) (*letterWithdrawOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.LetterVerify == nil {
			return nil, Err(CodeUnavailable, "Letter verification is not set up in this deployment.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such letter of yours.")
		}
		if err := a.d.LetterVerify.Withdraw(ctx, s, id); err != nil {
			return nil, lettersError(err)
		}
		out := &letterWithdrawOutput{}
		out.Body.Withdrawn = true
		return out, nil
	})
}
