// Package payee is a change of a payee's bank account in one currency: a vendor's (FR-CT-03; PRD-001 rev 0.152;
// p1-organisation-chart task 4.2) or a person's own, for reimbursements (FR-AP-22; rev 0.154; p1-payments-out task 1.2):
// what it holds, the checks it is saved, asked for and approved under, and how its account number is normalised, sealed
// and fingerprinted. On the owner's decisions of 27 Oct 2026 a vendor has at most one approved account per currency, a
// Singapore account or an overseas one, and no PayNow proxy; a person has a Singapore account in SGD. The number is
// sealed by the application (SEC-11) and never in the change's version, which keeps its last four characters and its
// keyed fingerprint (FR-CT-04, AUD-02).
package payee

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a change's refusals and warnings (ENG-23).
const (
	CodeNotChange = "PAYEE-01" // the document is not a change of bank details
	CodeBadDraft  = "PAYEE-02" // the saved change cannot be read
	CodeVendor    = "PAYEE-03" // the contact is not an active vendor
	CodeCurrency  = "PAYEE-04" // not a currency Fyniq knows
	CodeBIC       = "PAYEE-05" // not a SWIFT/BIC code
	CodeCountry   = "PAYEE-06" // the bank's country is not a country code, or not the kind's or the BIC's
	CodeNumber    = "PAYEE-07" // not an account number the kind takes, or an IBAN that fails its check
	CodeName      = "PAYEE-08" // the account name is blank or too long
	CodeTooLong   = "PAYEE-09" // the call-back note is too long
	CodeNotDraft  = "PAYEE-10" // only a draft changes or is asked for
	CodeEvidence  = "PAYEE-11" // no bank letter or vendor's confirmation is attached (✖)
	CodeFrozen    = "PAYEE-12" // a change waits for approval: payments to the vendor are frozen
	CodeWaiting   = "PAYEE-13" // another change for the vendor and currency waits for approval already
	CodeUnchanged = "PAYEE-14" // the account is the one in force already
	CodeNoKeys    = "PAYEE-15" // no field keys are configured, so no bank details can be kept
	CodeCallback  = "PAYEE-16" // no call-back note (⚠, recommended)
	CodeDuplicate = "PAYEE-17" // another vendor has this account in force (⚠, FR-CT-04)
	CodeOwnChange = "PAYEE-18" // whoever prepared the change never approves it (SOD-1)
	CodePerson    = "PAYEE-19" // a person's account for reimbursements is a Singapore bank's, in SGD (FR-AP-22)
	CodeMember    = "PAYEE-20" // the person is not an active member
	CodeOwnOnly   = "PAYEE-21" // a person prepares and asks for a change of their own account only, and never approves it (FR-AP-22, FM-1)
)

// The kinds of account.
const (
	Singapore = "singapore" // a Singapore bank's account: paid by FAST or GIRO
	Overseas  = "overseas"  // a bank's account outside Singapore: paid by telegraphic transfer
)

// The evidence types that meet a change's required evidence (FR-DOC-06: a bank letter, or a screenshot of the vendor's
// confirmation through a known channel).
var RequiredEvidence = []string{"bank_letter", "vendor_confirmation"}

// PersonEvidence are the evidence types that meet a person's change's required evidence (FR-DOC-06's payee default): a
// screenshot of their banking app, or a statement's header, showing their name and the number.
var PersonEvidence = []string{"bank_app_screenshot", "bank_statement"}

// Payee is whom a change of bank details pays: a vendor, by its contact, or a person, by their user ID; exactly one.
type Payee struct {
	ContactID uuid.UUID // a vendor; uuid.Nil for a person
	PersonID  uuid.UUID // a member's user ID; uuid.Nil for a vendor
}

// Vendor is a vendor as a payee.
func Vendor(contactID uuid.UUID) Payee { return Payee{ContactID: contactID} }

// Person is a member, by their user ID, as a payee of their own reimbursements.
func Person(userID uuid.UUID) Payee { return Payee{PersonID: userID} }

// IsPerson reports whether the payee is a person.
func (p Payee) IsPerson() bool { return p.PersonID != uuid.Nil }

// DocumentType is the type of the payee's changes.
func (p Payee) DocumentType() storage.DocumentType {
	if p.IsPerson() {
		return storage.DocumentClaimantBankChange
	}
	return storage.DocumentPayeeBankChange
}

// ObjectType is the approval object of the payee's changes.
func (p Payee) ObjectType() string {
	if p.IsPerson() {
		return approval.ObjectClaimantBankChange
	}
	return approval.ObjectPayeeBankChange
}

// Evidence are the evidence types that meet the payee's changes' required evidence.
func (p Payee) Evidence() []string {
	if p.IsPerson() {
		return PersonEvidence
	}
	return RequiredEvidence
}

// Of is the payee of an account.
func Of(a storage.PayeeBankAccount) Payee {
	return Payee{ContactID: a.ContactID, PersonID: a.MemberUserID}
}

// Is reports whether an account is the payee's.
func (p Payee) Is(a storage.PayeeBankAccount) bool { return Of(a) == p }

// The limits of a change's text.
const (
	MaxName     = 140 // the name a payment file carries (ISO 20022's Nm)
	MaxCallback = 500
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// Problems are a change's failed rules, all at once (ENG-21).
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s", x.Code, x.Field, x.Message)
	}
	return strings.Join(parts, "; ")
}

// Error is a change command's refusal with a stable code.
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Draft is a change as entered. Number is the account number as typed: it is sealed on save and never kept in the
// change's version. On a revision, an empty Number keeps the number saved before.
type Draft struct {
	Currency     string
	Kind         string
	BIC          string
	BankCountry  string
	AccountName  string
	Number       string
	CallbackNote string
}

// Stored is a change's version as its document keeps it: everything but the number, whose last four characters and
// fingerprint it repeats from the sealed account (the database checks they agree).
type Stored struct {
	ContactID         string `json:"contact_id,omitempty"`     // a vendor's change
	MemberUserID      string `json:"member_user_id,omitempty"` // a person's change
	Currency          string `json:"currency"`
	Kind              string `json:"kind"`
	BIC               string `json:"bic"`
	BankCountry       string `json:"bank_country"`
	AccountName       string `json:"account_name"`
	NumberKind        string `json:"number_kind"` // account or iban
	NumberLast4       string `json:"number_last4"`
	NumberFingerprint string `json:"number_fingerprint"` // hexadecimal
	CallbackNote      string `json:"callback_note,omitempty"`
}

// Read is a change's version as stored.
func Read(doc storage.Document) (Stored, error) {
	if doc.Type != storage.DocumentPayeeBankChange && doc.Type != storage.DocumentClaimantBankChange {
		return Stored{}, &Error{Code: CodeNotChange, Message: fmt.Sprintf("document %s is a %s, not a change of bank details", doc.ID, doc.Type),
			Fix: "Open it with its own module"}
	}
	var s Stored
	if err := json.Unmarshal(doc.Payload, &s); err != nil {
		return Stored{}, &Error{Code: CodeBadDraft, Message: "the saved change cannot be read", Fix: "Prepare the change again"}
	}
	return s, nil
}

// Payee is the payee a version names.
func (s Stored) Payee() (Payee, error) {
	bad := &Error{Code: CodeBadDraft, Message: "the saved change cannot be read", Fix: "Prepare the change again"}
	switch {
	case s.MemberUserID != "" && s.ContactID == "":
		id, err := uuid.Parse(s.MemberUserID)
		if err != nil {
			return Payee{}, bad
		}
		return Person(id), nil
	case s.ContactID != "" && s.MemberUserID == "":
		id, err := uuid.Parse(s.ContactID)
		if err != nil {
			return Payee{}, bad
		}
		return Vendor(id), nil
	}
	return Payee{}, bad
}

// Same reports whether two versions name the same account: the bank, the country, the name and the number.
func (s Stored) Same(o Stored) bool {
	return s.Kind == o.Kind && s.BIC == o.BIC && s.BankCountry == o.BankCountry && s.AccountName == o.AccountName &&
		s.NumberFingerprint == o.NumberFingerprint
}

var (
	bic       = regexp.MustCompile(`^[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}([A-Z0-9]{3})?$`)
	country   = regexp.MustCompile(`^[A-Z]{2}$`)
	sgNumber  = regexp.MustCompile(`^[0-9]{6,17}$`)
	anyNumber = regexp.MustCompile(`^[A-Z0-9]{4,34}$`)
	ibanShape = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)
)

// Normalise is an account number as kept: capital letters and digits, without the spaces and hyphens people type.
func Normalise(number string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, strings.TrimSpace(number))
}

// IBAN reports whether n (normalised) is an IBAN that passes its ISO 13616 check: mod 97 of the rearranged number is 1.
func IBAN(n string) bool {
	if !ibanShape.MatchString(n) {
		return false
	}
	var b strings.Builder
	for _, r := range n[4:] + n[:4] {
		if r >= 'A' && r <= 'Z' {
			fmt.Fprintf(&b, "%d", r-'A'+10)
		} else {
			b.WriteRune(r)
		}
	}
	v, ok := new(big.Int).SetString(b.String(), 10)
	return ok && new(big.Int).Mod(v, big.NewInt(97)).Int64() == 1
}

// NumberKind is how a normalised number on an overseas account reads: "iban" when it is one, else "account".
func NumberKind(kind, n string) string {
	if kind == Overseas && ibanShape.MatchString(n) {
		return "iban"
	}
	return "account"
}

// Check returns every rule d breaks, or nil. needNumber is false on a revision that keeps the number saved before.
func Check(d Draft, needNumber bool) Problems {
	var out Problems
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if _, err := money.Lookup(d.Currency); err != nil {
		add(CodeCurrency, "currency", fmt.Sprintf("%q is not a currency Fyniq knows", d.Currency), "Use an ISO 4217 code, such as SGD or USD")
	}
	if d.Kind != Singapore && d.Kind != Overseas {
		add(CodeCountry, "kind", fmt.Sprintf("%q is not a kind of account", d.Kind), "Choose singapore or overseas")
	}
	switch {
	case !country.MatchString(d.BankCountry):
		add(CodeCountry, "bank_country", fmt.Sprintf("%q is not a country code", d.BankCountry), "Use the bank's two-letter ISO 3166 code, such as SG or GB")
	case d.Kind == Singapore && d.BankCountry != "SG":
		add(CodeCountry, "bank_country", "a Singapore account is at a bank in Singapore", "Choose overseas for a bank outside Singapore")
	case d.Kind == Overseas && d.BankCountry == "SG":
		add(CodeCountry, "bank_country", "an overseas account is at a bank outside Singapore", "Choose singapore for a bank in Singapore")
	}
	if !bic.MatchString(d.BIC) {
		add(CodeBIC, "bic", fmt.Sprintf("%q is not a SWIFT/BIC code", d.BIC), "Enter the bank's 8 or 11 character code, such as DBSSSGSG")
	} else if country.MatchString(d.BankCountry) && d.BIC[4:6] != d.BankCountry {
		add(CodeCountry, "bic", fmt.Sprintf("the code %s is of a bank in %s, not %s", d.BIC, d.BIC[4:6], d.BankCountry), "Check the bank's code and its country")
	}
	if name := strings.TrimSpace(d.AccountName); name == "" || len([]rune(name)) > MaxName {
		add(CodeName, "account_name", fmt.Sprintf("is blank or longer than %d characters", MaxName), "Enter the name on the account, as the bank holds it")
	}
	if len([]rune(d.CallbackNote)) > MaxCallback {
		add(CodeTooLong, "callback_note", fmt.Sprintf("is longer than %d characters", MaxCallback), "Shorten it: who you called, when, and the number used")
	}
	if needNumber || d.Number != "" {
		n := Normalise(d.Number)
		switch {
		case d.Kind == Singapore && !sgNumber.MatchString(n):
			add(CodeNumber, "account_number", "a Singapore account number is 6 to 17 digits", "Enter the number as the bank prints it; spaces and hyphens are dropped")
		case d.Kind == Overseas && ibanShape.MatchString(n) && !IBAN(n):
			add(CodeNumber, "account_number", "the IBAN fails its check digits", "Check the IBAN against the vendor's letter: a character is wrong")
		case d.Kind == Overseas && ibanShape.MatchString(n) && n[:2] != d.BankCountry:
			add(CodeNumber, "account_number", fmt.Sprintf("the IBAN is of a bank in %s, not %s", n[:2], d.BankCountry), "Check the IBAN and the bank's country")
		case d.Kind == Overseas && !anyNumber.MatchString(n):
			add(CodeNumber, "account_number", "an account number is 4 to 34 letters and digits", "Enter the account number or IBAN as the bank prints it")
		}
	}
	return out
}

// AAD binds a sealed number to its change, vendor and currency in the organisation: copied to another change, it does
// not open.
func AAD(org, changeID, contactID uuid.UUID, currency string) []byte {
	return []byte("payee_bank_account:" + org.String() + ":" + changeID.String() + ":" + contactID.String() + ":" + currency)
}

// aad binds a sealed number to its change, payee and currency: a vendor's as AAD, a person's under its own prefix, so
// neither opens as the other.
func aad(org, changeID uuid.UUID, p Payee, currency string) []byte {
	if p.IsPerson() {
		return []byte("claimant_bank_account:" + org.String() + ":" + changeID.String() + ":" + p.PersonID.String() + ":" + currency)
	}
	return AAD(org, changeID, p.ContactID, currency)
}

// CheckFor returns every rule d breaks for the payee, or nil: Check's, and for a person a Singapore account in SGD.
func CheckFor(p Payee, d Draft, needNumber bool) Problems {
	out := Check(d, needNumber)
	if p.IsPerson() {
		if d.Kind != Singapore {
			out = append(out, Problem{CodePerson, "kind", "an account for reimbursements is a Singapore bank's account",
				"Enter an account at a bank in Singapore: reimbursements are paid by FAST or GIRO"})
		}
		if d.Currency != "SGD" {
			out = append(out, Problem{CodePerson, "currency", "an account for reimbursements is in SGD", "Enter your SGD account"})
		}
	}
	return out
}

// fingerprintOf is the keyed fingerprint of an account: its bank (the BIC's first eight characters) and its number, in
// the organisation. The same number at another bank is another account.
func fingerprintOf(ctx context.Context, c ports.Cipher, org uuid.UUID, bicCode, number string) ([]byte, error) {
	return c.Fingerprint(ctx, org[:], []byte(bicCode[:8]+"|"+number))
}

// Number is a typed number ready to keep: its last four characters, its fingerprint and how it reads; the number itself
// stays in memory until Seal.
type Number struct {
	Last4       string
	Fingerprint []byte
	Kind        string // account or iban
	n           string
}

// NumberOf normalises and fingerprints the number d typed.
func NumberOf(ctx context.Context, c ports.Cipher, org uuid.UUID, d Draft) (Number, error) {
	if c == nil {
		return Number{}, NoKeys()
	}
	n := Normalise(d.Number)
	fp, err := fingerprintOf(ctx, c, org, d.BIC, n)
	if err != nil {
		return Number{}, err
	}
	return Number{Last4: n[max(0, len(n)-4):], Fingerprint: fp, Kind: NumberKind(d.Kind, n), n: n}, nil
}

// Seal is the number sealed for its change, payee and currency.
func (x Number) Seal(ctx context.Context, c ports.Cipher, org, changeID uuid.UUID, p Payee, currency string) ([]byte, error) {
	if c == nil {
		return nil, NoKeys()
	}
	return c.Seal(ctx, []byte(x.n), aad(org, changeID, p, currency))
}

// Open is the full number of a kept account: for a reveal (audited) and a payment file.
func Open(ctx context.Context, c ports.Cipher, org uuid.UUID, a storage.PayeeBankAccount) (string, error) {
	if c == nil {
		return "", NoKeys()
	}
	b, err := c.Open(ctx, a.Sealed, aad(org, a.ChangeID, Of(a), a.Currency))
	if errors.Is(err, ports.ErrNotSealed) {
		return "", &Error{Code: CodeNoKeys, Message: "the account number does not open with this deployment's keys",
			Fix: "Prepare the change again with the number from the bank's letter or app"}
	}
	return string(b), err
}

// NoKeys is the refusal when no field keys are configured (SEC-11).
func NoKeys() error {
	return &Error{Code: CodeNoKeys, Message: "bank details cannot be kept here: no field encryption keys are configured",
		Fix: "Ask your administrator to configure the field keys (SEC-11)"}
}

// Encode is the stored version of d with its number, for a vendor.
func Encode(contactID uuid.UUID, d Draft, x Number) ([]byte, error) {
	return EncodeFor(Vendor(contactID), d, x)
}

// EncodeFor is the stored version of d with its number, for the payee.
func EncodeFor(p Payee, d Draft, x Number) ([]byte, error) {
	s := Stored{Currency: d.Currency, Kind: d.Kind, BIC: d.BIC, BankCountry: d.BankCountry,
		AccountName: strings.TrimSpace(d.AccountName), NumberKind: x.Kind, NumberLast4: x.Last4,
		NumberFingerprint: hex.EncodeToString(x.Fingerprint), CallbackNote: strings.TrimSpace(d.CallbackNote)}
	if p.IsPerson() {
		s.MemberUserID = p.PersonID.String()
	} else {
		s.ContactID = p.ContactID.String()
	}
	return json.Marshal(s)
}

// Kept is the number of a stored version that keeps it on a revision: its last four characters, fingerprint and kind.
func Kept(s Stored) (Number, error) {
	fp, err := hex.DecodeString(s.NumberFingerprint)
	if err != nil {
		return Number{}, &Error{Code: CodeBadDraft, Message: "the saved change cannot be read", Fix: "Prepare the change again"}
	}
	return Number{Last4: s.NumberLast4, Fingerprint: fp, Kind: s.NumberKind}, nil
}
