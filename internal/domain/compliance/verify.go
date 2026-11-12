package compliance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RunJob is the daily job that makes each organisation's verifications, after the checks have run (design D4).
const RunJob = "compliance.run"

// Window is how far back a verification reads: the last day, with an hour more, so a run that starts late misses
// nothing.
const Window = 25 * time.Hour

// Sample is how many of the latest events of each audited read the reconciliation reads (design D4).
const Sample = 1000

// AuditedReads are the actions of the reads and exports the API audits (AUD-05): internal/api's auditedReads names no
// other (TestEveryAuditedReadIsReconciled).
var AuditedReads = []string{"access_review.exported", "document.bank_account_revealed", "document_template_bundle.exported",
	"evidence.opened", "payment_batch.file_downloaded", "report.exported", "report_export.downloaded", "report_export.requested"}

// Finding is one thing a verification found, as the run records it and the Compliance Center lists it.
type Finding struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	At     string `json:"at,omitempty"`
	Detail string `json:"detail"`
}

// verification reads an organisation's records since the window began and returns what breaks its control, and in
// words what it read.
type verification func(ctx context.Context, u storage.UnitOfWork, since time.Time) (found []Finding, read string, err error)

// verifications are the verify checks built, by the name the registry gives them (registry.Verifications).
var verifications = map[string]verification{
	"payments.dual-control":    dualControl,
	"members.second-factor":    secondFactor,
	"audit.exports-reconciled": exportsReconciled,
}

// Built reports whether the verification name is built.
func Built(name string) bool { _, ok := verifications[name]; return ok }

// dualControl (CTL-FIN-08, FM-1): every approval of a payment batch or of a payee's or claimant's bank change, and
// every release of a batch, was another person's, or a break-glass's use by its Owner (FM-8).
func dualControl(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	approvals, releases, err := u.Compliance().DualControl(ctx, since)
	if err != nil {
		return nil, "", err
	}
	return dualControlFindings(approvals, releases), fmt.Sprintf("%d approval(s) and %d release(s)", len(approvals), len(releases)), nil
}

// dualControlFindings are the approvals nobody signed or that their preparer or requester signed, and the releases under
// such an approval, under a break-glass its Owner did not use for the batch, or under neither.
func dualControlFindings(approvals []storage.DualControlApproval, releases []storage.DualControlRelease) []Finding {
	var out []Finding
	for _, a := range approvals {
		switch {
		case a.Signatures == 0:
			out = append(out, Finding{Kind: a.ObjectType, ID: a.ObjectKey, At: day(a.DecidedAt), Detail: "Approved with no signature"})
		case a.OwnSignatures > 0:
			out = append(out, Finding{Kind: a.ObjectType, ID: a.ObjectKey, At: day(a.DecidedAt),
				Detail: "Approved with the signature of whoever prepared it or asked for it"})
		}
	}
	for _, r := range releases {
		var why string
		switch {
		case r.ApprovalID != nil && (!r.Approved || r.Signatures == 0 || r.OwnSignatures > 0):
			why = "Released under an approval that is not another person's"
		case r.ApprovalID == nil && r.BreakGlassID != nil && !r.BreakGlassUsed:
			why = "Released under a break-glass its Owner did not use for this batch"
		case r.ApprovalID == nil && r.BreakGlassID == nil:
			why = "Released under neither an approval nor a break-glass"
		}
		if why != "" {
			out = append(out, Finding{Kind: "payment_batch", ID: r.BatchID.String(), At: day(r.ReleasedAt), Detail: why})
		}
	}
	return out
}

// secondFactor (CTL-IAM-01, SEC-01): every active member has a second factor, a passkey or a sign-in that passed one, and
// none was seen in a session that passed none.
func secondFactor(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	members, err := u.Compliance().SecondFactors(ctx, since)
	if err != nil {
		return nil, "", err
	}
	return secondFactorFindings(members), fmt.Sprintf("%d active member(s)", len(members)), nil
}

// secondFactorFindings are the members with no second factor, and those seen in a session that passed none.
func secondFactorFindings(members []storage.MemberSecondFactor) []Finding {
	var out []Finding
	for _, m := range members {
		if m.Passkeys == 0 && m.SessionsWith == 0 {
			out = append(out, Finding{Kind: "member", ID: m.UserID.String(),
				Detail: "Has no second factor: no passkey, and no sign-in that passed one"})
		}
		if m.SessionsWithout > 0 && m.LastWithout != nil {
			out = append(out, Finding{Kind: "member", ID: m.UserID.String(), At: day(*m.LastWithout),
				Detail: fmt.Sprintf("Seen in %d session(s) that passed no second factor", m.SessionsWithout)})
		}
	}
	return out
}

// exportsReconciled (CTL-AUD-05, AUD-05): every report export asked for has its audit event, and every audited read
// (an export, a download, an opening or a reveal) keeps the request it came from beside the chain.
func exportsReconciled(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	reads, err := u.Compliance().UnreconciledReads(ctx, since, AuditedReads, Sample)
	if err != nil {
		return nil, "", err
	}
	out := make([]Finding, len(reads))
	for i, r := range reads {
		out[i] = Finding{Kind: r.Kind, ID: r.ObjectKey, At: day(r.At), Detail: capitalised(r.Reason)}
	}
	return out, fmt.Sprintf("the exports asked for and up to %d of each kind of audited read", Sample), nil
}

func day(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func capitalised(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Runner makes each organisation's verifications and records their runs.
type Runner struct {
	Store    storage.Store
	Clock    ports.Clock
	Controls []registry.Control
}

// Report is what a run of one organisation's verifications recorded.
type Report struct {
	Organisation uuid.UUID
	Runs         []storage.ControlRun
}

// Nightly makes every organisation's verifications.
func (r Runner) Nightly(ctx context.Context) ([]Report, error) {
	orgs, err := r.Store.Organisations(ctx)
	if err != nil {
		return nil, err
	}
	var out []Report
	var failed []error
	for _, org := range orgs {
		rep, err := r.Verify(ctx, org)
		out = append(out, rep)
		if err != nil {
			failed = append(failed, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	if len(failed) > 0 {
		return out, fmt.Errorf("the verifications of %d organisation(s) did not all run: %w", len(failed), errors.Join(failed...))
	}
	return out, nil
}

// Verify makes the organisation's verifications, each in its own unit of work, so one that fails to run never stops the
// others, and records each run.
func (r Runner) Verify(ctx context.Context, org uuid.UUID) (Report, error) {
	rep := Report{Organisation: org}
	var failed []error
	for _, c := range r.Controls {
		for _, ch := range c.Checks {
			kind, name := ch.Kind()
			v, ok := verifications[name]
			if kind != registry.KindVerify || ch.Status != "running" || !ok {
				continue
			}
			err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				now := r.Clock.Now()
				found, read, err := v(ctx, u, now.Add(-Window))
				if err != nil {
					return err
				}
				run, err := runOf(c.ID, ch, found, read, now)
				if err != nil {
					return err
				}
				run, err = u.Compliance().RecordRun(ctx, run)
				rep.Runs = append(rep.Runs, run)
				return err
			})
			if err != nil {
				failed = append(failed, fmt.Errorf("%s %s: %w", c.ID, ch.How, err))
			}
		}
	}
	return rep, errors.Join(failed...)
}

// runOf is a verification's run: what it found, the first 50 items, and in words.
func runOf(control string, ch registry.Check, found []Finding, read string, now time.Time) (storage.ControlRun, error) {
	run := storage.ControlRun{Control: control, Frequency: ch.Frequency, Check: ch.How, Since: now.Add(-Window), RanAt: now,
		OK: len(found) == 0, Count: len(found)}
	items, err := json.Marshal(append([]Finding{}, found[:min(len(found), 50)]...))
	if err != nil {
		return run, err
	}
	run.Items = items
	if run.OK {
		run.Detail = "Nothing found in " + read + " of the last day"
	} else {
		run.Detail = fmt.Sprintf("%d found in %s of the last day: %s", len(found), read, found[0].Detail)
		if len(found) > 1 {
			run.Detail += ", and more"
		}
	}
	if len(run.Detail) > 2000 {
		run.Detail = run.Detail[:1997] + "..."
	}
	return run, nil
}
