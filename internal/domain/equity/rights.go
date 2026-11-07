package equity

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of investor rights and controller entries (FR-EQ-22, FR-EQ-25; design D10).
const (
	CodeRights     = "EQUITY-20" // investor rights that do not fit: the holder, the day, the seats, the agreements or a text
	CodeController = "EQUITY-21" // a controller entry that does not fit: its conclusion, grounds, days or notices
)

// The longest lists and texts the two keep.
const (
	MaxConsents    = 20
	MaxConsentText = 200
	MaxBoardSeats  = 20
	MaxNotices     = 50
)

// The grounds on which a holder is a registrable controller (FR-EQ-25; ACRA's registrable-controllers guidance): more
// than 25% of the shares, or of the votes, the right to appoint or remove directors holding a majority of the board's
// votes, or significant influence or control.
var ControllerGrounds = []string{"shares", "votes", "board", "influence"}

// InvestorRights are a holder's rights from the agreements (FR-EQ-22, design D10), recorded and never computed: the
// directors they may appoint, an observer, whether they appoint or remove directors holding a majority of the board's
// votes, their information rights (what and when), a pro-rata right, and the matters that need their consent. A version
// is recorded with its agreements attached on another person's approval (FM-1); the last one recorded is in force.
type InvestorRights struct {
	Date          time.Time // the day it was drafted: what lists sort by
	HolderID      uuid.UUID
	From          time.Time // the day the agreements give them from
	BoardSeats    int
	Observer      bool
	BoardMajority bool
	Information   string
	ProRata       bool
	Consents      []string
	Agreements    string // the agreements they come from, such as a shareholders' agreement and a side letter
	Note          string
}

type rightsPayload struct {
	Date          string   `json:"date"`
	HolderID      string   `json:"holder_id"`
	From          string   `json:"from"`
	BoardSeats    int      `json:"board_seats"`
	Observer      bool     `json:"observer"`
	BoardMajority bool     `json:"board_majority"`
	Information   string   `json:"information,omitempty"`
	ProRata       bool     `json:"pro_rata"`
	Consents      []string `json:"consents"`
	Agreements    string   `json:"agreements"`
	Note          string   `json:"note,omitempty"`
}

// ConfersControl says whether the rights could make their holder a registrable controller whatever their shares: the
// right to appoint or remove a board majority, or matters needing their consent, which a person must weigh (FR-EQ-25,
// G4-101).
func (r InvestorRights) ConfersControl() bool { return r.BoardMajority || len(r.Consents) > 0 }

// EncodeRights is investor rights as a document version keeps them.
func EncodeRights(r InvestorRights) ([]byte, error) {
	consents := r.Consents
	if consents == nil {
		consents = []string{}
	}
	return json.Marshal(rightsPayload{Date: dateText(r.Date), HolderID: idText(r.HolderID), From: dateText(r.From), BoardSeats: r.BoardSeats,
		Observer: r.Observer, BoardMajority: r.BoardMajority, Information: r.Information, ProRata: r.ProRata, Consents: consents,
		Agreements: r.Agreements, Note: r.Note})
}

// ReadRights reads investor rights' version as it was saved.
func ReadRights(doc storage.Document) (InvestorRights, error) {
	var p rightsPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return InvestorRights{}, err
	}
	out := InvestorRights{BoardSeats: p.BoardSeats, Observer: p.Observer, BoardMajority: p.BoardMajority, Information: p.Information, ProRata: p.ProRata,
		Consents: p.Consents, Agreements: p.Agreements, Note: p.Note}
	var err error
	if out.HolderID, err = uuid.Parse(p.HolderID); err != nil {
		return out, err
	}
	if out.From, err = time.Parse(time.DateOnly, p.From); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// CheckRights is what is wrong with investor rights on their own, every problem at once (ENG-22): no holder, no day or
// one after today, seats outside 0 to 20, no agreements named, a consent matter empty, repeated or too long, too many of
// them, or a text too long.
func CheckRights(r InvestorRights, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeRights, Field: field, Message: msg, Fix: fix})
	}
	if r.HolderID == uuid.Nil {
		add("holder_id", "the holder is missing", "Choose a contact marked shareholder or investor")
	}
	switch {
	case r.From.IsZero():
		add("from", "the day the rights apply from is missing", "Enter the day of the agreement that gives them")
	case !today.IsZero() && r.From.After(today):
		add("from", "the rights apply from a day after today", "Record them once the agreement is signed")
	}
	if r.BoardSeats < 0 || r.BoardSeats > MaxBoardSeats {
		add("board_seats", fmt.Sprintf("%d board seats is outside 0 to %d", r.BoardSeats, MaxBoardSeats), "Enter the directors the holder may appoint")
	}
	if strings.TrimSpace(r.Agreements) == "" {
		add("agreements", "the agreements the rights come from are not named", "Name them, such as Shareholders' agreement 18 Jun 2026; side letter")
	} else if utf8.RuneCountInString(r.Agreements) > MaxAddress {
		add("agreements", fmt.Sprintf("the agreements are longer than %d characters", MaxAddress), "Shorten them")
	}
	if utf8.RuneCountInString(r.Information) > MaxNote {
		add("information", fmt.Sprintf("the information rights are longer than %d characters", MaxNote), "Shorten them")
	}
	if len(r.Consents) > MaxConsents {
		add("consents", fmt.Sprintf("%d consent matters are more than %d", len(r.Consents), MaxConsents), "Group them, or name the agreement's clause")
	}
	for k, c := range r.Consents {
		field := fmt.Sprintf("consents[%d]", k)
		switch {
		case strings.TrimSpace(c) == "":
			add(field, "the consent matter is empty", "Name it, such as Issuing new shares, or remove it")
		case utf8.RuneCountInString(c) > MaxConsentText:
			add(field, fmt.Sprintf("the consent matter is longer than %d characters", MaxConsentText), "Shorten it, or name the agreement's clause")
		case slices.Contains(r.Consents[:k], c):
			add(field, "the consent matter is named twice", "Remove the second")
		}
	}
	if utf8.RuneCountInString(r.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// holderOf is the holder the rights or the entry are about: a contact marked shareholder or investor (field, code).
func holderOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, field, code string) error {
	if id == uuid.Nil {
		return nil
	}
	_, err := Holder(ctx, u, id)
	if probs, ok := err.(ledger.Problems); ok {
		for i := range probs {
			probs[i].Code, probs[i].Field = code, field
		}
		return probs
	}
	return err
}

func checkedRights(ctx context.Context, u storage.UnitOfWork, r InvestorRights, today time.Time) error {
	probs := CheckRights(r, today)
	if err := holderOf(ctx, u, r.HolderID, "holder_id", CodeRights); err != nil {
		more, ok := err.(ledger.Problems)
		if !ok {
			return err
		}
		probs = append(probs, more...)
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftRightsIn saves investor rights to record in u: a draft, to attach the agreements to.
func DraftRightsIn(ctx context.Context, u storage.UnitOfWork, r InvestorRights, today time.Time) (storage.Document, error) {
	r.Date = today
	if err := checkedRights(ctx, u, r, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeRights(r)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentInvestorRights, Payload: b})
}

// ReviseRightsIn saves a new version of investor rights not recorded yet, in u; their holder stays.
func ReviseRightsIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, r InvestorRights, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentInvestorRights, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadRights(doc)
	if err != nil {
		return doc, err
	}
	r.HolderID, r.Date = was.HolderID, was.Date
	if err := checkedRights(ctx, u, r, today); err != nil {
		return doc, err
	}
	b, err := EncodeRights(r)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RightsOf are every investor rights version, drafts and recorded, by holder, in the order they were recorded: the last
// one recorded is in force (InForce).
func RightsOf(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]Recorded[InvestorRights], error) {
	docs, err := all(ctx, u, storage.DocumentInvestorRights)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Recorded[InvestorRights]{}
	for _, d := range docs {
		r, err := ReadRights(d)
		if err != nil {
			return nil, err
		}
		out[r.HolderID] = append(out[r.HolderID], Recorded[InvestorRights]{Doc: d, Value: r, RecordedAt: recordedAt(d)})
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[InvestorRights])
	}
	return out, nil
}

// ControllerNotice is a yearly notice sent to a registrable controller, and the day they replied (FR-EQ-25): the reply
// is due within 30 days.
type ControllerNotice struct {
	Sent    time.Time
	Replied *time.Time
}

// ControllerEntry is the company's conclusion about whether a holder is a registrable controller (FR-EQ-25, design
// D10), as its register of registrable controllers records it. A controller is one since a day, on their grounds,
// entered in the company's register (within 7 days) and lodged in ACRA's central register (within 2 business days
// after), with the yearly notices sent and answered, and the day they ceased. A holder whose rights could confer control
// and who is found not to be one has the reason. The named company secretary confirms each conclusion (open question
// 41): their confirmation is its evidence. Recorded on another person's approval; the last one recorded is in force.
type ControllerEntry struct {
	Date            time.Time // the day it was drafted
	HolderID        uuid.UUID
	Controller      bool
	Grounds         []string
	Reason          string    // why a holder is not a controller
	Since           time.Time // the day they became one, or the day of the conclusion that they are not
	CompanyRegister *time.Time
	CentralRegister *time.Time
	Ceased          *time.Time
	Notices         []ControllerNotice
	Note            string
}

type noticePayload struct {
	Sent    string `json:"sent_on"`
	Replied string `json:"replied_on,omitempty"`
}

type controllerPayload struct {
	Date            string          `json:"date"`
	HolderID        string          `json:"holder_id"`
	Controller      bool            `json:"controller"`
	Grounds         []string        `json:"grounds"`
	Reason          string          `json:"reason,omitempty"`
	Since           string          `json:"since"`
	CompanyRegister string          `json:"company_register_on,omitempty"`
	CentralRegister string          `json:"central_register_on,omitempty"`
	Ceased          string          `json:"ceased_on,omitempty"`
	Notices         []noticePayload `json:"notices"`
	Note            string          `json:"note,omitempty"`
}

func optionalDay(t *time.Time) string {
	if t == nil {
		return ""
	}
	return dateText(*t)
}

func readDay(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	return &d, err
}

// EncodeController is a controller entry as a document version keeps it.
func EncodeController(c ControllerEntry) ([]byte, error) {
	grounds := c.Grounds
	if grounds == nil {
		grounds = []string{}
	}
	notices := []noticePayload{}
	for _, n := range c.Notices {
		notices = append(notices, noticePayload{Sent: dateText(n.Sent), Replied: optionalDay(n.Replied)})
	}
	return json.Marshal(controllerPayload{Date: dateText(c.Date), HolderID: idText(c.HolderID), Controller: c.Controller, Grounds: grounds, Reason: c.Reason,
		Since: dateText(c.Since), CompanyRegister: optionalDay(c.CompanyRegister), CentralRegister: optionalDay(c.CentralRegister),
		Ceased: optionalDay(c.Ceased), Notices: notices, Note: c.Note})
}

// ReadController reads a controller entry's version as it was saved.
func ReadController(doc storage.Document) (ControllerEntry, error) {
	var p controllerPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return ControllerEntry{}, err
	}
	out := ControllerEntry{Controller: p.Controller, Grounds: p.Grounds, Reason: p.Reason, Note: p.Note}
	var err error
	if out.HolderID, err = uuid.Parse(p.HolderID); err != nil {
		return out, err
	}
	if out.Since, err = time.Parse(time.DateOnly, p.Since); err != nil {
		return out, err
	}
	for _, x := range []struct {
		to   **time.Time
		from string
	}{{&out.CompanyRegister, p.CompanyRegister}, {&out.CentralRegister, p.CentralRegister}, {&out.Ceased, p.Ceased}} {
		if *x.to, err = readDay(x.from); err != nil {
			return out, err
		}
	}
	for _, n := range p.Notices {
		var x ControllerNotice
		if x.Sent, err = time.Parse(time.DateOnly, n.Sent); err != nil {
			return out, err
		}
		if x.Replied, err = readDay(n.Replied); err != nil {
			return out, err
		}
		out.Notices = append(out.Notices, x)
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// CheckController is what is wrong with a controller entry on its own, every problem at once (ENG-22). A controller has
// grounds from the four, each once, and no reason; a holder found not to be one has a reason and no grounds, register
// days or notices. Every day is on or before today; the company's register is on or after the day they became one, the
// central register on or after the company's, the day they ceased after both; each notice is sent on or after they
// became one, in order, and answered on or after it was sent.
func CheckController(c ControllerEntry, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeController, Field: field, Message: msg, Fix: fix})
	}
	future := func(field string, d *time.Time, what string) {
		if d != nil && !today.IsZero() && d.After(today) {
			add(field, what+" is after today", "Enter the day it happened")
		}
	}
	if c.HolderID == uuid.Nil {
		add("holder_id", "the holder is missing", "Choose a contact marked shareholder or investor")
	}
	if c.Since.IsZero() {
		add("since", "the day is missing", "Enter the day they became a controller, or the day of the conclusion that they are not")
	} else {
		future("since", &c.Since, "the day they became a controller, or of the conclusion,")
	}
	if c.Controller {
		if len(c.Grounds) == 0 {
			add("grounds", "a controller has no grounds", "Choose at least one: shares, votes, board or influence")
		}
		for k, g := range c.Grounds {
			field := fmt.Sprintf("grounds[%d]", k)
			switch {
			case !slices.Contains(ControllerGrounds, g):
				add(field, fmt.Sprintf("%q is not a ground", g), "Choose shares, votes, board or influence")
			case slices.Contains(c.Grounds[:k], g):
				add(field, fmt.Sprintf("%q is named twice", g), "Remove the second")
			}
		}
		if strings.TrimSpace(c.Reason) != "" {
			add("reason", "a controller has no reason for not being one", "Leave the reason empty, or record that they are not a controller")
		}
	} else {
		if len(c.Grounds) > 0 {
			add("grounds", "a holder who is not a controller has no grounds", "Leave the grounds empty")
		}
		if strings.TrimSpace(c.Reason) == "" {
			add("reason", "the reason they are not a controller is missing",
				"Say why their rights do not confer control, as the company secretary concluded")
		}
		if c.CompanyRegister != nil || c.CentralRegister != nil || c.Ceased != nil || len(c.Notices) > 0 {
			add("controller", "a holder who is not a controller has no register days or notices", "Record them as a controller instead")
		}
	}
	if utf8.RuneCountInString(c.Reason) > MaxNote {
		add("reason", fmt.Sprintf("the reason is longer than %d characters", MaxNote), "Shorten it")
	}
	future("company_register_on", c.CompanyRegister, "the day they were entered in the company's register")
	future("central_register_on", c.CentralRegister, "the day they were lodged in the central register")
	future("ceased_on", c.Ceased, "the day they ceased")
	if c.CompanyRegister != nil && !c.Since.IsZero() && c.CompanyRegister.Before(c.Since) {
		add("company_register_on", "they were entered in the company's register before they became a controller", "Enter the day they were entered")
	}
	if c.CentralRegister != nil {
		if c.CompanyRegister == nil {
			add("central_register_on", "the central register comes after the company's own", "Enter the day they were entered in the company's register first")
		} else if c.CentralRegister.Before(*c.CompanyRegister) {
			add("central_register_on", "they were lodged in the central register before the company's own", "Enter the day ACRA's register was updated")
		}
	}
	if c.Ceased != nil && !c.Since.IsZero() && !c.Ceased.After(c.Since) {
		add("ceased_on", "they ceased on or before the day they became a controller", "Enter the day they ceased")
	}
	if len(c.Notices) > MaxNotices {
		add("notices", fmt.Sprintf("%d notices are more than %d", len(c.Notices), MaxNotices), "Keep the recent years' notices")
	}
	for k, n := range c.Notices {
		field := fmt.Sprintf("notices[%d]", k)
		switch {
		case n.Sent.IsZero():
			add(field+".sent_on", "the day the notice was sent is missing", "Enter it")
		case !c.Since.IsZero() && n.Sent.Before(c.Since):
			add(field+".sent_on", "the notice was sent before they became a controller", "Enter the day it was sent")
		case k > 0 && !n.Sent.After(c.Notices[k-1].Sent):
			add(field+".sent_on", "the notices are not in the order they were sent", "List each year's notice after the one before")
		}
		future(field+".sent_on", &n.Sent, "the notice")
		if n.Replied != nil && n.Replied.Before(n.Sent) {
			add(field+".replied_on", "the reply is dated before the notice was sent", "Enter the day they replied")
		}
		future(field+".replied_on", n.Replied, "the reply")
	}
	if utf8.RuneCountInString(c.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

func checkedController(ctx context.Context, u storage.UnitOfWork, c ControllerEntry, today time.Time) error {
	probs := CheckController(c, today)
	if err := holderOf(ctx, u, c.HolderID, "holder_id", CodeController); err != nil {
		more, ok := err.(ledger.Problems)
		if !ok {
			return err
		}
		probs = append(probs, more...)
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftControllerIn saves a controller entry to record in u: a draft, to attach the company secretary's confirmation to.
func DraftControllerIn(ctx context.Context, u storage.UnitOfWork, c ControllerEntry, today time.Time) (storage.Document, error) {
	c.Date = today
	if err := checkedController(ctx, u, c, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeController(c)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentControllerEntry, Payload: b})
}

// ReviseControllerIn saves a new version of a controller entry not recorded yet, in u; its holder stays.
func ReviseControllerIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c ControllerEntry, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentControllerEntry, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadController(doc)
	if err != nil {
		return doc, err
	}
	c.HolderID, c.Date = was.HolderID, was.Date
	if err := checkedController(ctx, u, c, today); err != nil {
		return doc, err
	}
	b, err := EncodeController(c)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// ControllersOf are every controller entry, drafts and recorded, by holder, in the order they were recorded.
func ControllersOf(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]Recorded[ControllerEntry], error) {
	docs, err := all(ctx, u, storage.DocumentControllerEntry)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Recorded[ControllerEntry]{}
	for _, d := range docs {
		c, err := ReadController(d)
		if err != nil {
			return nil, err
		}
		out[c.HolderID] = append(out[c.HolderID], Recorded[ControllerEntry]{Doc: d, Value: c, RecordedAt: recordedAt(d)})
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[ControllerEntry])
	}
	return out, nil
}

// RecordRightsIn records approved investor rights in u: checked again, their holder still marked, then recorded with
// the agreements attached (RecordIn).
func RecordRightsIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentInvestorRights, id)
	if err != nil {
		return doc, err
	}
	r, err := ReadRights(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedRights(ctx, u, r, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentInvestorRights, id)
}

// RecordControllerIn records an approved controller entry in u: checked again, its holder still marked, then recorded
// with the company secretary's confirmation attached (RecordIn).
func RecordControllerIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentControllerEntry, id)
	if err != nil {
		return doc, err
	}
	c, err := ReadController(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedController(ctx, u, c, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentControllerEntry, id)
}
