package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Verify integrity (AUD-03)

// AuditVerification is the outcome of verifying the audit trail for a range of days (AUD-03; p1-audit-explorer D5).
type AuditVerification struct {
	From            string `json:"from" format:"date" doc:"The range's first day, in the organisation's time zone." example:"2026-10-01"`
	To              string `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	FirstSeq        int64  `json:"first_seq" doc:"The first event of the range, by its place in the chain; 0 when no event occurred in it." example:"118"`
	LastSeq         int64  `json:"last_seq" doc:"The last event of the range." example:"342"`
	Events          int    `json:"events" doc:"How many events the range holds: each recomputed and linked to the one before it." example:"225"`
	Anchors         int    `json:"anchors" doc:"The hourly signed anchors in the range, each checked against the recomputed chain." example:"31"`
	CloseAnchors    int    `json:"close_anchors" doc:"The signed close snapshots in the range, each checked against the stored snapshot and the chain (AUD-04)." example:"1"`
	Details         int    `json:"details" doc:"The security events' request details checked against the fingerprints in their bodies (AUD-06)." example:"12"`
	DetailsChecked  bool   `json:"details_checked" doc:"false when this service holds no fingerprint key, so the details were not checked." example:"true"`
	AnchoredThrough int64  `json:"anchored_through" doc:"The range's events are anchored through this event; later ones are anchored within the hour." example:"336"`
	Outcome         string `json:"outcome" enum:"passed,failed,explained" doc:"passed: every link, anchor and detail agrees. failed: the trail was altered; problem and failed_seq say where. explained: a purge boundary or a restore left a gap, each with its signed record (AUD-09)." example:"passed"`
	FailedSeq       int64  `json:"failed_seq,omitempty" doc:"failed: the first event that does not agree, when the failure has one." example:"201"`
	Problem         string `json:"problem,omitempty" doc:"failed: what does not agree, in words. explained: what left the gap, naming its purge or recovery." example:"event 201 was altered: its body no longer gives its stored hash"`
}

type auditVerificationInput struct {
	From string `query:"from" required:"true" format:"date" doc:"The range's first day (YYYY-MM-DD), in the organisation's time zone." example:"2026-10-01"`
	To   string `query:"to" required:"true" format:"date" doc:"Its last day; at most 366 days after from." example:"2026-10-31"`
}

type auditVerificationOutput struct{ Body AuditVerification }

func registerAuditVerification(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAuditVerification, huma.Operation{
		OperationID: "getAuditVerification", Method: http.MethodGet, Path: "/v1/audit/verification", Tags: []string{"Audit trail"},
		Summary: "Verify the audit trail's integrity for a range of days",
		Description: "Verify integrity (AUD-03): recomputes the hash chain of the events that occurred on the days from from to to, from " +
			"the event before them, and checks every signed anchor, close anchor and security event's request details in the range " +
			"against it. A range is at most 366 days; the nightly verification checks the whole trail. Changes nothing; side effects: " +
			"its audit event, audit.verified_by_member.",
	}, append([]string{CodeInvalid, audit.CodeRange, CodeUnavailable}, locks...), func(ctx context.Context, in *auditVerificationInput) (*auditVerificationOutput, error) {
		from, err1 := time.Parse(time.DateOnly, in.From)
		to, err2 := time.Parse(time.DateOnly, in.To)
		var bad []FieldProblem
		if err1 != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.from", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
		}
		if err2 != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "query.to", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		if a.deps.Anchors == nil || a.deps.Signer == nil {
			return nil, problem(CodeUnavailable, "This installation holds no audit anchors to verify against.",
				"Ask whoever runs the installation; the nightly verification still runs where the anchors are.")
		}
		p, _ := authn.From(ctx)
		var out auditVerificationOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			loc, err := time.LoadLocation(o.Profile.TimeZone)
			if err != nil || o.Profile.TimeZone == "" {
				loc = time.UTC
			}
			r, err := audit.VerifyRange(ctx, u, a.deps.Anchors, a.deps.Signer, a.deps.Keyed, p.Organisation, from, to, loc)
			if err != nil {
				return err
			}
			out.Body = AuditVerification{From: in.From, To: in.To, FirstSeq: r.FirstSeq, LastSeq: r.LastSeq, Events: r.Events, Anchors: r.Anchors,
				CloseAnchors: r.Closes, Details: r.Details, DetailsChecked: !r.DetailsUnchecked, AnchoredThrough: r.AnchoredThrough,
				Outcome: r.Outcome, FailedSeq: r.FailedSeq, Problem: r.Problem}
			return nil
		})
		var re *audit.RangeError
		if errors.As(err, &re) {
			return nil, problem(audit.CodeRange, sentence(re.Message), sentence(re.Fix))
		}
		return &out, err
	})
}
