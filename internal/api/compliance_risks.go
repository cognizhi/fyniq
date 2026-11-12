package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ The risk register (CMP-06; p1-compliance-center D7, D9)

// CodeUnknownRisk: no risk of the organisation has that ID.
const CodeUnknownRisk = "COMPLIANCE-02"

// RiskReviewView is a review of a risk's version.
type RiskReviewView struct {
	Version      int    `json:"version" doc:"The version reviewed." example:"2"`
	ReviewedBy   string `json:"reviewed_by" doc:"Who reviewed it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	ReviewedAt   string `json:"reviewed_at" format:"date-time" doc:"When." example:"2027-03-30T09:12:00Z"`
	Note         string `json:"note,omitempty" doc:"What the reviewer noted." example:"Mitigations hold; no new exposure."`
	NextReviewOn string `json:"next_review_on" format:"date" doc:"The next review date it set." example:"2027-06-30"`
}

// RiskView is a risk of the register at its latest version.
type RiskView struct {
	ID            string          `json:"id" format:"uuid" doc:"The risk's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Ref           string          `json:"ref" doc:"Its reference in the register." example:"R7"`
	Title         string          `json:"title" doc:"The risk, in a sentence." example:"Data breach of payroll or personal data"`
	Description   string          `json:"description,omitempty" doc:"More about it." example:"§19 rates its likelihood Certain."`
	Owner         string          `json:"owner" enum:"owner,administrator,controller,accountant,dpo,auditor" doc:"The role that answers for it." example:"dpo"`
	Likelihood    string          `json:"likelihood" enum:"low,medium,high" doc:"How likely it is." example:"low"`
	Impact        string          `json:"impact" enum:"low,medium,high" doc:"How much it would hurt." example:"high"`
	Mitigation    string          `json:"mitigation" doc:"How it is reduced." example:"Encryption, masking, least privilege, monitoring, breach runbook"`
	ReviewMonths  int             `json:"review_months" minimum:"1" maximum:"12" doc:"How many months after a review the next one falls due: 3 is each quarter." example:"3"`
	NextReviewOn  string          `json:"next_review_on" format:"date" doc:"When it is next due for review." example:"2027-03-31"`
	ReviewOverdue bool            `json:"review_overdue" doc:"true once its review date has passed without a review." example:"false"`
	State         string          `json:"state" enum:"open,closed" doc:"open, or closed: kept in the register, no longer reviewed." example:"open"`
	Version       int             `json:"version" doc:"Its latest version; the ETag carries it for If-Match." example:"2"`
	SavedBy       string          `json:"saved_by" doc:"Who saved the latest version, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	SavedAt       string          `json:"saved_at" format:"date-time" doc:"When." example:"2027-01-05T10:00:00Z"`
	LastReview    *RiskReviewView `json:"last_review,omitempty" doc:"Its latest review; none before the first."`
}

// RiskInput is what a version of a risk says.
type RiskInput struct {
	Title        string `json:"title" minLength:"1" maxLength:"300" doc:"The risk, in a sentence." example:"A supplier's bank details are changed by a fraudster"`
	Description  string `json:"description,omitempty" maxLength:"4000" doc:"More about it." example:"Phishing emails ask to pay a new account."`
	Owner        string `json:"owner" enum:"owner,administrator,controller,accountant,dpo,auditor" doc:"The role that answers for it." example:"controller"`
	Likelihood   string `json:"likelihood" enum:"low,medium,high" doc:"How likely it is." example:"medium"`
	Impact       string `json:"impact" enum:"low,medium,high" doc:"How much it would hurt." example:"high"`
	Mitigation   string `json:"mitigation" minLength:"1" maxLength:"4000" doc:"How it is reduced." example:"A second person approves every bank change; the supplier is called back on a known number."`
	ReviewMonths int    `json:"review_months,omitempty" minimum:"1" maximum:"12" doc:"How many months after a review the next one falls due; left out, 3." example:"3"`
	NextReviewOn string `json:"next_review_on,omitempty" format:"date" doc:"When it is next due for review; left out of a new risk, the end of the next quarter, and of a change, as it was." example:"2027-03-31"`
	State        string `json:"state,omitempty" enum:"open,closed" doc:"open, or closed; left out, open, or as it was." example:"open"`
}

// RiskReviewInput is a review of a risk.
type RiskReviewInput struct {
	Note string `json:"note,omitempty" maxLength:"2000" doc:"What the reviewer noted." example:"Mitigations hold; no new exposure."`
}

// Risks is the register.
type Risks struct {
	Items []RiskView `json:"items" nullable:"false" doc:"Every risk, at its latest version, in the order of their references."`
}

type (
	// RiskPath is a risk's path parameter (exported: huma reads only an exported embedded struct).
	RiskPath struct {
		RiskID string `path:"risk_id" format:"uuid" doc:"The risk's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	risksOutput     struct{ Body Risks }
	riskCreateInput struct {
		KeyHeader
		PreferHeader
		Body RiskInput
	}
	riskReviseInput struct {
		RiskPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body RiskInput
	}
	riskReviewInput struct {
		RiskPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body RiskReviewInput
	}
	riskWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     RiskView
	}
)

func riskETag(v RiskView) string { return etag("v" + strconv.Itoa(v.Version)) }

func riskView(r storage.Risk, today time.Time) RiskView {
	out := RiskView{ID: r.ID.String(), Ref: r.Ref, Title: r.Title, Description: r.Description, Owner: r.Owner, Likelihood: r.Likelihood,
		Impact: r.Impact, Mitigation: r.Mitigation, ReviewMonths: r.ReviewMonths, NextReviewOn: r.NextReviewOn.Format(time.DateOnly),
		ReviewOverdue: r.State == "open" && r.NextReviewOn.Before(today), State: r.State, Version: r.Version, SavedBy: r.SavedBy,
		SavedAt: r.SavedAt.UTC().Format(time.RFC3339)}
	if l := r.LastReview; l != nil {
		out.LastReview = &RiskReviewView{Version: l.Version, ReviewedBy: l.ReviewedBy, ReviewedAt: l.ReviewedAt.UTC().Format(time.RFC3339),
			Note: l.Note, NextReviewOn: l.NextReviewOn.Format(time.DateOnly)}
	}
	return out
}

func riskAnswer(w written[RiskView]) *riskWriteOutput {
	if w.Previewed {
		return &riskWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &riskWriteOutput{Status: w.Status, ETag: riskETag(w.Body), Location: "/v1/compliance/risks/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// riskFields are a request's risk fields, over what the risk said before, refused with each field named.
func riskFields(in RiskInput, before *storage.Risk, now time.Time) (storage.RiskFields, error) {
	f := storage.RiskFields{Title: strings.TrimSpace(in.Title), Description: in.Description, Owner: in.Owner, Likelihood: in.Likelihood,
		Impact: in.Impact, Mitigation: strings.TrimSpace(in.Mitigation), ReviewMonths: in.ReviewMonths, State: in.State}
	if f.ReviewMonths == 0 {
		f.ReviewMonths = controls.ReviewMonths
		if before != nil {
			f.ReviewMonths = before.ReviewMonths
		}
	}
	if f.State == "" {
		f.State = "open"
		if before != nil {
			f.State = before.State
		}
	}
	switch {
	case in.NextReviewOn != "":
		day, err := time.Parse(time.DateOnly, in.NextReviewOn)
		if err != nil {
			return f, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.next_review_on", Message: "is not a date", Fix: "Send a date, such as 2027-03-31"}})
		}
		f.NextReviewOn = day
	case before != nil:
		f.NextReviewOn = before.NextReviewOn
	default:
		f.NextReviewOn = controls.NextQuarterEnd(now)
	}
	var problems []FieldProblem
	for _, p := range controls.CheckRisk(f, now) {
		if before != nil && p.Field == "next_review_on" && f.NextReviewOn.Equal(before.NextReviewOn) {
			continue // a change of something else keeps an overdue review date: the review moves it
		}
		problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body." + p.Field, Message: p.Message, Fix: p.Fix})
	}
	if len(problems) > 0 {
		return f, invalid(problems)
	}
	return f, nil
}

func (a *API) oneRisk(ctx context.Context, u storage.UnitOfWork, id string) (storage.Risk, error) {
	all, err := u.Compliance().Risks(ctx)
	if err != nil {
		return storage.Risk{}, err
	}
	for _, r := range all {
		if r.ID.String() == id {
			return r, nil
		}
	}
	return storage.Risk{}, problem(CodeUnknownRisk, "There is no risk "+id+" in the register.", "Use an ID from listRisks.")
}

func registerRisks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRisk}, locks...)
	register(a, authz.ListRisks, exempt(huma.Operation{
		OperationID: "listRisks", Method: http.MethodGet, Path: "/v1/compliance/risks", Tags: []string{"Compliance"},
		Summary: "List the risk register",
		Description: "Every risk of the register (CMP-06) at its latest version: the risk, its owner role, likelihood, impact and " +
			"mitigation, its review interval and next review date, whether that review is overdue, and its latest review. Read-only; " +
			"no side effects.",
	}, "cursor", "A company's risk register holds tens of risks, read whole: the page sorts and filters them itself."),
		append([]string{CodeInvalid}, locks...), func(ctx context.Context, _ *struct{}) (*risksOutput, error) {
			p, _ := authn.From(ctx)
			out := &risksOutput{Body: Risks{Items: []RiskView{}}}
			today := controls.Day(a.deps.Clock.Now())
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Compliance().Risks(ctx)
				for _, r := range all {
					out.Body.Items = append(out.Body.Items, riskView(r, today))
				}
				return err
			})
			return out, err
		})

	register(a, authz.CreateRisk, huma.Operation{
		OperationID: "createRisk", Method: http.MethodPost, Path: "/v1/compliance/risks", Tags: []string{"Compliance"},
		Summary:     "Add a risk to the register",
		Description: "Adds a risk at version 1, with the next reference (R and one more than the highest used); its first review is the end of the next quarter unless next_review_on says otherwise (CMP-06). Side effects: the risk, the audit event risk.saved. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *riskCreateInput) (*riskWriteOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RiskView, error) {
			f, err := riskFields(in.Body, nil, now)
			if err != nil {
				return RiskView{}, err
			}
			r, err := u.Compliance().CreateRisk(ctx, storage.NewRisk{RiskFields: f, SavedBy: uploader(ctx), At: now})
			return riskView(r, controls.Day(now)), err
		})
		if err != nil {
			return nil, err
		}
		return riskAnswer(w), nil
	})

	register(a, authz.ReviseRisk, huma.Operation{
		OperationID: "reviseRisk", Method: http.MethodPut, Path: "/v1/compliance/risks/{risk_id}", Tags: []string{"Compliance"},
		Summary:     "Change a risk",
		Description: "Saves the risk's next version (CMP-06): what it says, its owner, rating, mitigation, review interval or date, or closes it. Earlier versions are kept. Side effects: the new version, the audit event risk.saved. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeUnknownRisk, CodeStale, CodeIfMatchRequired}, writes...),
		func(ctx context.Context, in *riskReviseInput) (*riskWriteOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (RiskView, error) {
				before, err := a.oneRisk(ctx, u, in.RiskID)
				if err != nil {
					return RiskView{}, err
				}
				if err := checkIfMatch(in.IfMatch, riskETag(riskView(before, controls.Day(now)))); err != nil {
					return RiskView{}, err
				}
				f, err := riskFields(in.Body, &before, now)
				if err != nil {
					return RiskView{}, err
				}
				r, err := u.Compliance().ReviseRisk(ctx, before.ID, f, uploader(ctx), now, before.Version)
				return riskView(r, controls.Day(now)), err
			})
			if err != nil {
				return nil, err
			}
			return riskAnswer(w), nil
		})

	register(a, authz.ReviewRisk, huma.Operation{
		OperationID: "reviewRisk", Method: http.MethodPost, Path: "/v1/compliance/risks/{risk_id}/reviews", Tags: []string{"Compliance"},
		Summary:     "Review a risk",
		Description: "Records a review of the risk's latest version (CMP-06): who reviewed it and when, a note, and its next review date, review_months after today. A closed risk is not reviewed. Side effects: the review, the audit event risk.reviewed. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeUnknownRisk, CodeStale, CodeIfMatchRequired}, writes...),
		func(ctx context.Context, in *riskReviewInput) (*riskWriteOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RiskView, error) {
				before, err := a.oneRisk(ctx, u, in.RiskID)
				if err != nil {
					return RiskView{}, err
				}
				if err := checkIfMatch(in.IfMatch, riskETag(riskView(before, controls.Day(now)))); err != nil {
					return RiskView{}, err
				}
				if before.State != "open" {
					return RiskView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "risk_id", Message: "is closed",
						Fix: "Open the risk again with reviseRisk before reviewing it"}})
				}
				r, err := u.Compliance().ReviewRisk(ctx, before.ID, storage.RiskReview{Version: before.Version, ReviewedBy: uploader(ctx), ReviewedAt: now,
					Note: strings.TrimSpace(in.Body.Note), NextReviewOn: controls.NextReview(now, before.ReviewMonths)})
				if errors.Is(err, ports.ErrNotFound) {
					return RiskView{}, problem(CodeUnknownRisk, "There is no risk "+in.RiskID+" in the register.", "Use an ID from listRisks.")
				}
				return riskView(r, controls.Day(now)), err
			})
			if err != nil {
				return nil, err
			}
			return riskAnswer(w), nil
		})
}
