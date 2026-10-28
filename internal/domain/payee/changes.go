package payee

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The commands of a change run in the caller's unit of work, so the API checks, saves and audits together (ENG-02).

// vendor is the contact a change is for, refused unless it is an active vendor.
func vendor(ctx context.Context, u storage.UnitOfWork, contactID uuid.UUID) (storage.Contact, error) {
	c, err := u.Contacts().Get(ctx, contactID)
	if err != nil {
		return c, err
	}
	if !c.Vendor || !c.Active {
		return c, Problems{{CodeVendor, "contact_id", fmt.Sprintf("%s is not an active vendor", c.LegalName),
			"Mark the contact as a vendor, or choose the vendor you pay"}}
	}
	return c, nil
}

// payable refuses a payee that cannot be paid: a contact that is not an active vendor, or a person who is not an active
// member.
func payable(ctx context.Context, u storage.UnitOfWork, p Payee) error {
	if !p.IsPerson() {
		_, err := vendor(ctx, u, p.ContactID)
		return err
	}
	m, err := u.Members().Membership(ctx, p.PersonID)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && m.Status != "active") {
		return Problems{{CodeMember, "member_user_id", "the person is not an active member", "Only a member keeps a bank account for reimbursements"}}
	}
	return err
}

// Create saves a draft change of a vendor's bank account in one currency (✎), its number sealed with it. Asking for
// approval comes next, once its files are attached.
func Create(ctx context.Context, u storage.UnitOfWork, c ports.Cipher, org, contactID uuid.UUID, d Draft) (storage.Document, error) {
	return CreateFor(ctx, u, c, org, Vendor(contactID), d)
}

// CreateFor saves a draft change of the payee's bank account in one currency: a vendor's, or a person's own (FR-AP-22).
func CreateFor(ctx context.Context, u storage.UnitOfWork, c ports.Cipher, org uuid.UUID, p Payee, d Draft) (storage.Document, error) {
	if c == nil {
		return storage.Document{}, NoKeys()
	}
	if err := payable(ctx, u, p); err != nil {
		return storage.Document{}, err
	}
	if probs := CheckFor(p, d, true); len(probs) > 0 {
		return storage.Document{}, probs
	}
	x, err := NumberOf(ctx, c, org, d)
	if err != nil {
		return storage.Document{}, err
	}
	payload, err := EncodeFor(p, d, x)
	if err != nil {
		return storage.Document{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: p.DocumentType(), Payload: payload})
	if err != nil {
		return doc, err
	}
	sealed, err := x.Seal(ctx, c, org, doc.ID, p, d.Currency)
	if err != nil {
		return doc, err
	}
	_, err = u.PayeeBankAccounts().Add(ctx, storage.PayeeBankAccount{ChangeID: doc.ID, ChangeVersion: doc.Version, ContactID: p.ContactID,
		MemberUserID: p.PersonID, Currency: d.Currency, Sealed: sealed, Last4: x.Last4, Fingerprint: x.Fingerprint})
	return doc, err
}

// Revise saves a draft change's new content as its next version. Its payee and currency stay; an empty number keeps the
// number saved before, unless the bank changes, which needs the number typed again (the fingerprint is of the bank and
// the number).
func Revise(ctx context.Context, u storage.UnitOfWork, c ports.Cipher, org, id uuid.UUID, d Draft) (storage.Document, error) {
	if c == nil {
		return storage.Document{}, NoKeys()
	}
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	before, err := Read(doc)
	if err != nil {
		return doc, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{Code: CodeNotDraft, Message: "the change is " + StatusOf(doc, false), Fix: "An approved change never changes: prepare another"}
	}
	p, err := before.Payee()
	if err != nil {
		return doc, err
	}
	probs := CheckFor(p, d, false)
	if d.Currency != before.Currency {
		probs = append(probs, Problem{CodeCurrency, "currency", "a change keeps the currency it was prepared for", "Prepare another change for the other currency"})
	}
	if d.Number == "" && len(d.BIC) >= 8 && len(before.BIC) >= 8 && d.BIC[:8] != before.BIC[:8] {
		probs = append(probs, Problem{CodeNumber, "account_number", "the bank changed, so the number is needed again", "Type the account number at the new bank"})
	}
	if len(probs) > 0 {
		return doc, probs
	}
	if err := payable(ctx, u, p); err != nil {
		return doc, err
	}
	var x Number
	var sealed []byte
	if d.Number == "" {
		prev, err := u.PayeeBankAccounts().OfChange(ctx, id, doc.Version)
		if err != nil {
			return doc, err
		}
		if x, err = Kept(before); err != nil {
			return doc, err
		}
		sealed = prev.Sealed // bound to the change, its payee and its currency, which stay: it opens at any version
	} else if x, err = NumberOf(ctx, c, org, d); err != nil {
		return doc, err
	}
	payload, err := EncodeFor(p, d, x)
	if err != nil {
		return doc, err
	}
	out, err := u.Documents().Revise(ctx, id, payload)
	if err != nil {
		return out, err
	}
	if sealed == nil {
		if sealed, err = x.Seal(ctx, c, org, id, p, d.Currency); err != nil {
			return out, err
		}
	}
	_, err = u.PayeeBankAccounts().Add(ctx, storage.PayeeBankAccount{ChangeID: id, ChangeVersion: out.Version, ContactID: p.ContactID,
		MemberUserID: p.PersonID, Currency: d.Currency, Sealed: sealed, Last4: x.Last4, Fingerprint: x.Fingerprint})
	return out, err
}

// accounts are the account of each change of the payee's bank details, at the change's current version, newest first.
func accounts(ctx context.Context, u storage.UnitOfWork, p Payee) ([]storage.PayeeBankAccount, error) {
	if p.IsPerson() {
		return u.PayeeBankAccounts().OfMember(ctx, p.PersonID)
	}
	return u.PayeeBankAccounts().OfContact(ctx, p.ContactID)
}

// Accounts are the account of each change of the payee's bank details, at the change's current version, newest first.
func Accounts(ctx context.Context, u storage.UnitOfWork, p Payee) ([]storage.PayeeBankAccount, error) {
	return accounts(ctx, u, p)
}

// Waiting are the changes of a vendor's bank details that wait for approval at now, by change, with their requests:
// while one waits, payments to the vendor are frozen (FR-CT-03).
func Waiting(ctx context.Context, u storage.UnitOfWork, contactID uuid.UUID, now time.Time) (map[uuid.UUID]storage.ApprovalRequest, error) {
	return WaitingFor(ctx, u, Vendor(contactID), now)
}

// WaitingFor are the changes of the payee's bank details that wait for approval at now, by change, with their requests:
// while one waits, payments to the payee are frozen (FR-CT-03, FR-AP-22).
func WaitingFor(ctx context.Context, u storage.UnitOfWork, p Payee, now time.Time) (map[uuid.UUID]storage.ApprovalRequest, error) {
	accounts, err := accounts(ctx, u, p)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]storage.ApprovalRequest{}
	if len(accounts) == 0 {
		return out, nil
	}
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return nil, err
	}
	for _, r := range pending {
		if r.ObjectType != p.ObjectType() ||
			approval.StatusAt(approval.Request{Status: approval.Status(r.Status), RequestedBy: r.RequestedBy, ExpiresAt: r.ExpiresAt}, now) != approval.Pending {
			continue
		}
		for _, a := range accounts {
			if a.ChangeID.String() == r.ObjectKey {
				out[a.ChangeID] = r
			}
		}
	}
	return out, nil
}

// Frozen refuses paying a vendor while a change of its bank details waits for approval (FR-CT-03; the owner's decision
// of 27 Oct 2026: planning a payment date is refused until the change is approved, rejected or withdrawn).
func Frozen(ctx context.Context, u storage.UnitOfWork, contactID uuid.UUID, now time.Time) error {
	return FrozenFor(ctx, u, Vendor(contactID), now)
}

// FrozenFor refuses paying the payee while a change of their bank details waits for approval (FR-CT-03, FR-AP-22).
func FrozenFor(ctx context.Context, u storage.UnitOfWork, p Payee, now time.Time) error {
	w, err := WaitingFor(ctx, u, p, now)
	if err != nil || len(w) == 0 {
		return err
	}
	if p.IsPerson() {
		return &Error{Code: CodeFrozen, Message: "a change of the person's bank account waits for approval, so payments to them are frozen",
			Fix: "Have the change approved, rejected or withdrawn first, then pay"}
	}
	return &Error{Code: CodeFrozen, Message: "a change of the vendor's bank details waits for approval, so payments to it are frozen",
		Fix: "Have the change approved, rejected or withdrawn first, then plan the payment"}
}

// InForce is the vendor's account in force for a currency, if it has one.
func InForce(ctx context.Context, u storage.UnitOfWork, contactID uuid.UUID, currency string) (*storage.PayeeBankAccount, error) {
	return InForceFor(ctx, u, Vendor(contactID), currency)
}

// InForceFor is the payee's account in force for a currency, if they have one.
func InForceFor(ctx context.Context, u storage.UnitOfWork, p Payee, currency string) (*storage.PayeeBankAccount, error) {
	accounts, err := accounts(ctx, u, p)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.Currency == currency && a.State == storage.AccountInForce {
			return &a, nil
		}
	}
	return nil, nil
}

// HasEvidence reports whether a vendor's change has one of its required files attached and not removed (FR-DOC-06).
func HasEvidence(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (bool, error) {
	return hasEvidence(ctx, u, id, RequiredEvidence)
}

// hasEvidence reports whether a change has a file of one of the types attached and not removed.
func hasEvidence(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, types []string) (bool, error) {
	after := uuid.Nil
	for {
		files, err := u.Evidence().OfDocument(ctx, id, after, 100)
		if err != nil {
			return false, err
		}
		for _, f := range files {
			if f.State != storage.EvidenceRemoved && slices.Contains(types, f.EvidenceType) {
				return true, nil
			}
		}
		if len(files) < 100 {
			return false, nil
		}
		after = files[len(files)-1].ID
	}
}

// Checks are what stops a draft change being asked for or approved (✖) and what it is asked for with (⚠), all at once.
func Checks(ctx context.Context, u storage.UnitOfWork, doc storage.Document, now time.Time) (errs, warnings Problems, err error) {
	s, err := Read(doc)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.Payee()
	if err != nil {
		return nil, nil, err
	}
	if err := payable(ctx, u, p); err != nil {
		if probs, ok := err.(Problems); ok {
			errs = append(errs, probs...)
		} else {
			return nil, nil, err
		}
	}
	ok, err := hasEvidence(ctx, u, doc.ID, p.Evidence())
	if err != nil {
		return nil, nil, err
	}
	switch {
	case !ok && p.IsPerson():
		errs = append(errs, Problem{CodeEvidence, "evidence", "no screenshot of your banking app or statement header is attached",
			"Attach a screenshot of your banking app, or your statement's header, showing your name and the account number (evidence type bank_app_screenshot or bank_statement)"})
	case !ok:
		errs = append(errs, Problem{CodeEvidence, "evidence", "no bank letter or vendor's confirmation is attached",
			"Attach the vendor's letter, or a screenshot of their confirmation through a channel you already knew (evidence type bank_letter or vendor_confirmation)"})
	}
	current, err := InForceFor(ctx, u, p, s.Currency)
	if err != nil {
		return nil, nil, err
	}
	if current != nil {
		in, err := u.Documents().Get(ctx, current.ChangeID)
		if err != nil {
			return nil, nil, err
		}
		if cur, err := Read(in); err == nil && cur.Same(s) {
			errs = append(errs, Problem{CodeUnchanged, "account_number", "this is the account in force already", "Nothing to change"})
		}
	}
	waiting, err := WaitingFor(ctx, u, p, now)
	if err != nil {
		return nil, nil, err
	}
	for id := range waiting {
		if id == doc.ID {
			continue
		}
		other, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if o, err := Read(other); err == nil && o.Currency == s.Currency {
			errs = append(errs, Problem{CodeWaiting, "currency", fmt.Sprintf("another change of the %s account waits for approval", s.Currency),
				"Have that change approved, rejected or withdrawn first"})
		}
	}
	if s.CallbackNote == "" && !p.IsPerson() {
		warnings = append(warnings, Problem{CodeCallback, "callback_note", "no call-back note",
			"Call the vendor on a number you already had, not one from the letter, and note who, when and the number used"})
	}
	fp, _ := Kept(s)
	same, err := u.PayeeBankAccounts().WithFingerprint(ctx, fp.Fingerprint)
	if err != nil {
		return nil, nil, err
	}
	for _, a := range same {
		if p.Is(a) || !bytes.Equal(a.Fingerprint, fp.Fingerprint) {
			continue
		}
		if Of(a).IsPerson() { // a person's name is not shown: the approver opens the people's accounts to look
			warnings = append(warnings, Problem{CodeDuplicate, "account_number", "a person has this account in force for their reimbursements",
				"Check the account is the payee's own: a payment to a member's account in another payee's name is how money is diverted (FR-CT-04)"})
			continue
		}
		c, err := u.Contacts().Get(ctx, a.ContactID)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, Problem{CodeDuplicate, "account_number", fmt.Sprintf("%s has this account in force", c.LegalName),
			"Check it is not the same supplier twice, or a payment diverted to another vendor's account (FR-CT-04)"})
	}
	return errs, warnings, nil
}

// Approve puts a change in force on its approval, as the approver, in the approval's unit of work: its checks again, its
// document posted with no entry, the account it replaces retired and its own put in force. The database refuses it
// unless the request is approved and neither its requester, nor the change's preparer, nor the person whose account it
// is signed (CodePayee).
func Approve(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, now time.Time) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	s, err := Read(doc)
	if err != nil {
		return doc, err
	}
	p, err := s.Payee()
	if err != nil {
		return doc, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{Code: CodeNotDraft, Message: "the change is " + StatusOf(doc, false), Fix: "Only a change waiting for approval is approved"}
	}
	errs, _, err := Checks(ctx, u, doc, now)
	if err != nil {
		return doc, err
	}
	// The change's own request is approved by now, so it no longer waits; any other for the currency still would.
	if len(errs) > 0 {
		return doc, errs
	}
	current, err := InForceFor(ctx, u, p, s.Currency)
	if err != nil {
		return doc, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	own, err := u.PayeeBankAccounts().OfChange(ctx, id, doc.Version)
	if err != nil {
		return doc, err
	}
	if current != nil {
		if err := u.PayeeBankAccounts().Move(ctx, *current, storage.AccountRetired); err != nil {
			return doc, err
		}
	}
	if err := u.PayeeBankAccounts().Move(ctx, own, storage.AccountInForce); err != nil {
		return doc, err
	}
	doc.Status = storage.DocumentPosted
	return doc, nil
}

// StatusOf is a change's status as people say it: draft, waiting (for approval), approved (in force) or replaced.
func StatusOf(doc storage.Document, waiting bool) string {
	switch {
	case doc.Status == storage.DocumentDraft && waiting:
		return "waiting"
	case doc.Status == storage.DocumentDraft:
		return "draft"
	}
	return "approved"
}
