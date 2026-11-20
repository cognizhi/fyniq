package api

import (
	"context"
	"net/http"
	"strings"
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

// LetterFact is one fact of a letter a disclosure link shows.
type LetterFact struct {
	Label string `json:"label" doc:"What the fact is." example:"Job title"`
	Value string `json:"value" doc:"The fact, as the letter states it." example:"Product designer"`
}

// LetterDisclosureView is what a disclosure link shows: the letter's own facts, for as long as its verification stands.
type LetterDisclosureView struct {
	Found     bool         `json:"found" doc:"False for a link that is unknown, malformed, expired or revoked, all alike." example:"true"`
	Status    string       `json:"status,omitempty" enum:"valid,withdrawn" doc:"valid: the facts are shown; withdrawn: the person withdrew the letter's verification, so none are." example:"valid"`
	Type      string       `json:"type,omitempty" doc:"The kind of letter." example:"employment_confirmation"`
	IssuedOn  string       `json:"issued_on,omitempty" format:"date" doc:"The day it was issued." example:"2026-11-20"`
	Company   string       `json:"company,omitempty" doc:"The company that issued it." example:"Acme Pte. Ltd."`
	ExpiresAt string       `json:"expires_at,omitempty" format:"date-time" doc:"When the link stops working." example:"2026-11-27T10:00:00Z"`
	Facts     []LetterFact `json:"facts,omitempty" doc:"The letter's facts about the person, in the order the letter's catalogue lists them; empty once the verification is withdrawn."`
}

// LetterShare is one of a person's disclosure links; the token was shown once and is not kept.
type LetterShare struct {
	ID        string `json:"id" format:"uuid" doc:"The link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e91"`
	CreatedAt string `json:"created_at" format:"date-time" doc:"When it was made." example:"2026-11-20T10:00:00Z"`
	ExpiresAt string `json:"expires_at" format:"date-time" doc:"When it stops working." example:"2026-11-27T10:00:00Z"`
	RevokedAt string `json:"revoked_at,omitempty" format:"date-time" doc:"When the person revoked it, when they did." example:"2026-11-21T10:00:00Z"`
}

// LetterVerify is the public look-ups, the person's withdrawal and their disclosure links, and the limit the public ones share.
type LetterVerify interface {
	Verify(ctx context.Context, reference, code string) (LetterVerificationView, error)
	VerifyFile(ctx context.Context, sha256 string) (LetterVerificationView, error)
	Withdraw(ctx context.Context, s Session, letter uuid.UUID) error
	// Allow counts a try against a limit every API instance shares (held in the database) and says whether it is within `limit` tries for the key in the window.
	Allow(ctx context.Context, scope, key string, window time.Duration, limit int) (bool, error)
	Share(ctx context.Context, s Session, letter uuid.UUID, days int) (id, token, expires string, err error)
	Shares(ctx context.Context, s Session, letter uuid.UUID) ([]LetterShare, error)
	Unshare(ctx context.Context, s Session, id uuid.UUID) error
	Disclose(ctx context.Context, token string) (LetterDisclosureView, error)
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

type letterFileInput struct {
	Body struct {
		SHA256 string `json:"sha256" pattern:"^[0-9a-fA-F]{64}$" doc:"The SHA-256 of the exact bytes of the document, in hexadecimal, which the page works out in the browser so the file never leaves the device." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	}
}
type letterDisclosureInput struct {
	Body struct {
		Token string `json:"token" minLength:"40" maxLength:"200" doc:"The token in the link the person shared." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44.abc"`
	}
}
type letterDisclosureOutput struct{ Body LetterDisclosureView }
type letterShareInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The issued letter." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
	Body   struct {
		Days int `json:"days,omitempty" minimum:"0" maximum:"30" doc:"How many days the link works, 1 to 30; 0 or left out is 7." example:"7"`
	}
}
type letterShareOutput struct {
	Body struct {
		ID      string `json:"id" format:"uuid" doc:"The link, for revoking it." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e91"`
		Token   string `json:"token" doc:"The token, shown once: the page /verify#<token> shows the letter's facts until the link expires or is revoked." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44.abc"`
		Expires string `json:"expires_at" format:"date-time" doc:"When the link stops working." example:"2026-11-27T10:00:00Z"`
	}
}
type letterSharesInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The issued letter." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"`
}
type letterSharesOutput struct {
	Body struct {
		Links []LetterShare `json:"links" doc:"The links made for the letter, newest first; their tokens were shown once and are not kept."`
	}
}
type letterRevokeOutput struct {
	Body struct {
		Revoked bool `json:"revoked" doc:"True once the link is revoked." example:"true"`
	}
}
type letterUnshareInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The disclosure link." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e91"`
}

// The limits of the public look-ups, counted in the database so every API instance adds to the same ones: ten tries for a reference an hour, thirty for a document or a link an hour, and 300 in all
// a minute across the three.
const (
	rateRef, rateFile, rateToken = 10, 30, 60
	rateAll                      = 300
)

func init() {
	IdempotencyExempt["POST /v1/letter-verification"] = "a look-up changes nothing the caller can see (it is audited and counted against the rate limit)"
	IdempotencyExempt["POST /v1/letter-verification/file"] = "a look-up changes nothing the caller can see (it is audited and counted against the rate limit)"
	IdempotencyExempt["POST /v1/letter-disclosure"] = "a look-up changes nothing the caller can see (it is audited and counted against the rate limit)"
	IdempotencyExempt["POST /v1/me/letters/{id}/verification/withdrawal"] = "a verification is withdrawn once: a second withdrawal is refused (NM_LETTER_CONFLICT)"
	IdempotencyExempt["POST /v1/me/letters/{id}/disclosure-links"] = "making a second link for a letter is harmless: each has its own token, and the person revokes any of them"
	IdempotencyExempt["POST /v1/me/letter-disclosure-links/{id}/revocation"] = "a link is revoked once: a second revocation is refused (NM_LETTER_CONFLICT)"
}

// allowPublic counts a public look-up against its own key's limit and the limit of all of them, both shared by every API instance. A limit it cannot count fails closed.
func (a *API) allowPublic(ctx context.Context, scope, key string, per int) error {
	if a.d.LetterVerify == nil {
		return Err(CodeUnavailable, "Letter verification is not set up in this deployment.")
	}
	one, err := a.d.LetterVerify.Allow(ctx, scope, key, time.Hour, per)
	if err != nil {
		return Err(CodeUnavailable, "The letter could not be looked up just now. Try again in a while.")
	}
	all, err := a.d.LetterVerify.Allow(ctx, "letter-public", "all", time.Minute, rateAll)
	if err != nil {
		return Err(CodeUnavailable, "The letter could not be looked up just now. Try again in a while.")
	}
	if !one || !all {
		return Err("NM_RATE_LIMITED", "Too many tries for this reference or in all just now. Wait a while and try again with the reference and code exactly as printed.")
	}
	return nil
}

func (a *API) registerLetterVerify() {
	huma.Register(a.API, huma.Operation{
		OperationID: "verifyLetter", Method: http.MethodPost, Path: "/v1/letter-verification", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Verify an issued letter",
		Description: "PRD-002 NM-LTR-05. With no sign-in: the reference and the verification code printed on a letter. The answer is whether the company issued it and it stands or was withdrawn, its type, the day it was issued and the company, " +
			"and nothing of the person or the words. A wrong code, an unknown reference and a letter not issued yet answer alike. At most 10 tries for a reference in an hour and 300 in all in a minute, counted across every API instance (NM_RATE_LIMITED); every look-up of a " +
			"found letter is audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *letterVerifyInput) (*letterVerifyOutput, error) {
		if err := a.allowPublic(ctx, "letter-ref", in.Body.Reference, rateRef); err != nil {
			return nil, err
		}
		v, err := a.d.LetterVerify.Verify(ctx, in.Body.Reference, in.Body.Code)
		if err != nil {
			return nil, huma.Error500InternalServerError("The letter could not be looked up.")
		}
		return &letterVerifyOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "verifyLetterFile", Method: http.MethodPost, Path: "/v1/letter-verification/file", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Verify an issued letter by its document",
		Description: "PRD-002 NM-LTR-05. With no sign-in: the SHA-256 of the exact bytes of a document, which the verification page works out in the browser so the file itself never leaves the recipient's device. " +
			"The answer is what the look-up by reference and code answers; a document that is not an issued letter, or that changed by one byte, is not found. At most 30 tries for one hash in an hour and 300 in all in a minute, " +
			"counted across every API instance (NM_RATE_LIMITED); every look-up of a found letter is audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *letterFileInput) (*letterVerifyOutput, error) {
		if err := a.allowPublic(ctx, "letter-file", strings.ToLower(in.Body.SHA256), rateFile); err != nil {
			return nil, err
		}
		v, err := a.d.LetterVerify.VerifyFile(ctx, in.Body.SHA256)
		if err != nil {
			return nil, huma.Error500InternalServerError("The letter could not be looked up.")
		}
		return &letterVerifyOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "discloseLetter", Method: http.MethodPost, Path: "/v1/letter-disclosure", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Read what a disclosure link shows",
		Description: "PRD-002 NM-LTR-05. With no sign-in: the token in a link the person a letter is about shared. It shows the letter's type, day and company and the letter's own facts about the person, while the link lives and the letter's " +
			"verification stands; a link that is unknown, expired or revoked answers found false, all alike. The token is in the body so it is never in a URL or a log. Counted against the shared limits (NM_RATE_LIMITED); every look-up is audited without the facts. " +
			"It is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *letterDisclosureInput) (*letterDisclosureOutput, error) {
		if err := a.allowPublic(ctx, "letter-token", in.Body.Token, rateToken); err != nil {
			return nil, err
		}
		v, err := a.d.LetterVerify.Disclose(ctx, strings.TrimSpace(in.Body.Token))
		if err != nil {
			return nil, huma.Error500InternalServerError("The link could not be looked up.")
		}
		return &letterDisclosureOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "withdrawLetterVerification", Method: http.MethodPost, Path: "/v1/me/letters/{id}/verification/withdrawal", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Withdraw the verification of your own letter",
		Description: "The person the letter is about withdraws its verification: a recipient who looks it up is told it is withdrawn, and a disclosure link stops showing the facts. It never withdraws the letter, which stays as issued. Audited. It is on the never-list for the AI and MCP surfaces.",
		Errors:      []int{401, 404, 409, 503},
	}, func(ctx context.Context, in *letterWithdrawInput) (*letterWithdrawOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
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
	huma.Register(a.API, huma.Operation{
		OperationID: "createLetterDisclosureLink", Method: http.MethodPost, Path: "/v1/me/letters/{id}/disclosure-links", Tags: []string{"Letters"}, DefaultStatus: http.StatusCreated, Summary: "Share your own letter's facts through a link",
		Description: "PRD-002 NM-LTR-05. The person the letter is about makes a link that shows the letter's own facts to whoever holds it, for 1 to 30 days (7 by default). The token is shown once and only its hash is kept. Refused for a letter whose " +
			"verification is withdrawn. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 404, 409, 422},
	}, func(ctx context.Context, in *letterShareInput) (*letterShareOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
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
		link, token, exp, err := a.d.LetterVerify.Share(ctx, s, id, in.Body.Days)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterShareOutput{}
		out.Body.ID, out.Body.Token, out.Body.Expires = link, token, exp
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLetterDisclosureLinks", Method: http.MethodGet, Path: "/v1/me/letters/{id}/disclosure-links", Tags: []string{"Letters"}, Summary: "The links you made for your own letter",
		Description: "The disclosure links of the caller's own issued letter, newest first, with their expiry and whether they were revoked. Tokens are never shown again.", Errors: []int{401, 404},
	}, func(ctx context.Context, in *letterSharesInput) (*letterSharesOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
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
		links, err := a.d.LetterVerify.Shares(ctx, s, id)
		if err != nil {
			return nil, lettersError(err)
		}
		out := &letterSharesOutput{}
		out.Body.Links = links
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "revokeLetterDisclosureLink", Method: http.MethodPost, Path: "/v1/me/letter-disclosure-links/{id}/revocation", Tags: []string{"Letters"}, DefaultStatus: http.StatusOK, Summary: "Revoke a link to your own letter's facts",
		Description: "The person revokes one of their own disclosure links: it stops showing the facts at once. It never withdraws the letter or its verification. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 404, 409},
	}, func(ctx context.Context, in *letterUnshareInput) (*letterRevokeOutput, error) {
		s, err := a.signedInOrLeaver(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if a.d.LetterVerify == nil {
			return nil, Err(CodeUnavailable, "Letter verification is not set up in this deployment.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such link of yours.")
		}
		if err := a.d.LetterVerify.Unshare(ctx, s, id); err != nil {
			return nil, lettersError(err)
		}
		out := &letterRevokeOutput{}
		out.Body.Revoked = true
		return out, nil
	})
}
