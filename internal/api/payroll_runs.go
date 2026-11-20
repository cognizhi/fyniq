package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ pay runs from Nemura (FR-PAY-07, FR-PPL-04, 09, 10, 11; p1-payroll-link)
//
// An approved Nemura run reaches Fyniq as payroll.run.approved, through the link's client with the scope payroll.journal.submit and as an
// envelope signed with the link key. Before anything posts, Fyniq verifies, in the posting's own transaction: the envelope as every event
// (organisations, epoch, 5-minute window, signature, digest); that the approval envelope's SHA-256 is the one named; that it was signed for the
// linked organisation; that the preparer is not the approver and the approver held the right; the approver's signed record, a passkey assertion
// over that SHA-256, against a passkey the approver enrolled in Fyniq (design D3: never a key the message carries); that the run was calculated
// against Fyniq's current mapping; that the envelope was not used before and the version is newer than any posted. The figures it posts are the
// approval envelope's own, so an altered total under an earlier approval has nothing to stand on. Any failure refuses the run with its own
// code, creates nothing and is audited in a transaction of its own.

// PayrollMapping is what each department's salary and each kind of deduction post to, by version (FR-EMP-02's minimum; design D8).
type PayrollMapping struct {
	Version int               `json:"version" doc:"The mapping's version: a run is calculated against one, and Fyniq refuses a run for another than the current one." example:"1"`
	Entries map[string]string `json:"entries" doc:"department:<department ID> or deduction:<kind> -> the account's ID." example:"{\"department:0192a000-0000-7000-8000-0000000000d1\":\"0192a000-0000-7000-8000-000000006011\"}"`
	SetBy   string            `json:"set_by,omitempty" doc:"Who set it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	SetAt   *time.Time        `json:"set_at,omitempty" doc:"When it was set." example:"2026-11-20T09:00:00Z"`
}

// PayrollMappingChange sets the next version of the mapping.
type PayrollMappingChange struct {
	Entries map[string]string `json:"entries" minProperties:"1" maxProperties:"200" doc:"department:<department ID> (a dimension value of the Department dimension) or deduction:<kind> -> the account's ID: a salary expense account, or the asset or liability account a deduction is recovered through (1250 for an advance recovered)." example:"{\"department:0192a000-0000-7000-8000-0000000000d1\":\"0192a000-0000-7000-8000-000000006011\"}"`
}

// PayrollRunReceipt is what became of a pay run's version.
type PayrollRunReceipt struct {
	EventID     string `json:"event_id" format:"uuid" doc:"The event's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Outcome     string `json:"outcome" enum:"posted,unchanged,duplicate" doc:"posted: the difference is in the ledger; unchanged: the version states the figures already posted, so nothing posted; duplicate: the event was received before." example:"posted"`
	RunID       string `json:"run_id,omitempty" format:"uuid" doc:"The run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Version     int    `json:"version,omitempty" doc:"The version." example:"2"`
	EntryID     string `json:"entry_id,omitempty" format:"uuid" doc:"The journal entry it posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	PostingDate string `json:"posting_date,omitempty" format:"date" doc:"The day it posted: the wage month's last day, or the current open month's day when the wage month is closed (FR-PPL-11)." example:"2026-10-31"`
}

type (
	payrollMappingOutput struct {
		Body PayrollMapping
	}
	payrollMappingInput struct {
		KeyHeader
		PreferHeader
		Body PayrollMappingChange
	}
	payrollRunOutput struct{ Body PayrollRunReceipt }
	payrollRunInput  struct {
		RawBody []byte
		Body    PeopleLinkEvent
	}
)

var runFixes = map[string]string{
	peoplelink.CodeRunEnvelope:  "Send the approval envelope whose canonical SHA-256 the approval names",
	peoplelink.CodeRunOrg:       "Approve the run in the Nemura organisation that is linked to this one",
	peoplelink.CodeRunSegregate: "Have someone other than the run's preparer approve it",
	peoplelink.CodeRunRight:     "Have a person who holds the payroll-approval right approve it",
	peoplelink.CodeRunPasskey:   "Approve the run again with a passkey the approver enrolled in Fyniq",
	peoplelink.CodeRunMapping:   "Calculate the run against Fyniq's current account mapping, shown on getPayrollMapping",
	peoplelink.CodeRunUsed:      "Approve the run again: each approval is used once",
	peoplelink.CodeRunStale:     "Send the latest version of the run: a version no newer than one posted is refused",
	peoplelink.CodeRunApprover:  "Have the approver sign in to Fyniq once and enrol a passkey there; no Fyniq role is needed",
	peoplelink.CodeRunPosting:   "Fix what the ledger's code names, then send the run again",
}

func runRefusal(code, detail string) *Problem { return problem(code, detail, runFixes[code]) }

func registerPayrollRuns(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetPayrollMapping, huma.Operation{
		OperationID: "getPayrollMapping", Method: http.MethodGet, Path: "/v1/payroll-mapping", Tags: []string{"People"},
		Summary: "Show the account mapping pay runs post by",
		Description: "The current version of what each department's salary and each kind of deduction post to (FR-EMP-02's minimum, p1-payroll-link design D8), " +
			"for the people with payroll rights. A pay run is calculated against one version and Fyniq refuses a run for another. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, _ *struct{}) (*payrollMappingOutput, error) {
		p, _ := authn.From(ctx)
		var out PayrollMapping
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			m, err := u.PayrollRuns().Mapping(ctx)
			if errors.Is(err, ports.ErrNotFound) {
				out = PayrollMapping{Entries: map[string]string{}}
				return nil
			}
			if err != nil {
				return err
			}
			at := m.SetAt
			out = PayrollMapping{Version: m.Version, Entries: map[string]string{}, SetBy: m.SetBy, SetAt: &at}
			for k, acc := range m.Entries {
				out.Entries[k] = acc.String()
			}
			return nil
		})
		return &payrollMappingOutput{Body: out}, err
	})
	register(a, authz.SetPayrollMapping, huma.Operation{
		OperationID: "setPayrollMapping", Method: http.MethodPost, Path: "/v1/payroll-mapping", Tags: []string{"People"},
		Summary: "Set the next version of the account mapping",
		Description: "Sets the next version of the mapping pay runs post by (FR-EMP-02's minimum): each department's salary expense account and each kind of " +
			"deduction's account. A new version makes a run calculated against the old one refused until Nemura recalculates it. Side effects: the new " +
			"version and its audit event, and, with a link that has keys, `people.department.changed` for Nemura (the mapping's version and its departments by ID and name, never an account). Idempotent with Idempotency-Key. On the never-list for the AI and MCP (§9.6). Left: dual control by the approval engine.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid}, locks...), func(ctx context.Context, in *payrollMappingInput) (*payrollMappingOutput, error) {
		p, _ := authn.From(ctx)
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PayrollMapping, error) {
			entries := map[string]uuid.UUID{}
			for k, v := range in.Body.Entries {
				id, err := uuid.Parse(v)
				if err != nil {
					return PayrollMapping{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.entries." + k, Message: "is not an account ID", Fix: "Send the account's ID from the chart"}})
				}
				if !strings.HasPrefix(k, "department:") && !strings.HasPrefix(k, "deduction:") {
					return PayrollMapping{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.entries", Message: fmt.Sprintf("%q is not a department or a deduction kind", k), Fix: "Use department:<department ID> or deduction:<kind>"}})
				}
				entries[k] = id
			}
			now := a.deps.Clock.Now()
			m, err := u.PayrollRuns().SetMapping(ctx, entries, "user:"+p.UserID.String(), now)
			if err != nil {
				return PayrollMapping{}, err
			}
			// Nemura is told the mapping's version and its departments, in the same unit of work, so a pay run is calculated against the mapping Fyniq holds (PPL-16); nothing is queued with no link
			// that has keys. The accounts never travel: only the departments' IDs and names.
			if err := a.tellNemuraOfTheMapping(ctx, u, p.Organisation, m.Version, entries); err != nil {
				return PayrollMapping{}, err
			}
			out := PayrollMapping{Version: m.Version, Entries: in.Body.Entries, SetBy: m.SetBy, SetAt: &now}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &payrollMappingOutput{Body: resp.Body}, nil
	})
	register(a, authz.SubmitPayrollRun, exemptByEventID(huma.Operation{
		OperationID: "submitPayrollRun", Method: http.MethodPost, Path: "/v1/people-link/payroll-runs", Tags: []string{"People"},
		Summary: "Receive an approved pay run from Nemura and post it",
		Description: "Nemura's `payroll.run.approved` (PRD-002 Appendix E, NM-INT-06): the link's envelope carrying the approval envelope over the run's canonical " +
			"figures, its SHA-256 and the approver's signed record. It needs the link's client and the scope payroll.journal.submit. Before anything posts, " +
			"Fyniq verifies in one transaction the link's signature and epoch, the approval envelope's digest, the linked organisation, that the preparer is " +
			"not the approver and the approver held the right, the approver's passkey assertion against a passkey they enrolled in Fyniq, the account-mapping " +
			"version, that the envelope is unused and the version newer than any posted; each failure has its own code (PPL-11 to PPL-19), posts nothing, " +
			"creates nothing and is audited. It then posts the difference from what the run's earlier versions posted (payroll_run@1) under the period locks " +
			"and checks of any posting: department salary expense, employer CPF, SDL, 2110, 2100, deductions and claims paid with salary, no per-person line, " +
			"attributed to the approver via Nemura; a closed wage month's difference posts in the current open month. Side effects: the entry, the receipt and " +
			"the inbox row, with their audit events. Idempotent on the event ID (a repeat answers duplicate). Only the link's client may call it; on the " +
			"never-list for the AI and MCP (§9.6).",
	}), append([]string{CodeNoLink, CodeLinkClient, CodeLinkMinimum, peoplelink.CodeMalformed, peoplelink.CodeSignature, peoplelink.CodeDigest, peoplelink.CodeReplay,
		peoplelink.CodeEpoch, peoplelink.CodeOrg, peoplelink.CodeRunEnvelope, peoplelink.CodeRunOrg, peoplelink.CodeRunSegregate, peoplelink.CodeRunRight,
		peoplelink.CodeRunPasskey, peoplelink.CodeRunMapping, peoplelink.CodeRunUsed, peoplelink.CodeRunStale, peoplelink.CodeRunApprover,
		storage.CodePayrollRun, storage.CodePeriodClosed, ledger.CodePayroll, ledger.CodeSystemRole, ledger.CodeUnknownAccount, ledger.CodeDimensionRequired,
		ledger.CodeDimensionValue, ledger.CodeNoDate, ledger.CodeTooLong}, locks...), func(ctx context.Context, in *payrollRunInput) (*payrollRunOutput, error) {
		return a.submitPayrollRun(ctx, in)
	})
}

// tellNemuraOfTheMapping queues people.department.changed for a mapping version: each department the mapping names, by Fyniq's dimension value and its name, in order.
func (a *API) tellNemuraOfTheMapping(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, version int, entries map[string]uuid.UUID) error {
	payload := linksend.DepartmentChanged{MappingVersion: version, Departments: []linksend.Department{}}
	for k := range entries {
		id, ok := strings.CutPrefix(k, "department:")
		if !ok {
			continue
		}
		value, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		v, err := u.Dimensions().GetValue(ctx, value)
		if errors.Is(err, ports.ErrNotFound) {
			continue // a department the mapping names and the chart of dimensions does not have cannot be named to Nemura
		}
		if err != nil {
			return err
		}
		payload.Departments = append(payload.Departments, linksend.Department{DepartmentID: v.ID.String(), Name: v.Name})
	}
	sort.Slice(payload.Departments, func(i, j int) bool { return payload.Departments[i].DepartmentID < payload.Departments[j].DepartmentID })
	_, err := linksend.Queue(ctx, u, linksend.TypeDepartmentChanged, "payroll_mapping", org.String(), version, payload)
	return err
}

func exemptByEventID(op huma.Operation) huma.Operation {
	op = exempt(op, "Idempotency-Key", "The event's ID is its idempotency key: the inbox receives each ID once and a repeat answers duplicate (FR-PPL-15, NM-INT-10).")
	return exempt(op, "Prefer", "A pay run is a signed, approved message between the apps, not a person's command: a preview of it would prove nothing (FR-INT-14).")
}

// assertion is the approver's signed record: a passkey assertion over the approval envelope's SHA-256.
func assertion(sub peoplelink.RunSubmission) (storage.PasskeyAssertion, error) {
	dec := func(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
	ad, e1 := dec(sub.Approval.AuthenticatorData)
	cd, e2 := dec(sub.Approval.ClientData)
	sig, e3 := dec(sub.Approval.Signature)
	if err := errors.Join(e1, e2, e3); err != nil || len(ad) == 0 || len(cd) == 0 || len(sig) == 0 {
		return storage.PasskeyAssertion{}, errors.New("the approver's signed record is not a passkey assertion (base64url)")
	}
	return storage.PasskeyAssertion{AuthenticatorData: ad, ClientData: cd, Signature: sig}, nil
}

// verifyApprover checks the approver's passkey assertion against the passkeys they enrolled in Fyniq, matched by issuer and subject (design D3).
func (a *API) verifyApprover(ctx context.Context, u storage.UnitOfWork, link storage.PeopleLinkRow, who peoplelink.Party, challenge []byte, sub peoplelink.RunSubmission) *Problem {
	if a.passkeys == nil || link.NemuraOrigin == "" || a.deps.Installation == nil {
		return runRefusal(peoplelink.CodeRunPasskey, "Fyniq cannot verify the approver's passkey here: the link has no Nemura origin, or no relying party is configured.")
	}
	user, err := a.deps.Installation.UserForIdentity(ctx, who.Issuer, who.Subject)
	if err != nil {
		return runRefusal(peoplelink.CodeRunApprover, "The approver has no sign-in identity in Fyniq.")
	}
	keys, err := u.Passkeys().List(ctx, user, false)
	if err != nil || len(keys) == 0 {
		return runRefusal(peoplelink.CodeRunApprover, "The approver has no passkey enrolled in Fyniq.")
	}
	as, err := assertion(sub)
	if err != nil {
		return runRefusal(peoplelink.CodeRunPasskey, err.Error()+".")
	}
	for _, k := range keys {
		if a.passkeys.ReverifyAt(link.NemuraOrigin, k, challenge, as) == nil {
			return nil
		}
	}
	return runRefusal(peoplelink.CodeRunPasskey, "The approver's signed record does not verify against their passkey.")
}

// runLedger reads a verified approval envelope into the ledger's run, through the mapping (FR-EMP-02's minimum).
func runLedger(env peoplelink.RunEnvelope, m storage.PayrollMapping, base money.Currency, posting time.Time) (ledger.PayrollRun, *Problem) {
	run := ledger.PayrollRun{Source: "nemura", RunID: uuid.MustParse(env.RunID), Version: env.Version, PostingDate: posting,
		Memo: fmt.Sprintf("Pay run %s (wage month %s, version %d), via Nemura", env.RunID[:8], env.WageMonth, env.Version)}
	run.WageMonth, _ = time.Parse("2006-01", env.WageMonth)
	amount := func(s string) (money.Money, *Problem) {
		if s == "" {
			s = "0"
		}
		v, err := money.Parse(s, base)
		if err != nil {
			return v, runRefusal(peoplelink.CodeRunEnvelope, fmt.Sprintf("%q is not an amount in %s.", s, base.Code))
		}
		return v, nil
	}
	var pr *Problem
	for _, f := range []struct {
		to *money.Money
		v  string
	}{{&run.EmployeeCPF, env.EmployeeCPF}, {&run.EmployerCPF, env.EmployerCPF}, {&run.SDL, env.SDL}, {&run.SHG, env.SHG}, {&run.Reimbursements, env.Reimbursements}} {
		if *f.to, pr = amount(f.v); pr != nil {
			return run, pr
		}
	}
	for _, d := range env.Departments {
		id, err := uuid.Parse(d.DepartmentID)
		acc, ok := m.Entries["department:"+strings.ToLower(d.DepartmentID)]
		if err != nil || !ok {
			return run, runRefusal(peoplelink.CodeRunMapping, fmt.Sprintf("The mapping has no account for the department %s.", d.DepartmentID))
		}
		g, pr := amount(d.Gross)
		if pr != nil {
			return run, pr
		}
		run.Departments = append(run.Departments, ledger.PayrollDepartment{Account: acc, Values: []uuid.UUID{id}, Gross: g})
	}
	for _, d := range env.Deductions {
		acc, ok := m.Entries["deduction:"+d.Kind]
		if !ok {
			return run, runRefusal(peoplelink.CodeRunMapping, fmt.Sprintf("The mapping has no account for the deduction %q.", d.Kind))
		}
		v, pr := amount(d.Amount)
		if pr != nil {
			return run, pr
		}
		run.Deductions = append(run.Deductions, ledger.PayrollDeduction{Account: acc, Amount: v})
	}
	return run, nil
}

// submitPayrollRun verifies and posts one approved run (FR-PPL-04, 09, 10, 11).
func (a *API) submitPayrollRun(ctx context.Context, in *payrollRunInput) (*payrollRunOutput, error) {
	p, _ := authn.From(ctx)
	var env peoplelink.Envelope
	if err := json.NewDecoder(bytes.NewReader(in.RawBody)).Decode(&env); err != nil {
		return nil, problem(peoplelink.CodeMalformed, "The envelope is not well formed.", linkFixes[peoplelink.CodeMalformed])
	}
	if env.Header.Type != peoplelink.TypePayrollRunApproved {
		return nil, problem(peoplelink.CodeMalformed, fmt.Sprintf("This operation takes %q, not %q.", peoplelink.TypePayrollRunApproved, env.Header.Type), "Send the event to the operation that takes its type.")
	}
	now := a.deps.Clock.Now()
	var receipt PayrollRunReceipt
	var runID uuid.UUID
	var version int
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		l, refusal, err := a.linkedEnvelope(ctx, u, p, env, now)
		if err != nil {
			return err
		}
		if refusal != nil {
			return refusal
		}
		eventID, err := uuid.Parse(env.Header.EventID)
		if err != nil {
			return linkRefusal(peoplelink.CodeMalformed, "The event ID is not an ID.")
		}
		entry := storage.InboxEntry{EventID: eventID, LinkID: l.ID, Epoch: env.Header.Epoch, Sequence: env.Header.Sequence, Type: env.Header.Type, ObjectType: env.Header.ObjectType,
			ObjectID: env.Header.ObjectID, ObjectVersion: env.Header.ObjectVersion, PayloadSHA256: env.Header.PayloadSHA256, Outcome: peoplelink.OutcomeApplied, ReceivedAt: now}
		if fresh, err := u.PeopleLinks().Receive(ctx, entry); err != nil {
			return err
		} else if !fresh {
			receipt = PayrollRunReceipt{EventID: env.Header.EventID, Outcome: "duplicate"}
			return nil
		}
		run, sub, challenge, err := peoplelink.ParseRun(env.Payload, l.NemuraOrganisationID.String())
		if err != nil {
			var r *peoplelink.Refusal
			if errors.As(err, &r) {
				return runRefusal(r.Code, r.Message+".")
			}
			return err
		}
		runID, version = uuid.MustParse(run.RunID), run.Version
		if used, err := u.PayrollRuns().EnvelopeUsed(ctx, sub.EnvelopeSHA256); err != nil {
			return err
		} else if used {
			return runRefusal(peoplelink.CodeRunUsed, "The approval envelope was used before.")
		}
		mapping, err := u.PayrollRuns().Mapping(ctx)
		if errors.Is(err, ports.ErrNotFound) || (err == nil && mapping.Version != run.MappingVersion) {
			return runRefusal(peoplelink.CodeRunMapping, fmt.Sprintf("The run was calculated against mapping version %d; Fyniq's current mapping is version %d.", run.MappingVersion, mapping.Version))
		}
		if err != nil {
			return err
		}
		latest, err := u.PayrollRuns().LatestReceipt(ctx, run.SourceSystem, runID)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		if err == nil && run.Version <= latest.Version {
			return runRefusal(peoplelink.CodeRunStale, fmt.Sprintf("Version %d is not newer than version %d, which is posted.", run.Version, latest.Version))
		}
		if pr := a.verifyApprover(ctx, u, l, run.Approver, challenge, sub); pr != nil {
			return pr
		}
		org, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(org.BaseCurrency)
		if err != nil {
			return err
		}
		wage, _ := time.Parse("2006-01", run.WageMonth)
		posting := wage.AddDate(0, 1, -1)
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		for _, per := range periods { // FR-PPL-11: a closed wage month's difference posts in the current open month; the original month stays in the memo
			if !posting.Before(per.StartsOn) && !posting.After(per.EndsOn) && per.Status == storage.PeriodClosed {
				posting = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			}
		}
		ledgerRun, pr := runLedger(run, mapping, base, posting)
		if pr != nil {
			return pr
		}
		var prior ledger.PayrollPosted
		if err == nil || latest.ID != uuid.Nil {
			if prior, err = ledger.DecodePayrollPosted(latest.Version, latest.Totals, base); err != nil {
				return err
			}
		}
		posted, stored, err := journal.PostPayrollRunIn(ctx, u, ledgerRun, prior)
		if err != nil {
			return err
		}
		next := ledger.PayrollTotals(prior, posted)
		if len(posted.Lines) == 0 { // nothing changed: the totals stay, the version is recorded
			next = ledger.PayrollPosted{Version: run.Version, Totals: prior.Totals}
		}
		if _, err := u.PayrollRuns().Receive(ctx, storage.PayrollRunReceipt{LinkID: l.ID, Epoch: l.Epoch, SourceSystem: run.SourceSystem, RunID: runID, Version: run.Version,
			WageMonth: wage, EnvelopeSHA256: sub.EnvelopeSHA256, ApproverIssuer: run.Approver.Issuer, ApproverSubject: run.Approver.Subject, PreparerIssuer: run.Preparer.Issuer,
			PreparerSubject: run.Preparer.Subject, MappingVersion: run.MappingVersion, EntryID: stored.ID, Totals: next.Encode(), ReceivedAt: now}); err != nil {
			return err
		}
		receipt = PayrollRunReceipt{EventID: env.Header.EventID, Outcome: "unchanged", RunID: run.RunID, Version: run.Version}
		ref := "unchanged" // a version that changed nothing is answered with the entry the run's earlier versions posted
		if latest.EntryID != uuid.Nil {
			ref = latest.EntryID.String()
		}
		if stored.ID != uuid.Nil {
			receipt.Outcome, receipt.EntryID, receipt.PostingDate = "posted", stored.ID.String(), posting.Format(time.DateOnly)
			ref = stored.ID.String()
		}
		// Nemura is told what became of the run, in the same unit of work (NM-INT-03): the posting's reference. Nothing is queued with no link that has keys.
		_, err = linksend.Queue(ctx, u, linksend.TypeRunPosted, "payroll_run", run.RunID, run.Version, linksend.RunPosted{RunID: run.RunID, Version: run.Version, Status: "posted", PostingRef: ref})
		return err
	})
	if err != nil {
		// FR-PPL-09: the refusal is audited, in a transaction of its own, since the run's was rolled back.
		pr := problemFrom(ctx, err)
		code := pr.Code
		_ = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "payroll_run.refused", ObjectType: "payroll_run", ObjectID: cmpID(runID, env.Header.ObjectID),
				Changes: map[string][2]any{"refusal": {nil, map[string]any{"code": code, "event_id": env.Header.EventID, "version": version, "epoch": env.Header.Epoch}}}}); err != nil {
				return err
			}
			// A refused run is answered to Nemura with the refusal's code and its plain words, so it waits there with them (NM-INT-03, NM-INT-06). The run is the one the link's header names, which the link's
			// signature covers. What came before the run was read (PPL-01 to PPL-09: a forged or malformed call, no link, not the link's client, an old epoch, a stale time), an envelope already used
			// (PPL-17) and a version that is not newer than one posted (PPL-18) are not answered: they say nothing about a run that is waiting, and a stale or repeated one must not read as a
			// refusal of a version that posted.
			if id, perr := uuid.Parse(env.Header.ObjectID); perr == nil && env.Header.ObjectVersion >= 1 && !strings.HasPrefix(code, "PPL-0") && code != peoplelink.CodeRunUsed && code != peoplelink.CodeRunStale {
				// The link's own refusals (PPL-*) say what is wrong with the run in words that carry no figure; any other refusal (a ledger's) is answered with its title and fix, which never change between occurrences.
				message := strings.TrimSpace(pr.Title + ". " + pr.Fix)
				if strings.HasPrefix(code, "PPL-") {
					message = pr.Detail
				}
				if l, lerr := u.PeopleLinks().Live(ctx); lerr == nil { // kept, so C.1 #19 lists the run until a version posts
					if err := u.PayrollRuns().RecordRefusal(ctx, l.ID, id, env.Header.ObjectVersion, code, now); err != nil {
						return err
					}
				}
				_, err := linksend.Queue(ctx, u, linksend.TypeRunPosted, "payroll_run", id.String(), env.Header.ObjectVersion,
					linksend.RunPosted{RunID: id.String(), Version: env.Header.ObjectVersion, Status: "refused", Code: code, Message: message})
				return err
			}
			return nil
		})
		return nil, err
	}
	return &payrollRunOutput{Body: receipt}, nil
}

// cmpID is the run's ID when the envelope was read far enough to give one, else the event's object ID, else a new one: what the refusal's audit names.
func cmpID(run uuid.UUID, object string) uuid.UUID {
	if run != uuid.Nil {
		return run
	}
	if id, err := uuid.Parse(object); err == nil {
		return id
	}
	return uuid.Must(uuid.NewV7())
}
