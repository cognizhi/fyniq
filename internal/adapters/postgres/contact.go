package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Contacts() storage.Contacts { return contacts{u} }

type contacts struct{ u *unitOfWork }

// contactRow is every contact query's row: they all select the same columns.
type contactRow = sqlcdb.GetContactRow

func toContact(r contactRow) storage.Contact {
	c := storage.Contact{ID: r.ID, Active: r.IsActive, Version: int(r.Version), ContactDetails: storage.ContactDetails{
		LegalName: r.LegalName, Customer: r.IsCustomer, Vendor: r.IsVendor, UENOrTaxID: str(r.UenOrTaxID), PeppolID: str(r.PeppolID),
		BillingEmail: str(r.BillingEmail), RemittanceEmail: str(r.RemittanceEmail), Phone: str(r.Phone), Address: str(r.Address),
		Country: str(r.Country), TaxResidence: str(r.TaxResidence), Currency: str(r.Currency), DefaultAccountID: r.DefaultAccountID,
		DefaultGSTCode: str(r.DefaultGstCode)}}
	if r.PaymentTermsDays != nil {
		d := int(*r.PaymentTermsDays)
		c.PaymentTermsDays = &d
	}
	c.DiscountPercent = r.DiscountPercent
	if r.DiscountDays != nil {
		d := int(*r.DiscountDays)
		c.DiscountDays = &d
	}
	return c
}

// terms is the payment terms as the column holds them, refused outside 0 to 365 days.
func terms(d *int) (*int16, error) {
	if d == nil {
		return nil, nil
	}
	if *d < 0 || *d > 365 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("payment terms of %d days", *d), Hint: "Use 0 to 365 days"}
	}
	v := int16(*d) // 0 to 365, checked above
	return &v, nil
}

// contactChanges are the fields that differ, by column name; a zero contact gives every field.
func contactChanges(a, b storage.Contact) map[string][2]any {
	out := map[string][2]any{}
	diff := func(k string, x, y any) {
		if fmt.Sprint(x) != fmt.Sprint(y) {
			out[k] = [2]any{x, y}
		}
	}
	ptr := func(p any) any {
		switch v := p.(type) {
		case *int:
			if v == nil {
				return nil
			}
			return *v
		case *uuid.UUID:
			if v == nil {
				return nil
			}
			return v.String()
		}
		return p
	}
	diff("legal_name", nilIfEmpty(a.LegalName), nilIfEmpty(b.LegalName))
	diff("is_customer", a.Customer, b.Customer)
	diff("is_vendor", a.Vendor, b.Vendor)
	diff("uen_or_tax_id", nilIfEmpty(a.UENOrTaxID), nilIfEmpty(b.UENOrTaxID))
	diff("peppol_id", nilIfEmpty(a.PeppolID), nilIfEmpty(b.PeppolID))
	diff("billing_email", nilIfEmpty(a.BillingEmail), nilIfEmpty(b.BillingEmail))
	diff("remittance_email", nilIfEmpty(a.RemittanceEmail), nilIfEmpty(b.RemittanceEmail))
	diff("phone", nilIfEmpty(a.Phone), nilIfEmpty(b.Phone))
	diff("address", nilIfEmpty(a.Address), nilIfEmpty(b.Address))
	diff("country", nilIfEmpty(a.Country), nilIfEmpty(b.Country))
	diff("tax_residence", nilIfEmpty(a.TaxResidence), nilIfEmpty(b.TaxResidence))
	diff("currency", nilIfEmpty(a.Currency), nilIfEmpty(b.Currency))
	diff("payment_terms_days", ptr(a.PaymentTermsDays), ptr(b.PaymentTermsDays))
	diff("discount_percent", nilIfEmpty(a.DiscountPercent), nilIfEmpty(b.DiscountPercent))
	diff("discount_days", ptr(a.DiscountDays), ptr(b.DiscountDays))
	diff("default_account_id", ptr(a.DefaultAccountID), ptr(b.DefaultAccountID))
	diff("default_gst_code", nilIfEmpty(a.DefaultGSTCode), nilIfEmpty(b.DefaultGSTCode))
	diff("is_active", a.Active, b.Active)
	return out
}

func (r contacts) Create(ctx context.Context, d storage.ContactDetails) (storage.Contact, error) {
	if err := r.u.check(); err != nil {
		return storage.Contact{}, err
	}
	t, err := terms(d.PaymentTermsDays)
	if err != nil {
		return storage.Contact{}, err
	}
	dd, err := terms(d.DiscountDays)
	if err != nil {
		return storage.Contact{}, err
	}
	row, err := r.u.q.InsertContact(ctx, sqlcdb.InsertContactParams{LegalName: d.LegalName, IsCustomer: d.Customer, IsVendor: d.Vendor,
		UenOrTaxID: opt(d.UENOrTaxID), PeppolID: opt(d.PeppolID), BillingEmail: opt(d.BillingEmail), RemittanceEmail: opt(d.RemittanceEmail),
		Phone: opt(d.Phone), Address: opt(d.Address), Country: opt(d.Country), TaxResidence: opt(d.TaxResidence), Currency: opt(d.Currency), PaymentTermsDays: t,
		DiscountPercent: opt(d.DiscountPercent), DiscountDays: dd,
		DefaultAccountID: d.DefaultAccountID, DefaultGstCode: opt(d.DefaultGSTCode)})
	if err != nil {
		return storage.Contact{}, mapError(err)
	}
	c := toContact(contactRow(row))
	if err := r.u.event(ctx, "contact.created", "contact", c.ID, &c.Version, contactChanges(storage.Contact{}, c)); err != nil {
		return storage.Contact{}, err
	}
	return c, nil
}

func (r contacts) Get(ctx context.Context, id uuid.UUID) (storage.Contact, error) {
	if err := r.u.check(); err != nil {
		return storage.Contact{}, err
	}
	row, err := r.u.q.GetContact(ctx, id)
	if err != nil {
		return storage.Contact{}, mapError(err)
	}
	return toContact(row), nil
}

func (r contacts) Update(ctx context.Context, id uuid.UUID, version int, d storage.ContactDetails, active bool) (storage.Contact, error) {
	if err := r.u.check(); err != nil {
		return storage.Contact{}, err
	}
	t, err := terms(d.PaymentTermsDays)
	if err != nil {
		return storage.Contact{}, err
	}
	dd, err := terms(d.DiscountDays)
	if err != nil {
		return storage.Contact{}, err
	}
	cur, err := r.u.q.LockContact(ctx, id)
	if err != nil {
		return storage.Contact{}, mapError(err)
	}
	before := toContact(contactRow(cur))
	if before.Version != version {
		return storage.Contact{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the contact changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	row, err := r.u.q.UpdateContact(ctx, sqlcdb.UpdateContactParams{ID: id, LegalName: d.LegalName, IsCustomer: d.Customer, IsVendor: d.Vendor,
		UenOrTaxID: opt(d.UENOrTaxID), PeppolID: opt(d.PeppolID), BillingEmail: opt(d.BillingEmail), RemittanceEmail: opt(d.RemittanceEmail),
		Phone: opt(d.Phone), Address: opt(d.Address), Country: opt(d.Country), TaxResidence: opt(d.TaxResidence), Currency: opt(d.Currency), PaymentTermsDays: t,
		DiscountPercent: opt(d.DiscountPercent), DiscountDays: dd,
		DefaultAccountID: d.DefaultAccountID, DefaultGstCode: opt(d.DefaultGSTCode), IsActive: active})
	if err != nil {
		return storage.Contact{}, mapError(err)
	}
	after := toContact(contactRow(row))
	changes := contactChanges(before, after)
	changes["version"] = [2]any{before.Version, after.Version}
	if err := r.u.event(ctx, "contact.updated", "contact", after.ID, &after.Version, changes); err != nil {
		return storage.Contact{}, err
	}
	return after, nil
}

func (r contacts) Page(ctx context.Context, after uuid.UUID, role string, limit int) ([]storage.Contact, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d contacts", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.PageContacts(ctx, sqlcdb.PageContactsParams{After: after, Role: opt(role), PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Contact, 0, len(rows))
	for _, row := range rows {
		out = append(out, toContact(contactRow(row)))
	}
	return out, nil
}

func (r contacts) Duplicates(ctx context.Context, self uuid.UUID, d storage.ContactDetails) ([]storage.ContactDuplicate, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ContactDuplicates(ctx, sqlcdb.ContactDuplicatesParams{ID: self, LegalName: d.LegalName, UenOrTaxID: opt(d.UENOrTaxID),
		BillingEmail: opt(d.BillingEmail), RemittanceEmail: opt(d.RemittanceEmail)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ContactDuplicate, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.ContactDuplicate{ID: row.ID, LegalName: row.LegalName, SameName: row.SameName, SameUEN: row.SameUen,
			SameEmail: row.SameEmail})
	}
	return out, nil
}
