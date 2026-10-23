package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/contact"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ contacts (FR-CT-01, FR-CT-04)

// PossibleDuplicate is another contact this one may duplicate (FR-CT-04).
type PossibleDuplicate struct {
	ID        string `json:"id" format:"uuid" doc:"The other contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db1"`
	LegalName string `json:"legal_name" doc:"Its legal name." example:"Northwind Analytics Pte Ltd"`
	SameName  bool   `json:"same_name" doc:"The same name, case and spacing aside." example:"true"`
	SameUEN   bool   `json:"same_uen" doc:"The same UEN or tax ID." example:"false"`
	SameEmail bool   `json:"same_email" doc:"A billing or remittance email in common." example:"false"`
}

// Contact is a customer, a vendor or both.
type Contact struct {
	ID                 string              `json:"id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	LegalName          string              `json:"legal_name" doc:"Its legal name." example:"Northwind Analytics Pte. Ltd."`
	Customer           bool                `json:"customer" doc:"A customer: invoices go to it." example:"true"`
	Vendor             bool                `json:"vendor" doc:"A vendor: bills come from it." example:"false"`
	UENOrTaxID         string              `json:"uen_or_tax_id,omitempty" doc:"Its UEN, or a foreign contact's tax ID." example:"202312345K"`
	PeppolID           string              `json:"peppol_id,omitempty" doc:"Its Peppol participant ID for InvoiceNow." example:"0195:SGUEN202312345K"`
	BillingEmail       string              `json:"billing_email,omitempty" doc:"Where invoices go." example:"accounts@northwind.example"`
	RemittanceEmail    string              `json:"remittance_email,omitempty" doc:"Where remittance advice goes." example:"payments@northwind.example"`
	Phone              string              `json:"phone,omitempty" doc:"Its phone number." example:"+65 6123 4567"`
	Address            string              `json:"address,omitempty" doc:"Its address." example:"1 Raffles Place, #20-01, Singapore 048616"`
	Country            string              `json:"country,omitempty" doc:"Its country, ISO 3166 two-letter code." example:"SG"`
	TaxResidence       string              `json:"tax_residence,omitempty" doc:"Where it is resident for tax, ISO 3166 two-letter code, when that is not its country (FR-AP-19). A vendor resident outside Singapore (this, else its country) has its bills record the nature of the payment, and the tax agent decides whether tax is withheld." example:"US"`
	Currency           string              `json:"currency,omitempty" doc:"The currency it trades in." example:"SGD"`
	PaymentTermsDays   *int                `json:"payment_terms_days,omitempty" doc:"Days until its documents fall due." example:"30"`
	DefaultAccountID   string              `json:"default_account_id,omitempty" format:"uuid" doc:"The category its lines start with." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	DefaultGSTCode     string              `json:"default_gst_code,omitempty" doc:"The GST code its lines start with." example:"SR"`
	Active             bool                `json:"active" doc:"An inactive contact is kept for its history and offered for nothing new." example:"true"`
	Version            int                 `json:"version" doc:"The contact's version; it also makes the ETag." example:"1"`
	PossibleDuplicates []PossibleDuplicate `json:"possible_duplicates" doc:"Other contacts with the same name, UEN or tax ID, or email (FR-CT-04): review them, and merge or tell them apart."`
}

// ContactDraft is a new contact.
type ContactDraft struct {
	LegalName        string `json:"legal_name" maxLength:"200" doc:"Its legal name." example:"Northwind Analytics Pte. Ltd."`
	Customer         bool   `json:"customer,omitempty" doc:"A customer." example:"true"`
	Vendor           bool   `json:"vendor,omitempty" doc:"A vendor; at least one of customer and vendor." example:"false"`
	UENOrTaxID       string `json:"uen_or_tax_id,omitempty" maxLength:"30" doc:"Its UEN or tax ID." example:"202312345K"`
	PeppolID         string `json:"peppol_id,omitempty" maxLength:"70" doc:"Its Peppol participant ID." example:"0195:SGUEN202312345K"`
	BillingEmail     string `json:"billing_email,omitempty" maxLength:"254" doc:"Where invoices go." example:"accounts@northwind.example"`
	RemittanceEmail  string `json:"remittance_email,omitempty" maxLength:"254" doc:"Where remittance advice goes." example:"payments@northwind.example"`
	Phone            string `json:"phone,omitempty" maxLength:"40" doc:"Its phone number." example:"+65 6123 4567"`
	Address          string `json:"address,omitempty" maxLength:"500" doc:"Its address." example:"1 Raffles Place, #20-01, Singapore 048616"`
	Country          string `json:"country,omitempty" doc:"ISO 3166 two-letter code." example:"SG"`
	TaxResidence     string `json:"tax_residence,omitempty" doc:"Where it is resident for tax, ISO 3166 two-letter code, when that is not its country (FR-AP-19)." example:"US"`
	Currency         string `json:"currency,omitempty" doc:"ISO 4217 code." example:"SGD"`
	PaymentTermsDays *int   `json:"payment_terms_days,omitempty" doc:"0 to 365 days." example:"30"`
	DefaultAccountID string `json:"default_account_id,omitempty" doc:"The default category: an active account that takes postings." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	DefaultGSTCode   string `json:"default_gst_code,omitempty" doc:"The default GST code." example:"SR"`
}

// ContactChanges change a contact: a field left out keeps its value, and "" clears a text.
type ContactChanges struct {
	LegalName        *string `json:"legal_name,omitempty" maxLength:"200" doc:"Its legal name." example:"Northwind Analytics Pte. Ltd."`
	Customer         *bool   `json:"customer,omitempty" doc:"A customer." example:"true"`
	Vendor           *bool   `json:"vendor,omitempty" doc:"A vendor." example:"true"`
	UENOrTaxID       *string `json:"uen_or_tax_id,omitempty" maxLength:"30" doc:"Its UEN or tax ID." example:"202312345K"`
	PeppolID         *string `json:"peppol_id,omitempty" maxLength:"70" doc:"Its Peppol participant ID." example:"0195:SGUEN202312345K"`
	BillingEmail     *string `json:"billing_email,omitempty" maxLength:"254" doc:"Where invoices go." example:"accounts@northwind.example"`
	RemittanceEmail  *string `json:"remittance_email,omitempty" maxLength:"254" doc:"Where remittance advice goes." example:"payments@northwind.example"`
	Phone            *string `json:"phone,omitempty" maxLength:"40" doc:"Its phone number." example:"+65 6123 4567"`
	Address          *string `json:"address,omitempty" maxLength:"500" doc:"Its address." example:"1 Raffles Place, #20-01, Singapore 048616"`
	Country          *string `json:"country,omitempty" doc:"ISO 3166 two-letter code." example:"SG"`
	TaxResidence     *string `json:"tax_residence,omitempty" doc:"Where it is resident for tax, when that is not its country (FR-AP-19); \"\" clears it." example:"US"`
	Currency         *string `json:"currency,omitempty" doc:"ISO 4217 code." example:"USD"`
	PaymentTermsDays *int    `json:"payment_terms_days,omitempty" doc:"0 to 365 days; -1 clears them." example:"45"`
	DefaultAccountID *string `json:"default_account_id,omitempty" doc:"The default category; \"\" clears it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	DefaultGSTCode   *string `json:"default_gst_code,omitempty" doc:"The default GST code." example:"SR"`
	Active           *bool   `json:"active,omitempty" doc:"false keeps it for its history and offers it for nothing new." example:"false"`
}

// ContactPage is a page of contacts, in the order they were added.
type ContactPage struct {
	Items      []Contact `json:"items" doc:"The contacts on this page."`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// ContactPath names a contact.
type ContactPath struct {
	ContactID string `path:"contact_id" format:"uuid" doc:"The contact's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
}

type contactListInput struct {
	PageQuery
	Role string `query:"role" enum:"customer,vendor" doc:"Only customers, or only vendors." example:"customer"`
}

type contactCreateInput struct {
	KeyHeader
	PreferHeader
	Body ContactDraft
}

type contactUpdateInput struct {
	ContactPath
	IfMatchHeader
	PreferHeader
	Body ContactChanges
}

type contactListOutput struct{ Body ContactPage }

type contactReadOutput struct {
	ETag string `header:"ETag" doc:"The contact's version, for If-Match."`
	Body Contact
}

type contactWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The contact's version, for If-Match."`
	Location string `header:"Location" doc:"The contact's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Contact
}

// ContactCodes are a contact's rules (internal/domain/contact).
var ContactCodes = []string{contact.CodeName, contact.CodeRole, contact.CodeEmail, contact.CodeCountry, contact.CodeCurrency,
	contact.CodeTerms, contact.CodeGSTCode, contact.CodePeppol, contact.CodeText, contact.CodeCategory}

func contactOf(c storage.Contact, dups []storage.ContactDuplicate) Contact {
	out := Contact{ID: c.ID.String(), LegalName: c.LegalName, Customer: c.Customer, Vendor: c.Vendor, UENOrTaxID: c.UENOrTaxID,
		PeppolID: c.PeppolID, BillingEmail: c.BillingEmail, RemittanceEmail: c.RemittanceEmail, Phone: c.Phone, Address: c.Address,
		Country: c.Country, TaxResidence: c.TaxResidence, Currency: c.Currency, PaymentTermsDays: c.PaymentTermsDays, DefaultGSTCode: c.DefaultGSTCode,
		Active: c.Active, Version: c.Version, PossibleDuplicates: []PossibleDuplicate{}}
	if c.DefaultAccountID != nil {
		out.DefaultAccountID = c.DefaultAccountID.String()
	}
	for _, d := range dups {
		out.PossibleDuplicates = append(out.PossibleDuplicates, PossibleDuplicate{ID: d.ID.String(), LegalName: d.LegalName,
			SameName: d.SameName, SameUEN: d.SameUEN, SameEmail: d.SameEmail})
	}
	return out
}

// checkContact runs a contact's rules, and checks its default category is an account that takes postings.
func checkContact(ctx context.Context, u storage.UnitOfWork, d storage.ContactDetails, bad []FieldProblem) error {
	problems := contact.Check(d)
	if d.DefaultAccountID != nil {
		acc, err := u.Accounts().Get(ctx, *d.DefaultAccountID)
		if err != nil {
			bad = append(bad, FieldProblem{Code: contact.CodeCategory, Field: "body.default_account_id", Message: "is not an account of this organisation",
				Fix: "Choose an active account that takes postings"})
		} else {
			problems = append(problems, contact.CheckCategory(acc)...)
		}
	}
	for _, x := range problems {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

func parseAccountRef(field, s string, bad *[]FieldProblem) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		*bad = append(*bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not an account ID", Fix: "Send the account's ID"})
		return nil
	}
	return &id
}

func registerContacts(a *API) {
	codes := append([]string{storage.CodeRetry, storage.CodeLockTimeout}, ContactCodes...)

	register(a, authz.ListContacts, huma.Operation{
		OperationID: "listContacts", Method: http.MethodGet, Path: "/v1/contacts", Tags: []string{"Contacts"},
		Summary:     "List the contacts",
		Description: "The organisation's customers and vendors, in the order they were added, a page at a time; role narrows the list to customers or vendors (FR-CT-01). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *contactListInput) (*contactListOutput, error) {
		after := uuid.Nil
		if in.Cursor != "" {
			s, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if after, err = uuid.Parse(s); err != nil {
				return nil, badCursor()
			}
		}
		p, _ := authn.From(ctx)
		out := contactListOutput{Body: ContactPage{Items: []Contact{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.Contacts().Page(ctx, after, in.Role, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, c := range page {
				out.Body.Items = append(out.Body.Items, contactOf(c, nil))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateContact, huma.Operation{
		OperationID: "createContact", Method: http.MethodPost, Path: "/v1/contacts", Tags: []string{"Contacts"},
		DefaultStatus: http.StatusCreated, Summary: "Add a contact",
		Description: "Adds a customer, a vendor or both (FR-CT-01), and lists the existing contacts it may duplicate on name, UEN or " +
			"tax ID, or email (FR-CT-04); a possible duplicate is flagged, not refused. Side effects: the contact and its audit event. " +
			"Vendor bank details have their own dual-controlled flow (FR-CT-03). Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it, lists the possible duplicates and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, codes...),
		func(ctx context.Context, in *contactCreateInput) (*contactWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Contact, error) {
				b := in.Body
				d := storage.ContactDetails{LegalName: strings.TrimSpace(b.LegalName), Customer: b.Customer, Vendor: b.Vendor,
					UENOrTaxID: strings.TrimSpace(b.UENOrTaxID), PeppolID: strings.TrimSpace(b.PeppolID), BillingEmail: strings.TrimSpace(b.BillingEmail),
					RemittanceEmail: strings.TrimSpace(b.RemittanceEmail), Phone: strings.TrimSpace(b.Phone), Address: strings.TrimSpace(b.Address),
					Country: b.Country, TaxResidence: b.TaxResidence, Currency: b.Currency, PaymentTermsDays: b.PaymentTermsDays, DefaultGSTCode: b.DefaultGSTCode}
				var bad []FieldProblem
				if b.DefaultAccountID != "" {
					d.DefaultAccountID = parseAccountRef("body.default_account_id", b.DefaultAccountID, &bad)
				}
				if err := checkContact(ctx, u, d, bad); err != nil {
					return Contact{}, err
				}
				c, err := u.Contacts().Create(ctx, d)
				if err != nil {
					return Contact{}, err
				}
				dups, err := u.Contacts().Duplicates(ctx, c.ID, d)
				return contactOf(c, dups), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &contactWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &contactWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/contacts/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetContact, huma.Operation{
		OperationID: "getContact", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}", Tags: []string{"Contacts"},
		Summary:     "Get a contact",
		Description: "Returns a contact with its ETag and the contacts it may duplicate (FR-CT-01, FR-CT-04). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ContactPath) (*contactReadOutput, error) {
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out contactReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Contacts().Get(ctx, id)
			if err != nil {
				return err
			}
			dups, err := u.Contacts().Duplicates(ctx, c.ID, c.ContactDetails)
			out.Body, out.ETag = contactOf(c, dups), etag(c.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateContact, huma.Operation{
		OperationID: "updateContact", Method: http.MethodPatch, Path: "/v1/contacts/{contact_id}", Tags: []string{"Contacts"},
		Summary: "Change a contact",
		Description: "Changes the fields sent and keeps the rest; \"\" clears a text, -1 the payment terms. active false keeps the contact " +
			"for its history and offers it for nothing new; a contact is never deleted. Lists the contacts it may then duplicate (FR-CT-04). " +
			"Side effects: the next version and its audit event. Needs If-Match with the contact's ETag. Prefer: return=preview checks it " +
			"and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion}, codes...),
		func(ctx context.Context, in *contactUpdateInput) (*contactWriteOutput, error) {
			id, err := parseID("contact_id", in.ContactID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Contact, error) {
				cur, err := u.Contacts().Get(ctx, id)
				if err != nil {
					return Contact{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return Contact{}, err
				}
				b, d, active := in.Body, cur.ContactDetails, cur.Active
				text := func(dst *string, v *string) {
					if v != nil {
						*dst = strings.TrimSpace(*v)
					}
				}
				text(&d.LegalName, b.LegalName)
				text(&d.UENOrTaxID, b.UENOrTaxID)
				text(&d.PeppolID, b.PeppolID)
				text(&d.BillingEmail, b.BillingEmail)
				text(&d.RemittanceEmail, b.RemittanceEmail)
				text(&d.Phone, b.Phone)
				text(&d.Address, b.Address)
				text(&d.Country, b.Country)
				text(&d.TaxResidence, b.TaxResidence)
				text(&d.Currency, b.Currency)
				text(&d.DefaultGSTCode, b.DefaultGSTCode)
				if b.Customer != nil {
					d.Customer = *b.Customer
				}
				if b.Vendor != nil {
					d.Vendor = *b.Vendor
				}
				if b.Active != nil {
					active = *b.Active
				}
				if b.PaymentTermsDays != nil {
					d.PaymentTermsDays = b.PaymentTermsDays
					if *b.PaymentTermsDays == -1 {
						d.PaymentTermsDays = nil
					}
				}
				var bad []FieldProblem
				if b.DefaultAccountID != nil {
					d.DefaultAccountID = nil
					if strings.TrimSpace(*b.DefaultAccountID) != "" {
						d.DefaultAccountID = parseAccountRef("body.default_account_id", *b.DefaultAccountID, &bad)
					}
				}
				if err := checkContact(ctx, u, d, bad); err != nil {
					return Contact{}, err
				}
				c, err := u.Contacts().Update(ctx, id, cur.Version, d, active)
				if err != nil {
					return Contact{}, err
				}
				dups, err := u.Contacts().Duplicates(ctx, c.ID, d)
				return contactOf(c, dups), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &contactWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &contactWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
