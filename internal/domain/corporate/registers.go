package corporate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The registers of nominee directors and nominee shareholders (s386AL), and the directors' approval of the financial
// statements (FR-CS-03, C.2 #33; p1-company-secretary design D2 and D4). Each is a document, drafted and recorded on
// another person's approval with its evidence, as the profile's details and officers are.

// The codes of the registers' records.
const (
	CodeNominee  = "CORP-03" // a nominee entry that does not fit: its kind, the director or holder, whom they act for, or its days
	CodeApproval = "CORP-04" // a statements approval that does not fit: the year's end or its days
)

// The kinds of nominee entry: the register of nominee directors, or of nominee shareholders.
const (
	NomineeDirector    = "director"
	NomineeShareholder = "shareholder"
)

// NomineeEntry is one entry of the register of nominee directors or nominee shareholders (s386AL): a director among the
// officers, or a holder of the equity register, who acts or holds for another, since when; the day they informed the
// company (30 days), the day the company's register entered it (7 days), the day ACRA's central register was told (2
// business days after) and the day they ceased.
type NomineeEntry struct {
	Date      time.Time  // the day it was drafted
	ID        uuid.UUID  // the entry's first version; zero on it
	Kind      string     // director or shareholder
	Officer   *uuid.UUID // a nominee director: the officer, by their first version
	Holder    *uuid.UUID // a nominee shareholder: the contact
	Nominator string     // whom they act or hold for
	Since     time.Time
	Informed  *time.Time
	Register  *time.Time
	Central   *time.Time
	Ceased    *time.Time
	Note      string
}

type nomineePayload struct {
	Date      string `json:"date"`
	ID        string `json:"entry_id,omitempty"`
	Kind      string `json:"kind"`
	Officer   string `json:"officer_id,omitempty"`
	Holder    string `json:"holder_id,omitempty"`
	Nominator string `json:"nominator"`
	Since     string `json:"since"`
	Informed  string `json:"informed_on,omitempty"`
	Register  string `json:"register_on,omitempty"`
	Central   string `json:"central_on,omitempty"`
	Ceased    string `json:"ceased_on,omitempty"`
	Note      string `json:"note,omitempty"`
}

// StatementsApproval is the directors' approval of a financial year's statements (FR-CS-03, C.2 #33): the year's end,
// the day they approved them and the day they were sent to the members.
type StatementsApproval struct {
	Date     time.Time // the day it was drafted
	FYEnd    time.Time
	Approved time.Time
	Sent     *time.Time
	Note     string
}

type approvalPayload struct {
	Date     string `json:"date"`
	FYEnd    string `json:"fy_end"`
	Approved string `json:"approved_on"`
	Sent     string `json:"sent_on,omitempty"`
	Note     string `json:"note,omitempty"`
}

// CheckNominee is what is wrong with a nominee entry on its own, every problem at once (ENG-22): a director or a
// shareholder named as its kind says, whom they act for, the days on or after they became one with ACRA told after the
// company's register, none after today, and no NRIC or FIN in full.
func CheckNominee(n NomineeEntry, today time.Time) ledger.Problems {
	p := &problems{code: CodeNominee, today: today}
	switch n.Kind {
	case NomineeDirector:
		if n.Officer == nil || n.Holder != nil {
			p.add("officer_id", "a nominee director is named by their officer's record, and holds no shares here", "Choose the director among the officers")
		}
	case NomineeShareholder:
		if n.Holder == nil || n.Officer != nil {
			p.add("holder_id", "a nominee shareholder is named by their contact", "Choose a contact marked shareholder or investor")
		}
	default:
		p.add("kind", fmt.Sprintf("%q is not director or shareholder", n.Kind), "Choose director or shareholder")
	}
	p.text("nominator", n.Nominator, MaxName, "whom they act for")
	if n.Since.IsZero() {
		p.add("since", "the day they became a nominee is missing", "Enter it")
	}
	for _, x := range []struct {
		field string
		d     *time.Time
		what  string
	}{{"informed_on", n.Informed, "they informed the company"}, {"register_on", n.Register, "the company's register entered it"},
		{"ceased_on", n.Ceased, "they ceased"}} {
		if x.d != nil && x.d.Before(n.Since) {
			p.add(x.field, "the day "+x.what+" is before they became a nominee", "Enter the days in order")
		}
	}
	switch {
	case n.Central != nil && n.Register == nil:
		p.add("central_on", "ACRA's central register is told after the company's register", "Enter the day the company's register entered it first")
	case n.Central != nil && n.Central.Before(*n.Register):
		p.add("central_on", "ACRA's central register is told before the company's register entered it", "Enter the days in order")
	}
	p.future("since", &n.Since, "the day they became a nominee")
	p.future("informed_on", n.Informed, "the day they informed the company")
	p.future("register_on", n.Register, "the day the company's register entered it")
	p.future("central_on", n.Central, "the day ACRA's central register was told")
	p.future("ceased_on", n.Ceased, "the day they ceased")
	p.note(n.Note)
	if fullNRIC.MatchString(n.Nominator + " " + n.Note) {
		p.add("note", "it shows an NRIC or FIN in full", "Keep the last 4 characters only")
	}
	return p.out
}

// CheckStatementsApproval is what is wrong with a statements approval, every problem at once: approved after the year
// ends, sent on or after approval, nothing after today.
func CheckStatementsApproval(a StatementsApproval, today time.Time) ledger.Problems {
	p := &problems{code: CodeApproval, today: today}
	if a.FYEnd.IsZero() {
		p.add("fy_end", "the financial year's end is missing", "Enter the last day of the year the statements are for")
	}
	switch {
	case a.Approved.IsZero():
		p.add("approved_on", "the day the directors approved them is missing", "Enter the day of the directors' resolution or statement")
	case !a.Approved.After(a.FYEnd):
		p.add("approved_on", "statements are approved after their year ends", "Enter the day the directors approved them")
	}
	if a.Sent != nil && a.Sent.Before(a.Approved) {
		p.add("sent_on", "they were sent before they were approved", "Enter the day they were sent to the members")
	}
	p.future("approved_on", &a.Approved, "the day they were approved")
	p.future("sent_on", a.Sent, "the day they were sent")
	p.note(a.Note)
	return p.out
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func readID(s string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	return &id, err
}

// EncodeNominee is a nominee entry as a document version keeps it.
func EncodeNominee(n NomineeEntry) ([]byte, error) {
	p := nomineePayload{Date: day(n.Date), Kind: n.Kind, Officer: idText(n.Officer), Holder: idText(n.Holder), Nominator: strings.TrimSpace(n.Nominator),
		Since: day(n.Since), Informed: optional(n.Informed), Register: optional(n.Register), Central: optional(n.Central), Ceased: optional(n.Ceased), Note: n.Note}
	if n.ID != uuid.Nil {
		p.ID = n.ID.String()
	}
	return json.Marshal(p)
}

// ReadNominee reads a nominee entry's version as it was saved.
func ReadNominee(doc storage.Document) (NomineeEntry, error) {
	var p nomineePayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return NomineeEntry{}, err
	}
	out := NomineeEntry{Kind: p.Kind, Nominator: p.Nominator, Note: p.Note}
	var err error
	if p.ID != "" {
		if out.ID, err = uuid.Parse(p.ID); err != nil {
			return out, err
		}
	}
	if out.Officer, err = readID(p.Officer); err != nil {
		return out, err
	}
	if out.Holder, err = readID(p.Holder); err != nil {
		return out, err
	}
	if out.Since, err = time.Parse(time.DateOnly, p.Since); err != nil {
		return out, err
	}
	for _, x := range []struct {
		to   **time.Time
		from string
	}{{&out.Informed, p.Informed}, {&out.Register, p.Register}, {&out.Central, p.Central}, {&out.Ceased, p.Ceased}} {
		if *x.to, err = readDay(x.from); err != nil {
			return out, err
		}
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// EncodeStatementsApproval is a statements approval as a document version keeps it.
func EncodeStatementsApproval(a StatementsApproval) ([]byte, error) {
	return json.Marshal(approvalPayload{Date: day(a.Date), FYEnd: day(a.FYEnd), Approved: day(a.Approved), Sent: optional(a.Sent), Note: a.Note})
}

// ReadStatementsApproval reads a statements approval's version as it was saved.
func ReadStatementsApproval(doc storage.Document) (StatementsApproval, error) {
	var p approvalPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return StatementsApproval{}, err
	}
	out := StatementsApproval{Note: p.Note}
	var err error
	if out.FYEnd, err = time.Parse(time.DateOnly, p.FYEnd); err != nil {
		return out, err
	}
	if out.Approved, err = time.Parse(time.DateOnly, p.Approved); err != nil {
		return out, err
	}
	if out.Sent, err = readDay(p.Sent); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// NomineeRoot is the entry's first version: the one every later version names.
func NomineeRoot(doc storage.Document, n NomineeEntry) uuid.UUID {
	if n.ID != uuid.Nil {
		return n.ID
	}
	return doc.ID
}

// checkedNominee is what is wrong with a nominee entry in u: a director among the officers, whose version in force is
// a director's; a holder marked shareholder or investor; a later version following an entry with a version recorded.
func checkedNominee(ctx context.Context, u storage.UnitOfWork, n NomineeEntry, today time.Time) error {
	probs := CheckNominee(n, today)
	if n.Officer != nil {
		officers, err := Officers(ctx, u)
		if err != nil {
			return err
		}
		if cur := equity.InForce(officers[*n.Officer]); cur == nil || cur.Value.Role != RoleDirector {
			probs = append(probs, ledger.Problem{Code: CodeNominee, Field: "officer_id", Message: "the officer is not a recorded director",
				Fix: "Choose a director among the officers, by their first version"})
		}
	}
	if n.Holder != nil {
		if _, err := equity.Holder(ctx, u, *n.Holder); err != nil {
			var lp ledger.Problems
			if !errors.As(err, &lp) {
				return err
			}
			for _, x := range lp {
				probs = append(probs, ledger.Problem{Code: CodeNominee, Field: "holder_id", Message: x.Message, Fix: x.Fix})
			}
		}
	}
	if n.ID != uuid.Nil {
		entries, err := Nominees(ctx, u)
		if err != nil {
			return err
		}
		if equity.InForce(entries[n.ID]) == nil {
			probs = append(probs, ledger.Problem{Code: CodeNominee, Field: "entry_id", Message: "the entry has no recorded version to follow",
				Fix: "Record the entry first, then draft the change from it"})
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

func checkedApproval(a StatementsApproval, today time.Time) error {
	if probs := CheckStatementsApproval(a, today); len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftNomineeIn saves a nominee entry to record in u: a new entry, or a later version of one (n.ID names its first).
func DraftNomineeIn(ctx context.Context, u storage.UnitOfWork, n NomineeEntry, today time.Time) (storage.Document, error) {
	n.Date = today
	if err := checkedNominee(ctx, u, n, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeNominee(n)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentNomineeEntry, Payload: b})
}

// ReviseNomineeIn saves a new version of a nominee entry not recorded yet, in u; the entry it follows stays.
func ReviseNomineeIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, n NomineeEntry, today time.Time) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentNomineeEntry, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadNominee(doc)
	if err != nil {
		return doc, err
	}
	n.ID, n.Date = was.ID, was.Date
	if err := checkedNominee(ctx, u, n, today); err != nil {
		return doc, err
	}
	b, err := EncodeNominee(n)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordNomineeIn records an approved nominee entry in u: checked again, then recorded with its evidence.
func RecordNomineeIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentNomineeEntry, id)
	if err != nil {
		return doc, err
	}
	n, err := ReadNominee(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedNominee(ctx, u, n, time.Time{}); err != nil {
		return doc, err
	}
	return equity.RecordIn(ctx, u, storage.DocumentNomineeEntry, id)
}

// DraftStatementsApprovalIn saves a statements approval to record in u: a draft, to attach the directors' resolution
// or statement to.
func DraftStatementsApprovalIn(ctx context.Context, u storage.UnitOfWork, a StatementsApproval, today time.Time) (storage.Document, error) {
	a.Date = today
	if err := checkedApproval(a, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeStatementsApproval(a)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatementsApproval, Payload: b})
}

// ReviseStatementsApprovalIn saves a new version of a statements approval not recorded yet, in u.
func ReviseStatementsApprovalIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, a StatementsApproval, today time.Time) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentStatementsApproval, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadStatementsApproval(doc)
	if err != nil {
		return doc, err
	}
	a.Date = was.Date
	if err := checkedApproval(a, today); err != nil {
		return doc, err
	}
	b, err := EncodeStatementsApproval(a)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordStatementsApprovalIn records an approved statements approval in u: checked again, then recorded with its
// evidence.
func RecordStatementsApprovalIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := equity.Drafted(ctx, u, storage.DocumentStatementsApproval, id)
	if err != nil {
		return doc, err
	}
	a, err := ReadStatementsApproval(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedApproval(a, time.Time{}); err != nil {
		return doc, err
	}
	return equity.RecordIn(ctx, u, storage.DocumentStatementsApproval, id)
}

// Nominees are every nominee entry's versions, drafts and recorded, by the entry's first version, in the order they
// were recorded (drafts last).
func Nominees(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]equity.Recorded[NomineeEntry], error) {
	docs, err := all(ctx, u, storage.DocumentNomineeEntry)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]equity.Recorded[NomineeEntry]{}
	for _, d := range docs {
		v, err := ReadNominee(d)
		if err != nil {
			return nil, err
		}
		root := NomineeRoot(d, v)
		out[root] = append(out[root], recorded(d, v))
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[NomineeEntry])
	}
	return out, nil
}

// NomineesOn are the entries in force on day: each entry's version in force, a nominee since then and not ceased by
// then, directors first and then by whom they act for.
func NomineesOn(entries map[uuid.UUID][]equity.Recorded[NomineeEntry], on time.Time) []equity.Recorded[NomineeEntry] {
	var out []equity.Recorded[NomineeEntry]
	for _, versions := range entries {
		v := equity.InForce(versions)
		if v == nil || v.Value.Since.After(on) || (v.Value.Ceased != nil && !v.Value.Ceased.After(on)) {
			continue
		}
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b equity.Recorded[NomineeEntry]) int {
		if c := strings.Compare(a.Value.Kind, b.Value.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.Value.Nominator, b.Value.Nominator); c != 0 {
			return c
		}
		return strings.Compare(a.Doc.ID.String(), b.Doc.ID.String())
	})
	return out
}

// StatementsApprovals are every statements approval, drafts and recorded, in the order they were recorded (drafts
// last).
func StatementsApprovals(ctx context.Context, u storage.UnitOfWork) ([]equity.Recorded[StatementsApproval], error) {
	docs, err := all(ctx, u, storage.DocumentStatementsApproval)
	if err != nil {
		return nil, err
	}
	var out []equity.Recorded[StatementsApproval]
	for _, d := range docs {
		v, err := ReadStatementsApproval(d)
		if err != nil {
			return nil, err
		}
		out = append(out, recorded(d, v))
	}
	slices.SortStableFunc(out, byRecording[StatementsApproval])
	return out, nil
}

// ApprovalFor is the approval in force of the financial year ending on fyEnd: the last recorded for it; nil when none.
func ApprovalFor(approvals []equity.Recorded[StatementsApproval], fyEnd time.Time) *equity.Recorded[StatementsApproval] {
	var out *equity.Recorded[StatementsApproval]
	for i := range approvals {
		if approvals[i].Doc.Status == storage.DocumentPosted && approvals[i].Value.FYEnd.Equal(fyEnd) {
			out = &approvals[i]
		}
	}
	return out
}
