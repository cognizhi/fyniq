package api

import (
	"context"
	"errors"
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
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ holders and share classes (FR-EQ-15, FR-EQ-16)
//
// The equity register's holders are contacts marked shareholder or investor. A holder's details are a holder record,
// and a share class with its rights a share class version: each is drafted, takes its evidence (an identity document;
// the constitution and the shareholders' agreement) and is recorded on another person's approval (FM-1), never the
// holder's own; a later one supersedes it (p1-equity-register design D1, D2). An individual's identifier is kept as its
// type and last 4 characters only (the owner's decision of 7 Nov 2026, rev 0.203).

// EquityRecordCodes are the codes a holder record's or share class's own rules answer.
var EquityRecordCodes = []string{equity.CodeNotRecord, equity.CodeHolderRecord, equity.CodeShareClass, equity.CodeRecorded, storage.CodeEquityRecord}

// HolderShares are a holder's shares in one class, from the register of share issues.
type HolderShares struct {
	ClassID     string `json:"class_id,omitempty" format:"uuid" doc:"The share class, once its account has a recorded class." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	ClassName   string `json:"class_name" doc:"The class's name, or its account's while it has none." example:"Ordinary shares"`
	AccountCode string `json:"account_code" doc:"Its share-capital account: 3010 ordinary, 3020 preference." example:"3010"`
	Shares      string `json:"shares" pattern:"^[0-9]+$" doc:"The shares held, a whole number." example:"600000"`
}

// Holder is a contact the equity register holds (FR-EQ-15).
type Holder struct {
	ContactID   string         `json:"contact_id" format:"uuid" doc:"The holder's contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Name        string         `json:"name" doc:"The holder's legal name." example:"Mei Lin Tan"`
	Shareholder bool           `json:"shareholder" doc:"Marked shareholder." example:"true"`
	Investor    bool           `json:"investor" doc:"Marked investor: a SAFE, note or option holder." example:"false"`
	Director    bool           `json:"director" doc:"Also a director." example:"true"`
	Kind        string         `json:"kind,omitempty" enum:"individual,entity" doc:"From the record in force; absent before one is recorded." example:"individual"`
	Identifier  string         `json:"identifier,omitempty" doc:"From the record in force: an individual's identifier type and last 4 characters, or an entity's number." example:"NRIC ending 567A"`
	Recorded    bool           `json:"recorded" doc:"Whether a holder record is in force." example:"true"`
	Holdings    []HolderShares `json:"holdings" nullable:"false" doc:"The shares held, by class, from the register of share issues."`
	Pending     string         `json:"pending_record_id,omitempty" format:"uuid" doc:"A drafted record not recorded yet." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e21"`
}

// HolderPage is the holders, by name.
type HolderPage struct {
	Items      []Holder `json:"items" nullable:"false"`
	NextCursor string   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Ik1laSJ9"`
}

// ContactPersonView is someone to contact at a holder.
type ContactPersonView struct {
	Name  string `json:"name" maxLength:"100" example:"Jane Lim"`
	Role  string `json:"role,omitempty" maxLength:"60" example:"Company secretary"`
	Email string `json:"email,omitempty" maxLength:"254" example:"jane@example.com"`
}

// HolderRecordNew is a new holder record: its holder and its details.
type HolderRecordNew struct {
	ContactID string `json:"contact_id" format:"uuid" doc:"The holder: a contact marked shareholder or investor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderRecordDraft
}

// HolderRecordDraft is a holder's details as drafted.
type HolderRecordDraft struct {
	Kind               string              `json:"kind" enum:"individual,entity" doc:"An individual or an entity." example:"individual"`
	IDType             string              `json:"id_type,omitempty" enum:"nric,fin,passport,uen,foreign_registration" doc:"An individual's NRIC, FIN or passport; an entity's UEN or foreign registration." example:"nric"`
	IDLast4            string              `json:"id_last4,omitempty" maxLength:"4" doc:"An individual's identifier: its last 4 characters only. The full number stays in ACRA's register and the identity document (FR-EQ-15)." example:"567A"`
	RegistrationNumber string              `json:"registration_number,omitempty" maxLength:"30" doc:"An entity's UEN or foreign registration number." example:"202312345K"`
	Nationality        string              `json:"nationality,omitempty" maxLength:"60" doc:"An individual's nationality, or an entity's place of incorporation." example:"Singapore"`
	Address            string              `json:"address,omitempty" maxLength:"500" doc:"The registered address." example:"1 Raffles Place, #20-01, Singapore 048616"`
	ContactPeople      []ContactPersonView `json:"contact_people,omitempty" maxItems:"10" doc:"The people to contact at the holder."`
	Note               string              `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Founder; subscriber at incorporation"`
}

// HolderRecordView is a holder record, drafted or recorded.
type HolderRecordView struct {
	ID         string              `json:"id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e21"`
	ContactID  string              `json:"contact_id" format:"uuid" doc:"The holder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Status     string              `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its identity document and approval; recorded: in force; superseded: a later record is in force." example:"recorded"`
	Version    int                 `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Masked     bool                `json:"masked,omitempty" doc:"true when the caller reads it masked (Appendix D's M): the identifier, address, contact people and note are left out." example:"false"`
	Draft      HolderRecordDraft   `json:"details" doc:"The holder's details."`
	Evidence   int                 `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, such as the identity document." example:"1"`
	ApprovalID string              `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e31"`
	RecordedBy *Person             `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time          `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-07T09:30:00Z"`
	Checks     []BillCheck         `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
	People     []ContactPersonView `json:"-"`
}

// HolderDetail is a holder with its record in force, its records' history and its share issues.
type HolderDetail struct {
	Holder
	Record  *HolderRecordView  `json:"record,omitempty" doc:"The record in force."`
	History []HolderRecordView `json:"history" nullable:"false" doc:"Every record, drafts and superseded ones included, the latest first."`
	Issues  []ShareIssue       `json:"issues" nullable:"false" doc:"The holder's share issues, earliest effective date first."`
}

// Liquidation and dividend rights as the API shows them.
type (
	LiquidationRight struct {
		Multiple      string `json:"multiple,omitempty" doc:"Times the issue price, such as 1." example:"1"`
		Participating bool   `json:"participating,omitempty" doc:"Whether it also shares in what is left." example:"false"`
		CapMultiple   string `json:"cap_multiple,omitempty" doc:"A participating preference's cap, as a multiple." example:"3"`
		Seniority     int    `json:"seniority,omitempty" minimum:"0" maximum:"20" doc:"1 is paid first; 0 when it has no preference." example:"1"`
	}
	DividendRight struct {
		Kind string `json:"kind" enum:"none,non_cumulative,cumulative" doc:"The dividend right." example:"none"`
		Rate string `json:"rate,omitempty" doc:"A percentage a year." example:"8"`
	}
)

// ShareClassDraft is a share class and its rights as drafted (FR-EQ-16). The rights are recorded, never computed, in P1.
type ShareClassDraft struct {
	ClassID         string           `json:"class_id,omitempty" format:"uuid" doc:"A recorded class whose rights this version supersedes; left out for a new class." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Name            string           `json:"name" maxLength:"100" doc:"The class's name, as the constitution gives it." example:"Ordinary shares"`
	Kind            string           `json:"kind" enum:"ordinary,preference" doc:"Ordinary or preference." example:"ordinary"`
	Series          string           `json:"series,omitempty" maxLength:"20" doc:"A preference series, such as A." example:"A"`
	AccountID       string           `json:"account_id" format:"uuid" doc:"Its share-capital account: 3010 ordinary or 3020 preference. A class keeps its account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	VotesPerShare   string           `json:"votes_per_share" doc:"The votes each share carries, a number of zero or more." example:"1"`
	Liquidation     LiquidationRight `json:"liquidation" doc:"The liquidation preference."`
	Dividend        DividendRight    `json:"dividend" doc:"The dividend right."`
	ConversionRatio string           `json:"conversion_ratio,omitempty" doc:"Into ordinary shares, such as 1." example:"1"`
	AntiDilution    string           `json:"anti_dilution" enum:"none,broad_based,full_ratchet" doc:"The anti-dilution right: none, broad-based weighted average or full ratchet." example:"none"`
	ProRata         bool             `json:"pro_rata,omitempty" doc:"A pro-rata right in later rounds." example:"false"`
	Redemption      string           `json:"redemption,omitempty" maxLength:"500" doc:"The redemption terms, summarised." example:"None"`
	Note            string           `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"As adopted at incorporation"`
}

// ShareClassView is a share class version, drafted or recorded.
type ShareClassView struct {
	ShareClassDraft
	ID          string      `json:"id" format:"uuid" doc:"The class: its first version's ID, which share issues name." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	VersionID   string      `json:"version_id" format:"uuid" doc:"This version of its rights." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Status      string      `json:"status" enum:"draft,recorded,superseded" doc:"draft: waiting for its documents and approval; recorded: in force; superseded: a later version is in force." example:"recorded"`
	Version     int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	AccountCode string      `json:"account_code" doc:"Its account's code." example:"3010"`
	Evidence    int         `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, such as the constitution." example:"2"`
	ApprovalID  string      `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e41"`
	RecordedBy  *Person     `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt  *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-07T09:30:00Z"`
	Checks      []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// ShareClassPage is every class with its version in force, and every draft, by name.
type ShareClassPage struct {
	Items      []ShareClassView `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Ik9yZGluYXJ5In0"`
}

// Paths.
type (
	HolderPath struct {
		ContactID string `path:"contact_id" format:"uuid" doc:"The holder's contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	}
	HolderRecordPath struct {
		RecordID string `path:"record_id" format:"uuid" doc:"The holder record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e21"`
	}
	ShareClassPath struct {
		VersionID string `path:"version_id" format:"uuid" doc:"The share class version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	}
)

type (
	holderListInput  struct{ PageQuery }
	holderListOutput struct{ Body HolderPage }
	holderOutput     struct{ Body HolderDetail }
	holderDraftInput struct {
		KeyHeader
		PreferHeader
		Body HolderRecordNew
	}
	holderReviseInput struct {
		HolderRecordPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body HolderRecordDraft
	}
	holderAskInput struct {
		HolderRecordPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	holderRecordReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body HolderRecordView
	}
	holderRecordWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     HolderRecordView
	}
	classListInput  struct{ PageQuery }
	classListOutput struct{ Body ShareClassPage }
	classDraftInput struct {
		KeyHeader
		PreferHeader
		Body ShareClassDraft
	}
	classReviseInput struct {
		ShareClassPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body ShareClassDraft
	}
	classAskInput struct {
		ShareClassPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	classReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body ShareClassView
	}
	classWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ShareClassView
	}
	equityAskOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
		Body     Approval
	}
)

func recordETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// equityProblems turns a holder record's or share class's own problems into the request's fields, every one at once;
// other errors pass through.
func equityProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		field := "body." + x.Field
		fields = append(fields, FieldProblem{Code: x.Code, Field: field, Message: x.Message, Fix: x.Fix})
	}
	return invalid(fields)
}

// masked says whether the caller reads the cap table masked (Appendix D's M, the DPO).
func masked(ctx context.Context) bool {
	d, _ := ctx.Value(decisionKey{}).(authz.Decision)
	return d.Right == authz.Masked
}

// identifierText is a record's identifier as a list shows it.
func identifierText(r equity.HolderRecord) string {
	names := map[string]string{"nric": "NRIC", "fin": "FIN", "passport": "Passport", "uen": "UEN", "foreign_registration": "Registration"}
	switch {
	case r.Kind == "individual" && r.IDLast4 != "":
		return names[r.IDType] + " ending " + r.IDLast4
	case r.Kind == "entity" && r.Registration != "":
		return names[r.IDType] + " " + r.Registration
	}
	return ""
}

// waiting is the pending approval of each equity record of object type t, by its document ID.
func (a *API) waiting(ctx context.Context, u storage.UnitOfWork, t string) (map[string]string, error) {
	out := map[string]string{}
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return nil, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == t && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out[r.ObjectKey] = r.ID.String()
		}
	}
	return out, nil
}

// recordStatus is a version's status in its series: a draft, the one in force, or superseded.
func recordStatus(doc storage.Document, inForce *uuid.UUID) string {
	switch {
	case doc.Status == storage.DocumentDraft:
		return "draft"
	case inForce != nil && *inForce == doc.ID:
		return "recorded"
	}
	return "superseded"
}

// recordCommon fills what holder records and share classes show alike: evidence, the request waiting, who recorded it.
func (a *API) recordCommon(ctx context.Context, u storage.UnitOfWork, doc storage.Document, asked map[string]string) (evidence int, approvalID string,
	by *Person, at *time.Time, checks []BillCheck, err error) {
	checks = []BillCheck{}
	if evidence, err = attachedFiles(ctx, u, doc.ID); err != nil {
		return
	}
	approvalID = asked[doc.ID.String()]
	if doc.Status == storage.DocumentPosted {
		by, at = personOf(doc.PostedBy), doc.PostedAt
		err = a.name(ctx, u, by)
		return
	}
	if evidence == 0 {
		checks = append(checks, BillCheck{Severity: "error", Code: equity.CodeNoEvidence, Field: "evidence",
			Message: "it has no evidence attached, so it cannot be asked for approval", Fix: "Attach the identity document, or the constitution and the shareholders' agreement"})
	}
	return
}

func holderDraftOf(r equity.HolderRecord) HolderRecordDraft {
	out := HolderRecordDraft{Kind: r.Kind, IDType: r.IDType, IDLast4: r.IDLast4, RegistrationNumber: r.Registration, Nationality: r.Nationality,
		Address: r.Address, Note: r.Note}
	for _, p := range r.ContactPeople {
		out.ContactPeople = append(out.ContactPeople, ContactPersonView(p))
	}
	return out
}

func holderRecordIn(contact uuid.UUID, d HolderRecordDraft, today time.Time) equity.HolderRecord {
	r := equity.HolderRecord{ContactID: contact, Kind: d.Kind, IDType: d.IDType, IDLast4: strings.ToUpper(strings.TrimSpace(d.IDLast4)),
		Registration: strings.ToUpper(strings.TrimSpace(d.RegistrationNumber)), Nationality: strings.TrimSpace(d.Nationality),
		Address: strings.TrimSpace(d.Address), Note: strings.TrimSpace(d.Note), Date: today}
	for _, p := range d.ContactPeople {
		r.ContactPeople = append(r.ContactPeople, equity.ContactPerson{Name: strings.TrimSpace(p.Name), Role: strings.TrimSpace(p.Role), Email: strings.TrimSpace(p.Email)})
	}
	return r
}

// holderRecordOf is a holder record as the API shows it.
func (a *API) holderRecordOf(ctx context.Context, u storage.UnitOfWork, x equity.Recorded[equity.HolderRecord], inForce *uuid.UUID,
	asked map[string]string) (HolderRecordView, error) {
	out := HolderRecordView{ID: x.Doc.ID.String(), ContactID: x.Value.ContactID.String(), Status: recordStatus(x.Doc, inForce), Version: x.Doc.Version,
		Draft: holderDraftOf(x.Value)}
	var err error
	if out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, asked); err != nil {
		return out, err
	}
	if masked(ctx) {
		out.Masked = true
		out.Draft = HolderRecordDraft{Kind: x.Value.Kind, IDType: x.Value.IDType, Nationality: x.Value.Nationality}
	}
	return out, nil
}

// holderView reads every holder record and the register's issues once, for lists and records.
type holderView struct {
	records map[uuid.UUID][]equity.Recorded[equity.HolderRecord]
	issues  map[uuid.UUID][]storage.ShareIssue
	moves   map[uuid.UUID][]storage.HoldingMovement
	classes map[uuid.UUID][]equity.Recorded[equity.ShareClass]
	codes   map[uuid.UUID]string
	asked   map[string]string
}

func (a *API) holderViewOf(ctx context.Context, u storage.UnitOfWork) (holderView, error) {
	v := holderView{issues: map[uuid.UUID][]storage.ShareIssue{}, moves: map[uuid.UUID][]storage.HoldingMovement{}, codes: map[uuid.UUID]string{}}
	var err error
	if v.records, err = equity.HolderRecords(ctx, u); err != nil {
		return v, err
	}
	if v.classes, err = equity.ShareClasses(ctx, u); err != nil {
		return v, err
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return v, err
	}
	for _, s := range issues {
		v.issues[s.HolderID] = append(v.issues[s.HolderID], s)
	}
	moves, err := u.Holdings().List(ctx)
	if err != nil {
		return v, err
	}
	for _, m := range moves {
		v.moves[m.HolderID] = append(v.moves[m.HolderID], m)
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
	}
	v.asked, err = a.waiting(ctx, u, approval.ObjectHolderRecord)
	return v, err
}

// holdingsOf are a holder's shares by class, from their holding movements (FR-EQ-17): shares with no class count under
// their account's first recorded class, or their account while it has none (design D2). A class they no longer hold is
// left out.
func (v holderView) holdingsOf(contact uuid.UUID) []HolderShares {
	type key struct {
		class   string
		account uuid.UUID
	}
	totals := map[key]int64{}
	var order []key
	for _, s := range v.moves[contact] {
		k := key{account: s.ClassAccountID}
		if s.ClassID != nil {
			k.class = s.ClassID.String()
		} else if id := v.firstClass(s.ClassAccountID); id != nil {
			k.class = id.String()
		}
		n, _ := strconv.ParseInt(string(s.Shares), 10, 64)
		if _, ok := totals[k]; !ok {
			order = append(order, k)
		}
		totals[k] += n
	}
	out := []HolderShares{}
	for _, k := range order {
		if totals[k] == 0 {
			continue
		}
		h := HolderShares{ClassID: k.class, AccountCode: v.codes[k.account], Shares: strconv.FormatInt(totals[k], 10),
			ClassName: map[string]string{"3010": "Ordinary shares", "3020": "Preference shares"}[v.codes[k.account]]}
		if id, err := uuid.Parse(k.class); err == nil {
			if cur := equity.InForce(v.classes[id]); cur != nil {
				h.ClassName = cur.Value.Name
			}
		}
		if h.ClassName == "" {
			h.ClassName = "Share capital " + h.AccountCode
		}
		out = append(out, h)
	}
	return out
}

// firstClass is an account's first recorded class, as equity.DefaultClass reads it, from the classes read once.
func (v holderView) firstClass(account uuid.UUID) *uuid.UUID {
	var best *uuid.UUID
	var at time.Time
	for id, versions := range v.classes {
		for _, x := range versions {
			if x.Doc.Status != storage.DocumentPosted {
				continue
			}
			if x.Value.AccountID == account && (best == nil || x.RecordedAt.Before(at) || (x.RecordedAt.Equal(at) && id.String() < best.String())) {
				id := id
				best, at = &id, x.RecordedAt
			}
			break
		}
	}
	return best
}

func (v holderView) holderOf(c storage.Contact) Holder {
	h := Holder{ContactID: c.ID.String(), Name: c.LegalName, Shareholder: c.Shareholder, Investor: c.Investor, Director: c.Director,
		Holdings: v.holdingsOf(c.ID)}
	versions := v.records[c.ID]
	if cur := equity.InForce(versions); cur != nil {
		h.Recorded, h.Kind, h.Identifier = true, cur.Value.Kind, identifierText(cur.Value)
	}
	for _, x := range versions {
		if x.Doc.Status == storage.DocumentDraft {
			h.Pending = x.Doc.ID.String()
		}
	}
	return h
}

// holderRecord reads a holder record the caller may read, its series and the view: not found for anything else.
func (a *API) holderRecord(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (equity.Recorded[equity.HolderRecord], *uuid.UUID, holderView, error) {
	v, err := a.holderViewOf(ctx, u)
	if err != nil {
		return equity.Recorded[equity.HolderRecord]{}, nil, v, err
	}
	for _, versions := range v.records {
		for _, x := range versions {
			if x.Doc.ID == id {
				var inForce *uuid.UUID
				if cur := equity.InForce(versions); cur != nil {
					inForce = &cur.Doc.ID
				}
				return x, inForce, v, nil
			}
		}
	}
	return equity.Recorded[equity.HolderRecord]{}, nil, v, ports.ErrNotFound
}

func (a *API) shownHolderRecord(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (HolderRecordView, error) {
	x, inForce, v, err := a.holderRecord(ctx, u, id)
	if err != nil {
		return HolderRecordView{}, err
	}
	return a.holderRecordOf(ctx, u, x, inForce, v.asked)
}

func classDraftOf(c equity.ShareClass) ShareClassDraft {
	out := ShareClassDraft{Name: c.Name, Kind: c.Kind, Series: c.Series, AccountID: c.AccountID.String(), VotesPerShare: c.VotesPerShare,
		Liquidation: LiquidationRight(c.Liquidation), Dividend: DividendRight(c.Dividend), ConversionRatio: c.ConversionRatio, AntiDilution: c.AntiDilution,
		ProRata: c.ProRata, Redemption: c.Redemption, Note: c.Note}
	if c.ClassID != nil {
		out.ClassID = c.ClassID.String()
	}
	return out
}

func classIn(d ShareClassDraft, today time.Time) (equity.ShareClass, error) {
	var bad []FieldProblem
	account, err := uuid.Parse(d.AccountID)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.account_id", Message: "is not an account's ID", Fix: "Send the share-capital account's ID"})
	}
	c := equity.ShareClass{Name: strings.TrimSpace(d.Name), Kind: d.Kind, Series: strings.TrimSpace(d.Series), AccountID: account,
		VotesPerShare: strings.TrimSpace(d.VotesPerShare), Liquidation: equity.Liquidation(d.Liquidation), Dividend: equity.Dividend(d.Dividend),
		ConversionRatio: strings.TrimSpace(d.ConversionRatio), AntiDilution: d.AntiDilution, ProRata: d.ProRata, Redemption: strings.TrimSpace(d.Redemption),
		Note: strings.TrimSpace(d.Note), Date: today}
	if d.ClassID != "" {
		id, err := uuid.Parse(d.ClassID)
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.class_id", Message: "is not a share class's ID", Fix: "Send a recorded class's ID, or leave it out"})
		}
		c.ClassID = &id
	}
	if len(bad) > 0 {
		return c, invalid(bad)
	}
	return c, nil
}

// shareClassOf is a share class version as the API shows it.
func (a *API) shareClassOf(ctx context.Context, u storage.UnitOfWork, x equity.Recorded[equity.ShareClass], root uuid.UUID, inForce *uuid.UUID,
	asked map[string]string, codes map[uuid.UUID]string) (ShareClassView, error) {
	out := ShareClassView{ShareClassDraft: classDraftOf(x.Value), ID: root.String(), VersionID: x.Doc.ID.String(), Status: recordStatus(x.Doc, inForce),
		Version: x.Doc.Version, AccountCode: codes[x.Value.AccountID]}
	var err error
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, x.Doc, asked)
	return out, err
}

// classView reads every share class version once.
type classView struct {
	classes map[uuid.UUID][]equity.Recorded[equity.ShareClass]
	codes   map[uuid.UUID]string
	asked   map[string]string
}

func (a *API) classViewOf(ctx context.Context, u storage.UnitOfWork) (classView, error) {
	v := classView{codes: map[uuid.UUID]string{}}
	var err error
	if v.classes, err = equity.ShareClasses(ctx, u); err != nil {
		return v, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
	}
	v.asked, err = a.waiting(ctx, u, approval.ObjectShareClass)
	return v, err
}

// classVersion finds a share class version: its series' root, the version in force, and the view; not found otherwise.
func (a *API) classVersion(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (equity.Recorded[equity.ShareClass], uuid.UUID, *uuid.UUID, classView, error) {
	v, err := a.classViewOf(ctx, u)
	if err != nil {
		return equity.Recorded[equity.ShareClass]{}, uuid.Nil, nil, v, err
	}
	for root, versions := range v.classes {
		for _, x := range versions {
			if x.Doc.ID == id {
				var inForce *uuid.UUID
				if cur := equity.InForce(versions); cur != nil {
					inForce = &cur.Doc.ID
				}
				return x, root, inForce, v, nil
			}
		}
	}
	return equity.Recorded[equity.ShareClass]{}, uuid.Nil, nil, v, ports.ErrNotFound
}

func (a *API) shownShareClass(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ShareClassView, error) {
	x, root, inForce, v, err := a.classVersion(ctx, u, id)
	if err != nil {
		return ShareClassView{}, err
	}
	return a.shareClassOf(ctx, u, x, root, inForce, v.asked, v.codes)
}

// holderRecordDependencies are a holder record's envelope values: its status and its holder (FR-APR-05).
func holderRecordDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	r, err := equity.ReadHolderRecord(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "holder": r.ContactID.String()}, nil
}

// shareClassDependencies are a share class version's envelope values: its status and its account.
func shareClassDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	c, err := equity.ReadShareClass(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "account": c.AccountID.String()}, nil
}

// holderRecordExecutable refuses the holder themselves: the member linked to the holder never approves their own record
// (FM-1, FM-9's reading).
func holderRecordExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	rec, err := equity.ReadHolderRecord(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, rec.ContactID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeNotApprover, "This is your own holder record, so another person approves it (FM-1).", "Ask another person who holds the approval right")
	}
	return nil
}

func registerEquityRecords(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListHolders, huma.Operation{
		OperationID: "listHolders", Method: http.MethodGet, Path: "/v1/holders", Tags: []string{"Equity"},
		Summary: "List the holders",
		Description: "The equity register's holders, by name: contacts marked shareholder or investor (FR-EQ-15), each with the record in force " +
			"(individual or entity, and its identifier: an individual's type and last 4 characters only), a record drafted and not yet " +
			"recorded, and the shares held by class from the register of share issues. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *holderListInput) (*holderListOutput, error) {
		p, _ := authn.From(ctx)
		out := holderListOutput{Body: HolderPage{Items: []Holder{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.holderViewOf(ctx, u)
			if err != nil {
				return err
			}
			var all []Holder
			after := uuid.Nil
			for {
				page, err := u.Contacts().Page(ctx, after, "holder", 500)
				if err != nil {
					return err
				}
				for _, c := range page {
					all = append(all, v.holderOf(c))
					after = c.ID
				}
				if len(page) < 500 {
					break
				}
			}
			slices.SortStableFunc(all, func(x, y Holder) int {
				if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
					return c
				}
				return strings.Compare(x.ContactID, y.ContactID)
			})
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(h Holder) string { return strings.ToLower(h.Name) + "|" + h.ContactID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []Holder{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.GetHolder, huma.Operation{
		OperationID: "getHolder", Method: http.MethodGet, Path: "/v1/holders/{contact_id}", Tags: []string{"Equity"},
		Summary:     "Get a holder",
		Description: "A holder with the record in force, every record (drafts and superseded ones included, the latest first) and their share issues (FR-EQ-15). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *HolderPath) (*holderOutput, error) {
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out holderOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := equity.Holder(ctx, u, id)
			var lp ledger.Problems
			if errors.As(err, &lp) {
				return problem(CodeNotFound, "There is no holder with that ID.", "List the holders: contacts marked shareholder or investor")
			}
			if err != nil {
				return err
			}
			v, err := a.holderViewOf(ctx, u)
			if err != nil {
				return err
			}
			out.Body = HolderDetail{Holder: v.holderOf(c), History: []HolderRecordView{}, Issues: []ShareIssue{}}
			versions := v.records[id]
			var inForce *uuid.UUID
			if cur := equity.InForce(versions); cur != nil {
				inForce = &cur.Doc.ID
			}
			for i := len(versions) - 1; i >= 0; i-- {
				r, err := a.holderRecordOf(ctx, u, versions[i], inForce, v.asked)
				if err != nil {
					return err
				}
				if r.Status == "recorded" {
					rr := r
					out.Body.Record = &rr
				}
				out.Body.History = append(out.Body.History, r)
			}
			for _, s := range v.issues[id] {
				out.Body.Issues = append(out.Body.Issues, shareIssueOf(s, c.LegalName, v.codes[s.ClassAccountID]))
			}
			return nil
		})
		return &out, err
	})

	recordAnswer := func(w written[HolderRecordView]) *holderRecordWriteOutput {
		if w.Previewed {
			return &holderRecordWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status != "draft" {
			status = "posted"
		}
		return &holderRecordWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/holder-records/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.CreateHolderRecord, huma.Operation{
		OperationID: "createHolderRecord", Method: http.MethodPost, Path: "/v1/holder-records", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a holder record",
		Description: "Drafts a holder's details (FR-EQ-15): individual or entity; an individual's identifier as its type and last 4 characters " +
			"only, the full number staying in ACRA's register and the identity document (the owner's decision of 7 Nov 2026); an entity's " +
			"UEN or foreign registration number; nationality, address and the people to contact. Attach the identity document with " +
			"addHolderRecordEvidence, then ask another person to approve it with requestHolderRecordApproval: recorded, it supersedes the " +
			"record before it. The holder is a contact marked shareholder or investor. Side effects: the draft, audit events. Every " +
			"problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), EquityRecordCodes...), func(ctx context.Context, in *holderDraftInput) (*holderRecordWriteOutput, error) {
		id, err := uuid.Parse(in.Body.ContactID)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.contact_id", Message: "is not a contact's ID", Fix: "Send the holder's contact ID"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (HolderRecordView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return HolderRecordView{}, err
			}
			doc, err := equity.DraftHolderRecordIn(ctx, u, holderRecordIn(id, in.Body.HolderRecordDraft, today))
			if err != nil {
				return HolderRecordView{}, equityProblems(err)
			}
			return a.shownHolderRecord(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return recordAnswer(w), nil
	})

	register(a, authz.GetHolderRecord, huma.Operation{
		OperationID: "getHolderRecord", Method: http.MethodGet, Path: "/v1/holder-records/{record_id}", Tags: []string{"Equity"},
		Summary:     "Get a holder record",
		Description: "Returns a holder record with its ETag: its details, its status, how many files are attached and the approval waiting (FR-EQ-15). The DPO reads it masked. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *HolderRecordPath) (*holderRecordReadOutput, error) {
		id, err := parseID("record_id", in.RecordID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out holderRecordReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, inForce, v, err := a.holderRecord(ctx, u, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(x.Doc)
			out.Body, err = a.holderRecordOf(ctx, u, x, inForce, v.asked)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceHolderRecord, huma.Operation{
		OperationID: "replaceHolderRecord", Method: http.MethodPut, Path: "/v1/holder-records/{record_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted holder record",
		Description: "Saves a new version of a holder record not recorded yet (FR-EQ-15); its holder stays. A recorded one changes no more " +
			"(EQUITY-05): draft a new one. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), EquityRecordCodes...), func(ctx context.Context, in *holderReviseInput) (*holderRecordWriteOutput, error) {
		id, err := parseID("record_id", in.RecordID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (HolderRecordView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentHolderRecord, id)
			if err != nil {
				return HolderRecordView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return HolderRecordView{}, err
			}
			if _, err := equity.ReviseHolderRecordIn(ctx, u, id, holderRecordIn(uuid.Nil, in.Body, time.Time{})); err != nil {
				return HolderRecordView{}, equityProblems(err)
			}
			return a.shownHolderRecord(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return recordAnswer(w), nil
	})

	register(a, authz.RequestHolderRecordApproval, huma.Operation{
		OperationID: "requestHolderRecordApproval", Method: http.MethodPost, Path: "/v1/holder-records/{record_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a holder record",
		Description: "Asks another person to approve a drafted holder record at the version read (FR-EQ-15, FM-1): a holder record is recorded " +
			"only this way. It needs its identity document attached (EQUITY-06). The answer is 202 with an approval request, which a person " +
			"who holds A on \"Cap table, holders, investors, equity transactions\" approves, stepped up: never the one who asked, nor the " +
			"holder. On the approval the record is recorded, superseding the one before it. The request holds the record's version, " +
			"status and holder, so a change voids it. Needs a person signed in who passed step-up, and If-Match with the record's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), EquityRecordCodes...),
		func(ctx context.Context, in *holderAskInput) (*equityAskOutput, error) {
			id, err := parseID("record_id", in.RecordID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentHolderRecord, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the record has no evidence attached",
						Fix: "Attach the holder's identity document, then ask again"}
				}
				r, err := equity.ReadHolderRecord(doc)
				if err != nil {
					return Approval{}, err
				}
				c, err := u.Contacts().Get(ctx, r.ContactID)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "kind", After: r.Kind}}
				if s := identifierText(r); s != "" {
					diff = append(diff, ApprovalDiff{Field: "identifier", After: s})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectHolderRecord, ObjectKey: doc.ID.String(),
					Operation: approval.OpHolderRecordRecord, Summary: fmt.Sprintf("Record %s's holder details", c.LegalName),
					Execute: map[string]any{"record_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "contact_id": r.ContactID.String()},
					Diff:    diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	classAnswer := func(w written[ShareClassView]) *classWriteOutput {
		if w.Previewed {
			return &classWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status != "draft" {
			status = "posted"
		}
		return &classWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/share-classes/" + w.Body.VersionID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.ListShareClasses, huma.Operation{
		OperationID: "listShareClasses", Method: http.MethodGet, Path: "/v1/share-classes", Tags: []string{"Equity"},
		Summary: "List the share classes",
		Description: "Every share class with the version of its rights in force, and every version drafted and not yet recorded, by name " +
			"(FR-EQ-16). The rights are recorded, never computed, in P1. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *classListInput) (*classListOutput, error) {
		p, _ := authn.From(ctx)
		out := classListOutput{Body: ShareClassPage{Items: []ShareClassView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.classViewOf(ctx, u)
			if err != nil {
				return err
			}
			var all []ShareClassView
			for root, versions := range v.classes {
				var inForce *uuid.UUID
				if cur := equity.InForce(versions); cur != nil {
					inForce = &cur.Doc.ID
				}
				for _, x := range versions {
					if x.Doc.Status == storage.DocumentPosted && (inForce == nil || *inForce != x.Doc.ID) {
						continue // superseded: the class's history
					}
					c, err := a.shareClassOf(ctx, u, x, root, inForce, v.asked, v.codes)
					if err != nil {
						return err
					}
					all = append(all, c)
				}
			}
			slices.SortStableFunc(all, func(x, y ShareClassView) int {
				if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
					return c
				}
				return strings.Compare(x.VersionID, y.VersionID)
			})
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(c ShareClassView) string { return strings.ToLower(c.Name) + "|" + c.VersionID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []ShareClassView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateShareClass, huma.Operation{
		OperationID: "createShareClass", Method: http.MethodPost, Path: "/v1/share-classes", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a share class",
		Description: "Drafts a share class with its rights (FR-EQ-16): its name, ordinary or preference with its series, its share-capital " +
			"account (3010 or 3020), votes per share, the liquidation preference, dividend, conversion, anti-dilution, pro-rata and " +
			"redemption rights, recorded and never computed in P1. With class_id it drafts a new version of a recorded class's rights, " +
			"keeping its account. Attach the constitution and the shareholders' agreement with addShareClassEvidence, then ask for " +
			"approval with requestShareClassApproval. Side effects: the draft, audit events. Every problem comes back together. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), EquityRecordCodes...), func(ctx context.Context, in *classDraftInput) (*classWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ShareClassView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ShareClassView{}, err
			}
			c, err := classIn(in.Body, today)
			if err != nil {
				return ShareClassView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return ShareClassView{}, err
			}
			doc, err := equity.DraftShareClassIn(ctx, u, c, s)
			if errors.Is(err, ports.ErrNotFound) {
				return ShareClassView{}, invalid([]FieldProblem{{Code: equity.CodeShareClass, Field: "body.class_id", Message: "is not a recorded share class",
					Fix: "Send the ID of a recorded class whose rights change, or leave it out for a new class"}})
			}
			if err != nil {
				return ShareClassView{}, equityProblems(err)
			}
			return a.shownShareClass(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return classAnswer(w), nil
	})

	register(a, authz.GetShareClass, huma.Operation{
		OperationID: "getShareClass", Method: http.MethodGet, Path: "/v1/share-classes/{version_id}", Tags: []string{"Equity"},
		Summary:     "Get a share class version",
		Description: "Returns a share class version with its ETag: its rights, its status, how many files are attached and the approval waiting (FR-EQ-16). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *ShareClassPath) (*classReadOutput, error) {
		id, err := parseID("version_id", in.VersionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out classReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, root, inForce, v, err := a.classVersion(ctx, u, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(x.Doc)
			out.Body, err = a.shareClassOf(ctx, u, x, root, inForce, v.asked, v.codes)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceShareClass, huma.Operation{
		OperationID: "replaceShareClass", Method: http.MethodPut, Path: "/v1/share-classes/{version_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted share class version",
		Description: "Saves a new version of a share class version not recorded yet (FR-EQ-16); the class it supersedes stays, and a new " +
			"version of a recorded class keeps its account. A recorded version changes no more (EQUITY-05). Side effects: the new " +
			"version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), EquityRecordCodes...), func(ctx context.Context, in *classReviseInput) (*classWriteOutput, error) {
		id, err := parseID("version_id", in.VersionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ShareClassView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentShareClass, id)
			if err != nil {
				return ShareClassView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return ShareClassView{}, err
			}
			body := in.Body
			body.ClassID = ""
			c, err := classIn(body, time.Time{})
			if err != nil {
				return ShareClassView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return ShareClassView{}, err
			}
			if _, err := equity.ReviseShareClassIn(ctx, u, id, c, s); err != nil {
				return ShareClassView{}, equityProblems(err)
			}
			return a.shownShareClass(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return classAnswer(w), nil
	})

	register(a, authz.RequestShareClassApproval, huma.Operation{
		OperationID: "requestShareClassApproval", Method: http.MethodPost, Path: "/v1/share-classes/{version_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a share class",
		Description: "Asks another person to approve a drafted share class version at the version read (FR-EQ-16, FM-1): a class and its " +
			"rights are recorded only this way. It needs its documents attached, such as the constitution (EQUITY-06). The answer is 202 " +
			"with an approval request, which a person who holds A on \"Cap table, holders, investors, equity transactions\" approves, " +
			"stepped up, never the one who asked. On the approval the version is recorded, superseding the one before it. The request " +
			"holds the version's version, status and account, so a change voids it. Needs a person signed in who passed step-up, and " +
			"If-Match with the version's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), EquityRecordCodes...),
		func(ctx context.Context, in *classAskInput) (*equityAskOutput, error) {
			id, err := parseID("version_id", in.VersionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentShareClass, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the share class has no evidence attached",
						Fix: "Attach the constitution and the shareholders' agreement, then ask again"}
				}
				c, err := equity.ReadShareClass(doc)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "kind", After: c.Kind},
					{Field: "votes_per_share", After: c.VotesPerShare}, {Field: "anti_dilution", After: c.AntiDilution}}
				if c.Liquidation.Multiple != "" {
					diff = append(diff, ApprovalDiff{Field: "liquidation", After: c.Liquidation.Multiple + "x"})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectShareClass, ObjectKey: doc.ID.String(),
					Operation: approval.OpShareClassRecord, Summary: fmt.Sprintf("Record the share class %s and its rights", c.Name),
					Execute: map[string]any{"version_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "account_id": c.AccountID.String()},
					Diff:    diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	registerEquityEvidence(a)
}
