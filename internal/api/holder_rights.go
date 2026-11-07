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
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ investor rights and registrable controllers (FR-EQ-22, FR-EQ-25)
//
// A holder's rights from the agreements are recorded, never computed (design D10): board seats, an observer, a board
// majority, information rights, pro-rata and consent matters, with the agreements attached. Whether a holder is a
// registrable controller is the company's conclusion: the cap table tests the shares and the votes (more than 25%), and
// rights that could confer control are weighed by a person, the named company secretary confirming (open question 41).
// Each is drafted by the Accountant and recorded on another person's approval, never the holder's (FM-1).

// RightsCodes are the codes investor rights and controller entries answer besides the common ones.
var RightsCodes = []string{equity.CodeRights, equity.CodeController, equity.CodeHolderRecord, equity.CodeRecorded, storage.CodeInvestorRights,
	storage.CodeControllerEntry}

// ConsentMatter is a matter that needs an investor's consent, as the agreement names it.
type ConsentMatter string

// Schema implements huma.SchemaProvider.
func (ConsentMatter) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", MaxLength: new(equity.MaxConsentText), Description: "A matter that needs the holder's consent, such as a reserved matter.",
		Examples: []any{"Issuing new shares"}}
}

// ControllerGround is a ground for being a registrable controller.
type ControllerGround string

// Schema implements huma.SchemaProvider.
func (ControllerGround) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"shares", "votes", "board", "influence"},
		Description: "shares or votes: more than 25% of them; board: the right to appoint or remove directors holding a majority of " +
			"the board's votes; influence: significant influence or control.", Examples: []any{"shares"}}
}

// InvestorRightsDraft is a holder's rights from the agreements, as drafted (FR-EQ-22).
type InvestorRightsDraft struct {
	From          string          `json:"from" format:"date" doc:"The day the agreements give the rights from." example:"2026-06-18"`
	BoardSeats    int             `json:"board_seats" minimum:"0" maximum:"20" doc:"The directors the holder may appoint." example:"0"`
	Observer      bool            `json:"observer" doc:"A board observer seat." example:"false"`
	BoardMajority bool            `json:"board_majority" doc:"The right to appoint or remove directors holding a majority of the board's votes: a ground for being a registrable controller (FR-EQ-25)." example:"false"`
	Information   string          `json:"information,omitempty" maxLength:"1000" doc:"The information rights: what and when." example:"Quarterly management accounts within 45 days; annual accounts within 120 days"`
	ProRata       bool            `json:"pro_rata" doc:"A pro-rata right in later rounds." example:"true"`
	Consents      []ConsentMatter `json:"consents" nullable:"false" maxItems:"20" doc:"The matters that need the holder's consent; any could amount to significant influence, so a person weighs them (FR-EQ-25)."`
	Agreements    string          `json:"agreements" maxLength:"500" doc:"The agreements the rights come from." example:"Side letter of 18 Jun 2026"`
	Note          string          `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"The pro-rata right lapses on a listing"`
}

// InvestorRightsNew is new investor rights: their holder and the rights.
type InvestorRightsNew struct {
	HolderID string `json:"holder_id" format:"uuid" doc:"The holder: a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	InvestorRightsDraft
}

// InvestorRightsView is a holder's rights, drafted or recorded.
type InvestorRightsView struct {
	ID             string              `json:"id" format:"uuid" doc:"The rights' ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7b01"`
	HolderID       string              `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName     string              `json:"holder_name" doc:"Their name." example:"Lion Rock Ventures Pte. Ltd."`
	Status         string              `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its agreements and approval; recorded: in force; superseded: later rights are in force." example:"recorded"`
	Version        int                 `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Rights         InvestorRightsDraft `json:"rights" doc:"The rights."`
	ConfersControl bool                `json:"confers_control" doc:"A board majority or consent matters: rights that could make the holder a registrable controller whatever their shares, which a person weighs (G4-101)." example:"false"`
	Evidence       int                 `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, such as the side letter." example:"1"`
	ApprovalID     string              `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7b11"`
	RecordedBy     *Person             `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt     *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-06-19T09:30:00Z"`
	Checks         []BillCheck         `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no agreement is attached."`
}

// ControllerNoticeDraft is a yearly notice sent to a registrable controller, and the day they replied.
type ControllerNoticeDraft struct {
	SentOn    string `json:"sent_on" format:"date" doc:"The day the notice was sent." example:"2027-03-15"`
	RepliedOn string `json:"replied_on,omitempty" format:"date" doc:"The day they replied; the reply is due within 30 days." example:"2027-03-29"`
}

// ControllerEntryDraft is the company's conclusion about a holder, as drafted (FR-EQ-25).
type ControllerEntryDraft struct {
	Controller        bool                    `json:"controller" doc:"true: a registrable controller; false: found not to be one, with the reason, such as rights that do not confer control." example:"true"`
	Grounds           []ControllerGround      `json:"grounds" nullable:"false" maxItems:"4" doc:"A controller's grounds, each once; none for a holder who is not one."`
	Reason            string                  `json:"reason,omitempty" maxLength:"1000" doc:"Why a holder is not a controller, as the company secretary concluded." example:"The consent matters protect the investment and do not direct the company"`
	Since             string                  `json:"since" format:"date" doc:"The day they became a controller, or the day of the conclusion that they are not." example:"2027-03-08"`
	CompanyRegisterOn string                  `json:"company_register_on,omitempty" format:"date" doc:"The day they were entered in the company's register of registrable controllers: within 7 days." example:"2027-03-12"`
	CentralRegisterOn string                  `json:"central_register_on,omitempty" format:"date" doc:"The day ACRA's central register was updated: within 2 business days after the company's." example:"2027-03-15"`
	CeasedOn          string                  `json:"ceased_on,omitempty" format:"date" doc:"The day they ceased to be a controller." example:"2028-01-31"`
	Notices           []ControllerNoticeDraft `json:"notices" nullable:"false" maxItems:"50" doc:"The yearly notices sent and the replies, oldest first."`
	Note              string                  `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Confirmed by the company secretary"`
}

// ControllerEntryNew is a new controller entry: its holder and the conclusion.
type ControllerEntryNew struct {
	HolderID string `json:"holder_id" format:"uuid" doc:"The holder: a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	ControllerEntryDraft
}

// ControllerEntryView is a controller entry, drafted or recorded.
type ControllerEntryView struct {
	ID         string               `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c01"`
	HolderID   string               `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName string               `json:"holder_name" doc:"Their name." example:"Mei Lin Tan"`
	Status     string               `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for the company secretary's confirmation and approval; recorded: in force; superseded: a later entry is in force." example:"recorded"`
	Version    int                  `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Entry      ControllerEntryDraft `json:"entry" doc:"The conclusion."`
	Evidence   int                  `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, the company secretary's confirmation." example:"1"`
	ApprovalID string               `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c11"`
	RecordedBy *Person              `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time           `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2027-03-12T09:30:00Z"`
	Checks     []BillCheck          `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no confirmation is attached."`
}

// HolderName is a holder named by their legal name.
type HolderName string

// Schema implements huma.SchemaProvider.
func (HolderName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A holder's legal name.", Examples: []any{"Lion Rock Ventures Pte. Ltd."}}
}

// ControllerTestView is a holder the controller test names today, with what the register still needs (FR-EQ-25).
type ControllerTestView struct {
	HolderID           string               `json:"holder_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName         string               `json:"holder_name" doc:"Their name." example:"Mei Lin Tan"`
	Shares             bool                 `json:"shares" doc:"More than 25% of the shares issued." example:"true"`
	Votes              bool                 `json:"votes" doc:"More than 25% of the votes." example:"true"`
	Ownership          string               `json:"ownership,omitempty" doc:"Their percentage of the shares issued today (RND-DISPLAY)." example:"42.19"`
	Voting             string               `json:"voting,omitempty" doc:"Their percentage of the votes today." example:"42.19"`
	Since              string               `json:"since,omitempty" format:"date" doc:"The first day of their latest run over 25%." example:"2025-01-02"`
	Rights             bool                 `json:"rights" doc:"Their recorded rights could confer control (a board majority, consent matters): a person weighs them." example:"false"`
	Standing           string               `json:"standing" enum:"to_enter,to_lodge,registered,not_assessed,not_a_controller" doc:"to_enter: not yet in the company's register; to_lodge: entered, ACRA's central register not updated; registered: both; not_assessed: rights to weigh and no conclusion recorded; not_a_controller: concluded not to be one." example:"registered"`
	CompanyRegisterDue string               `json:"company_register_due,omitempty" format:"date" doc:"The day the company's register is due: 7 days after they became one." example:"2025-01-09"`
	CentralRegisterDue string               `json:"central_register_due,omitempty" format:"date" doc:"The day ACRA's central register is due: 2 business days after the company's, weekends left out (public holidays are not, so it is never later than the law's) [verify]." example:"2025-01-13"`
	NoticeDue          string               `json:"notice_due,omitempty" format:"date" doc:"A registered controller's yearly notice: due a year after the last one sent, or after they were entered." example:"2026-01-02"`
	Entry              *ControllerEntryView `json:"entry,omitempty" doc:"The entry in force."`
}

// ExemptTestView is the register's part of the exempt-private-company test today (FR-EQ-25, FR-EQ-11).
type ExemptTestView struct {
	Members  int          `json:"members" doc:"Holders of shares issued." example:"2"`
	Entities []HolderName `json:"entities" nullable:"false" doc:"Members whose holder record says entity: any ends exempt status."`
	Unknown  []HolderName `json:"unknown" nullable:"false" doc:"Members with no holder record recorded: the test cannot tell until each has one."`
	Decided  bool         `json:"decided" doc:"Whether the register decides it: more than 20 members or an entity does; otherwise every member's record must say individual." example:"true"`
	Exempt   bool         `json:"exempt" doc:"When decided: the register keeps the company exempt (at most 20 members, none an entity) [accountant to confirm the Act's exceptions]." example:"true"`
	Recorded *bool        `json:"recorded,omitempty" doc:"The exempt status recorded in force today, from the ACRA business profile (FR-EQ-11); absent when none is recorded." example:"true"`
	Differs  bool         `json:"differs" doc:"The register decides otherwise than the status recorded: record the new status, and re-check the directors' accounts in debit (C.2 #28)." example:"false"`
}

// StatutoryTests are the cap table's statutory tests today (FR-EQ-25): who the register names as registrable
// controllers, and whether it keeps the company exempt.
type StatutoryTests struct {
	At          string                `json:"at" format:"date" doc:"Today, the day tested." example:"2026-11-07"`
	Controllers []ControllerTestView  `json:"controllers" nullable:"false" doc:"Each holder over 25% of the shares or votes, or with rights that could confer control, by name."`
	Lapsed      []ControllerEntryView `json:"lapsed" nullable:"false" doc:"Controllers in force whom the test no longer names, such as one whose shares fell to 25% or less: record the day they ceased, unless other grounds keep them one."`
	Exempt      ExemptTestView        `json:"exempt" doc:"The exempt-private-company test."`
}

type (
	InvestorRightsPath struct {
		RightsID string `path:"rights_id" format:"uuid" doc:"The investor rights." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7b01"`
	}
	ControllerEntryPath struct {
		EntryID string `path:"entry_id" format:"uuid" doc:"The controller entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c01"`
	}
	rightsDraftInput struct {
		KeyHeader
		PreferHeader
		Body InvestorRightsNew
	}
	rightsReviseInput struct {
		InvestorRightsPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body InvestorRightsDraft
	}
	rightsAskInput struct {
		InvestorRightsPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	rightsReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body InvestorRightsView
	}
	rightsWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     InvestorRightsView
	}
	controllerDraftInput struct {
		KeyHeader
		PreferHeader
		Body ControllerEntryNew
	}
	controllerReviseInput struct {
		ControllerEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body ControllerEntryDraft
	}
	controllerAskInput struct {
		ControllerEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	controllerReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body ControllerEntryView
	}
	controllerWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ControllerEntryView
	}
	statutoryOutput struct{ Body StatutoryTests }
)

// dayIn reads an optional day of a request, or what is wrong with it.
func dayIn(field, s string, fields *[]FieldProblem) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		*fields = append(*fields, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a date", Fix: "Send it as YYYY-MM-DD"})
		return nil
	}
	return &d
}

func dayOut(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format(time.DateOnly)
}

// rightsIn reads investor rights' fields, or what of them is wrong.
func rightsIn(holder string, d InvestorRightsDraft) (equity.InvestorRights, error) {
	r := equity.InvestorRights{BoardSeats: d.BoardSeats, Observer: d.Observer, BoardMajority: d.BoardMajority, Information: strings.TrimSpace(d.Information),
		ProRata: d.ProRata, Consents: []string{}, Agreements: strings.TrimSpace(d.Agreements), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	if holder != "" {
		id, err := uuid.Parse(holder)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.holder_id", Message: "is not a contact's ID", Fix: "Send the holder's contact ID"})
		}
		r.HolderID = id
	}
	if from := dayIn("body.from", d.From, &fields); from != nil {
		r.From = *from
	}
	for _, c := range d.Consents {
		r.Consents = append(r.Consents, strings.TrimSpace(string(c)))
	}
	if len(fields) > 0 {
		return r, invalid(fields)
	}
	return r, nil
}

func rightsDraftOf(r equity.InvestorRights) InvestorRightsDraft {
	out := InvestorRightsDraft{From: r.From.Format(time.DateOnly), BoardSeats: r.BoardSeats, Observer: r.Observer, BoardMajority: r.BoardMajority,
		Information: r.Information, ProRata: r.ProRata, Consents: []ConsentMatter{}, Agreements: r.Agreements, Note: r.Note}
	for _, c := range r.Consents {
		out.Consents = append(out.Consents, ConsentMatter(c))
	}
	return out
}

// controllerIn reads a controller entry's fields, or what of them is wrong.
func controllerIn(holder string, d ControllerEntryDraft) (equity.ControllerEntry, error) {
	c := equity.ControllerEntry{Controller: d.Controller, Grounds: []string{}, Reason: strings.TrimSpace(d.Reason), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	if holder != "" {
		id, err := uuid.Parse(holder)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.holder_id", Message: "is not a contact's ID", Fix: "Send the holder's contact ID"})
		}
		c.HolderID = id
	}
	for _, g := range d.Grounds {
		c.Grounds = append(c.Grounds, string(g))
	}
	if since := dayIn("body.since", d.Since, &fields); since != nil {
		c.Since = *since
	}
	c.CompanyRegister = dayIn("body.company_register_on", d.CompanyRegisterOn, &fields)
	c.CentralRegister = dayIn("body.central_register_on", d.CentralRegisterOn, &fields)
	c.Ceased = dayIn("body.ceased_on", d.CeasedOn, &fields)
	for k, n := range d.Notices {
		x := equity.ControllerNotice{Replied: dayIn(fmt.Sprintf("body.notices[%d].replied_on", k), n.RepliedOn, &fields)}
		if sent := dayIn(fmt.Sprintf("body.notices[%d].sent_on", k), n.SentOn, &fields); sent != nil {
			x.Sent = *sent
		}
		c.Notices = append(c.Notices, x)
	}
	if len(fields) > 0 {
		return c, invalid(fields)
	}
	return c, nil
}

func controllerDraftOf(c equity.ControllerEntry) ControllerEntryDraft {
	out := ControllerEntryDraft{Controller: c.Controller, Grounds: []ControllerGround{}, Reason: c.Reason, Since: c.Since.Format(time.DateOnly),
		CompanyRegisterOn: dayOut(c.CompanyRegister), CentralRegisterOn: dayOut(c.CentralRegister), CeasedOn: dayOut(c.Ceased),
		Notices: []ControllerNoticeDraft{}, Note: c.Note}
	for _, g := range c.Grounds {
		out.Grounds = append(out.Grounds, ControllerGround(g))
	}
	for _, n := range c.Notices {
		out.Notices = append(out.Notices, ControllerNoticeDraft{SentOn: n.Sent.Format(time.DateOnly), RepliedOn: dayOut(n.Replied)})
	}
	return out
}

// heldView is what the rights and controller entries read: every version of each, by holder, the holders' names and the
// requests waiting.
type heldView struct {
	rights      map[uuid.UUID][]equity.Recorded[equity.InvestorRights]
	controllers map[uuid.UUID][]equity.Recorded[equity.ControllerEntry]
	names       map[uuid.UUID]string
	asked       map[string]string
}

func (a *API) heldViewOf(ctx context.Context, u storage.UnitOfWork) (heldView, error) {
	v := heldView{names: map[uuid.UUID]string{}, asked: map[string]string{}}
	var err error
	if v.rights, err = equity.RightsOf(ctx, u); err != nil {
		return v, err
	}
	if v.controllers, err = equity.ControllersOf(ctx, u); err != nil {
		return v, err
	}
	for _, o := range []string{approval.ObjectInvestorRights, approval.ObjectControllerEntry} {
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

func (v heldView) nameOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (string, error) {
	if n, ok := v.names[id]; ok {
		return n, nil
	}
	c, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return "", err
	}
	v.names[id] = c.LegalName
	return c.LegalName, nil
}

func inForceID[T any](versions []equity.Recorded[T]) *uuid.UUID {
	if cur := equity.InForce(versions); cur != nil {
		return &cur.Doc.ID
	}
	return nil
}

func (a *API) rightsViewOf(ctx context.Context, u storage.UnitOfWork, v heldView, x equity.Recorded[equity.InvestorRights]) (InvestorRightsView, error) {
	name, err := v.nameOf(ctx, u, x.Value.HolderID)
	if err != nil {
		return InvestorRightsView{}, err
	}
	out := InvestorRightsView{ID: x.Doc.ID.String(), HolderID: x.Value.HolderID.String(), HolderName: name,
		Status: recordStatus(x.Doc, inForceID(v.rights[x.Value.HolderID])), Version: x.Doc.Version, Rights: rightsDraftOf(x.Value),
		ConfersControl: x.Value.ConfersControl()}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the agreements the rights come from, such as the shareholders' agreement or a side letter"
		}
	}
	return out, err
}

func (a *API) controllerViewOf(ctx context.Context, u storage.UnitOfWork, v heldView, x equity.Recorded[equity.ControllerEntry]) (ControllerEntryView, error) {
	name, err := v.nameOf(ctx, u, x.Value.HolderID)
	if err != nil {
		return ControllerEntryView{}, err
	}
	out := ControllerEntryView{ID: x.Doc.ID.String(), HolderID: x.Value.HolderID.String(), HolderName: name,
		Status: recordStatus(x.Doc, inForceID(v.controllers[x.Value.HolderID])), Version: x.Doc.Version, Entry: controllerDraftOf(x.Value)}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, v.asked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the company secretary's confirmation, with the register's extract or ACRA's acknowledgement"
		}
	}
	return out, err
}

// shownRights is investor rights by document ID, as the caller reads them: not found for anything else.
func (a *API) shownRights(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (InvestorRightsView, error) {
	v, err := a.heldViewOf(ctx, u)
	if err != nil {
		return InvestorRightsView{}, err
	}
	for _, versions := range v.rights {
		for _, x := range versions {
			if x.Doc.ID == id {
				return a.rightsViewOf(ctx, u, v, x)
			}
		}
	}
	return InvestorRightsView{}, ports.ErrNotFound
}

// shownController is a controller entry by document ID, as the caller reads it.
func (a *API) shownController(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ControllerEntryView, error) {
	v, err := a.heldViewOf(ctx, u)
	if err != nil {
		return ControllerEntryView{}, err
	}
	for _, versions := range v.controllers {
		for _, x := range versions {
			if x.Doc.ID == id {
				return a.controllerViewOf(ctx, u, v, x)
			}
		}
	}
	return ControllerEntryView{}, ports.ErrNotFound
}

// heldHistory are a holder's rights and controller entries, the latest first, and those in force.
func (a *API) heldHistory(ctx context.Context, u storage.UnitOfWork, holder uuid.UUID) (rights []InvestorRightsView, inRights *InvestorRightsView,
	entries []ControllerEntryView, inEntry *ControllerEntryView, err error) {
	v, err := a.heldViewOf(ctx, u)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	rights, entries = []InvestorRightsView{}, []ControllerEntryView{}
	versions := v.rights[holder]
	for i := len(versions) - 1; i >= 0; i-- {
		x, err := a.rightsViewOf(ctx, u, v, versions[i])
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if x.Status == "recorded" {
			xx := x
			inRights = &xx
		}
		rights = append(rights, x)
	}
	cv := v.controllers[holder]
	for i := len(cv) - 1; i >= 0; i-- {
		x, err := a.controllerViewOf(ctx, u, v, cv[i])
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if x.Status == "recorded" {
			xx := x
			inEntry = &xx
		}
		entries = append(entries, x)
	}
	return rights, inRights, entries, inEntry, nil
}

// heldRecordDependencies are investor rights' or a controller entry's envelope values: their status and their holder.
func heldRecordDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	holder, err := heldHolder(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "holder": holder.String()}, nil
}

func heldHolder(doc storage.Document) (uuid.UUID, error) {
	switch doc.Type {
	case storage.DocumentInvestorRights:
		r, err := equity.ReadRights(doc)
		return r.HolderID, err
	case storage.DocumentControllerEntry:
		c, err := equity.ReadController(doc)
		return c.HolderID, err
	}
	return uuid.Nil, fmt.Errorf("document %s is a %s, not investor rights or a controller entry", doc.ID, doc.Type)
}

// heldRecordExecutable refuses the holder themselves (FM-1): another person records their rights or their entry.
func heldRecordExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	holder, err := heldHolder(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, holder)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeNotApprover, "These are your own rights or your own entry as a controller, so another person approves them (FM-1).",
			"Ask another person who holds the approval right")
	}
	return nil
}

func rightsAnswer(w written[InvestorRightsView]) *rightsWriteOutput {
	if w.Previewed {
		return &rightsWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &rightsWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/investor-rights/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func controllerAnswer(w written[ControllerEntryView]) *controllerWriteOutput {
	if w.Previewed {
		return &controllerWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &controllerWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/controller-entries/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// statutoryTestsIn are the cap table's statutory tests on today, as the API shows them and C.2 #27 and #28 read them.
func (a *API) statutoryTestsIn(ctx context.Context, u storage.UnitOfWork, today time.Time) (StatutoryTests, error) {
	out := StatutoryTests{At: today.Format(time.DateOnly), Controllers: []ControllerTestView{}, Exempt: ExemptTestView{Entities: []HolderName{}, Unknown: []HolderName{}}}
	t, err := equity.StatutoryTestsIn(ctx, u, today)
	if err != nil {
		return out, err
	}
	v, err := a.heldViewOf(ctx, u)
	if err != nil {
		return out, err
	}
	for _, f := range t.Controllers {
		name, err := v.nameOf(ctx, u, f.HolderID)
		if err != nil {
			return out, err
		}
		x := ControllerTestView{HolderID: f.HolderID.String(), HolderName: name, Shares: f.Shares, Votes: f.Votes, Ownership: f.Ownership,
			Voting: f.Voting, Rights: f.Rights, Standing: f.Standing, NoticeDue: dayOut(f.NoticeDue)}
		if !f.Since.IsZero() {
			x.Since = f.Since.Format(time.DateOnly)
		}
		x.CompanyRegisterDue, x.CentralRegisterDue = dayOut(f.CompanyRegisterDue), dayOut(f.CentralRegisterDue)
		if f.Entry != nil {
			e, err := a.controllerViewOf(ctx, u, v, *f.Entry)
			if err != nil {
				return out, err
			}
			x.Entry = &e
		}
		out.Controllers = append(out.Controllers, x)
	}
	out.Lapsed = []ControllerEntryView{}
	for _, x := range t.Lapsed {
		e, err := a.controllerViewOf(ctx, u, v, x)
		if err != nil {
			return out, err
		}
		out.Lapsed = append(out.Lapsed, e)
	}
	slices.SortStableFunc(out.Controllers, func(x, y ControllerTestView) int {
		return strings.Compare(strings.ToLower(x.HolderName), strings.ToLower(y.HolderName))
	})
	e := t.Exempt
	out.Exempt.Members, out.Exempt.Decided, out.Exempt.Exempt, out.Exempt.Differs = e.Finding.Members, e.Finding.Decided, e.Finding.Exempt, e.Differs
	if e.Recorded != nil {
		r := e.Recorded.Status.Exempt
		out.Exempt.Recorded = &r
	}
	for _, list := range []struct {
		from []uuid.UUID
		to   *[]HolderName
	}{{e.Finding.Entities, &out.Exempt.Entities}, {e.Finding.Unknown, &out.Exempt.Unknown}} {
		for _, id := range list.from {
			name, err := v.nameOf(ctx, u, id)
			if err != nil {
				return out, err
			}
			*list.to = append(*list.to, HolderName(name))
		}
		slices.Sort(*list.to)
	}
	return out, nil
}

func registerHolderRights(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	ask := append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), RightsCodes...)

	register(a, authz.CreateInvestorRights, huma.Operation{
		OperationID: "createInvestorRights", Method: http.MethodPost, Path: "/v1/investor-rights", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a holder's rights from the agreements",
		Description: "Drafts a holder's rights from the agreements (FR-EQ-22, design D10), recorded and never computed: the directors " +
			"they may appoint, an observer, whether they appoint or remove a board majority, information rights, pro-rata and the matters " +
			"that need their consent. Attach the agreements with addInvestorRightsEvidence, then ask another person to approve them " +
			"with requestInvestorRightsApproval: recorded, they supersede the rights before. A board majority or consent matters make " +
			"the holder one the controller test names for a person to weigh (FR-EQ-25). Side effects: the draft, audit events. Every " +
			"problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), RightsCodes...), func(ctx context.Context, in *rightsDraftInput) (*rightsWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvestorRightsView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return InvestorRightsView{}, err
			}
			r, err := rightsIn(in.Body.HolderID, in.Body.InvestorRightsDraft)
			if err != nil {
				return InvestorRightsView{}, err
			}
			doc, err := equity.DraftRightsIn(ctx, u, r, today)
			if err != nil {
				return InvestorRightsView{}, equityProblems(err)
			}
			return a.shownRights(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return rightsAnswer(w), nil
	})

	register(a, authz.GetInvestorRights, huma.Operation{
		OperationID: "getInvestorRights", Method: http.MethodGet, Path: "/v1/investor-rights/{rights_id}", Tags: []string{"Equity"},
		Summary:     "Get a holder's rights from the agreements",
		Description: "Returns investor rights with their ETag: the rights, whether they could confer control, their status, files and the approval waiting (FR-EQ-22). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *InvestorRightsPath) (*rightsReadOutput, error) {
		id, err := parseID("rights_id", in.RightsID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out rightsReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentInvestorRights, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownRights(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceInvestorRights, huma.Operation{
		OperationID: "replaceInvestorRights", Method: http.MethodPut, Path: "/v1/investor-rights/{rights_id}", Tags: []string{"Equity"},
		Summary: "Change a holder's drafted rights",
		Description: "Saves a new version of investor rights not recorded yet (FR-EQ-22); their holder stays. Recorded ones change no more " +
			"(EQUITY-05): draft new rights to supersede them. Side effects: the new version, audit events. Needs If-Match with its ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), RightsCodes...), func(ctx context.Context, in *rightsReviseInput) (*rightsWriteOutput, error) {
		id, err := parseID("rights_id", in.RightsID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvestorRightsView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentInvestorRights, id)
			if err != nil {
				return InvestorRightsView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return InvestorRightsView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return InvestorRightsView{}, err
			}
			r, err := rightsIn("", in.Body)
			if err != nil {
				return InvestorRightsView{}, err
			}
			if _, err := equity.ReviseRightsIn(ctx, u, id, r, today); err != nil {
				return InvestorRightsView{}, equityProblems(err)
			}
			return a.shownRights(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return rightsAnswer(w), nil
	})

	register(a, authz.RequestInvestorRightsApproval, huma.Operation{
		OperationID: "requestInvestorRightsApproval", Method: http.MethodPost, Path: "/v1/investor-rights/{rights_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a holder's rights",
		Description: "Asks another person to approve drafted investor rights at the version read (FR-EQ-22, FM-1). It needs the agreements " +
			"attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A on \"Cap table, holders, " +
			"investors, equity transactions\" approves, stepped up, never the holder nor the one who asked. Needs a person signed in who " +
			"passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *rightsAskInput) (*equityAskOutput, error) {
		id, err := parseID("rights_id", in.RightsID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentInvestorRights, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the rights have no agreement attached",
					Fix: "Attach the agreements the rights come from, then ask again"}
			}
			r, err := equity.ReadRights(doc)
			if err != nil {
				return Approval{}, err
			}
			c, err := u.Contacts().Get(ctx, r.HolderID)
			if err != nil {
				return Approval{}, err
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "board_seats", After: strconv.Itoa(r.BoardSeats)},
				{Field: "board_majority", After: strconv.FormatBool(r.BoardMajority)}, {Field: "consents", After: strings.Join(r.Consents, "; ")},
				{Field: "pro_rata", After: strconv.FormatBool(r.ProRata)}, {Field: "from", After: r.From.Format(time.DateOnly)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectInvestorRights, ObjectKey: doc.ID.String(),
				Operation: approval.OpInvestorRightsRecord, Summary: "Record " + c.LegalName + "'s rights from " + r.Agreements,
				Execute: map[string]any{"rights_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.CreateControllerEntry, huma.Operation{
		OperationID: "createControllerEntry", Method: http.MethodPost, Path: "/v1/controller-entries", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a registrable controller entry",
		Description: "Drafts the company's conclusion about a holder (FR-EQ-25, design D10), as its register of registrable controllers " +
			"records it: a controller since a day, on their grounds (more than 25% of the shares or votes, a board majority, significant " +
			"influence or control), entered in the company's register within 7 days and lodged in ACRA's central register within 2 " +
			"business days after, with the yearly notices sent and answered; or not a controller, with the reason. Attach the company " +
			"secretary's confirmation with addControllerEntryEvidence, then ask another person to approve it with " +
			"requestControllerEntryApproval. Side effects: the draft, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), RightsCodes...), func(ctx context.Context, in *controllerDraftInput) (*controllerWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ControllerEntryView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ControllerEntryView{}, err
			}
			c, err := controllerIn(in.Body.HolderID, in.Body.ControllerEntryDraft)
			if err != nil {
				return ControllerEntryView{}, err
			}
			doc, err := equity.DraftControllerIn(ctx, u, c, today)
			if err != nil {
				return ControllerEntryView{}, equityProblems(err)
			}
			return a.shownController(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return controllerAnswer(w), nil
	})

	register(a, authz.GetControllerEntry, huma.Operation{
		OperationID: "getControllerEntry", Method: http.MethodGet, Path: "/v1/controller-entries/{entry_id}", Tags: []string{"Equity"},
		Summary:     "Get a registrable controller entry",
		Description: "Returns a controller entry with its ETag: the conclusion, the register days and notices, its status, files and the approval waiting (FR-EQ-25). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *ControllerEntryPath) (*controllerReadOutput, error) {
		id, err := parseID("entry_id", in.EntryID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out controllerReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentControllerEntry, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownController(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceControllerEntry, huma.Operation{
		OperationID: "replaceControllerEntry", Method: http.MethodPut, Path: "/v1/controller-entries/{entry_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted registrable controller entry",
		Description: "Saves a new version of a controller entry not recorded yet (FR-EQ-25); its holder stays. A recorded one changes no " +
			"more (EQUITY-05): draft a new entry to add a register day, a notice or the day they ceased. Side effects: the new version, " +
			"audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), RightsCodes...), func(ctx context.Context, in *controllerReviseInput) (*controllerWriteOutput, error) {
		id, err := parseID("entry_id", in.EntryID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ControllerEntryView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentControllerEntry, id)
			if err != nil {
				return ControllerEntryView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return ControllerEntryView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ControllerEntryView{}, err
			}
			c, err := controllerIn("", in.Body)
			if err != nil {
				return ControllerEntryView{}, err
			}
			if _, err := equity.ReviseControllerIn(ctx, u, id, c, today); err != nil {
				return ControllerEntryView{}, equityProblems(err)
			}
			return a.shownController(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return controllerAnswer(w), nil
	})

	register(a, authz.RequestControllerEntryApproval, huma.Operation{
		OperationID: "requestControllerEntryApproval", Method: http.MethodPost, Path: "/v1/controller-entries/{entry_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a registrable controller entry",
		Description: "Asks another person to approve a drafted controller entry at the version read (FR-EQ-25, FM-1). It needs the company " +
			"secretary's confirmation attached (EQUITY-06). The answer is 202 with an approval request, which a person who holds A on " +
			"\"Cap table, holders, investors, equity transactions\" approves, stepped up, never the holder nor the one who asked. Needs a " +
			"person signed in who passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, ask, func(ctx context.Context, in *controllerAskInput) (*equityAskOutput, error) {
		id, err := parseID("entry_id", in.EntryID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentControllerEntry, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return Approval{}, err
			}
			if n, err := attachedFiles(ctx, u, id); err != nil {
				return Approval{}, err
			} else if n == 0 {
				return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the entry has no confirmation attached",
					Fix: "Attach the company secretary's confirmation, then ask again"}
			}
			x, err := equity.ReadController(doc)
			if err != nil {
				return Approval{}, err
			}
			c, err := u.Contacts().Get(ctx, x.HolderID)
			if err != nil {
				return Approval{}, err
			}
			summary := "Record " + c.LegalName + " as a registrable controller"
			if !x.Controller {
				summary = "Record that " + c.LegalName + " is not a registrable controller"
			}
			diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "controller", After: strconv.FormatBool(x.Controller)},
				{Field: "grounds", After: strings.Join(x.Grounds, ", ")}, {Field: "since", After: x.Since.Format(time.DateOnly)},
				{Field: "company_register_on", After: dayOut(x.CompanyRegister)}, {Field: "central_register_on", After: dayOut(x.CentralRegister)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectControllerEntry, ObjectKey: doc.ID.String(),
				Operation: approval.OpControllerEntryRecord, Summary: summary,
				Execute: map[string]any{"entry_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.GetStatutoryTests, huma.Operation{
		OperationID: "getStatutoryTests", Method: http.MethodGet, Path: "/v1/cap-table/statutory-tests", Tags: []string{"Equity"},
		Summary: "Get the cap table's statutory tests",
		Description: "The cap table's statutory tests today (FR-EQ-25, design D10). The controller test: each holder over 25% of the " +
			"shares issued or of the votes, from the first day of their latest run over it, with the company's register due 7 days " +
			"after and ACRA's central register 2 business days after that; and each holder whose recorded rights could confer control, " +
			"not assessed until a person's conclusion is recorded (G4-101). The exempt-private-company test: more than 20 members or a " +
			"member recorded as an entity ends exempt status, against the status recorded (FR-EQ-11). Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*statutoryOutput, error) {
		p, _ := authn.From(ctx)
		var out statutoryOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			out.Body, err = a.statutoryTestsIn(ctx, u, today)
			return err
		})
		return &out, err
	})
}
