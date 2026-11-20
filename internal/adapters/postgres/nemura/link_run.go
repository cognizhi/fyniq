package nemura

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ------------------------------------------------------------------ a pay run's proof to Fyniq while linked (NM-INT-03, 06, 07; FR-PAY-07)

// runLinkState says whether pay runs are approved with proof: any link past its proposal (active, suspended or ending) closes the plain approval, so a run approved while the apps are linked always has the
// envelope's proof or is not approved. Only an active link sends.
func runLinkState(ctx context.Context, tx pgx.Tx) (state string, err error) {
	l, err := LiveLink(ctx, tx)
	if noLink(err) {
		return "", nil
	}
	if err != nil || l.State == "proposed" {
		return "", err
	}
	return l.State, nil
}

// MemberIdentity is the sign-in identity of a member of the organisation: the issuer and subject the shared sign-in knows them by, never an email (NM-INT-07, NM-INT-12).
func MemberIdentity(ctx context.Context, tx pgx.Tx, member uuid.UUID) (link.Party, bool, error) {
	var p link.Party
	err := tx.QueryRow(ctx, `SELECT issuer, subject FROM nemura.member_identity($1)`, member).Scan(&p.Issuer, &p.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return link.Party{}, false, nil
	}
	return p, err == nil, err
}

// RunEnvelopeOf makes the approval envelope for a version of a run, for an approver, from the run's own rows: gross pay by Fyniq's department (found by the department's name in the departments Fyniq last
// told Nemura of, never a department Nemura invented), the month's CPF, SDL and SHG, the deductions by kind and the reimbursements, the packs it was calculated under, Fyniq's current mapping version,
// and the preparer and approver as issuer and subject. It returns the envelope, its canonical bytes and SHA-256, and the reasons it cannot be made yet, in plain words; with reasons the figures
// are not final and nothing may be signed.
func RunEnvelopeOf(ctx context.Context, tx pgx.Tx, org, runID uuid.UUID, number int, approver uuid.UUID) (env link.RunEnvelope, canonical []byte, sha string, issues []string, err error) {
	r, versions, err := RunOf(ctx, tx, runID)
	if err != nil {
		return env, nil, "", nil, err
	}
	var v *VersionRow
	supersedes := 0
	for i := range versions {
		if versions[i].Number == number {
			v = &versions[i]
		} else if versions[i].Number < number && (versions[i].State == "approved" || versions[i].State == "superseded") && versions[i].Number > supersedes {
			supersedes = versions[i].Number
		}
	}
	if v == nil {
		return env, nil, "", nil, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such version of that pay run."}
	}
	env = link.RunEnvelope{Organisation: org.String(), SourceSystem: "nemura", RunID: runID.String(), Version: number, Supersedes: supersedes, WageMonth: r.Month.Format("2006-01"),
		EmployeeCPF: v.Totals.CPFEmployee, EmployerCPF: v.Totals.CPFEmployer, SDL: v.Totals.SDLPayable, SHG: v.Totals.SHG, Reimbursements: v.Totals.Reimbursements, ApproverRight: link.RightApprove}
	var statutory []string
	for _, p := range v.Packs {
		if strings.HasPrefix(p, "sg-people") {
			env.PolicyVersion = p
		} else {
			statutory = append(statutory, p)
		}
	}
	slices.Sort(statutory)
	env.PackVersion = strings.Join(statutory, ",")
	// Fyniq's side: the version of its account mapping the run is calculated against.
	if err := tx.QueryRow(ctx, `SELECT mapping_version FROM nemura.link_mapping ORDER BY mapping_version DESC LIMIT 1`).Scan(&env.MappingVersion); errors.Is(err, pgx.ErrNoRows) {
		issues = append(issues, "Fyniq has not told Nemura its account mapping yet: ask the accountant to set the mapping in Fyniq (Settings, People and HR), then open this run again.")
	} else if err != nil {
		return env, nil, "", nil, err
	}
	// The people, by Fyniq's department.
	lines, err := RunLines(ctx, tx, v.ID)
	if err != nil {
		return env, nil, "", nil, err
	}
	byDept := map[string]*apdSum{}
	unmatched := map[string]bool{}
	var noDept []string
	lastDay := monthEnd(r.Month)
	kinds := map[string]*apdSum{}
	sumGross := newSum()
	for _, l := range lines {
		g := dec(l.Gross)
		sumGross.add(g)
		for _, it := range l.Items {
			if it.Class == "deduction" {
				k := link.DeductionKind(it.Name)
				if kinds[k] == nil {
					kinds[k] = newSum()
				}
				kinds[k].add(dec(it.Amount))
			}
		}
		if g.IsZero() {
			continue
		}
		p, err := PersonOn(ctx, tx, l.Person, lastDay)
		if err != nil {
			return env, nil, "", nil, err
		}
		if strings.TrimSpace(p.Department) == "" {
			noDept = append(noDept, l.PersonName)
			continue
		}
		id, _, err := departmentOf(ctx, tx, p.Department)
		if err != nil {
			return env, nil, "", nil, err
		}
		if id == nil {
			unmatched[strings.TrimSpace(p.Department)] = true
			continue
		}
		if byDept[id.String()] == nil {
			byDept[id.String()] = newSum()
		}
		byDept[id.String()].add(g)
	}
	if len(noDept) > 0 {
		sort.Strings(noDept)
		issues = append(issues, fmt.Sprintf("These people have no department on their job: %s. Set it on their record, calculate the run again, then approve it.", strings.Join(noDept, ", ")))
	}
	if len(unmatched) > 0 {
		names := make([]string, 0, len(unmatched))
		for n := range unmatched {
			names = append(names, n)
		}
		sort.Strings(names)
		issues = append(issues, fmt.Sprintf("Fyniq has no department named %s. Departments come from Fyniq: add it there and map its salary account, or give the people a department Fyniq has.", strings.Join(names, ", ")))
	}
	for id, s := range byDept {
		env.Departments = append(env.Departments, link.RunDepartment{DepartmentID: id, Gross: cents2(s.v)})
	}
	for k, s := range kinds {
		if !s.v.IsZero() {
			env.Deductions = append(env.Deductions, link.RunDeduction{Kind: k, Amount: cents2(s.v)})
		}
	}
	if len(issues) == 0 {
		// The departments carry the whole of the gross pay, and the kinds the whole of the deductions: the entry Fyniq posts balances only if they do.
		if cents2(sumGross.v) != v.Totals.Gross {
			issues = append(issues, "The people's pay does not add up to the run's gross pay: calculate the run again.")
		}
		got := newSum()
		for _, d := range env.Departments {
			got.add(dec(d.Gross))
		}
		if cents2(got.v) != v.Totals.Gross {
			issues = append(issues, "The departments' gross pay does not add up to the run's gross pay: calculate the run again.")
		}
		ded := newSum()
		for _, d := range env.Deductions {
			ded.add(dec(d.Amount))
		}
		if cents2(ded.v) != v.Totals.Deductions {
			issues = append(issues, "The deductions by kind do not add up to the run's deductions: calculate the run again.")
		}
	}
	// The preparer and the approver, by the identity the shared sign-in knows them by.
	who := func(member uuid.UUID, role string) link.Party {
		p, ok, e := MemberIdentity(ctx, tx, member)
		if e != nil {
			err = e
		}
		if !ok {
			issues = append(issues, fmt.Sprintf("The %s has no sign-in identity in Nemura yet, so Fyniq cannot match them: they sign in once and try again.", role))
		}
		return p
	}
	env.Preparer, env.Approver = who(v.PreparedBy, "preparer"), who(approver, "approver")
	if err != nil {
		return env, nil, "", nil, err
	}
	if len(issues) > 0 {
		return env, nil, "", issues, nil
	}
	canonical, sha, err = env.Seal()
	return env, canonical, sha, nil, err
}

// runSubmissionAt builds the payload of payroll.run.approved from the stored proof, and nothing else: the bytes the passkey signed, their digest and the assertion (NM-INT-06).
func runSubmissionAt(ctx context.Context, tx pgx.Tx, run uuid.UUID, version int) (link.RunSubmission, bool, error) {
	var sub link.RunSubmission
	var env, ad, cd, sg []byte
	err := tx.QueryRow(ctx, `SELECT envelope, envelope_sha256, authenticator_data, client_data, signature FROM nemura.link_run_approval WHERE run_id = $1 AND version = $2`, run, version).
		Scan(&env, &sub.EnvelopeSHA256, &ad, &cd, &sg)
	if errors.Is(err, pgx.ErrNoRows) {
		return sub, false, nil
	}
	if err != nil {
		return sub, false, err
	}
	sub.Envelope = env
	sub.Approval = link.RunApproval{AuthenticatorData: b64url(ad), ClientData: b64url(cd), Signature: b64url(sg)}
	return sub, true, nil
}

// RunLinkService is the pay run's door to the link: the envelope to read and sign, the approval with its proof, and what became of the submission.
type RunLinkService struct {
	Store  *Store
	Clock  ports.Clock
	Origin string // Nemura's origin as the browser sees it, where the approver's passkey answers
	RPID   string // the relying party the apps share, so one authenticator works in both
}

var _ api.RunLink = RunLinkService{}

func (c RunLinkService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return c.Store.InOrg(ctx, s.Organisation, fn)
}

func envelopeView(e link.RunEnvelope, sha string) api.RunEnvelopeView {
	v := api.RunEnvelopeView{Organisation: e.Organisation, RunID: e.RunID, Version: e.Version, Supersedes: e.Supersedes, WageMonth: e.WageMonth, EmployeeCPF: e.EmployeeCPF, EmployerCPF: e.EmployerCPF,
		SDL: e.SDL, SHG: e.SHG, Reimbursements: e.Reimbursements, PackVersion: e.PackVersion, PolicyVersion: e.PolicyVersion, MappingVersion: e.MappingVersion, ApproverRight: e.ApproverRight,
		Departments: []api.RunEnvelopeDepartment{}, Deductions: []api.RunEnvelopeDeduction{}}
	for _, d := range e.Departments {
		v.Departments = append(v.Departments, api.RunEnvelopeDepartment{DepartmentID: d.DepartmentID, Gross: d.Gross})
	}
	for _, d := range e.Deductions {
		v.Deductions = append(v.Deductions, api.RunEnvelopeDeduction{Kind: d.Kind, Amount: d.Amount})
	}
	return v
}

// Envelope is what the approver reads and signs: the figures that will be posted, by department, with the digest their passkey signs. While the apps are not linked it says so and the plain approval applies.
func (c RunLinkService) Envelope(ctx context.Context, s api.Session, run uuid.UUID, version int) (api.RunApprovalEnvelope, error) {
	out := api.RunApprovalEnvelope{Issues: []string{}, RPID: c.RPID, Origin: c.Origin}
	err := c.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		state, err := runLinkState(ctx, tx)
		if err != nil {
			return err
		}
		out.Linked = state != ""
		if state == "" {
			return nil
		}
		env, _, sha, issues, err := RunEnvelopeOf(ctx, tx, s.Organisation, run, version, s.Member)
		if err != nil {
			return err
		}
		out.Envelope, out.Issues = envelopeView(env, sha), append(out.Issues, issues...)
		if state != "active" {
			out.Issues = append(out.Issues, fmt.Sprintf("The link to Fyniq is %s: a run is approved once it is active again.", state))
		}
		if len(out.Issues) == 0 {
			ch, err := link.Challenge(sha)
			if err != nil {
				return err
			}
			out.EnvelopeSHA256, out.Challenge, out.Ready = sha, b64url(ch), true
		}
		return nil
	})
	return out, err
}

// Approve approves the version with proof: the approver read the envelope, confirmed the session with their passkey, and signed the envelope's digest with the passkey they enrolled in Fyniq. Nemura checks what
// it can of that answer (the challenge is the digest, the origin is its own, the site is the shared one, the person was present and verified) and keeps it with the envelope; the approval, the proof and the
// queued submission are one transaction, and Fyniq verifies the signature itself before it posts anything (NM-INT-06, p1-payroll-link design D3).
func (c RunLinkService) Approve(ctx context.Context, s api.Session, run uuid.UUID, in api.LinkedApprovalInput) error {
	now := c.Clock.Now()
	raw, err := hex.DecodeString(in.Digest)
	if err != nil || len(raw) != 32 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The digest is 64 hexadecimal characters: copy it from the run as it was calculated."}
	}
	var confirmed [32]byte
	copy(confirmed[:], raw)
	stepUp := now
	if s.StepUpAt != nil {
		stepUp = *s.StepUpAt
	}
	return c.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		state, err := runLinkState(ctx, tx)
		if err != nil {
			return err
		}
		if state == "" {
			return &payroll.Refusal{Code: CodeLinkNone, Message: "Nemura is not linked to Fyniq: approve the run the usual way."}
		}
		if state != "active" {
			return &payroll.Refusal{Code: CodeLinkState, Message: "The link to Fyniq is " + state + ": a run is approved once it is active again."}
		}
		env, canonical, sha, issues, err := RunEnvelopeOf(ctx, tx, s.Organisation, run, in.Version, s.Member)
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			return &payroll.Refusal{Code: payroll.CodeInvalid, Message: issues[0]}
		}
		if sha != in.EnvelopeSHA256 {
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "The run, or Fyniq's mapping, changed since you read the envelope: open the run again and sign the one shown."}
		}
		challenge, err := link.Challenge(sha)
		if err != nil {
			return err
		}
		if err := link.CheckAssertion(link.RunApproval(in.Approval), challenge, c.Origin, c.RPID); err != nil {
			return &payroll.Refusal{Code: payroll.CodeInvalid, Message: proofMessage(err)}
		}
		if err := ApproveRun(ctx, tx, s.Organisation, now, s.Member, run, in.Version, confirmed, stepUp); err != nil {
			return err
		}
		b64d := func(x string) []byte { b, _ := base64.RawURLEncoding.DecodeString(x); return b }
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO nemura.link_run_approval (run_id, version, envelope, envelope_sha256, mapping_version, approver_member_id, approver_issuer, approver_subject, preparer_issuer, preparer_subject,
authenticator_data, client_data, signature, approved_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`, run, in.Version, canonical, sha, env.MappingVersion, s.Member,
			env.Approver.Issuer, env.Approver.Subject, env.Preparer.Issuer, env.Preparer.Subject, b64d(in.Approval.AuthenticatorData), b64d(in.Approval.ClientData), b64d(in.Approval.Signature), now).Scan(&id); err != nil {
			return err
		}
		// What was signed, never the figures: the run, the version and the digest.
		if err := Audit(ctx, tx, s.Organisation, now, user(s.Member), "nemura.link_run.approval_kept", audit.Object{Type: "link_run_approval", ID: id},
			map[string]audit.Change{"run": {nil, run.String()}, "version": {nil, in.Version}, "envelope_sha256": {nil, sha}, "mapping_version": {nil, env.MappingVersion}}, ""); err != nil {
			return err
		}
		_, err = QueueLinkEvent(ctx, tx, s.Organisation, now, link.TypePayrollRunApproved, "payroll_run", run.String(), in.Version)
		return err
	})
}

func proofMessage(err error) string {
	switch {
	case errors.Is(err, link.ErrAssertionChallenge):
		return "Your passkey signed something other than this run's approval envelope: open the run again and sign the digest shown."
	case errors.Is(err, link.ErrAssertionOrigin):
		return "Your passkey answered at another address than Nemura's: approve the run from Nemura itself."
	case errors.Is(err, link.ErrAssertionSite):
		return "Your passkey is for another site, or it did not confirm that you were present: use the passkey you enrolled in Fyniq, and confirm with your fingerprint, face or PIN."
	}
	return "Your passkey's answer is not a passkey assertion: try again."
}

// Submission says what became of the run's newest approved version: whether it was sent and Fyniq's answer, a posting reference or a refusal's code and words, which the run shows and waits on.
func (c RunLinkService) Submission(ctx context.Context, s api.Session, run uuid.UUID) (api.RunSubmissionView, error) {
	out := api.RunSubmissionView{State: "none"}
	err := c.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		state, err := runLinkState(ctx, tx)
		if err != nil {
			return err
		}
		out.Linked = state != ""
		var version int
		err = tx.QueryRow(ctx, `SELECT version FROM nemura.link_run_approval WHERE run_id = $1 ORDER BY version DESC LIMIT 1`, run).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.Version = version
		var code *string
		var sentAt *time.Time
		err = tx.QueryRow(ctx, `SELECT state, code, settled_at FROM nemura.link_event WHERE type = $1 AND object_type = 'payroll_run' AND object_id = $2 AND object_version = $3 ORDER BY sequence DESC LIMIT 1`,
			link.TypePayrollRunApproved, run.String(), version).Scan(&out.State, &code, &sentAt)
		if errors.Is(err, pgx.ErrNoRows) {
			out.State = "none"
			err = nil
		}
		if err != nil {
			return err
		}
		if code != nil {
			out.Code = *code
		}
		if sentAt != nil {
			t := sentAt.UTC().Format(time.RFC3339)
			out.SentAt = &t
		}
		out.Retryable = out.State == "refused" && retryableRefusal(out.Code)
		var status string
		var ref, pcode, msg *string
		var at time.Time
		err = tx.QueryRow(ctx, `SELECT status, posting_ref, code, message, received_at FROM nemura.link_run_posting WHERE run_id = $1 AND version = $2 ORDER BY received_at DESC LIMIT 1`, run, version).Scan(&status, &ref, &pcode, &msg, &at)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.Retryable = out.Retryable || (status == "refused" && retryableRefusal(derefOr(pcode)))
		out.Posting = &api.RunPosting{Status: status, ReceivedAt: at.UTC().Format(time.RFC3339)}
		if ref != nil {
			out.Posting.Reference = *ref
		}
		if pcode != nil {
			out.Posting.Code = *pcode
		}
		if msg != nil {
			out.Posting.Message = *msg
		}
		return nil
	})
	return out, err
}

// retryableRefusal says whether sending the same approval again can change Fyniq's answer: only when what is wrong is on Fyniq's side to put right (the approver has no passkey enrolled there, a period is
// closed, an account is missing). A refusal of the figures or the proof themselves needs a new calculation and a new approval.
func retryableRefusal(code string) bool {
	if code == "" {
		return false
	}
	if strings.HasPrefix(code, "PPL-") {
		return code == "PPL-19"
	}
	return true
}

// Retry sends the run's approved version to Fyniq again, as a new event with the same stored proof, after Fyniq's refusal that can change: the approver enrols a passkey in Fyniq, the accountant
// reopens a period. The proof is never remade: it is what was approved.
func (c RunLinkService) Retry(ctx context.Context, s api.Session, run uuid.UUID, version int) error {
	now := c.Clock.Now()
	return c.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		state, err := runLinkState(ctx, tx)
		if err != nil {
			return err
		}
		if state != "active" {
			return &payroll.Refusal{Code: CodeLinkNone, Message: "The link to Fyniq is not active: the run is sent once it is."}
		}
		var newest int
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM nemura.link_run_approval WHERE run_id = $1`, run).Scan(&newest); err != nil {
			return err
		}
		if newest == 0 || version != newest {
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "Only the newest approved version of a run is sent again."}
		}
		var status string
		var pcode *string
		err = tx.QueryRow(ctx, `SELECT status, code FROM nemura.link_run_posting WHERE run_id = $1 AND version = $2 ORDER BY received_at DESC LIMIT 1`, run, version).Scan(&status, &pcode)
		var evState string
		var evCode *string
		e2 := tx.QueryRow(ctx, `SELECT state, code FROM nemura.link_event WHERE type = $1 AND object_type = 'payroll_run' AND object_id = $2 AND object_version = $3 ORDER BY sequence DESC LIMIT 1`,
			link.TypePayrollRunApproved, run.String(), version).Scan(&evState, &evCode)
		if e2 != nil && !errors.Is(e2, pgx.ErrNoRows) {
			return e2
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		refusedByAnswer := err == nil && status == "refused" && pcode != nil && retryableRefusal(*pcode)
		refusedOnTheWay := e2 == nil && evState == "refused" && evCode != nil && retryableRefusal(*evCode)
		if !refusedByAnswer && !refusedOnTheWay {
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "There is nothing Fyniq refused that sending the run again could change: if the figures or the proof were refused, calculate the run again and approve the new version."}
		}
		_, err = QueueLinkEvent(ctx, tx, s.Organisation, now, link.TypePayrollRunApproved, "payroll_run", run.String(), version)
		return err
	})
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// apdSum is a running exact sum of money, never a float (ENG-10).
type apdSum struct{ v *apd.Decimal }

func newSum() *apdSum                { return &apdSum{v: dec("0")} }
func (s *apdSum) add(x *apd.Decimal) { s.v = addDec(s.v, x) }

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
