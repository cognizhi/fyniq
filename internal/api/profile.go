package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the organisation's profile (FR-ORG-01, FR-ORG-02)

// OrganisationProfile is what the organisation's documents print about it, and how its financial years run.
type OrganisationProfile struct {
	LegalName         string `json:"legal_name,omitempty" doc:"The legal name, as registered with ACRA." example:"Northwind Analytics Pte. Ltd."`
	UEN               string `json:"uen,omitempty" doc:"The Unique Entity Number: 9 or 10 capital letters and digits." example:"202312345K"`
	RegisteredAddress string `json:"registered_address,omitempty" doc:"The registered office address." example:"1 Raffles Place, #20-01, Singapore 048616"`
	MailingAddress    string `json:"mailing_address,omitempty" doc:"Where post goes, when not to the registered address." example:"PO Box 1234, Singapore 912345"`
	Email             string `json:"email,omitempty" doc:"The address documents give for questions." example:"accounts@northwind.example"`
	Phone             string `json:"phone,omitempty" doc:"The telephone number documents give." example:"+65 6123 4567"`
	Website           string `json:"website,omitempty" doc:"The website documents give." example:"https://northwind.example"`
	BankName          string `json:"bank_name,omitempty" doc:"The bank customers pay into, as documents print it." example:"DBS Bank"`
	BankAccountName   string `json:"bank_account_name,omitempty" doc:"The account's name." example:"Northwind Analytics Pte. Ltd."`
	BankAccountNumber string `json:"bank_account_number,omitempty" doc:"The account's number, as the bank prints it. Masked in logs and the audit trail (SEC-15)." example:"012-345678-9"`
	PayNowUEN         string `json:"paynow_uen,omitempty" doc:"The UEN customers pay to by PayNow." example:"202312345K"`
	DocumentFooter    string `json:"document_footer,omitempty" doc:"The text at the foot of every document." example:"Thank you for your business."`
	FYStartMonth      int    `json:"fy_start_month" minimum:"1" maximum:"12" doc:"The month each financial year starts in, 1 (January) to 12." example:"4"`
	BooksStart        string `json:"books_start,omitempty" format:"date" doc:"The date the books start: the first financial year begins on it." example:"2026-03-15"`
	FirstYearEnd      string `json:"first_year_end,omitempty" format:"date" doc:"The last day of the first financial year: the day before a year starts, after the books start, so the first year may be shorter or longer than twelve months." example:"2027-03-31"`
	TimeZone          string `json:"time_zone" doc:"The IANA time zone the organisation's days are counted in." example:"Asia/Singapore"`
	Version           int    `json:"version" doc:"The organisation's version; it also makes the ETag." example:"3"`
}

// ProfileChanges change the profile: a field left out (or null) keeps its value, and "" clears it.
type ProfileChanges struct {
	LegalName         *string `json:"legal_name,omitempty" maxLength:"200" doc:"The legal name; \"\" clears it." example:"Northwind Analytics Pte. Ltd."`
	UEN               *string `json:"uen,omitempty" maxLength:"10" doc:"The UEN; \"\" clears it." example:"202312345K"`
	RegisteredAddress *string `json:"registered_address,omitempty" maxLength:"500" doc:"The registered office address; \"\" clears it." example:"1 Raffles Place, #20-01, Singapore 048616"`
	MailingAddress    *string `json:"mailing_address,omitempty" maxLength:"500" doc:"The mailing address; \"\" clears it." example:"PO Box 1234, Singapore 912345"`
	Email             *string `json:"email,omitempty" maxLength:"254" doc:"The address documents give; \"\" clears it." example:"accounts@northwind.example"`
	Phone             *string `json:"phone,omitempty" maxLength:"40" doc:"The telephone number; \"\" clears it." example:"+65 6123 4567"`
	Website           *string `json:"website,omitempty" maxLength:"200" doc:"The website; \"\" clears it." example:"https://northwind.example"`
	BankName          *string `json:"bank_name,omitempty" maxLength:"100" doc:"The bank's name; \"\" clears it." example:"DBS Bank"`
	BankAccountName   *string `json:"bank_account_name,omitempty" maxLength:"140" doc:"The account's name; \"\" clears it." example:"Northwind Analytics Pte. Ltd."`
	BankAccountNumber *string `json:"bank_account_number,omitempty" maxLength:"34" doc:"The account's number; \"\" clears it." example:"012-345678-9"`
	PayNowUEN         *string `json:"paynow_uen,omitempty" maxLength:"10" doc:"The PayNow UEN; \"\" clears it." example:"202312345K"`
	DocumentFooter    *string `json:"document_footer,omitempty" maxLength:"1000" doc:"The document footer; \"\" clears it." example:"Thank you for your business."`
	FYStartMonth      *int    `json:"fy_start_month,omitempty" doc:"The month each financial year starts in, 1 to 12." example:"4"`
	BooksStart        *string `json:"books_start,omitempty" doc:"The date the books start, YYYY-MM-DD; \"\" clears it." example:"2026-03-15"`
	FirstYearEnd      *string `json:"first_year_end,omitempty" doc:"The last day of the first financial year, YYYY-MM-DD; \"\" clears it." example:"2027-03-31"`
	TimeZone          *string `json:"time_zone,omitempty" maxLength:"64" doc:"The IANA time zone." example:"Asia/Singapore"`
}

type profileOutput struct {
	ETag    string `header:"ETag" doc:"The organisation's version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    OrganisationProfile
}

type profileReadOutput struct {
	ETag string `header:"ETag" doc:"The organisation's version, for If-Match."`
	Body OrganisationProfile
}

type updateProfileInput struct {
	IfMatchHeader
	PreferHeader
	Body ProfileChanges
}

// OrganisationCodes are the profile's rules (internal/domain/organisation).
var OrganisationCodes = []string{organisation.CodeText, organisation.CodeUEN, organisation.CodeEmail, organisation.CodeTimeZone,
	organisation.CodeYearStart, organisation.CodeFirstYear, organisation.CodeBank}

func profileOf(o storage.Organisation) OrganisationProfile {
	p := o.Profile
	return OrganisationProfile{LegalName: p.LegalName, UEN: p.UEN, RegisteredAddress: p.RegisteredAddress, MailingAddress: p.MailingAddress,
		Email: p.Email, Phone: p.Phone, Website: p.Website, BankName: p.BankName, BankAccountName: p.BankAccountName,
		BankAccountNumber: p.BankAccountNumber, PayNowUEN: p.PayNowUEN, DocumentFooter: p.DocumentFooter, FYStartMonth: p.FYStartMonth,
		BooksStart: dayOrEmpty(p.BooksStart), FirstYearEnd: dayOrEmpty(p.FirstYearEnd), TimeZone: p.TimeZone, Version: o.Version}
}

func dayOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

// applyChanges returns p with c applied, and a problem for every date that cannot be read.
func applyChanges(p storage.OrganisationProfile, c ProfileChanges) (storage.OrganisationProfile, []FieldProblem) {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&p.LegalName, c.LegalName)
	set(&p.UEN, c.UEN)
	set(&p.RegisteredAddress, c.RegisteredAddress)
	set(&p.MailingAddress, c.MailingAddress)
	set(&p.Email, c.Email)
	set(&p.Phone, c.Phone)
	set(&p.Website, c.Website)
	set(&p.BankName, c.BankName)
	set(&p.BankAccountName, c.BankAccountName)
	set(&p.BankAccountNumber, c.BankAccountNumber)
	set(&p.PayNowUEN, c.PayNowUEN)
	set(&p.DocumentFooter, c.DocumentFooter)
	set(&p.TimeZone, c.TimeZone)
	if c.FYStartMonth != nil {
		p.FYStartMonth = *c.FYStartMonth
	}
	var bad []FieldProblem
	date := func(field string, dst **time.Time, v *string) {
		switch {
		case v == nil:
		case strings.TrimSpace(*v) == "":
			*dst = nil
		default:
			d, err := time.Parse(time.DateOnly, strings.TrimSpace(*v))
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
				return
			}
			*dst = &d
		}
	}
	date("books_start", &p.BooksStart, c.BooksStart)
	date("first_year_end", &p.FirstYearEnd, c.FirstYearEnd)
	return p, bad
}

func registerProfile(a *API) {
	register(a, authz.GetOrganisationProfile, huma.Operation{
		OperationID: "getOrganisationProfile", Method: http.MethodGet, Path: "/v1/organisation/profile", Tags: []string{"Organisation"},
		Summary: "Get the organisation's profile",
		Description: "What the organisation's documents print about it (legal name, UEN, addresses, contact details, the bank and PayNow " +
			"details customers pay to, the footer) and how its financial years run (the start month, the books' start, the first " +
			"year's end) and its time zone (FR-ORG-01, FR-ORG-02), with the organisation's ETag. Read-only; no side effects.",
	}, nil, func(ctx context.Context, _ *struct{}) (*profileReadOutput, error) {
		p, _ := authn.From(ctx)
		var out profileReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			out.Body, out.ETag = profileOf(o), etag(o.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateOrganisationProfile, huma.Operation{
		OperationID: "updateOrganisationProfile", Method: http.MethodPatch, Path: "/v1/organisation/profile", Tags: []string{"Organisation"},
		Summary: "Change the organisation's profile",
		Description: "Changes the fields sent and keeps the rest: a field left out keeps its value, and \"\" clears it. Every rule " +
			"that fails is reported at once (UEN, email, time zone, a first financial year that ends after the books start, on the " +
			"day before a year starts). Side effects: the organisation's next version and an audit event with the changes, bank and " +
			"PayNow numbers masked. Needs If-Match with the organisation's ETag. Approval policy: the Owner approves profile changes " +
			"once the approval engine arrives (P1). Once a period exists, the books start, the start month and the first year end no longer " +
			"change (PERIOD-04). Prefer: return=preview checks the change and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion, storage.CodeOrganisation, storage.CodeRetry, period.CodeYearsOpen,
		storage.CodeLockTimeout}, OrganisationCodes...),
		func(ctx context.Context, in *updateProfileInput) (*profileOutput, error) {
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OrganisationProfile, error) {
				o, err := u.Organisation(ctx)
				if err != nil {
					return OrganisationProfile{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(o.Version)); err != nil {
					return OrganisationProfile{}, err
				}
				p, bad := applyChanges(o.Profile, in.Body)
				for _, x := range organisation.Check(p) {
					bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
				}
				if len(bad) > 0 {
					return OrganisationProfile{}, invalid(bad)
				}
				periods, err := u.Periods().List(ctx)
				if err != nil {
					return OrganisationProfile{}, err
				}
				if err := period.Fixed(o.Profile, p, len(periods)); err != nil {
					return OrganisationProfile{}, err
				}
				after, err := u.UpdateOrganisation(ctx, o.Version, p)
				return profileOf(after), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &profileOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &profileOutput{ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
