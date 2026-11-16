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

// ------------------------------------------------------------------ the corporate profile (FR-CS-01, FR-CS-06)
//
// The company's name, registered office and SSIC code from a day, and its officers: each drafted by the Accountant,
// recorded on another person's approval with its evidence (p1-company-secretary design D2, rev 0.282). A correction or a
// later step (a cessation, a lodgement) is a new version; the last recorded is in force. Nothing here posts.

// CorporateCodes are the codes the corporate profile's records answer besides the common ones.
var CorporateCodes = []string{corporate.CodeDetail, corporate.CodeOfficer, equity.CodeRecorded, storage.CodeCorporateDetail,
	storage.CodeCorporateOfficer}

// CorporateDetailDraft is the company's name, registered office and SSIC code from a day, as drafted (FR-CS-01).
type CorporateDetailDraft struct {
	From             string `json:"from" format:"date" doc:"The day they take effect: incorporation, or the day the name, office or SSIC code changed." example:"2026-03-02"`
	Name             string `json:"name" maxLength:"200" doc:"The company's name as ACRA registers it." example:"Cognizhi Pte. Ltd."`
	RegisteredOffice string `json:"registered_office" maxLength:"500" doc:"The registered office's address." example:"1 North Bridge Road, #08-08, Singapore 179094"`
	SSIC             string `json:"ssic" maxLength:"5" doc:"The primary activity's five-digit SSIC code." example:"62011"`
	LodgedOn         string `json:"lodged_on,omitempty" format:"date" doc:"The day BizFile+ acknowledged the change: within 14 days for the registered office (s143)." example:"2026-03-09"`
	Note             string `json:"note,omitempty" maxLength:"1000" doc:"A note for the record." example:"Moved to the new office"`
}

// CorporateDetailView is a corporate detail, drafted or recorded.
type CorporateDetailView struct {
	ID         string               `json:"id" format:"uuid" doc:"The detail's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7d01"`
	Status     string               `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and approval; recorded: in force today; superseded: a later one is in force." example:"recorded"`
	Version    int                  `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Detail     CorporateDetailDraft `json:"detail" doc:"The details."`
	Evidence   int                  `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one." example:"1"`
	ApprovalID string               `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7d11"`
	RecordedBy *Person              `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time           `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-03-10T09:30:00Z"`
	Checks     []BillCheck          `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// CorporateOfficerDraft is one appointment, as drafted (FR-CS-01, FR-CS-06).
type CorporateOfficerDraft struct {
	Role                string `json:"role" enum:"director,secretary,ceo,auditor" doc:"The office: ACRA keeps the registers of directors, secretaries and auditors, and the CEO." example:"director"`
	Name                string `json:"name" maxLength:"200" doc:"Their name as ACRA registers it." example:"Mei Lin Tan"`
	MemberUserID        string `json:"member_user_id,omitempty" format:"uuid" doc:"The member of the organisation they sign in as, if they are one: the named secretary holds the Company secretary role (FR-CS-06)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	Kind                string `json:"kind" enum:"individual,entity" doc:"individual; or entity, for an audit firm only: a director, the secretary and the CEO are natural persons (s145, s171)." example:"individual"`
	IDType              string `json:"id_type,omitempty" enum:"nric,fin,passport,uen,foreign_registration" doc:"The identifier's type: nric, fin or passport for an individual; uen or foreign_registration for an audit firm." example:"nric"`
	IDLast4             string `json:"id_last4,omitempty" maxLength:"4" doc:"An individual's identifier, its last 4 characters only (SEC-15); the full number stays in ACRA's register." example:"567A"`
	RegistrationNumber  string `json:"registration_number,omitempty" maxLength:"30" doc:"An audit firm's UEN or foreign registration number." example:"T08LL1234A"`
	AppointedOn         string `json:"appointed_on" format:"date" doc:"The day they were appointed." example:"2026-03-02"`
	CeasedOn            string `json:"ceased_on,omitempty" format:"date" doc:"The day they ceased to hold the office." example:"2027-06-30"`
	AppointmentLodgedOn string `json:"appointment_lodged_on,omitempty" format:"date" doc:"The day BizFile+ acknowledged the appointment: within 14 days (s173A)." example:"2026-03-09"`
	CessationLodgedOn   string `json:"cessation_lodged_on,omitempty" format:"date" doc:"The day BizFile+ acknowledged the cessation: within 14 days (s173A)." example:"2027-07-07"`
	Nominee             bool   `json:"nominee" doc:"true: a nominee director, who acts on another's directions (s386AL)." example:"false"`
	Nominator           string `json:"nominator,omitempty" maxLength:"200" doc:"Whom a nominee director acts for." example:"Lion Rock Ventures Pte. Ltd."`
	Note                string `json:"note,omitempty" maxLength:"1000" doc:"A note for the record." example:"Appointed at incorporation"`
}

// CorporateOfficerNew is a new officer's record: a new appointment, or a later version of one.
type CorporateOfficerNew struct {
	OfficerID string `json:"officer_id,omitempty" format:"uuid" doc:"The officer it follows, by their first version's ID, with a version recorded: to record a cessation, a lodgement or a correction. Empty for a new appointment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e01"`
	CorporateOfficerDraft
}

// CorporateOfficerView is an officer's version, drafted or recorded.
type CorporateOfficerView struct {
	ID         string                `json:"id" format:"uuid" doc:"The version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e02"`
	OfficerID  string                `json:"officer_id" format:"uuid" doc:"The officer, by their first version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e01"`
	Status     string                `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its evidence and approval; recorded: the officer's version in force; superseded: a later version is in force." example:"recorded"`
	Version    int                   `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Officer    CorporateOfficerDraft `json:"officer" doc:"The appointment."`
	Masked     bool                  `json:"masked" doc:"true: the identifier and the note are hidden, as the caller reads the profile masked (Appendix D's M)." example:"false"`
	Evidence   int                   `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one." example:"1"`
	ApprovalID string                `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e11"`
	RecordedBy *Person               `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time            `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-03-10T09:30:00Z"`
	Checks     []BillCheck           `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// CorporateProfile is the corporate profile on a day (FR-CS-01): the details and the officers in force then, and every
// version of each.
type CorporateProfile struct {
	On             string                 `json:"on" format:"date" doc:"The day it shows." example:"2026-11-16"`
	Detail         *CorporateDetailView   `json:"detail,omitempty" doc:"The name, office and SSIC code in force on the day; none before the first is recorded."`
	Officers       []CorporateOfficerView `json:"officers" nullable:"false" doc:"The officers in office on the day: appointed by then and not ceased, directors first, then the secretary, the CEO and auditors."`
	Secretary      bool                   `json:"secretary" doc:"true: a secretary is in office on the day (FR-CS-06)." example:"true"`
	Details        []CorporateDetailView  `json:"details" nullable:"false" doc:"Every detail, drafts and superseded ones included, the latest first."`
	OfficerHistory []CorporateOfficerView `json:"officer_history" nullable:"false" doc:"Every officer's every version, drafts and superseded ones included, the latest first."`
}

type (
	CorporateDetailPath struct {
		DetailID string `path:"detail_id" format:"uuid" doc:"The corporate detail." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7d01"`
	}
	CorporateOfficerPath struct {
		OfficerVersionID string `path:"version_id" format:"uuid" doc:"The officer's version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e02"`
	}
	CorporateDetailEvidencePath struct {
		CorporateDetailPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	CorporateOfficerEvidencePath struct {
		CorporateOfficerPath
		EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	}
	corporateProfileInput struct {
		On string `query:"on" format:"date" doc:"The day to show; today when left out." example:"2026-11-16"`
	}
	corporateProfileOutput struct{ Body CorporateProfile }
	detailDraftInput       struct {
		KeyHeader
		PreferHeader
		Body CorporateDetailDraft
	}
	detailReviseInput struct {
		CorporateDetailPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body CorporateDetailDraft
	}
	detailAskInput struct {
		CorporateDetailPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	detailReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body CorporateDetailView
	}
	detailWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     CorporateDetailView
	}
	officerDraftInput struct {
		KeyHeader
		PreferHeader
		Body CorporateOfficerNew
	}
	officerReviseInput struct {
		CorporateOfficerPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body CorporateOfficerDraft
	}
	officerAskInput struct {
		CorporateOfficerPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	officerReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body CorporateOfficerView
	}
	officerWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     CorporateOfficerView
	}
	addDetailEvidenceInput struct {
		CorporateDetailPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listDetailEvidenceInput struct {
		CorporateDetailPath
		PageQuery
	}
	removeDetailEvidenceInput struct {
		CorporateDetailEvidencePath
		IfMatchHeader
		PreferHeader
	}
	addOfficerEvidenceInput struct {
		CorporateOfficerPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		RawBody huma.MultipartFormFiles[EvidenceUpload]
	}
	listOfficerEvidenceInput struct {
		CorporateOfficerPath
		PageQuery
	}
	removeOfficerEvidenceInput struct {
		CorporateOfficerEvidencePath
		IfMatchHeader
		PreferHeader
	}
)

// detailIn reads a corporate detail's fields, or what of them is wrong.
func detailIn(d CorporateDetailDraft) (corporate.Detail, error) {
	out := corporate.Detail{Name: strings.TrimSpace(d.Name), Office: strings.TrimSpace(d.RegisteredOffice), SSIC: strings.TrimSpace(d.SSIC),
		Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	if from := dayIn("body.from", d.From, &fields); from != nil {
		out.From = *from
	}
	out.Lodged = dayIn("body.lodged_on", d.LodgedOn, &fields)
	if len(fields) > 0 {
		return out, invalid(fields)
	}
	return out, nil
}

func detailDraftOf(d corporate.Detail) CorporateDetailDraft {
	return CorporateDetailDraft{From: d.From.Format(time.DateOnly), Name: d.Name, RegisteredOffice: d.Office, SSIC: d.SSIC, LodgedOn: dayOut(d.Lodged),
		Note: d.Note}
}

// officerIn reads an officer's fields, or what of them is wrong.
func officerIn(follows string, d CorporateOfficerDraft) (corporate.Officer, error) {
	out := corporate.Officer{Role: d.Role, Name: strings.TrimSpace(d.Name), Kind: d.Kind, IDType: d.IDType, IDLast4: strings.TrimSpace(d.IDLast4),
		Registration: strings.TrimSpace(d.RegistrationNumber), Nominee: d.Nominee, Nominator: strings.TrimSpace(d.Nominator), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	ids := []struct {
		field, value, fix string
		to                *uuid.UUID
	}{{"body.officer_id", follows, "Send the officer's first version's ID, or leave it empty for a new appointment", &out.ID}}
	var member uuid.UUID
	ids = append(ids, struct {
		field, value, fix string
		to                *uuid.UUID
	}{"body.member_user_id", d.MemberUserID, "Send a member's user ID, or leave it empty", &member})
	for _, x := range ids {
		if strings.TrimSpace(x.value) == "" {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(x.value))
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: x.fix})
			continue
		}
		*x.to = id
	}
	if member != uuid.Nil {
		out.Member = &member
	}
	if appointed := dayIn("body.appointed_on", d.AppointedOn, &fields); appointed != nil {
		out.Appointed = *appointed
	}
	out.Ceased = dayIn("body.ceased_on", d.CeasedOn, &fields)
	out.AppointmentLodged = dayIn("body.appointment_lodged_on", d.AppointmentLodgedOn, &fields)
	out.CessationLodged = dayIn("body.cessation_lodged_on", d.CessationLodgedOn, &fields)
	if len(fields) > 0 {
		return out, invalid(fields)
	}
	return out, nil
}

func officerDraftOf(o corporate.Officer) CorporateOfficerDraft {
	out := CorporateOfficerDraft{Role: o.Role, Name: o.Name, Kind: o.Kind, IDType: o.IDType, IDLast4: o.IDLast4, RegistrationNumber: o.Registration,
		AppointedOn: o.Appointed.Format(time.DateOnly), CeasedOn: dayOut(o.Ceased), AppointmentLodgedOn: dayOut(o.AppointmentLodged),
		CessationLodgedOn: dayOut(o.CessationLodged), Nominee: o.Nominee, Nominator: o.Nominator, Note: o.Note}
	if o.Member != nil {
		out.MemberUserID = o.Member.String()
	}
	return out
}

// corporateView is what the profile's reads share: every version of each record, and the requests waiting.
type corporateView struct {
	details  []equity.Recorded[corporate.Detail]
	officers map[uuid.UUID][]equity.Recorded[corporate.Officer]
	asked    map[string]string
	today    time.Time
}

func (a *API) corporateViewOf(ctx context.Context, u storage.UnitOfWork) (corporateView, error) {
	v := corporateView{asked: map[string]string{}}
	var err error
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return v, err
	}
	if v.details, err = corporate.Details(ctx, u); err != nil {
		return v, err
	}
	if v.officers, err = corporate.Officers(ctx, u); err != nil {
		return v, err
	}
	for _, o := range []string{approval.ObjectCorporateDetail, approval.ObjectCorporateOfficer} {
		asked, err := a.waiting(ctx, u, o)
		if err != nil {
			return v, err
		}
		for k, x := range asked {
			v.asked[k] = x
		}
	}
	return v, nil
}

func (a *API) detailViewOf(ctx context.Context, u storage.UnitOfWork, v corporateView, x equity.Recorded[corporate.Detail]) (CorporateDetailView, error) {
	var inForce *uuid.UUID
	if cur := corporate.DetailOn(v.details, v.today); cur != nil {
		inForce = &cur.Doc.ID
	}
	out := CorporateDetailView{ID: x.Doc.ID.String(), Status: recordStatus(x.Doc, inForce), Version: x.Doc.Version, Detail: detailDraftOf(x.Value)}
	var err error
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the BizFile+ acknowledgement of the change, or the business profile that shows it"
		}
	}
	return out, err
}

func (a *API) officerViewOf(ctx context.Context, u storage.UnitOfWork, v corporateView, x equity.Recorded[corporate.Officer]) (CorporateOfficerView, error) {
	root := corporate.Root(x.Doc, x.Value)
	out := CorporateOfficerView{ID: x.Doc.ID.String(), OfficerID: root.String(), Status: recordStatus(x.Doc, inForceID(v.officers[root])),
		Version: x.Doc.Version, Officer: officerDraftOf(x.Value)}
	if masked(ctx) {
		out.Masked = true
		out.Officer.IDLast4, out.Officer.RegistrationNumber, out.Officer.Note = "", "", ""
	}
	var err error
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the officer's consent to act or the resolution, the BizFile+ acknowledgement, or the business profile that shows them"
		}
	}
	return out, err
}

// shownDetail is a corporate detail by document ID: not found for anything else.
func (a *API) shownDetail(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (CorporateDetailView, error) {
	v, err := a.corporateViewOf(ctx, u)
	if err != nil {
		return CorporateDetailView{}, err
	}
	for _, x := range v.details {
		if x.Doc.ID == id {
			return a.detailViewOf(ctx, u, v, x)
		}
	}
	return CorporateDetailView{}, ports.ErrNotFound
}

// shownOfficer is an officer's version by document ID: not found for anything else.
func (a *API) shownOfficer(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (CorporateOfficerView, error) {
	v, err := a.corporateViewOf(ctx, u)
	if err != nil {
		return CorporateOfficerView{}, err
	}
	for _, versions := range v.officers {
		for _, x := range versions {
			if x.Doc.ID == id {
				return a.officerViewOf(ctx, u, v, x)
			}
		}
	}
	return CorporateOfficerView{}, ports.ErrNotFound
}

// corporateProfileOn is the profile on a day, as the API shows it.
func (a *API) corporateProfileOn(ctx context.Context, u storage.UnitOfWork, on *time.Time) (CorporateProfile, error) {
	v, err := a.corporateViewOf(ctx, u)
	if err != nil {
		return CorporateProfile{}, err
	}
	day := v.today
	if on != nil {
		day = *on
	}
	out := CorporateProfile{On: day.Format(time.DateOnly), Officers: []CorporateOfficerView{}, Details: []CorporateDetailView{},
		OfficerHistory: []CorporateOfficerView{}}
	if cur := corporate.DetailOn(v.details, day); cur != nil {
		d, err := a.detailViewOf(ctx, u, v, *cur)
		if err != nil {
			return out, err
		}
		out.Detail = &d
	}
	for _, x := range corporate.OfficersOn(v.officers, day) {
		o, err := a.officerViewOf(ctx, u, v, x)
		if err != nil {
			return out, err
		}
		out.Secretary = out.Secretary || x.Value.Role == corporate.RoleSecretary
		out.Officers = append(out.Officers, o)
	}
	for i := len(v.details) - 1; i >= 0; i-- {
		d, err := a.detailViewOf(ctx, u, v, v.details[i])
		if err != nil {
			return out, err
		}
		out.Details = append(out.Details, d)
	}
	var history []equity.Recorded[corporate.Officer]
	for _, versions := range v.officers {
		history = append(history, versions...)
	}
	// The latest first: drafts, then by when they were recorded, as each officer's versions are ordered.
	slices.SortStableFunc(history, func(x, y equity.Recorded[corporate.Officer]) int {
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
	})
	for _, x := range history {
		o, err := a.officerViewOf(ctx, u, v, x)
		if err != nil {
			return out, err
		}
		out.OfficerHistory = append(out.OfficerHistory, o)
	}
	return out, nil
}

// corporateRecordDependencies are a detail's or an officer's envelope values: its status, and its day or the officer it
// follows.
func corporateRecordDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	switch doc.Type {
	case storage.DocumentCorporateDetail:
		d, err := corporate.ReadDetail(doc)
		if err != nil {
			return 0, nil, err
		}
		return doc.Version, map[string]string{"status": string(doc.Status), "from": d.From.Format(time.DateOnly)}, nil
	case storage.DocumentCorporateOfficer:
		o, err := corporate.ReadOfficer(doc)
		if err != nil {
			return 0, nil, err
		}
		return doc.Version, map[string]string{"status": string(doc.Status), "officer": corporate.Root(doc, o).String()}, nil
	}
	return 0, nil, fmt.Errorf("document %s is a %s, not a corporate detail or officer", doc.ID, doc.Type)
}

// corporateOfficerExecutable refuses the officer themselves (FM-1): another person records their appointment.
func corporateOfficerExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	o, err := corporate.ReadOfficer(doc)
	if err != nil {
		return err
	}
	if o.Member != nil && *o.Member == signer {
		return problem(approval.CodeNotApprover, "This is your own appointment, so another person approves it (FM-1).",
			"Ask another person who holds the approval right")
	}
	return nil
}

func detailAnswer(w written[CorporateDetailView]) *detailWriteOutput {
	if w.Previewed {
		return &detailWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &detailWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/corporate-profile/details/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func officerAnswer(w written[CorporateOfficerView]) *officerWriteOutput {
	if w.Previewed {
		return &officerWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &officerWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/corporate-profile/officers/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

var roleWords = map[string]string{corporate.RoleDirector: "director", corporate.RoleSecretary: "secretary", corporate.RoleCEO: "CEO",
	corporate.RoleAuditor: "auditor"}

func registerCorporateProfile(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	ask := append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...),
		CorporateCodes...)
	addCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, equity.CodeRecorded}, append(locks, EvidenceCodes...)...)
	removeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeEvidence, storage.CodeVersion, equity.CodeRecorded}, locks...)
	addNote := " The file's type is decided from its content; it is scanned for malware and its image metadata is stripped before it " +
		"is stored (FR-DOC-08). Side effects: the file in the staging store, its evidence row and its audit event; once recorded its " +
		"files are committed to the retention-locked store (FR-DOC-13). Needs If-Match with the record's ETag. Idempotent with " +
		"Idempotency-Key; Prefer: return=preview saves nothing."
	removeNote := " Once recorded its evidence is append-only, and the database refuses. The row stays, marked removed, for the audit " +
		"trail. Side effects: the removal and its audit event. Needs If-Match with the file's ETag; Prefer: return=preview saves nothing."

	register(a, authz.GetCorporateProfile, huma.Operation{
		OperationID: "getCorporateProfile", Method: http.MethodGet, Path: "/v1/corporate-profile", Tags: []string{"Corporate profile"},
		Summary: "Get the corporate profile",
		Description: "The corporate profile on a day (FR-CS-01, FR-CS-06; design D2): the name, registered office and SSIC code in force, " +
			"and the officers in office (directors, the secretary, the CEO and auditors), with whether a secretary is in office, and every " +
			"version of each, drafts included. The UEN and the financial years are the organisation profile's. Read masked (Appendix D's " +
			"M), an officer's identifier and note are hidden. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *corporateProfileInput) (*corporateProfileOutput, error) {
		var fields []FieldProblem
		on := dayIn("on", in.On, &fields)
		if len(fields) > 0 {
			return nil, invalid(fields)
		}
		p, _ := authn.From(ctx)
		var out corporateProfileOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out.Body, err = a.corporateProfileOn(ctx, u, on)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateCorporateDetail, huma.Operation{
		OperationID: "createCorporateDetail", Method: http.MethodPost, Path: "/v1/corporate-profile/details", Tags: []string{"Corporate profile"},
		DefaultStatus: http.StatusCreated, Summary: "Draft the company's name, registered office and SSIC code",
		Description: "Drafts the company's details from a day (FR-CS-01): its name, registered office and five-digit SSIC code, and the day " +
			"BizFile+ acknowledged the change once it is lodged (14 days for the registered office, s143). Attach the acknowledgement or the " +
			"business profile with addCorporateDetailEvidence, then ask another person to approve it with requestCorporateDetailApproval; " +
			"once recorded, the latest to take effect by a day is in force. Side effects: the draft, audit events. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(writes, CorporateCodes...), func(ctx context.Context, in *detailDraftInput) (*detailWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CorporateDetailView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateDetailView{}, err
			}
			d, err := detailIn(in.Body)
			if err != nil {
				return CorporateDetailView{}, err
			}
			doc, err := corporate.DraftDetailIn(ctx, u, d, today)
			if err != nil {
				return CorporateDetailView{}, equityProblems(err)
			}
			return a.shownDetail(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return detailAnswer(w), nil
	})

	register(a, authz.GetCorporateDetail, huma.Operation{
		OperationID: "getCorporateDetail", Method: http.MethodGet, Path: "/v1/corporate-profile/details/{detail_id}", Tags: []string{"Corporate profile"},
		Summary:     "Get a corporate detail",
		Description: "Returns a corporate detail with its ETag: the name, office, SSIC code and lodgement, its status, files and the approval waiting (FR-CS-01). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *CorporateDetailPath) (*detailReadOutput, error) {
		id, err := parseID("detail_id", in.DetailID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out detailReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentCorporateDetail, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownDetail(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceCorporateDetail, huma.Operation{
		OperationID: "replaceCorporateDetail", Method: http.MethodPut, Path: "/v1/corporate-profile/details/{detail_id}", Tags: []string{"Corporate profile"},
		Summary: "Change a drafted corporate detail",
		Description: "Saves a new version of a corporate detail not recorded yet (FR-CS-01). A recorded one changes no more (EQUITY-05): " +
			"draft a new detail to correct it or to add the day it was lodged. Side effects: the new version, audit events. Needs If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), CorporateCodes...), func(ctx context.Context, in *detailReviseInput) (*detailWriteOutput, error) {
		id, err := parseID("detail_id", in.DetailID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CorporateDetailView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDetail, id)
			if err != nil {
				return CorporateDetailView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return CorporateDetailView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateDetailView{}, err
			}
			d, err := detailIn(in.Body)
			if err != nil {
				return CorporateDetailView{}, err
			}
			if _, err := corporate.ReviseDetailIn(ctx, u, id, d, today); err != nil {
				return CorporateDetailView{}, equityProblems(err)
			}
			return a.shownDetail(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return detailAnswer(w), nil
	})

	register(a, authz.RequestCorporateDetailApproval, huma.Operation{
		OperationID: "requestCorporateDetailApproval", Method: http.MethodPost, Path: "/v1/corporate-profile/details/{detail_id}/approval",
		Tags: []string{"Corporate profile"}, DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a corporate detail",
		Description: "Asks another person to approve a drafted corporate detail at the version read (FR-CS-01, FM-1). It needs its evidence " +
			"attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A on \"Corporate profile and " +
			"registers\" approves, stepped up, never the one who asked. Needs a person signed in who passed step-up, and If-Match with its " +
			"ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *detailAskInput) (*equityAskOutput, error) {
		id, err := parseID("detail_id", in.DetailID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDetail, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the detail has no evidence attached",
					Fix: "Attach the BizFile+ acknowledgement of the change, or the business profile that shows it, then ask again"}
			}
			d, err := corporate.ReadDetail(doc)
			if err != nil {
				return Approval{}, err
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "from", After: d.From.Format(time.DateOnly)},
				{Field: "name", After: d.Name}, {Field: "registered_office", After: d.Office}, {Field: "ssic", After: d.SSIC},
				{Field: "lodged_on", After: dayOut(d.Lodged)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectCorporateDetail, ObjectKey: doc.ID.String(),
				Operation: approval.OpCorporateDetailRecord, Summary: "Record " + d.Name + "'s details from " + d.From.Format("2 Jan 2006"),
				Execute: map[string]any{"detail_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.CreateCorporateOfficer, huma.Operation{
		OperationID: "createCorporateOfficer", Method: http.MethodPost, Path: "/v1/corporate-profile/officers", Tags: []string{"Corporate profile"},
		DefaultStatus: http.StatusCreated, Summary: "Draft an officer's appointment, or a later version of it",
		Description: "Drafts an officer (FR-CS-01, FR-CS-06): a director, the secretary, the CEO or an auditor, by name and identifier (an " +
			"individual's last 4 characters only, SEC-15; an audit firm's UEN), the member they sign in as if any, the days appointed and " +
			"ceased and the day each was lodged (14 days, s173A), and for a director whether they are a nominee and for whom (s386AL). " +
			"With officer_id it is a later version of an officer with a version recorded: a cessation, a lodgement or a correction. Attach " +
			"the evidence with addCorporateOfficerEvidence, then ask another person to approve it with requestCorporateOfficerApproval; the " +
			"last recorded version is in force. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), CorporateCodes...), func(ctx context.Context, in *officerDraftInput) (*officerWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CorporateOfficerView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateOfficerView{}, err
			}
			o, err := officerIn(in.Body.OfficerID, in.Body.CorporateOfficerDraft)
			if err != nil {
				return CorporateOfficerView{}, err
			}
			doc, err := corporate.DraftOfficerIn(ctx, u, o, today)
			if err != nil {
				return CorporateOfficerView{}, equityProblems(err)
			}
			return a.shownOfficer(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return officerAnswer(w), nil
	})

	register(a, authz.GetCorporateOfficer, huma.Operation{
		OperationID: "getCorporateOfficer", Method: http.MethodGet, Path: "/v1/corporate-profile/officers/{version_id}", Tags: []string{"Corporate profile"},
		Summary:     "Get an officer's version",
		Description: "Returns an officer's version with its ETag: the appointment, its days and lodgements, its status, files and the approval waiting (FR-CS-01). Read masked, the identifier and note are hidden. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *CorporateOfficerPath) (*officerReadOutput, error) {
		id, err := parseID("version_id", in.OfficerVersionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out officerReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentCorporateOfficer, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownOfficer(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceCorporateOfficer, huma.Operation{
		OperationID: "replaceCorporateOfficer", Method: http.MethodPut, Path: "/v1/corporate-profile/officers/{version_id}", Tags: []string{"Corporate profile"},
		Summary: "Change a drafted officer's version",
		Description: "Saves a new version of an officer's draft not recorded yet (FR-CS-01); the officer it follows stays. A recorded one " +
			"changes no more (EQUITY-05): draft a later version with officer_id. Side effects: the new version, audit events. Needs If-Match " +
			"with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), CorporateCodes...), func(ctx context.Context, in *officerReviseInput) (*officerWriteOutput, error) {
		id, err := parseID("version_id", in.OfficerVersionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CorporateOfficerView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateOfficer, id)
			if err != nil {
				return CorporateOfficerView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return CorporateOfficerView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateOfficerView{}, err
			}
			o, err := officerIn("", in.Body)
			if err != nil {
				return CorporateOfficerView{}, err
			}
			if _, err := corporate.ReviseOfficerIn(ctx, u, id, o, today); err != nil {
				return CorporateOfficerView{}, equityProblems(err)
			}
			return a.shownOfficer(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return officerAnswer(w), nil
	})

	register(a, authz.RequestCorporateOfficerApproval, huma.Operation{
		OperationID: "requestCorporateOfficerApproval", Method: http.MethodPost, Path: "/v1/corporate-profile/officers/{version_id}/approval",
		Tags: []string{"Corporate profile"}, DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record an officer's version",
		Description: "Asks another person to approve a drafted officer's version at the version read (FR-CS-01, FM-1). It needs its evidence " +
			"attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A on \"Corporate profile and " +
			"registers\" approves, stepped up, never the officer themselves nor the one who asked. Needs a person signed in who passed " +
			"step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *officerAskInput) (*equityAskOutput, error) {
		id, err := parseID("version_id", in.OfficerVersionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateOfficer, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the officer's record has no evidence attached",
					Fix: "Attach the officer's consent to act or the resolution, the BizFile+ acknowledgement, or the business profile, then ask again"}
			}
			o, err := corporate.ReadOfficer(doc)
			if err != nil {
				return Approval{}, err
			}
			summary := "Record " + o.Name + "'s appointment as " + roleWords[o.Role]
			if o.Ceased != nil {
				summary = "Record that " + o.Name + " ceased as " + roleWords[o.Role] + " on " + o.Ceased.Format("2 Jan 2006")
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "role", After: o.Role}, {Field: "name", After: o.Name},
				{Field: "appointed_on", After: o.Appointed.Format(time.DateOnly)}, {Field: "ceased_on", After: dayOut(o.Ceased)},
				{Field: "appointment_lodged_on", After: dayOut(o.AppointmentLodged)}, {Field: "cessation_lodged_on", After: dayOut(o.CessationLodged)},
				{Field: "nominee", After: strconv.FormatBool(o.Nominee)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectCorporateOfficer, ObjectKey: doc.ID.String(),
				Operation: approval.OpCorporateOfficerRecord, Summary: summary,
				Execute: map[string]any{"version_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	for _, x := range []struct {
		t                                storage.DocumentType
		base, field, what, evidence, add string
		addP, listP, getP, removeP       authz.Policy
	}{
		{storage.DocumentCorporateDetail, "/v1/corporate-profile/details", "detail_id", "corporate detail",
			"the BizFile+ acknowledgement of the change, or the business profile that shows it", "addCorporateDetailEvidence",
			authz.AddCorporateDetailEvidence, authz.ListCorporateDetailEvidence, authz.GetCorporateDetailEvidence, authz.RemoveCorporateDetailEvidence},
		{storage.DocumentCorporateOfficer, "/v1/corporate-profile/officers", "version_id", "officer's version",
			"the officer's consent to act or the resolution, the BizFile+ acknowledgement, or the business profile that shows them",
			"addCorporateOfficerEvidence", authz.AddCorporateOfficerEvidence, authz.ListCorporateOfficerEvidence,
			authz.GetCorporateOfficerEvidence, authz.RemoveCorporateOfficerEvidence},
	} {
		path := x.base + "/{" + x.field + "}/evidence"
		desc := "Attaches a file to a drafted " + x.what + " (FR-CS-01, FR-DOC-05), as multipart/form-data: " + x.evidence +
			". Asking for approval needs one." + addNote
		if x.t == storage.DocumentCorporateDetail {
			register(a, x.addP, huma.Operation{OperationID: x.addP.Operation, Method: http.MethodPost, Path: path, Tags: []string{"Evidence"},
				DefaultStatus: http.StatusCreated, Summary: "Attach a corporate detail's evidence", Description: desc,
			}, addCodes, func(ctx context.Context, in *addDetailEvidenceInput) (*equityEvidenceWriteOutput, error) {
				return a.attachEquityEvidence(ctx, x.t, x.base, in.DetailID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
			})
			register(a, x.listP, huma.Operation{OperationID: x.listP.Operation, Method: http.MethodGet, Path: path, Tags: []string{"Evidence"},
				Summary:     "List a corporate detail's files",
				Description: "The files attached to a corporate detail, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
			}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listDetailEvidenceInput) (*equityEvidenceListOutput, error) {
				return a.listEquityEvidence(ctx, x.t, x.field, in.DetailID, in.PageQuery)
			})
			register(a, x.getP, huma.Operation{OperationID: x.getP.Operation, Method: http.MethodGet, Path: path + "/{evidence_id}", Tags: []string{"Evidence"},
				Summary:     "Get a corporate detail's file",
				Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
			}, []string{CodeNotFound}, func(ctx context.Context, in *CorporateDetailEvidencePath) (*equityEvidenceReadOutput, error) {
				return a.getEquityEvidence(ctx, x.t, x.field, in.DetailID, in.EvidenceID)
			})
			register(a, x.removeP, huma.Operation{OperationID: x.removeP.Operation, Method: http.MethodDelete, Path: path + "/{evidence_id}", Tags: []string{"Evidence"},
				Summary: "Remove a file from a drafted corporate detail", Description: "Takes a file off a corporate detail not recorded yet (FR-DOC-07)." + removeNote,
			}, removeCodes, func(ctx context.Context, in *removeDetailEvidenceInput) (*equityEvidenceWriteOutput, error) {
				return a.removeEquityEvidence(ctx, x.t, x.field, in.DetailID, in.EvidenceID, in.IfMatch, in.Prefer, in)
			})
		} else {
			register(a, x.addP, huma.Operation{OperationID: x.addP.Operation, Method: http.MethodPost, Path: path, Tags: []string{"Evidence"},
				DefaultStatus: http.StatusCreated, Summary: "Attach an officer's evidence", Description: desc,
			}, addCodes, func(ctx context.Context, in *addOfficerEvidenceInput) (*equityEvidenceWriteOutput, error) {
				return a.attachEquityEvidence(ctx, x.t, x.base, in.OfficerVersionID, in.IdempotencyKey, in.IfMatch, in.Prefer, in.RawBody.Data())
			})
			register(a, x.listP, huma.Operation{OperationID: x.listP.Operation, Method: http.MethodGet, Path: path, Tags: []string{"Evidence"},
				Summary:     "List an officer's version's files",
				Description: "The files attached to an officer's version, oldest first, removed ones included, a page at a time (FR-DOC-05). Read-only; no side effects.",
			}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *listOfficerEvidenceInput) (*equityEvidenceListOutput, error) {
				return a.listEquityEvidence(ctx, x.t, x.field, in.OfficerVersionID, in.PageQuery)
			})
			register(a, x.getP, huma.Operation{OperationID: x.getP.Operation, Method: http.MethodGet, Path: path + "/{evidence_id}", Tags: []string{"Evidence"},
				Summary:     "Get an officer's version's file",
				Description: "Returns a file's details, hashes, state and retain-until, with its ETag (FR-DOC-05, FR-DOC-07). Read-only; no side effects.",
			}, []string{CodeNotFound}, func(ctx context.Context, in *CorporateOfficerEvidencePath) (*equityEvidenceReadOutput, error) {
				return a.getEquityEvidence(ctx, x.t, x.field, in.OfficerVersionID, in.EvidenceID)
			})
			register(a, x.removeP, huma.Operation{OperationID: x.removeP.Operation, Method: http.MethodDelete, Path: path + "/{evidence_id}", Tags: []string{"Evidence"},
				Summary: "Remove a file from a drafted officer's version", Description: "Takes a file off an officer's version not recorded yet (FR-DOC-07)." + removeNote,
			}, removeCodes, func(ctx context.Context, in *removeOfficerEvidenceInput) (*equityEvidenceWriteOutput, error) {
				return a.removeEquityEvidence(ctx, x.t, x.field, in.OfficerVersionID, in.EvidenceID, in.IfMatch, in.Prefer, in)
			})
		}
		if op := a.OpenAPI().Paths[path].Post; op != nil && op.RequestBody != nil {
			if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
				mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
			}
		}
	}
}
