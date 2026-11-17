// Package corporate keeps the corporate profile (PRD-001 FR-CS-01, FR-CS-06; p1-company-secretary design D2): the
// company's name, registered office and SSIC code from a day, and its officers (directors, the secretary, auditors and
// the CEO) with the days they were appointed and ceased and the days each was lodged with ACRA. Each is a document,
// drafted, then recorded on another person's approval with its evidence, as the equity register's records are: a
// correction or a later step is a new version, and the last one recorded is in force. Nothing here posts.
package corporate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of the corporate profile's records.
const (
	CodeDetail  = "CORP-01" // a corporate detail that does not fit: its day, name, office, SSIC code or lodgement
	CodeOfficer = "CORP-02" // an officer that does not fit: the role, the person, the days or the nominee
)

// The officers' roles (FR-CS-01): ACRA keeps the registers of directors, secretaries and auditors, and the CEO.
const (
	RoleDirector  = "director"
	RoleSecretary = "secretary"
	RoleAuditor   = "auditor"
	RoleCEO       = "ceo"
)

// Roles are the officers' roles, in the order the profile lists them.
var Roles = []string{RoleDirector, RoleSecretary, RoleCEO, RoleAuditor}

// The longest texts a record keeps, as the database does (FY352, FY353).
const (
	MaxName   = 200
	MaxOffice = 500
	MaxNote   = 1000
)

var (
	ssicFormat = regexp.MustCompile(`^[0-9]{5}$`)
	last4      = regexp.MustCompile(`^[A-Za-z0-9]{1,4}$`)
	uenFormat  = regexp.MustCompile(`^[0-9A-Z]{9,10}$`)
	fullNRIC   = regexp.MustCompile(`\b[STFGMstfgm][0-9]{7}[A-Za-z]\b`) // a word of its own, as the database's guards read it (FY352, FY353)
)

// Detail is the company's name, registered office and SSIC code from a day, and the day the change was lodged with
// ACRA (FR-CS-01). The UEN and the financial years stay in the organisation profile.
type Detail struct {
	Date   time.Time // the day it was drafted
	From   time.Time // the day it takes effect
	Name   string
	Office string
	SSIC   string
	Lodged *time.Time // the day BizFile+ acknowledged the change
	Note   string
}

type detailPayload struct {
	Date   string `json:"date"`
	From   string `json:"from"`
	Name   string `json:"name"`
	Office string `json:"registered_office"`
	SSIC   string `json:"ssic"`
	Lodged string `json:"lodged_on,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Officer is one appointment (FR-CS-01, FR-CS-06): a director, the secretary, an auditor or the CEO, by name and
// identifier, the member of the organisation when they are one, the days they were appointed and ceased and the days
// each was lodged, and for a director whether they are a nominee and for whom (s386AL). An individual's identifier is
// its type and last 4 characters only (SEC-15); an audit firm's is its UEN.
type Officer struct {
	Date              time.Time // the day it was drafted
	ID                uuid.UUID // the officer's first version; zero on it
	Role              string
	Name              string
	Member            *uuid.UUID // a member's user ID
	Kind              string     // individual, or entity for an audit firm
	IDType            string
	IDLast4           string
	Registration      string
	Appointed         time.Time
	Ceased            *time.Time
	AppointmentLodged *time.Time
	CessationLodged   *time.Time
	Nominee           bool
	Nominator         string // whom a nominee director acts for
	Note              string
}

type officerPayload struct {
	Date              string `json:"date"`
	ID                string `json:"officer_id,omitempty"`
	Role              string `json:"role"`
	Name              string `json:"name"`
	Member            string `json:"member_user_id,omitempty"`
	Kind              string `json:"kind"`
	IDType            string `json:"id_type,omitempty"`
	IDLast4           string `json:"id_last4,omitempty"`
	Registration      string `json:"registration_number,omitempty"`
	Appointed         string `json:"appointed_on"`
	Ceased            string `json:"ceased_on,omitempty"`
	AppointmentLodged string `json:"appointment_lodged_on,omitempty"`
	CessationLodged   string `json:"cessation_lodged_on,omitempty"`
	Nominee           bool   `json:"nominee"`
	Nominator         string `json:"nominator,omitempty"`
	Note              string `json:"note,omitempty"`
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

func optional(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

func readDay(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	return &d, err
}

// problems collects what is wrong with a record, under one code.
type problems struct {
	code  string
	today time.Time
	out   ledger.Problems
}

func (p *problems) add(field, msg, fix string) {
	p.out = append(p.out, ledger.Problem{Code: p.code, Field: field, Message: msg, Fix: fix})
}

func (p *problems) text(field, s string, most int, what string) {
	switch {
	case strings.TrimSpace(s) == "":
		p.add(field, what+" is empty", "Enter it as ACRA's business profile shows it")
	case utf8.RuneCountInString(s) > most:
		p.add(field, fmt.Sprintf("%s is longer than %d characters", what, most), "Shorten it")
	}
}

func (p *problems) future(field string, d *time.Time, what string) {
	if d != nil && !p.today.IsZero() && d.After(p.today) {
		p.add(field, what+" is after today", "Record it once it has happened")
	}
}

func (p *problems) note(s string) {
	if utf8.RuneCountInString(s) > MaxNote {
		p.add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
}

// CheckDetail is what is wrong with a corporate detail, every problem at once (ENG-22): a day, a name, an office and a
// five-digit SSIC code, the change lodged on or after it takes effect, nothing after today, and no NRIC or FIN in full.
func CheckDetail(d Detail, today time.Time) ledger.Problems {
	p := &problems{code: CodeDetail, today: today}
	if d.From.IsZero() {
		p.add("from", "the day it takes effect is missing", "Enter the day the name, office or SSIC code changed")
	}
	p.text("name", d.Name, MaxName, "the name")
	p.text("registered_office", d.Office, MaxOffice, "the registered office")
	if !ssicFormat.MatchString(d.SSIC) {
		p.add("ssic", fmt.Sprintf("%q is not a five-digit SSIC code", d.SSIC), "Enter the primary activity's SSIC code, such as 62011")
	}
	if d.Lodged != nil && d.Lodged.Before(d.From) {
		p.add("lodged_on", "the change is lodged before it takes effect", "Enter the day BizFile+ acknowledged it")
	}
	p.future("from", &d.From, "the day it takes effect")
	p.future("lodged_on", d.Lodged, "the day it was lodged")
	p.note(d.Note)
	if fullNRIC.MatchString(strings.Join([]string{d.Name, d.Office, d.Note}, " ")) {
		p.add("note", "it shows an NRIC or FIN in full", "Keep the last 4 characters only")
	}
	return p.out
}

// CheckOfficer is what is wrong with an officer, every problem at once (ENG-22): a known role; a natural person unless
// an auditor (s145, s171); an identifier as its type and last 4 characters, or an audit firm's UEN; the days in order
// and none after today; a nominee only as a director, naming whom they act for; and no NRIC or FIN in full.
func CheckOfficer(o Officer, today time.Time) ledger.Problems {
	p := &problems{code: CodeOfficer, today: today}
	if !slices.Contains(Roles, o.Role) {
		p.add("role", fmt.Sprintf("%q is not an officer's role", o.Role), "Choose director, secretary, auditor or ceo")
	}
	p.text("name", o.Name, MaxName, "the name")
	switch {
	case o.Kind == "individual":
		if o.Registration != "" {
			p.add("registration_number", "an individual has no registration number", "Leave it empty")
		}
		if o.IDType != "" && !slices.Contains(equity.IndividualIDs, o.IDType) {
			p.add("id_type", fmt.Sprintf("%q is not an individual's identifier", o.IDType), "Choose nric, fin or passport")
		}
		if (o.IDType == "") != (o.IDLast4 == "") {
			p.add("id_last4", "the identifier needs its type and its last 4 characters together", "Give both, or neither")
		} else if o.IDLast4 != "" && !last4.MatchString(o.IDLast4) {
			p.add("id_last4", "Fyniq keeps only the last 4 letters or digits of an individual's identifier",
				"Enter the last 4 characters, such as 567A; the full number stays in ACRA's register")
		}
	case o.Kind == "entity" && o.Role == RoleAuditor:
		if o.IDLast4 != "" || o.Member != nil {
			p.add("kind", "an audit firm is identified by its registration number and is no member", "Enter its UEN instead")
		}
		if o.IDType != "" && !slices.Contains(equity.EntityIDs, o.IDType) {
			p.add("id_type", fmt.Sprintf("%q is not a firm's identifier", o.IDType), "Choose uen or foreign_registration")
		}
		if (o.IDType == "") != (o.Registration == "") {
			p.add("registration_number", "the identifier needs its type and number together", "Give both, or neither")
		} else if o.IDType == "uen" && !uenFormat.MatchString(o.Registration) {
			p.add("registration_number", fmt.Sprintf("%q is not a UEN", o.Registration), "Enter the 9 or 10 character UEN, such as 202312345K")
		} else if utf8.RuneCountInString(o.Registration) > 30 {
			p.add("registration_number", "the registration number is longer than 30 characters", "Enter the number only")
		}
	case o.Kind == "entity":
		p.add("kind", "a director, the secretary or the CEO is a natural person", "Record them as an individual (s145, s171)")
	default:
		p.add("kind", fmt.Sprintf("%q is not individual or entity", o.Kind), "Choose individual, or entity for an audit firm")
	}
	if o.Appointed.IsZero() {
		p.add("appointed_on", "the day appointed is missing", "Enter it as ACRA's business profile shows it")
	}
	if o.Ceased != nil && o.Ceased.Before(o.Appointed) {
		p.add("ceased_on", "they cease before they were appointed", "Enter the day they ceased")
	}
	if o.AppointmentLodged != nil && o.AppointmentLodged.Before(o.Appointed) {
		p.add("appointment_lodged_on", "the appointment is lodged before it", "Enter the day BizFile+ acknowledged it")
	}
	switch {
	case o.CessationLodged != nil && o.Ceased == nil:
		p.add("cessation_lodged_on", "a cessation is lodged with no day they ceased", "Enter the day they ceased first")
	case o.CessationLodged != nil && o.CessationLodged.Before(*o.Ceased):
		p.add("cessation_lodged_on", "the cessation is lodged before it", "Enter the day BizFile+ acknowledged it")
	}
	p.future("appointed_on", &o.Appointed, "the day appointed")
	p.future("ceased_on", o.Ceased, "the day they ceased")
	p.future("appointment_lodged_on", o.AppointmentLodged, "the day the appointment was lodged")
	p.future("cessation_lodged_on", o.CessationLodged, "the day the cessation was lodged")
	switch {
	case o.Nominee && o.Role != RoleDirector:
		p.add("nominee", "only a director is a nominee here", "Record a nominee shareholder in the register of nominee shareholders")
	case o.Nominee:
		p.text("nominator", o.Nominator, MaxName, "whom the nominee acts for")
	case o.Nominator != "":
		p.add("nominator", "only a nominee acts for someone", "Leave it empty, or mark the director a nominee")
	}
	p.note(o.Note)
	if fullNRIC.MatchString(strings.Join([]string{o.Name, o.Nominator, o.Registration, o.Note}, " ")) {
		p.add("note", "it shows an NRIC or FIN in full", "Keep the last 4 characters only")
	}
	return p.out
}

// EncodeDetail is a corporate detail as a document version keeps it.
func EncodeDetail(d Detail) ([]byte, error) {
	return json.Marshal(detailPayload{Date: day(d.Date), From: day(d.From), Name: strings.TrimSpace(d.Name), Office: strings.TrimSpace(d.Office),
		SSIC: d.SSIC, Lodged: optional(d.Lodged), Note: d.Note})
}

// ReadDetail reads a corporate detail's version as it was saved.
func ReadDetail(doc storage.Document) (Detail, error) {
	var p detailPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Detail{}, err
	}
	out := Detail{Name: p.Name, Office: p.Office, SSIC: p.SSIC, Note: p.Note}
	var err error
	if out.From, err = time.Parse(time.DateOnly, p.From); err != nil {
		return out, err
	}
	if out.Lodged, err = readDay(p.Lodged); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// EncodeOfficer is an officer as a document version keeps it.
func EncodeOfficer(o Officer) ([]byte, error) {
	p := officerPayload{Date: day(o.Date), Role: o.Role, Name: strings.TrimSpace(o.Name), Kind: o.Kind, IDType: o.IDType, IDLast4: o.IDLast4,
		Registration: o.Registration, Appointed: day(o.Appointed), Ceased: optional(o.Ceased), AppointmentLodged: optional(o.AppointmentLodged),
		CessationLodged: optional(o.CessationLodged), Nominee: o.Nominee, Nominator: strings.TrimSpace(o.Nominator), Note: o.Note}
	if o.ID != uuid.Nil {
		p.ID = o.ID.String()
	}
	if o.Member != nil {
		p.Member = o.Member.String()
	}
	return json.Marshal(p)
}

// ReadOfficer reads an officer's version as it was saved.
func ReadOfficer(doc storage.Document) (Officer, error) {
	var p officerPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Officer{}, err
	}
	out := Officer{Role: p.Role, Name: p.Name, Kind: p.Kind, IDType: p.IDType, IDLast4: p.IDLast4, Registration: p.Registration,
		Nominee: p.Nominee, Nominator: p.Nominator, Note: p.Note}
	var err error
	if p.ID != "" {
		if out.ID, err = uuid.Parse(p.ID); err != nil {
			return out, err
		}
	}
	if p.Member != "" {
		m, err := uuid.Parse(p.Member)
		if err != nil {
			return out, err
		}
		out.Member = &m
	}
	if out.Appointed, err = time.Parse(time.DateOnly, p.Appointed); err != nil {
		return out, err
	}
	for _, x := range []struct {
		to   **time.Time
		from string
	}{{&out.Ceased, p.Ceased}, {&out.AppointmentLodged, p.AppointmentLodged}, {&out.CessationLodged, p.CessationLodged}} {
		if *x.to, err = readDay(x.from); err != nil {
			return out, err
		}
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// Root is the officer's first version: the one every later version names.
func Root(doc storage.Document, o Officer) uuid.UUID {
	if o.ID != uuid.Nil {
		return o.ID
	}
	return doc.ID
}

// checkedDetail is what is wrong with a detail in u.
func checkedDetail(d Detail, today time.Time) error {
	if probs := CheckDetail(d, today); len(probs) > 0 {
		return probs
	}
	return nil
}

// checkedOfficer is what is wrong with an officer in u: their member a member of the organisation, and a later version
// naming an officer with a version recorded.
func checkedOfficer(ctx context.Context, u storage.UnitOfWork, o Officer, today time.Time) error {
	probs := CheckOfficer(o, today)
	if o.Member != nil {
		if _, err := u.Members().Membership(ctx, *o.Member); errors.Is(err, ports.ErrNotFound) {
			probs = append(probs, ledger.Problem{Code: CodeOfficer, Field: "member_user_id", Message: "the member does not belong to this organisation",
				Fix: "Choose a member, or leave it empty for an officer who does not sign in"})
		} else if err != nil {
			return err
		}
	}
	if o.ID != uuid.Nil {
		versions, err := Officers(ctx, u)
		if err != nil {
			return err
		}
		if equity.InForce(versions[o.ID]) == nil {
			probs = append(probs, ledger.Problem{Code: CodeOfficer, Field: "officer_id", Message: "the officer has no recorded version to follow",
				Fix: "Record the officer's appointment first, then draft the change from it"})
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftDetailIn saves a corporate detail to record in u: a draft, to attach its evidence to.
func DraftDetailIn(ctx context.Context, u storage.UnitOfWork, d Detail, today time.Time) (storage.Document, error) {
	d.Date = today
	if err := checkedDetail(d, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeDetail(d)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCorporateDetail, Payload: b})
}

// ReviseDetailIn saves a new version of a corporate detail not recorded yet, in u.
func ReviseDetailIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, d Detail, today time.Time) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDetail, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadDetail(doc)
	if err != nil {
		return doc, err
	}
	d.Date = was.Date
	if err := checkedDetail(d, today); err != nil {
		return doc, err
	}
	b, err := EncodeDetail(d)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordDetailIn records an approved corporate detail in u: checked again, then recorded with its evidence
// (equity.RecordIn).
func RecordDetailIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDetail, id)
	if err != nil {
		return doc, err
	}
	d, err := ReadDetail(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedDetail(d, time.Time{}); err != nil {
		return doc, err
	}
	return equity.RecordIn(ctx, u, storage.DocumentCorporateDetail, id)
}

// DraftOfficerIn saves an officer to record in u: a new appointment, or a later version of one (o.ID names its first
// version), a draft to attach its evidence to.
func DraftOfficerIn(ctx context.Context, u storage.UnitOfWork, o Officer, today time.Time) (storage.Document, error) {
	o.Date = today
	if err := checkedOfficer(ctx, u, o, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeOfficer(o)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCorporateOfficer, Payload: b})
}

// ReviseOfficerIn saves a new version of an officer not recorded yet, in u; the officer it follows stays.
func ReviseOfficerIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, o Officer, today time.Time) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateOfficer, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadOfficer(doc)
	if err != nil {
		return doc, err
	}
	o.ID, o.Date = was.ID, was.Date
	if err := checkedOfficer(ctx, u, o, today); err != nil {
		return doc, err
	}
	b, err := EncodeOfficer(o)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordOfficerIn records an approved officer in u: checked again, then recorded with its evidence (equity.RecordIn).
func RecordOfficerIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateOfficer, id)
	if err != nil {
		return doc, err
	}
	o, err := ReadOfficer(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedOfficer(ctx, u, o, time.Time{}); err != nil {
		return doc, err
	}
	return equity.RecordIn(ctx, u, storage.DocumentCorporateOfficer, id)
}

// all reads every document of type t, drafts and recorded.
func all(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType) ([]storage.Document, error) {
	var out []storage.Document
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: t, AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			afterDate, afterID = d.Date, &d.ID
			out = append(out, d)
		}
		if len(docs) < 500 {
			return out, nil
		}
	}
}

func recorded[T any](d storage.Document, v T) equity.Recorded[T] {
	r := equity.Recorded[T]{Doc: d, Value: v}
	if d.PostedAt != nil {
		r.RecordedAt = *d.PostedAt
	}
	return r
}

// byRecording orders drafts last, recorded ones by when they were recorded.
func byRecording[T any](a, b equity.Recorded[T]) int {
	da, db := a.Doc.Status == storage.DocumentDraft, b.Doc.Status == storage.DocumentDraft
	switch {
	case da != db:
		if da {
			return 1
		}
		return -1
	case !a.RecordedAt.Equal(b.RecordedAt):
		return a.RecordedAt.Compare(b.RecordedAt)
	}
	return strings.Compare(a.Doc.ID.String(), b.Doc.ID.String())
}

// Details are every corporate detail, drafts and recorded, in the order they were recorded (drafts last).
func Details(ctx context.Context, u storage.UnitOfWork) ([]equity.Recorded[Detail], error) {
	docs, err := all(ctx, u, storage.DocumentCorporateDetail)
	if err != nil {
		return nil, err
	}
	var out []equity.Recorded[Detail]
	for _, d := range docs {
		v, err := ReadDetail(d)
		if err != nil {
			return nil, err
		}
		out = append(out, recorded(d, v))
	}
	slices.SortStableFunc(out, byRecording[Detail])
	return out, nil
}

// DetailOn is the detail in force on day: of the recorded ones taking effect by then, the latest to take effect, and of
// those the last recorded (a correction); nil when none.
func DetailOn(details []equity.Recorded[Detail], on time.Time) *equity.Recorded[Detail] {
	var out *equity.Recorded[Detail]
	for i := range details {
		d := &details[i]
		if d.Doc.Status != storage.DocumentPosted || d.Value.From.After(on) {
			continue
		}
		if out == nil || !d.Value.From.Before(out.Value.From) {
			out = d
		}
	}
	return out
}

// Officers are every officer's versions, drafts and recorded, by the officer's first version, in the order they were
// recorded (drafts last).
func Officers(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]equity.Recorded[Officer], error) {
	docs, err := all(ctx, u, storage.DocumentCorporateOfficer)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]equity.Recorded[Officer]{}
	for _, d := range docs {
		v, err := ReadOfficer(d)
		if err != nil {
			return nil, err
		}
		root := Root(d, v)
		out[root] = append(out[root], recorded(d, v))
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[Officer])
	}
	return out, nil
}

// OfficersOn are the officers in office on day: each officer's version in force, appointed by then and not ceased by
// then, by role in the profile's order and then by name.
func OfficersOn(officers map[uuid.UUID][]equity.Recorded[Officer], on time.Time) []equity.Recorded[Officer] {
	var out []equity.Recorded[Officer]
	for _, versions := range officers {
		v := equity.InForce(versions)
		if v == nil || v.Value.Appointed.After(on) || (v.Value.Ceased != nil && !v.Value.Ceased.After(on)) {
			continue
		}
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b equity.Recorded[Officer]) int {
		if c := slices.Index(Roles, a.Value.Role) - slices.Index(Roles, b.Value.Role); c != 0 {
			return c
		}
		if c := strings.Compare(a.Value.Name, b.Value.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Doc.ID.String(), b.Doc.ID.String())
	})
	return out
}
