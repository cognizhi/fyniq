package equity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of holder records and share classes (FR-EQ-15, FR-EQ-16; p1-equity-register design D1, D2).
const (
	CodeNotRecord    = "EQUITY-02" // the document is not the holder record or share class asked for
	CodeHolderRecord = "EQUITY-03" // a holder record that does not fit: its holder, kind, identifier, address or contact people
	CodeShareClass   = "EQUITY-04" // a share class that does not fit: its name, kind, account or rights
	CodeRecorded     = "EQUITY-05" // recorded already, so it changes no more
	CodeNoEvidence   = "EQUITY-06" // asked to record with no evidence attached
)

// Error is a refusal of the equity register in plain words, with its code and how to fix it (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// The longest texts a record keeps.
const (
	MaxNote          = 1000
	MaxAddress       = 500
	MaxContactPeople = 10
)

// The kinds of holder and their identifiers. An individual's identifier is its type and last 4 characters only (the
// owner's decision of 7 Nov 2026, rev 0.203); an entity's is its UEN or foreign registration number.
var (
	IndividualIDs = []string{"nric", "fin", "passport"}
	EntityIDs     = []string{"uen", "foreign_registration"}
)

var (
	last4       = regexp.MustCompile(`^[A-Za-z0-9]{1,4}$`)
	uenFormat   = regexp.MustCompile(`^[0-9A-Z]{9,10}$`)
	fullNRIC    = regexp.MustCompile(`[STFGMstfgm][0-9]{7}[A-Za-z]`)
	decimalText = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,8})?$`)
)

// ContactPerson is someone to contact at a holder.
type ContactPerson struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Email string `json:"email,omitempty"`
}

// HolderRecord is a holder's details (FR-EQ-15): the contact, individual or entity, the identifier as decided, the
// nationality or place of incorporation, the registered address and the people to contact.
type HolderRecord struct {
	ContactID     uuid.UUID
	Kind          string // individual or entity
	IDType        string
	IDLast4       string // an individual's last 4 characters
	Registration  string // an entity's UEN or foreign registration number
	Nationality   string
	Address       string
	ContactPeople []ContactPerson
	Note          string
	Date          time.Time // the day it was drafted: what lists sort by
}

type holderPayload struct {
	Date          string          `json:"date"`
	ContactID     string          `json:"contact_id"`
	Kind          string          `json:"kind"`
	IDType        string          `json:"id_type,omitempty"`
	IDLast4       string          `json:"id_last4,omitempty"`
	Registration  string          `json:"registration_number,omitempty"`
	Nationality   string          `json:"nationality,omitempty"`
	Address       string          `json:"address,omitempty"`
	ContactPeople []ContactPerson `json:"contact_people,omitempty"`
	Note          string          `json:"note,omitempty"`
}

// CheckHolderRecord is what is wrong with a holder record, every problem at once (ENG-22). It refuses a full NRIC or
// FIN anywhere in it, as the database does (FY285).
func CheckHolderRecord(r HolderRecord) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeHolderRecord, Field: field, Message: msg, Fix: fix})
	}
	switch r.Kind {
	case "individual":
		if r.Registration != "" {
			add("registration_number", "an individual has no registration number", "Leave it empty, or mark the holder an entity")
		}
		if r.IDType != "" && !slices.Contains(IndividualIDs, r.IDType) {
			add("id_type", fmt.Sprintf("%q is not an individual's identifier", r.IDType), "Choose nric, fin or passport")
		}
		if (r.IDType == "") != (r.IDLast4 == "") {
			add("id_last4", "the identifier needs its type and its last 4 characters together", "Give both, or neither")
		} else if r.IDLast4 != "" && !last4.MatchString(r.IDLast4) {
			add("id_last4", "Fyniq keeps only the last 4 letters or digits of an individual's identifier",
				"Enter the last 4 characters, such as 567A; the full number stays in ACRA's register and the identity document")
		}
	case "entity":
		if r.IDLast4 != "" {
			add("id_last4", "an entity is identified by its registration number", "Enter its UEN or foreign registration number instead")
		}
		if r.IDType != "" && !slices.Contains(EntityIDs, r.IDType) {
			add("id_type", fmt.Sprintf("%q is not an entity's identifier", r.IDType), "Choose uen or foreign_registration")
		}
		if (r.IDType == "") != (r.Registration == "") {
			add("registration_number", "the identifier needs its type and number together", "Give both, or neither")
		} else if r.IDType == "uen" && !uenFormat.MatchString(r.Registration) {
			add("registration_number", fmt.Sprintf("%q is not a UEN", r.Registration), "Enter the 9 or 10 character UEN, such as 202312345K")
		} else if utf8.RuneCountInString(r.Registration) > 30 {
			add("registration_number", "the registration number is longer than 30 characters", "Enter the number only")
		}
	default:
		add("kind", fmt.Sprintf("%q is not a kind of holder", r.Kind), "Choose individual or entity")
	}
	if utf8.RuneCountInString(r.Nationality) > 60 {
		add("nationality", "the nationality is longer than 60 characters", "Enter the country only")
	}
	if utf8.RuneCountInString(r.Address) > MaxAddress {
		add("address", fmt.Sprintf("the address is longer than %d characters", MaxAddress), "Shorten it")
	}
	if utf8.RuneCountInString(r.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	if len(r.ContactPeople) > MaxContactPeople {
		add("contact_people", fmt.Sprintf("%d people to contact; the most is %d", len(r.ContactPeople), MaxContactPeople), "Keep the main ones")
	}
	for i, p := range r.ContactPeople {
		field := fmt.Sprintf("contact_people[%d]", i)
		if n := utf8.RuneCountInString(strings.TrimSpace(p.Name)); n == 0 || n > 100 {
			add(field+".name", "the name is blank or longer than 100 characters", "Enter the person's name")
		}
		if utf8.RuneCountInString(p.Role) > 60 {
			add(field+".role", "the role is longer than 60 characters", "Shorten it")
		}
		if p.Email != "" {
			if a, err := mail.ParseAddress(p.Email); err != nil || a.Address != p.Email {
				add(field+".email", fmt.Sprintf("%q is not an email address", p.Email), "Enter an address such as jane@example.com")
			}
		}
	}
	text := strings.Join([]string{r.IDLast4, r.Registration, r.Nationality, r.Address, r.Note}, " ")
	for _, p := range r.ContactPeople {
		text += " " + p.Name + " " + p.Role + " " + p.Email
	}
	if fullNRIC.MatchString(text) {
		add("note", "the record shows an NRIC or FIN in full", "Keep only the last 4 characters; the full number stays in ACRA's register and the identity document")
	}
	return probs
}

// EncodeHolderRecord is a holder record as a document version keeps it.
func EncodeHolderRecord(r HolderRecord) ([]byte, error) {
	if probs := CheckHolderRecord(r); len(probs) > 0 {
		return nil, probs
	}
	return json.Marshal(holderPayload{Date: r.Date.Format(time.DateOnly), ContactID: r.ContactID.String(), Kind: r.Kind, IDType: r.IDType,
		IDLast4: r.IDLast4, Registration: r.Registration, Nationality: r.Nationality, Address: r.Address, ContactPeople: r.ContactPeople, Note: r.Note})
}

// ReadHolderRecord reads a holder record's version as it was saved.
func ReadHolderRecord(doc storage.Document) (HolderRecord, error) {
	if doc.Type != storage.DocumentHolderRecord {
		return HolderRecord{}, &Error{CodeNotRecord, fmt.Sprintf("document %s is a %s, not a holder record", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p holderPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return HolderRecord{}, &Error{CodeHolderRecord, "the saved holder record cannot be read: " + err.Error(), "Draft it again"}
	}
	contact, err := uuid.Parse(p.ContactID)
	if err != nil {
		return HolderRecord{}, &Error{CodeHolderRecord, "the saved holder record names no holder", "Draft it again"}
	}
	date, _ := time.Parse(time.DateOnly, p.Date)
	return HolderRecord{ContactID: contact, Kind: p.Kind, IDType: p.IDType, IDLast4: p.IDLast4, Registration: p.Registration, Nationality: p.Nationality,
		Address: p.Address, ContactPeople: p.ContactPeople, Note: p.Note, Date: date}, nil
}

// Holder is a contact the equity register holds: marked shareholder or investor (FR-EQ-15). Anything else is EQUITY-03.
func Holder(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Contact, error) {
	c, err := u.Contacts().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return c, ledger.Problems{{Code: CodeHolderRecord, Field: "contact_id", Message: "the holder does not exist in this organisation",
			Fix: "Choose a contact marked shareholder or investor"}}
	}
	if err != nil {
		return c, err
	}
	if !c.Shareholder && !c.Investor {
		return c, ledger.Problems{{Code: CodeHolderRecord, Field: "contact_id", Message: c.LegalName + " is not marked shareholder or investor",
			Fix: "Mark the contact a shareholder or an investor in Contacts first"}}
	}
	return c, nil
}

// DraftHolderRecordIn saves a holder record to record in u: a draft, to attach its identity document to.
func DraftHolderRecordIn(ctx context.Context, u storage.UnitOfWork, r HolderRecord) (storage.Document, error) {
	if _, err := Holder(ctx, u, r.ContactID); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeHolderRecord(r)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentHolderRecord, Payload: b})
}

// Drafted reads a holder record or share class (of type t) that is not recorded yet, its row locked until commit
// (ENG-47): CodeRecorded when it is.
func Drafted(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != t {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "it is recorded, so it changes no more", "Draft a new one; once recorded, it supersedes this one"}
	}
	return doc, nil
}

// ReviseHolderRecordIn saves a new version of a holder record not recorded yet, in u; its holder stays.
func ReviseHolderRecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, r HolderRecord) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentHolderRecord, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadHolderRecord(doc)
	if err != nil {
		return doc, err
	}
	r.ContactID, r.Date = was.ContactID, was.Date
	b, err := EncodeHolderRecord(r)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// Liquidation is a class's liquidation preference, recorded and not computed in P1 (FR-EQ-16).
type Liquidation struct {
	Multiple      string `json:"multiple,omitempty"` // times the issue price, such as "1"
	Participating bool   `json:"participating,omitempty"`
	CapMultiple   string `json:"cap_multiple,omitempty"` // a participating preference's cap, such as "3"
	Seniority     int    `json:"seniority,omitempty"`    // 1 is paid first
}

// Dividend is a class's dividend right.
type Dividend struct {
	Kind string `json:"kind"`           // none, non_cumulative or cumulative
	Rate string `json:"rate,omitempty"` // a percentage a year, such as "8"
}

// The rights a class records.
var (
	ClassKinds     = []string{"ordinary", "preference"}
	DividendKinds  = []string{"none", "non_cumulative", "cumulative"}
	AntiDilutions  = []string{"none", "broad_based", "full_ratchet"}
	MaxClassName   = 100
	MaxRedemption  = 500
	MaxClassSeries = 20
)

// ShareClass is a share class and its rights (FR-EQ-16). ClassID names the class whose rights it supersedes (its first
// version's document); nil for a new class. Its account never changes.
type ShareClass struct {
	ClassID         *uuid.UUID
	Name            string
	Kind            string // ordinary or preference
	Series          string // a preference series, such as A
	AccountID       uuid.UUID
	VotesPerShare   string
	Liquidation     Liquidation
	Dividend        Dividend
	ConversionRatio string
	AntiDilution    string
	ProRata         bool
	Redemption      string
	Note            string
	Date            time.Time
}

type classPayload struct {
	Date            string      `json:"date"`
	ClassID         string      `json:"class_id,omitempty"`
	Name            string      `json:"name"`
	Kind            string      `json:"kind"`
	Series          string      `json:"series,omitempty"`
	AccountID       string      `json:"account_id"`
	VotesPerShare   string      `json:"votes_per_share"`
	Liquidation     Liquidation `json:"liquidation"`
	Dividend        Dividend    `json:"dividend"`
	ConversionRatio string      `json:"conversion_ratio,omitempty"`
	AntiDilution    string      `json:"anti_dilution"`
	ProRata         bool        `json:"pro_rata,omitempty"`
	Redemption      string      `json:"redemption,omitempty"`
	Note            string      `json:"note,omitempty"`
}

// CheckShareClass is what is wrong with a share class, every problem at once: the account is checked against the chart
// in s (an active share-capital account; FY286 in the database).
func CheckShareClass(c ShareClass, s ledger.Settings) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeShareClass, Field: field, Message: msg, Fix: fix})
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(c.Name)); n == 0 || n > MaxClassName {
		add("name", fmt.Sprintf("the name is blank or longer than %d characters", MaxClassName), "Name it as the constitution does, such as Ordinary shares")
	}
	if !slices.Contains(ClassKinds, c.Kind) {
		add("kind", fmt.Sprintf("%q is not a kind of share", c.Kind), "Choose ordinary or preference")
	}
	if utf8.RuneCountInString(c.Series) > MaxClassSeries {
		add("series", fmt.Sprintf("the series is longer than %d characters", MaxClassSeries), "Enter the series only, such as A")
	}
	if a, ok := s.Accounts[c.AccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("account_id", "the class's account is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	decimal := func(field, v, example string, required bool) {
		if v == "" {
			if required {
				add(field, "it is missing", "Enter it, such as "+example)
			}
			return
		}
		if !decimalText.MatchString(v) {
			add(field, fmt.Sprintf("%q is not a number of zero or more", v), "Enter it as a plain number, such as "+example)
		}
	}
	decimal("votes_per_share", c.VotesPerShare, "1", true)
	decimal("liquidation.multiple", c.Liquidation.Multiple, "1", false)
	decimal("liquidation.cap_multiple", c.Liquidation.CapMultiple, "3", false)
	decimal("conversion_ratio", c.ConversionRatio, "1", false)
	decimal("dividend.rate", c.Dividend.Rate, "8", false)
	if c.Liquidation.CapMultiple != "" && !c.Liquidation.Participating {
		add("liquidation.cap_multiple", "only a participating preference has a cap", "Mark it participating, or leave the cap empty")
	}
	if c.Liquidation.Seniority < 0 || c.Liquidation.Seniority > 20 {
		add("liquidation.seniority", "the seniority is outside 0 to 20", "Enter 1 for the class paid first")
	}
	if !slices.Contains(DividendKinds, c.Dividend.Kind) {
		add("dividend.kind", fmt.Sprintf("%q is not a dividend right", c.Dividend.Kind), "Choose none, non_cumulative or cumulative")
	}
	if !slices.Contains(AntiDilutions, c.AntiDilution) {
		add("anti_dilution", fmt.Sprintf("%q is not an anti-dilution right", c.AntiDilution), "Choose none, broad_based or full_ratchet")
	}
	if utf8.RuneCountInString(c.Redemption) > MaxRedemption {
		add("redemption", fmt.Sprintf("the redemption terms are longer than %d characters", MaxRedemption), "Summarise them; attach the agreement")
	}
	if utf8.RuneCountInString(c.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// EncodeShareClass is a share class as a document version keeps it.
func EncodeShareClass(c ShareClass, s ledger.Settings) ([]byte, error) {
	if probs := CheckShareClass(c, s); len(probs) > 0 {
		return nil, probs
	}
	p := classPayload{Date: c.Date.Format(time.DateOnly), Name: strings.TrimSpace(c.Name), Kind: c.Kind, Series: c.Series, AccountID: c.AccountID.String(),
		VotesPerShare: c.VotesPerShare, Liquidation: c.Liquidation, Dividend: c.Dividend, ConversionRatio: c.ConversionRatio, AntiDilution: c.AntiDilution,
		ProRata: c.ProRata, Redemption: c.Redemption, Note: c.Note}
	if c.ClassID != nil {
		p.ClassID = c.ClassID.String()
	}
	return json.Marshal(p)
}

// ReadShareClass reads a share class's version as it was saved.
func ReadShareClass(doc storage.Document) (ShareClass, error) {
	if doc.Type != storage.DocumentShareClass {
		return ShareClass{}, &Error{CodeNotRecord, fmt.Sprintf("document %s is a %s, not a share class", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p classPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return ShareClass{}, &Error{CodeShareClass, "the saved share class cannot be read: " + err.Error(), "Draft it again"}
	}
	account, err := uuid.Parse(p.AccountID)
	if err != nil {
		return ShareClass{}, &Error{CodeShareClass, "the saved share class names no account", "Draft it again"}
	}
	out := ShareClass{Name: p.Name, Kind: p.Kind, Series: p.Series, AccountID: account, VotesPerShare: p.VotesPerShare, Liquidation: p.Liquidation,
		Dividend: p.Dividend, ConversionRatio: p.ConversionRatio, AntiDilution: p.AntiDilution, ProRata: p.ProRata, Redemption: p.Redemption, Note: p.Note}
	out.Date, _ = time.Parse(time.DateOnly, p.Date)
	if p.ClassID != "" {
		id, err := uuid.Parse(p.ClassID)
		if err != nil {
			return ShareClass{}, &Error{CodeShareClass, "the saved share class names a class that is not an ID", "Draft it again"}
		}
		out.ClassID = &id
	}
	return out, nil
}

// Votes reads a class's votes per share exactly.
func (c ShareClass) Votes() (apd.Decimal, error) {
	var d apd.Decimal
	_, _, err := d.SetString(c.VotesPerShare)
	return d, err
}

// Root is the class a version belongs to: the document it supersedes the rights of, or its own.
func Root(doc storage.Document, c ShareClass) uuid.UUID {
	if c.ClassID != nil {
		return *c.ClassID
	}
	return doc.ID
}

// DraftShareClassIn saves a share class to record in u: a draft, to attach the constitution and the shareholders'
// agreement to. A new version of a class's rights names the class, keeps its account, and is drafted only once the
// class is recorded.
func DraftShareClassIn(ctx context.Context, u storage.UnitOfWork, c ShareClass, s ledger.Settings) (storage.Document, error) {
	if c.ClassID != nil {
		cur, err := Class(ctx, u, *c.ClassID)
		if err != nil {
			return storage.Document{}, err
		}
		if cur.Class.AccountID != c.AccountID {
			return storage.Document{}, ledger.Problems{{Code: CodeShareClass, Field: "account_id", Message: "a class keeps its account",
				Fix: "Draft a new class for the other account"}}
		}
	}
	b, err := EncodeShareClass(c, s)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentShareClass, Payload: b})
}

// ReviseShareClassIn saves a new version of a share class not recorded yet, in u; the class it supersedes stays.
func ReviseShareClassIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c ShareClass, s ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentShareClass, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadShareClass(doc)
	if err != nil {
		return doc, err
	}
	c.ClassID, c.Date = was.ClassID, was.Date
	if was.ClassID != nil && c.AccountID != was.AccountID {
		return doc, ledger.Problems{{Code: CodeShareClass, Field: "account_id", Message: "a class keeps its account", Fix: "Draft a new class for the other account"}}
	}
	b, err := EncodeShareClass(c, s)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordIn records a drafted holder record or share class (of type t) in u, on its approval: append-only from then on,
// superseding the one before it. It needs its evidence attached (EQUITY-06) and posts no entry.
func RecordIn(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, t, id)
	if err != nil {
		return doc, err
	}
	if n, err := attached(ctx, u, id); err != nil {
		return doc, err
	} else if n == 0 {
		fix, ok := evidenceFix[t]
		if !ok {
			fix = "Attach the identity document, or the constitution and the shareholders' agreement"
		}
		return doc, &Error{CodeNoEvidence, "it has no evidence attached", fix + ", then ask again"}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// evidenceFix is what each kind of record takes as its evidence (EQUITY-06), where it is not a holder's identity
// document or a class's constitution.
var evidenceFix = map[storage.DocumentType]string{
	storage.DocumentInstrument:         "Attach the signed instrument",
	storage.DocumentInstrumentTemplate: "Attach the lawyer's confirmation",
	storage.DocumentOptionPool:         "Attach the resolution",
	storage.DocumentOptionGrant:        "Attach the grant letter",
}

// attached counts the files attached to a document and not removed.
func attached(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	n, after := 0, uuid.Nil
	for {
		page, err := u.Evidence().OfDocument(ctx, id, after, 200)
		if err != nil {
			return 0, err
		}
		for _, e := range page {
			if e.State != storage.EvidenceRemoved {
				n++
			}
			after = e.ID
		}
		if len(page) < 200 {
			return n, nil
		}
	}
}

// Recorded is a recorded holder record or share class version, with when it was recorded.
type Recorded[T any] struct {
	Doc        storage.Document
	Value      T
	RecordedAt time.Time
}

// all reads every document of type t, drafts and recorded, oldest first.
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

func recordedAt(d storage.Document) time.Time {
	if d.PostedAt != nil {
		return *d.PostedAt
	}
	return time.Time{}
}

// HolderRecords are every holder record, drafts and recorded, by holder; each holder's recorded ones are in the order
// they were recorded, and the last one is in force.
func HolderRecords(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]Recorded[HolderRecord], error) {
	docs, err := all(ctx, u, storage.DocumentHolderRecord)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Recorded[HolderRecord]{}
	for _, d := range docs {
		r, err := ReadHolderRecord(d)
		if err != nil {
			return nil, err
		}
		out[r.ContactID] = append(out[r.ContactID], Recorded[HolderRecord]{Doc: d, Value: r, RecordedAt: recordedAt(d)})
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[HolderRecord])
	}
	return out, nil
}

// byRecording orders drafts last, recorded ones by when they were recorded.
func byRecording[T any](a, b Recorded[T]) int {
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

// InForce is the last recorded one of a holder's or class's versions, nil when none is recorded.
func InForce[T any](versions []Recorded[T]) *Recorded[T] {
	var out *Recorded[T]
	for i := range versions {
		if versions[i].Doc.Status == storage.DocumentPosted {
			out = &versions[i]
		}
	}
	return out
}

// ShareClasses are every share class version, drafts and recorded, by class (its first version's document).
func ShareClasses(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]Recorded[ShareClass], error) {
	docs, err := all(ctx, u, storage.DocumentShareClass)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Recorded[ShareClass]{}
	for _, d := range docs {
		c, err := ReadShareClass(d)
		if err != nil {
			return nil, err
		}
		root := Root(d, c)
		out[root] = append(out[root], Recorded[ShareClass]{Doc: d, Value: c, RecordedAt: recordedAt(d)})
	}
	for k := range out {
		slices.SortStableFunc(out[k], byRecording[ShareClass])
	}
	return out, nil
}

// ClassInForce is a recorded class's rights in force, and its ID (its first version's document).
type ClassInForce struct {
	ID    uuid.UUID
	Class ShareClass
	Doc   storage.Document
}

// Class is the recorded class with its rights in force: not found when it is not a recorded class.
func Class(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ClassInForce, error) {
	classes, err := ShareClasses(ctx, u)
	if err != nil {
		return ClassInForce{}, err
	}
	cur := InForce(classes[id])
	if cur == nil {
		return ClassInForce{}, ports.ErrNotFound
	}
	return ClassInForce{ID: id, Class: cur.Value, Doc: cur.Doc}, nil
}

// DefaultClass is the class an issue of an account takes when it names none: the account's first recorded class, or
// nil while the account has none (design D2; FY287).
func DefaultClass(ctx context.Context, u storage.UnitOfWork, account uuid.UUID) (*uuid.UUID, error) {
	classes, err := ShareClasses(ctx, u)
	if err != nil {
		return nil, err
	}
	var best *uuid.UUID
	var at time.Time
	for id, versions := range classes {
		first := -1
		for i, v := range versions {
			if v.Doc.Status == storage.DocumentPosted {
				first = i
				break
			}
		}
		if first < 0 || versions[first].Value.AccountID != account {
			continue
		}
		if best == nil || versions[first].RecordedAt.Before(at) || (versions[first].RecordedAt.Equal(at) && id.String() < best.String()) {
			id := id
			best, at = &id, versions[first].RecordedAt
		}
	}
	return best, nil
}
