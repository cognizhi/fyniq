package proposal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/contact"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ct.update_contact (PRD-001 Appendix E; p1-ai-proposals task 4.1, design D8, AI-31, AI-91): six details of an active company contact, changed through the
// contact's own rules (contact.Check, the default category's check), as the approving person. The payload names only those six: the legal name, the
// UEN or tax ID, the Peppol ID, the remittance email, the country, the tax residence, the currency, the discount terms, the customer group, the
// activity flag, the director, shareholder, investor and individual flags, the linked member and every bank detail are not in the schema, the command
// refuses a payload that carries one, and the executor's column grant does not hold them (migration 000960). A contact that is a person's (an
// individual, a director, a shareholder, an investor, a linked member) is never changed by the AI: their values are sealed in the audit trail, and
// the undo would have to keep the old ones somewhere unsealed. The undo restores the old values through the same narrow query, as the person who
// undoes it, only while the contact is at the version the execution left it.

// ContactUpdatePayload is ct.update_contact's payload: a field left out is left alone, and none can be cleared (a cleared detail is a person's
// decision).
type ContactUpdatePayload struct {
	ContactID        string  `json:"contact_id"`
	BillingEmail     *string `json:"billing_email"`
	Phone            *string `json:"phone"`
	Address          *string `json:"address"`
	PaymentTermsDays *int    `json:"payment_terms_days"`
	DefaultCategory  *string `json:"default_category"`
	DefaultGSTCode   *string `json:"default_gst_code"`
}

func contactKey(id uuid.UUID) string { return "contact:" + id.String() }

// contactUpdateIn reads the payload strictly: a field the task type does not name is refused, whatever the schema said.
func contactUpdateIn(payload json.RawMessage) (ContactUpdatePayload, error) {
	var p ContactUpdatePayload
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, refusal("the payload holds a field ct.update_contact does not change: "+err.Error(),
			"Change only the billing email, phone, address, payment terms, default category or default GST code; anything else, bank details included, is a person's change")
	}
	return p, nil
}

// contactUpdate is the contact as it is and as it would be.
type contactUpdate struct {
	before, after storage.Contact
	fields        storage.ContactAIFields
	diff          []map[string]any
}

func (p ContactUpdatePayload) plan(ctx context.Context, u storage.UnitOfWork) (contactUpdate, error) {
	var out contactUpdate
	id, err := uuid.Parse(p.ContactID)
	if err != nil {
		return out, refusal("payload.contact_id is not an ID", "Use the contact's ID as records_search returned it")
	}
	out.before, err = u.Contacts().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return out, refusal("payload.contact_id is not a contact of this organisation", "Use the ID of a contact as records_search returned it")
	}
	if err != nil {
		return out, err
	}
	c := out.before
	if !c.Active {
		return out, refusal("the contact is inactive", "Only a person changes an inactive contact, after making it active again")
	}
	if c.Individual || c.Director || c.Shareholder || c.Investor || c.MemberUserID != nil {
		return out, refusal("the contact is a person's: an individual, a director, a shareholder, an investor or a signed-in member",
			"A person changes the details of a person's own record; the AI never does")
	}
	d := c.ContactDetails
	note := func(field string, before, after any) {
		out.diff = append(out.diff, map[string]any{"field": field, "before": before, "after": after})
	}
	text := func(field string, cur *string, set *bool, dst *string, v *string) {
		if v == nil {
			return
		}
		next := strings.TrimSpace(*v)
		if next == "" {
			return // nothing to set: a field cannot be cleared
		}
		if next != *cur {
			note(field, nilIfBlank(*cur), next)
			*set, *dst, *cur = true, next, next
		}
	}
	text("billing_email", &d.BillingEmail, &out.fields.SetBillingEmail, &out.fields.BillingEmail, p.BillingEmail)
	text("phone", &d.Phone, &out.fields.SetPhone, &out.fields.Phone, p.Phone)
	text("address", &d.Address, &out.fields.SetAddress, &out.fields.Address, p.Address)
	text("default_gst_code", &d.DefaultGSTCode, &out.fields.SetDefaultGSTCode, &out.fields.DefaultGSTCode, p.DefaultGSTCode)
	if p.PaymentTermsDays != nil && (d.PaymentTermsDays == nil || *d.PaymentTermsDays != *p.PaymentTermsDays) {
		var was any
		if d.PaymentTermsDays != nil {
			was = *d.PaymentTermsDays
		}
		note("payment_terms_days", was, *p.PaymentTermsDays)
		n := *p.PaymentTermsDays
		d.PaymentTermsDays, out.fields.SetPaymentTerms, out.fields.PaymentTermsDays = &n, true, &n
	}
	var bad []contact.Problem
	if p.DefaultCategory != nil && strings.TrimSpace(*p.DefaultCategory) != "" {
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return out, err
		}
		i := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == strings.TrimSpace(*p.DefaultCategory) })
		if i < 0 {
			return out, refusal("payload.default_category is not the code of an account of this organisation", "Use the code of an active account that takes postings, as records_search returned it")
		}
		bad = append(bad, contact.CheckCategory(accounts[i])...)
		if d.DefaultAccountID == nil || *d.DefaultAccountID != accounts[i].ID {
			var was any
			if d.DefaultAccountID != nil {
				if k := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.ID == *d.DefaultAccountID }); k >= 0 {
					was = accounts[k].Code
				}
			}
			note("default_category", was, accounts[i].Code)
			id := accounts[i].ID
			d.DefaultAccountID, out.fields.SetDefaultCategory, out.fields.DefaultAccountID = &id, true, &id
		}
	}
	bad = append(bad, contact.Check(d)...)
	if len(bad) > 0 {
		return out, refusal("payload."+bad[0].Field+" "+bad[0].Message, bad[0].Fix)
	}
	if len(out.diff) == 0 {
		return out, refusal("nothing would change: the contact holds these details already, or the payload gives none", "Give at least one detail that differs from the contact's")
	}
	out.after = c
	out.after.ContactDetails = d
	return out, nil
}

func nilIfBlank(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UpdateContact is ct.update_contact's command.
type UpdateContact struct{}

// Policy implements Command: the right to change a contact.
func (UpdateContact) Policy() authz.Policy { return authz.UpdateContact }

// Measure implements Command: one contact, no amount.
func (UpdateContact) Measure(payload json.RawMessage) (Measure, error) {
	if _, err := contactUpdateIn(payload); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command: what would change, field by field, and the contact's version.
func (UpdateContact) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	p, err := contactUpdateIn(payload)
	if err != nil {
		return Impact{}, err
	}
	plan, err := p.plan(ctx, u)
	if err != nil {
		return Impact{}, err
	}
	names := make([]string, 0, len(plan.diff))
	for _, d := range plan.diff {
		names = append(names, strings.ReplaceAll(d["field"].(string), "_", " "))
	}
	changes, err := json.Marshal(map[string]any{"contacts": []map[string]any{{"change": "update", "contact": plan.before.LegalName, "fields": plan.diff}},
		"ledger": "Nothing is posted. The change can be undone while nobody has changed the contact since"})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Changes the %s of %s", strings.Join(names, ", "), plan.before.LegalName), Changes: changes,
		Base: map[string]int64{contactKey(plan.before.ID): int64(plan.before.Version)}}, nil
}

// contactUndo is what the undo needs: the contact, the version the execution left it at and the values it replaced.
type contactUndo struct {
	Contact string        `json:"contact"`
	Version int           `json:"version"`
	Before  contactBefore `json:"before"`
	Fields  []string      `json:"fields"`
}

type contactBefore struct {
	BillingEmail     string     `json:"billing_email,omitempty"`
	Phone            string     `json:"phone,omitempty"`
	Address          string     `json:"address,omitempty"`
	PaymentTermsDays *int       `json:"payment_terms_days,omitempty"`
	DefaultAccountID *uuid.UUID `json:"default_account_id,omitempty"`
	DefaultGSTCode   string     `json:"default_gst_code,omitempty"`
}

// Execute implements Command: only while the contact is at the version the dry run read (AI-P5).
func (UpdateContact) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	p, err := contactUpdateIn(payload)
	if err != nil {
		return Execution{}, err
	}
	plan, err := p.plan(ctx, u)
	if err != nil {
		return Execution{}, err
	}
	want, ok := base[contactKey(plan.before.ID)]
	if !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", contactKey(plan.before.ID))
	}
	if int64(plan.before.Version) != want {
		return Execution{}, ErrStale
	}
	after, err := u.Contacts().UpdateAIFields(ctx, plan.before.ID, plan.before.Version, plan.fields)
	if err != nil {
		return Execution{}, err
	}
	b := plan.before
	undo, err := json.Marshal(contactUndo{Contact: after.ID.String(), Version: after.Version, Fields: fieldsSet(plan.fields), Before: contactBefore{
		BillingEmail: b.BillingEmail, Phone: b.Phone, Address: b.Address, PaymentTermsDays: b.PaymentTermsDays, DefaultAccountID: b.DefaultAccountID, DefaultGSTCode: b.DefaultGSTCode}})
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{contactKey(after.ID)}, Entries: []string{}, Undo: undo}, nil
}

func fieldsSet(f storage.ContactAIFields) []string {
	var out []string
	for name, set := range map[string]bool{"billing_email": f.SetBillingEmail, "phone": f.SetPhone, "address": f.SetAddress, "payment_terms_days": f.SetPaymentTerms,
		"default_category": f.SetDefaultCategory, "default_gst_code": f.SetDefaultGSTCode} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Undo implements Undoer: the replaced values come back, as the person who undoes it, through the narrow query, while the contact is at the version
// the execution left it (ErrStale when anyone changed it since).
func (UpdateContact) Undo(ctx context.Context, u storage.UnitOfWork, done Execution, as Actor) (Execution, error) {
	var st contactUndo
	if err := json.Unmarshal(done.Undo, &st); err != nil || st.Contact == "" {
		return Execution{}, refusal("the execution kept nothing to undo it with", "Correct the contact by hand")
	}
	id, err := uuid.Parse(st.Contact)
	if err != nil {
		return Execution{}, err
	}
	cur, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return Execution{}, err
	}
	if cur.Version != st.Version {
		return Execution{}, ErrStale
	}
	var f storage.ContactAIFields
	for _, name := range st.Fields {
		switch name {
		case "billing_email":
			f.SetBillingEmail, f.BillingEmail = true, st.Before.BillingEmail
		case "phone":
			f.SetPhone, f.Phone = true, st.Before.Phone
		case "address":
			f.SetAddress, f.Address = true, st.Before.Address
		case "payment_terms_days":
			f.SetPaymentTerms, f.PaymentTermsDays = true, st.Before.PaymentTermsDays
		case "default_category":
			f.SetDefaultCategory, f.DefaultAccountID = true, st.Before.DefaultAccountID
		case "default_gst_code":
			f.SetDefaultGSTCode, f.DefaultGSTCode = true, st.Before.DefaultGSTCode
		default:
			return Execution{}, refusal("the execution's record names a detail "+name+" the undo does not restore", "Correct the contact by hand")
		}
	}
	after, err := u.Contacts().UpdateAIFields(ctx, id, cur.Version, f)
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{contactKey(after.ID)}, Entries: []string{}}, nil
}

var _ Undoer = UpdateContact{}
