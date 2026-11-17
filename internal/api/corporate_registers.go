package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the nominee registers and the statements approval
//
// The registers of nominee directors and nominee shareholders (s386AL), and the directors' approval of each financial
// year's statements (FR-CS-03, C.2 #33): drafted by the Accountant, recorded on another person's approval with their
// evidence (p1-company-secretary design D2, D4), as the profile's details and officers are.

// RegisterCodes are the codes the registers' records answer besides the common ones.
var RegisterCodes = []string{corporate.CodeNominee, corporate.CodeApproval, equity.CodeRecorded, storage.CodeNomineeEntry,
	storage.CodeStatementsApproval}

// NomineeEntryDraft is an entry of the register of nominee directors or shareholders, as drafted (s386AL).
type NomineeEntryDraft struct {
	Kind       string `json:"kind" enum:"director,shareholder" doc:"The register: nominee directors or nominee shareholders." example:"shareholder"`
	OfficerID  string `json:"officer_id,omitempty" format:"uuid" doc:"A nominee director: the director among the officers, by their first version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e01"`
	HolderID   string `json:"holder_id,omitempty" format:"uuid" doc:"A nominee shareholder: the holder, a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Nominator  string `json:"nominator" maxLength:"200" doc:"Whom they act or hold shares for." example:"Lion Rock Ventures Pte. Ltd."`
	Since      string `json:"since" format:"date" doc:"The day they became a nominee." example:"2026-06-18"`
	InformedOn string `json:"informed_on,omitempty" format:"date" doc:"The day they informed the company: within 30 days of becoming one after incorporation." example:"2026-06-25"`
	RegisterOn string `json:"register_on,omitempty" format:"date" doc:"The day the company's register entered it: within 7 days." example:"2026-06-30"`
	CentralOn  string `json:"central_on,omitempty" format:"date" doc:"The day ACRA's central register was told: within 2 business days after the company's register." example:"2026-07-02"`
	CeasedOn   string `json:"ceased_on,omitempty" format:"date" doc:"The day they ceased to be a nominee." example:"2027-06-30"`
	Note       string `json:"note,omitempty" maxLength:"1000" doc:"A note for the record." example:"Holds for the fund under the side letter"`
}

// NomineeEntryNew is a new nominee entry: a new entry, or a later version of one.
type NomineeEntryNew struct {
	EntryID string `json:"entry_id,omitempty" format:"uuid" doc:"The entry it follows, by its first version's ID, with a version recorded: to add a day, record a cessation or correct it. Empty for a new entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7f01"`
	NomineeEntryDraft
}

// NomineeEntryView is a nominee entry's version, drafted or recorded.
type NomineeEntryView struct {
	ID         string            `json:"id" format:"uuid" doc:"The version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7f02"`
	EntryID    string            `json:"entry_id" format:"uuid" doc:"The entry, by its first version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7f01"`
	Name       string            `json:"name" doc:"The nominee's name: the director's or the holder's." example:"Ah Kow Holdings Pte. Ltd."`
	Status     string            `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and approval; recorded: the entry's version in force; superseded: a later version is in force." example:"recorded"`
	Version    int               `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Entry      NomineeEntryDraft `json:"entry" doc:"The entry."`
	Evidence   int               `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one." example:"1"`
	ApprovalID string            `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7f11"`
	RecordedBy *Person           `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time        `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-07-03T09:30:00Z"`
	Checks     []BillCheck       `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// StatementsApprovalDraft is the directors' approval of a financial year's statements, as drafted (FR-CS-03).
type StatementsApprovalDraft struct {
	FYEnd      string `json:"fy_end" format:"date" doc:"The last day of the financial year the statements are for." example:"2026-12-31"`
	ApprovedOn string `json:"approved_on" format:"date" doc:"The day the directors approved them, by resolution or by signing their statement." example:"2027-04-15"`
	SentOn     string `json:"sent_on,omitempty" format:"date" doc:"The day they were sent to the members: within 5 months of the year's end for a private company that dispenses with the AGM (s175A)." example:"2027-04-20"`
	Note       string `json:"note,omitempty" maxLength:"1000" doc:"A note for the record." example:"Approved by written resolution of the directors"`
}

// StatementsApprovalView is a statements approval, drafted or recorded.
type StatementsApprovalView struct {
	ID         string                  `json:"id" format:"uuid" doc:"The approval's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	Status     string                  `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and approval; recorded: in force for its year; superseded: a later one for the same year is in force." example:"recorded"`
	Version    int                     `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Approval   StatementsApprovalDraft `json:"approval" doc:"The approval."`
	Evidence   int                     `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one." example:"1"`
	ApprovalID string                  `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a11"`
	RecordedBy *Person                 `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time              `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2027-04-16T09:30:00Z"`
	Checks     []BillCheck             `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

type (
	NomineeEntryPath struct {
		NomineeVersionID string `path:"nominee_id" format:"uuid" doc:"The nominee entry's version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7f02"`
	}
	StatementsApprovalPath struct {
		StatementsApprovalID string `path:"statements_approval_id" format:"uuid" doc:"The statements approval." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	}
	NomineeEntryEvidencePath struct {
		NomineeEntryPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	StatementsApprovalEvidencePath struct {
		StatementsApprovalPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	nomineeDraftInput struct {
		KeyHeader
		PreferHeader
		Body NomineeEntryNew
	}
	nomineeReviseInput struct {
		NomineeEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body NomineeEntryDraft
	}
	nomineeAskInput struct {
		NomineeEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	nomineeReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body NomineeEntryView
	}
	nomineeWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     NomineeEntryView
	}
	statementsDraftInput struct {
		KeyHeader
		PreferHeader
		Body StatementsApprovalDraft
	}
	statementsReviseInput struct {
		StatementsApprovalPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body StatementsApprovalDraft
	}
	statementsAskInput struct {
		StatementsApprovalPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	statementsReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body StatementsApprovalView
	}
	statementsWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     StatementsApprovalView
	}
	addNomineeEvidenceInput struct {
		NomineeEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listNomineeEvidenceInput struct {
		NomineeEntryPath
		PageQuery
	}
	removeNomineeEvidenceInput struct {
		NomineeEntryEvidencePath
		IfMatchHeader
		PreferHeader
	}
	addStatementsEvidenceInput struct {
		StatementsApprovalPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listStatementsEvidenceInput struct {
		StatementsApprovalPath
		PageQuery
	}
	removeStatementsEvidenceInput struct {
		StatementsApprovalEvidencePath
		IfMatchHeader
		PreferHeader
	}
)

// nomineeIn reads a nominee entry's fields, or what of them is wrong.
func nomineeIn(follows string, d NomineeEntryDraft) (corporate.NomineeEntry, error) {
	out := corporate.NomineeEntry{Kind: d.Kind, Nominator: strings.TrimSpace(d.Nominator), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	id := func(field, value, fix string) *uuid.UUID {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		x, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not an ID", Fix: fix})
			return nil
		}
		return &x
	}
	if root := id("body.entry_id", follows, "Send the entry's first version's ID, or leave it empty for a new entry"); root != nil {
		out.ID = *root
	}
	out.Officer = id("body.officer_id", d.OfficerID, "Send the director's first version's ID")
	out.Holder = id("body.holder_id", d.HolderID, "Send the holder's contact ID")
	if since := dayIn("body.since", d.Since, &fields); since != nil {
		out.Since = *since
	}
	out.Informed = dayIn("body.informed_on", d.InformedOn, &fields)
	out.Register = dayIn("body.register_on", d.RegisterOn, &fields)
	out.Central = dayIn("body.central_on", d.CentralOn, &fields)
	out.Ceased = dayIn("body.ceased_on", d.CeasedOn, &fields)
	if len(fields) > 0 {
		return out, invalid(fields)
	}
	return out, nil
}

func nomineeDraftOf(n corporate.NomineeEntry) NomineeEntryDraft {
	out := NomineeEntryDraft{Kind: n.Kind, Nominator: n.Nominator, Since: n.Since.Format(time.DateOnly), InformedOn: dayOut(n.Informed),
		RegisterOn: dayOut(n.Register), CentralOn: dayOut(n.Central), CeasedOn: dayOut(n.Ceased), Note: n.Note}
	if n.Officer != nil {
		out.OfficerID = n.Officer.String()
	}
	if n.Holder != nil {
		out.HolderID = n.Holder.String()
	}
	return out
}

// statementsIn reads a statements approval's fields, or what of them is wrong.
func statementsIn(d StatementsApprovalDraft) (corporate.StatementsApproval, error) {
	out := corporate.StatementsApproval{Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	if end := dayIn("body.fy_end", d.FYEnd, &fields); end != nil {
		out.FYEnd = *end
	}
	if approved := dayIn("body.approved_on", d.ApprovedOn, &fields); approved != nil {
		out.Approved = *approved
	}
	out.Sent = dayIn("body.sent_on", d.SentOn, &fields)
	if len(fields) > 0 {
		return out, invalid(fields)
	}
	return out, nil
}

func statementsDraftOf(a corporate.StatementsApproval) StatementsApprovalDraft {
	return StatementsApprovalDraft{FYEnd: a.FYEnd.Format(time.DateOnly), ApprovedOn: a.Approved.Format(time.DateOnly), SentOn: dayOut(a.Sent), Note: a.Note}
}

// nomineeName is the nominee's name: the director's in force, or the holder's.
func nomineeName(ctx context.Context, u storage.UnitOfWork, v corporateView, n corporate.NomineeEntry) (string, error) {
	switch {
	case n.Officer != nil:
		if cur := equity.InForce(v.officers[*n.Officer]); cur != nil {
			return cur.Value.Name, nil
		}
		return "", nil
	case n.Holder != nil:
		c, err := u.Contacts().Get(ctx, *n.Holder)
		if err != nil {
			return "", err
		}
		return c.LegalName, nil
	}
	return "", nil
}

func (a *API) nomineeViewOf(ctx context.Context, u storage.UnitOfWork, v corporateView, x equity.Recorded[corporate.NomineeEntry]) (NomineeEntryView, error) {
	root := corporate.NomineeRoot(x.Doc, x.Value)
	out := NomineeEntryView{ID: x.Doc.ID.String(), EntryID: root.String(), Status: recordStatus(x.Doc, inForceID(v.nominees[root])),
		Version: x.Doc.Version, Entry: nomineeDraftOf(x.Value)}
	var err error
	if out.Name, err = nomineeName(ctx, u, v, x.Value); err != nil {
		return out, err
	}
	if masked(ctx) {
		out.Entry.Note = ""
	}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the nominee's notice to the company, the register's extract or ACRA's acknowledgement"
		}
	}
	return out, err
}

func (a *API) statementsViewOf(ctx context.Context, u storage.UnitOfWork, v corporateView, x equity.Recorded[corporate.StatementsApproval]) (StatementsApprovalView, error) {
	var inForce *uuid.UUID
	if cur := corporate.ApprovalFor(v.approvals, x.Value.FYEnd); cur != nil {
		inForce = &cur.Doc.ID
	}
	out := StatementsApprovalView{ID: x.Doc.ID.String(), Status: recordStatus(x.Doc, inForce), Version: x.Doc.Version, Approval: statementsDraftOf(x.Value)}
	var err error
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the directors' resolution or statement approving the financial statements"
		}
	}
	return out, err
}

// shownNominee is a nominee entry's version by document ID: not found for anything else.
func (a *API) shownNominee(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (NomineeEntryView, error) {
	v, err := a.corporateViewOf(ctx, u)
	if err != nil {
		return NomineeEntryView{}, err
	}
	for _, versions := range v.nominees {
		for _, x := range versions {
			if x.Doc.ID == id {
				return a.nomineeViewOf(ctx, u, v, x)
			}
		}
	}
	return NomineeEntryView{}, ports.ErrNotFound
}

// shownStatements is a statements approval by document ID: not found for anything else.
func (a *API) shownStatements(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (StatementsApprovalView, error) {
	v, err := a.corporateViewOf(ctx, u)
	if err != nil {
		return StatementsApprovalView{}, err
	}
	for _, x := range v.approvals {
		if x.Doc.ID == id {
			return a.statementsViewOf(ctx, u, v, x)
		}
	}
	return StatementsApprovalView{}, ports.ErrNotFound
}

// registersOn adds the nominee entries in force on day, every version of each, and every statements approval, the
// latest first, to the profile.
func (a *API) registersOn(ctx context.Context, u storage.UnitOfWork, v corporateView, day time.Time, out *CorporateProfile) error {
	out.Nominees, out.NomineeHistory, out.StatementsApprovals = []NomineeEntryView{}, []NomineeEntryView{}, []StatementsApprovalView{}
	for _, x := range corporate.NomineesOn(v.nominees, day) {
		n, err := a.nomineeViewOf(ctx, u, v, x)
		if err != nil {
			return err
		}
		out.Nominees = append(out.Nominees, n)
	}
	var history []equity.Recorded[corporate.NomineeEntry]
	for _, versions := range v.nominees {
		history = append(history, versions...)
	}
	slices.SortStableFunc(history, latestFirst[corporate.NomineeEntry])
	for _, x := range history {
		n, err := a.nomineeViewOf(ctx, u, v, x)
		if err != nil {
			return err
		}
		out.NomineeHistory = append(out.NomineeHistory, n)
	}
	for i := len(v.approvals) - 1; i >= 0; i-- {
		s, err := a.statementsViewOf(ctx, u, v, v.approvals[i])
		if err != nil {
			return err
		}
		out.StatementsApprovals = append(out.StatementsApprovals, s)
	}
	return nil
}

// latestFirst orders drafts first, then the recorded by when they were recorded, the latest first.
func latestFirst[T any](x, y equity.Recorded[T]) int {
	dx, dy := x.Doc.Status == storage.DocumentDraft, y.Doc.Status == storage.DocumentDraft
	switch {
	case dx != dy:
		if dx {
			return -1
		}
		return 1
	case !x.RecordedAt.Equal(y.RecordedAt):
		return y.RecordedAt.Compare(x.RecordedAt)
	}
	return strings.Compare(y.Doc.ID.String(), x.Doc.ID.String())
}

// registerRecordDependencies are a nominee entry's or a statements approval's envelope values: its status, and the
// entry it follows or its year's end.
func registerRecordDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	switch doc.Type {
	case storage.DocumentNomineeEntry:
		n, err := corporate.ReadNominee(doc)
		if err != nil {
			return 0, nil, err
		}
		return doc.Version, map[string]string{"status": string(doc.Status), "entry": corporate.NomineeRoot(doc, n).String()}, nil
	case storage.DocumentStatementsApproval:
		s, err := corporate.ReadStatementsApproval(doc)
		if err != nil {
			return 0, nil, err
		}
		return doc.Version, map[string]string{"status": string(doc.Status), "fy_end": s.FYEnd.Format(time.DateOnly)}, nil
	}
	return 0, nil, fmt.Errorf("document %s is a %s, not a nominee entry or statements approval", doc.ID, doc.Type)
}

// nomineeExecutable refuses the nominee themselves (FM-1): the director's member, or the holder's.
func nomineeExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	n, err := corporate.ReadNominee(doc)
	if err != nil {
		return err
	}
	own := false
	switch {
	case n.Officer != nil:
		officers, err := corporate.Officers(ctx, u)
		if err != nil {
			return err
		}
		cur := equity.InForce(officers[*n.Officer])
		own = cur != nil && cur.Value.Member != nil && *cur.Value.Member == signer
	case n.Holder != nil:
		c, err := u.Contacts().Get(ctx, *n.Holder)
		if err != nil {
			return err
		}
		own = c.MemberUserID != nil && *c.MemberUserID == signer
	}
	if own {
		return problem(approval.CodeNotApprover, "This is your own entry as a nominee, so another person approves it (FM-1).",
			"Ask another person who holds the approval right")
	}
	return nil
}

func nomineeAnswer(w written[NomineeEntryView]) *nomineeWriteOutput {
	if w.Previewed {
		return &nomineeWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &nomineeWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/corporate-profile/nominees/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func statementsAnswer(w written[StatementsApprovalView]) *statementsWriteOutput {
	if w.Previewed {
		return &statementsWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &statementsWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/corporate-profile/statements-approvals/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerCorporateRegisters(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	ask := append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...),
		RegisterCodes...)
	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(locks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, locks...)
	addNote := " The file's type is decided from its content; it is scanned for malware and its image metadata is stripped before it " +
		"is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event; once recorded its " +
		"files are committed to the retention-locked store (FR-DOC-13). Needs If-Match with the record's ETag. Idempotent with " +
		"Idempotency-Key; Prefer: return=preview saves nothing."
	removeNote := " Once recorded its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit " +
		"trail. Side effects: the removal and its audit event. Needs If-Match with the file's ETag; Prefer: return=preview saves nothing."
	tags := []string{"Corporate profile"}

	register(a, authz.CreateNomineeEntry, huma.Operation{
		OperationID: "createNomineeEntry", Method: http.MethodPost, Path: "/v1/corporate-profile/nominees", Tags: tags,
		DefaultStatus: http.StatusCreated, Summary: "Draft a nominee entry, or a later version of one",
		Description: "Drafts an entry of the register of nominee directors or nominee shareholders (s386AL; FR-CS-01): a director among " +
			"the officers or a holder of the equity register, whom they act or hold for, since when, and the days they informed the company " +
			"(30 days), the company's register entered it (7 days) and ACRA's central register was told (2 business days after). With " +
			"entry_id it is a later version of an entry with a version recorded. Attach the evidence with addNomineeEntryEvidence, then ask " +
			"another person to approve it with requestNomineeEntryApproval; the last recorded version is in force. Side effects: the draft, " +
			"audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), RegisterCodes...), func(ctx context.Context, in *nomineeDraftInput) (*nomineeWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (NomineeEntryView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return NomineeEntryView{}, err
			}
			n, err := nomineeIn(in.Body.EntryID, in.Body.NomineeEntryDraft)
			if err != nil {
				return NomineeEntryView{}, err
			}
			doc, err := corporate.DraftNomineeIn(ctx, u, n, today)
			if err != nil {
				return NomineeEntryView{}, equityProblems(err)
			}
			return a.shownNominee(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return nomineeAnswer(w), nil
	})

	register(a, authz.GetNomineeEntry, huma.Operation{
		OperationID: "getNomineeEntry", Method: http.MethodGet, Path: "/v1/corporate-profile/nominees/{nominee_id}", Tags: tags,
		Summary:     "Get a nominee entry's version",
		Description: "Returns a nominee entry's version with its ETag: the entry, its days, its status, files and the approval waiting (s386AL, FR-CS-01). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *NomineeEntryPath) (*nomineeReadOutput, error) {
		id, err := parseID("nominee_id", in.NomineeVersionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out nomineeReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentNomineeEntry, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownNominee(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceNomineeEntry, huma.Operation{
		OperationID: "replaceNomineeEntry", Method: http.MethodPut, Path: "/v1/corporate-profile/nominees/{nominee_id}", Tags: tags,
		Summary: "Change a drafted nominee entry",
		Description: "Saves a new version of a nominee entry's draft not recorded yet (s386AL); the entry it follows stays. A recorded one " +
			"changes no more (EQUITY-05): draft a later version with entry_id. Side effects: the new version, audit events. Needs If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), RegisterCodes...), func(ctx context.Context, in *nomineeReviseInput) (*nomineeWriteOutput, error) {
		id, err := parseID("nominee_id", in.NomineeVersionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (NomineeEntryView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentNomineeEntry, id)
			if err != nil {
				return NomineeEntryView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return NomineeEntryView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return NomineeEntryView{}, err
			}
			n, err := nomineeIn("", in.Body)
			if err != nil {
				return NomineeEntryView{}, err
			}
			if _, err := corporate.ReviseNomineeIn(ctx, u, id, n, today); err != nil {
				return NomineeEntryView{}, equityProblems(err)
			}
			return a.shownNominee(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return nomineeAnswer(w), nil
	})

	register(a, authz.RequestNomineeEntryApproval, huma.Operation{
		OperationID: "requestNomineeEntryApproval", Method: http.MethodPost, Path: "/v1/corporate-profile/nominees/{nominee_id}/approval", Tags: tags,
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a nominee entry",
		Description: "Asks another person to approve a drafted nominee entry at the version read (s386AL, FM-1). It needs its evidence " +
			"attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A on \"Corporate profile and " +
			"registers\" approves, stepped up, never the nominee nor the one who asked. Needs a person signed in who passed step-up, and " +
			"If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *nomineeAskInput) (*equityAskOutput, error) {
		id, err := parseID("nominee_id", in.NomineeVersionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentNomineeEntry, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the entry has no evidence attached",
					Fix: "Attach the nominee's notice to the company, the register's extract or ACRA's acknowledgement, then ask again"}
			}
			n, err := corporate.ReadNominee(doc)
			if err != nil {
				return Approval{}, err
			}
			v, err := a.corporateViewOf(ctx, u)
			if err != nil {
				return Approval{}, err
			}
			name, err := nomineeName(ctx, u, v, n)
			if err != nil {
				return Approval{}, err
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "kind", After: n.Kind}, {Field: "nominator", After: n.Nominator},
				{Field: "since", After: n.Since.Format(time.DateOnly)}, {Field: "register_on", After: dayOut(n.Register)},
				{Field: "central_on", After: dayOut(n.Central)}, {Field: "ceased_on", After: dayOut(n.Ceased)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectNomineeEntry, ObjectKey: doc.ID.String(),
				Operation: approval.OpNomineeEntryRecord, Summary: "Record " + name + " as a nominee " + n.Kind + " for " + n.Nominator,
				Execute: map[string]any{"nominee_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.CreateStatementsApproval, huma.Operation{
		OperationID: "createStatementsApproval", Method: http.MethodPost, Path: "/v1/corporate-profile/statements-approvals", Tags: tags,
		DefaultStatus: http.StatusCreated, Summary: "Draft the directors' approval of a year's financial statements",
		Description: "Drafts the directors' approval of a financial year's statements (FR-CS-03, C.2 #33): the year's end, the day they " +
			"approved them, and the day they were sent to the members. Attach the directors' resolution or statement with " +
			"addStatementsApprovalEvidence, then ask another person to approve it with requestStatementsApprovalApproval; the last recorded " +
			"for a year is in force. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(writes, RegisterCodes...), func(ctx context.Context, in *statementsDraftInput) (*statementsWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (StatementsApprovalView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return StatementsApprovalView{}, err
			}
			s, err := statementsIn(in.Body)
			if err != nil {
				return StatementsApprovalView{}, err
			}
			doc, err := corporate.DraftStatementsApprovalIn(ctx, u, s, today)
			if err != nil {
				return StatementsApprovalView{}, equityProblems(err)
			}
			return a.shownStatements(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return statementsAnswer(w), nil
	})

	register(a, authz.GetStatementsApproval, huma.Operation{
		OperationID: "getStatementsApproval", Method: http.MethodGet, Path: "/v1/corporate-profile/statements-approvals/{statements_approval_id}", Tags: tags,
		Summary:     "Get a statements approval",
		Description: "Returns a statements approval with its ETag: the year's end, the days approved and sent, its status, files and the approval waiting (FR-CS-03). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *StatementsApprovalPath) (*statementsReadOutput, error) {
		id, err := parseID("statements_approval_id", in.StatementsApprovalID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out statementsReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentStatementsApproval, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownStatements(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceStatementsApproval, huma.Operation{
		OperationID: "replaceStatementsApproval", Method: http.MethodPut, Path: "/v1/corporate-profile/statements-approvals/{statements_approval_id}", Tags: tags,
		Summary: "Change a drafted statements approval",
		Description: "Saves a new version of a statements approval not recorded yet (FR-CS-03). A recorded one changes no more (EQUITY-05): " +
			"draft a new one for the same year to correct it or add the day sent. Side effects: the new version, audit events. Needs If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), RegisterCodes...), func(ctx context.Context, in *statementsReviseInput) (*statementsWriteOutput, error) {
		id, err := parseID("statements_approval_id", in.StatementsApprovalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (StatementsApprovalView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentStatementsApproval, id)
			if err != nil {
				return StatementsApprovalView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return StatementsApprovalView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return StatementsApprovalView{}, err
			}
			s, err := statementsIn(in.Body)
			if err != nil {
				return StatementsApprovalView{}, err
			}
			if _, err := corporate.ReviseStatementsApprovalIn(ctx, u, id, s, today); err != nil {
				return StatementsApprovalView{}, equityProblems(err)
			}
			return a.shownStatements(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return statementsAnswer(w), nil
	})

	register(a, authz.RequestStatementsApprovalApproval, huma.Operation{
		OperationID: "requestStatementsApprovalApproval", Method: http.MethodPost,
		Path: "/v1/corporate-profile/statements-approvals/{statements_approval_id}/approval", Tags: tags,
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a statements approval",
		Description: "Asks another person to approve a drafted statements approval at the version read (FR-CS-03, FM-1). It needs the " +
			"directors' resolution or statement attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A " +
			"on \"Corporate profile and registers\" approves, stepped up, never the one who asked. Needs a person signed in who passed " +
			"step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *statementsAskInput) (*equityAskOutput, error) {
		id, err := parseID("statements_approval_id", in.StatementsApprovalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentStatementsApproval, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the approval has no evidence attached",
					Fix: "Attach the directors' resolution or statement approving the financial statements, then ask again"}
			}
			s, err := corporate.ReadStatementsApproval(doc)
			if err != nil {
				return Approval{}, err
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "fy_end", After: s.FYEnd.Format(time.DateOnly)},
				{Field: "approved_on", After: s.Approved.Format(time.DateOnly)}, {Field: "sent_on", After: dayOut(s.Sent)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectStatementsApproval, ObjectKey: doc.ID.String(),
				Operation: approval.OpStatementsApprovalRecord,
				Summary:   "Record the directors' approval of the financial statements for the year ending " + s.FYEnd.Format("2 Jan 2006"),
				Execute:   map[string]any{"statements_approval_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	// The two records' files, as the profile's details' and officers' are.
	nominees, statements := "/v1/corporate-profile/nominees", "/v1/corporate-profile/statements-approvals"
	register(a, authz.AddNomineeEntryEvidence, huma.Operation{OperationID: "addNomineeEntryEvidence", Method: http.MethodPost,
		Path: nominees + "/{nominee_id}/evidence", Tags: []string{"Evidence"}, DefaultStatus: http.StatusCreated, Summary: "Attach a nominee entry's evidence",
		Description: "Attaches a file to a drafted nominee entry (s386AL, FR-DOC-05), as multipart/form-data: the nominee's notice to the " +
			"company, the register's extract or ACRA's acknowledgement. Asking for approval needs one." + addNote,
	}, addCodes, func(ctx context.Context, in *addNomineeEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentNomineeEntry, nominees, in.NomineeVersionID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
	})
	register(a, authz.ListNomineeEntryEvidence, huma.Operation{OperationID: "listNomineeEntryEvidence", Method: http.MethodGet,
		Path: nominees + "/{nominee_id}/evidence", Tags: []string{"Evidence"}, Summary: "List a nominee entry's files",
		Description: "The files attached to a nominee entry's version, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listNomineeEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentNomineeEntry, "nominee_id", in.NomineeVersionID, in.PageQuery)
	})
	register(a, authz.GetNomineeEntryEvidence, huma.Operation{OperationID: "getNomineeEntryEvidence", Method: http.MethodGet,
		Path: nominees + "/{nominee_id}/evidence/{evidence_id}", Tags: []string{"Evidence"}, Summary: "Get a nominee entry's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *NomineeEntryEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentNomineeEntry, "nominee_id", in.NomineeVersionID, in.EvidenceID)
	})
	register(a, authz.RemoveNomineeEntryEvidence, huma.Operation{OperationID: "removeNomineeEntryEvidence", Method: http.MethodDelete,
		Path: nominees + "/{nominee_id}/evidence/{evidence_id}", Tags: []string{"Evidence"}, Summary: "Remove a file from a drafted nominee entry",
		Description: "Takes a file off a nominee entry not recorded yet (FR-DOC-07)." + removeNote,
	}, removeCodes, func(ctx context.Context, in *removeNomineeEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentNomineeEntry, "nominee_id", in.NomineeVersionID, in.EvidenceID, in.IfMatch, in.Prefer, in)
	})
	register(a, authz.AddStatementsApprovalEvidence, huma.Operation{OperationID: "addStatementsApprovalEvidence", Method: http.MethodPost,
		Path: statements + "/{statements_approval_id}/evidence", Tags: []string{"Evidence"}, DefaultStatus: http.StatusCreated,
		Summary: "Attach the directors' resolution or statement",
		Description: "Attaches a file to a drafted statements approval (FR-CS-03, FR-DOC-05), as multipart/form-data: the directors' " +
			"resolution or signed statement approving the financial statements. Asking for approval needs one." + addNote,
	}, addCodes, func(ctx context.Context, in *addStatementsEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.attachEquityEvidence(ctx, storage.DocumentStatementsApproval, statements, in.StatementsApprovalID, in.IdempotencyKey, in.IfMatch, in.Prefer,
			in.RawBody.Data())
	})
	register(a, authz.ListStatementsApprovalEvidence, huma.Operation{OperationID: "listStatementsApprovalEvidence", Method: http.MethodGet,
		Path: statements + "/{statements_approval_id}/evidence", Tags: []string{"Evidence"}, Summary: "List a statements approval's files",
		Description: "The files attached to a statements approval, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listStatementsEvidenceInput) (*equityEvidenceListOutput, error) {
		return a.listEquityEvidence(ctx, storage.DocumentStatementsApproval, "statements_approval_id", in.StatementsApprovalID, in.PageQuery)
	})
	register(a, authz.GetStatementsApprovalEvidence, huma.Operation{OperationID: "getStatementsApprovalEvidence", Method: http.MethodGet,
		Path: statements + "/{statements_approval_id}/evidence/{evidence_id}", Tags: []string{"Evidence"}, Summary: "Get a statements approval's file",
		Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *StatementsApprovalEvidencePath) (*equityEvidenceReadOutput, error) {
		return a.getEquityEvidence(ctx, storage.DocumentStatementsApproval, "statements_approval_id", in.StatementsApprovalID, in.EvidenceID)
	})
	register(a, authz.RemoveStatementsApprovalEvidence, huma.Operation{OperationID: "removeStatementsApprovalEvidence", Method: http.MethodDelete,
		Path: statements + "/{statements_approval_id}/evidence/{evidence_id}", Tags: []string{"Evidence"}, Summary: "Remove a file from a drafted statements approval",
		Description: "Takes a file off a statements approval not recorded yet (FR-DOC-07)." + removeNote,
	}, removeCodes, func(ctx context.Context, in *removeStatementsEvidenceInput) (*equityEvidenceWriteOutput, error) {
		return a.removeEquityEvidence(ctx, storage.DocumentStatementsApproval, "statements_approval_id", in.StatementsApprovalID, in.EvidenceID, in.IfMatch,
			in.Prefer, in)
	})
	for _, path := range []string{nominees + "/{nominee_id}/evidence", statements + "/{statements_approval_id}/evidence"} {
		if op := a.OpenAPI().Paths[path].Post; op != nil && op.RequestBody != nil {
			if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
				mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
			}
		}
	}
}
