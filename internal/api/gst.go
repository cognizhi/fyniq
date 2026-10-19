package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ GST registration (FR-ORG-03)

// GSTRegistration is one of the organisation's GST registrations, with its effective dates.
type GSTRegistration struct {
	ID                string    `json:"id" format:"uuid" doc:"The registration's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4da0"`
	RegisteredFrom    string    `json:"registered_from" format:"date" doc:"The first day the organisation is registered." example:"2026-04-01"`
	DeregisteredFrom  string    `json:"deregistered_from,omitempty" format:"date" doc:"The first day it is no longer registered; absent while the registration lasts." example:"2028-01-01"`
	GSTNumber         string    `json:"gst_number" doc:"The GST registration number on IRAS's notice of registration." example:"202312345K"`
	ReturnFrequency   string    `json:"return_frequency" enum:"quarterly,monthly" doc:"How often GST returns are filed." example:"quarterly"`
	QuarterStartMonth int       `json:"quarter_start_month" minimum:"1" maximum:"3" doc:"Quarters start in this month of the year and every third after it: 1 (January, April, July, October), 2 or 3." example:"1"`
	AccountingBasis   string    `json:"accounting_basis" doc:"invoice: cash basis under IRAS's Cash Accounting Scheme is offered once its rules are pinned (ENG-45)." example:"invoice"`
	Version           int       `json:"version" doc:"The registration's version; it also makes the ETag." example:"1"`
	RecordedAt        time.Time `json:"recorded_at" doc:"When this version was recorded." example:"2026-10-19T09:30:00Z"`
}

// GSTRegistrationDraft records a registration.
type GSTRegistrationDraft struct {
	RegisteredFrom    string `json:"registered_from" format:"date" doc:"The first day the organisation is registered." example:"2026-04-01"`
	DeregisteredFrom  string `json:"deregistered_from,omitempty" format:"date" doc:"The first day it is no longer registered, if known." example:"2028-01-01"`
	GSTNumber         string `json:"gst_number" maxLength:"20" doc:"The GST registration number on IRAS's notice of registration." example:"202312345K"`
	ReturnFrequency   string `json:"return_frequency,omitempty" doc:"quarterly (when left out) or monthly." example:"quarterly"`
	QuarterStartMonth int    `json:"quarter_start_month,omitempty" doc:"1 (when left out), 2 or 3: quarters start in this month and every third after it." example:"1"`
	AccountingBasis   string `json:"accounting_basis,omitempty" doc:"invoice (when left out): the only basis offered until the Cash Accounting Scheme's rules are pinned (ENG-45)." example:"invoice"`
}

// GSTRegistrationChanges change a registration: a field left out keeps its value, and "" clears the
// deregistration date.
type GSTRegistrationChanges struct {
	RegisteredFrom    *string `json:"registered_from,omitempty" doc:"The first day the organisation is registered, YYYY-MM-DD." example:"2026-04-01"`
	DeregisteredFrom  *string `json:"deregistered_from,omitempty" doc:"The first day it is no longer registered, YYYY-MM-DD; \"\" clears it." example:"2028-01-01"`
	GSTNumber         *string `json:"gst_number,omitempty" maxLength:"20" doc:"The GST registration number." example:"202312345K"`
	ReturnFrequency   *string `json:"return_frequency,omitempty" doc:"quarterly or monthly." example:"monthly"`
	QuarterStartMonth *int    `json:"quarter_start_month,omitempty" doc:"1, 2 or 3." example:"1"`
	AccountingBasis   *string `json:"accounting_basis,omitempty" doc:"invoice." example:"invoice"`
}

// GSTRegistrationPage is a page of registrations, by their start.
type GSTRegistrationPage struct {
	Items      []GSTRegistration `json:"items" doc:"The registrations on this page, by the date they start."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// GSTPeriod is a GST return period.
type GSTPeriod struct {
	StartsOn string `json:"starts_on" format:"date" doc:"Its first day." example:"2026-04-01"`
	EndsOn   string `json:"ends_on" format:"date" doc:"Its last day." example:"2026-06-30"`
}

// GSTStatus is the organisation's GST status on a date.
type GSTStatus struct {
	Date         string           `json:"date" format:"date" doc:"The date asked about." example:"2026-05-10"`
	Registered   bool             `json:"registered" doc:"Whether the organisation is GST-registered on that date." example:"true"`
	Registration *GSTRegistration `json:"registration,omitempty" doc:"The registration in force that day; absent when not registered."`
	ReturnPeriod *GSTPeriod       `json:"return_period,omitempty" doc:"The return period containing that day; absent when not registered."`
}

// GSTPath names a GST registration.
type GSTPath struct {
	RegistrationID string `path:"registration_id" format:"uuid" doc:"The registration's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4da0"`
}

type gstListInput struct {
	PageQuery
}

type gstCreateInput struct {
	KeyHeader
	PreferHeader
	Body GSTRegistrationDraft
}

type gstUpdateInput struct {
	GSTPath
	IfMatchHeader
	PreferHeader
	Body GSTRegistrationChanges
}

type gstStatusInput struct {
	Date string `query:"date" required:"true" format:"date" doc:"The date to ask about, YYYY-MM-DD." example:"2026-05-10"`
}

type gstListOutput struct{ Body GSTRegistrationPage }

type gstReadOutput struct {
	ETag string `header:"ETag" doc:"The registration's version, for If-Match."`
	Body GSTRegistration
}

type gstWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The registration's version, for If-Match."`
	Location string `header:"Location" doc:"The registration's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     GSTRegistration
}

type gstStatusOutput struct{ Body GSTStatus }

// GSTCodes are a registration's rules (internal/domain/gst).
var GSTCodes = []string{gst.CodeDates, gst.CodeNumber, gst.CodeFrequency, gst.CodeQuarter, gst.CodeBasis}

func gstOf(r storage.GSTRegistration) GSTRegistration {
	return GSTRegistration{ID: r.ID.String(), RegisteredFrom: r.RegisteredFrom.Format(time.DateOnly), DeregisteredFrom: dayOrEmpty(r.DeregisteredFrom),
		GSTNumber: r.GSTNumber, ReturnFrequency: r.ReturnFrequency, QuarterStartMonth: r.QuarterStartMonth, AccountingBasis: r.AccountingBasis,
		Version: r.Version, RecordedAt: r.RecordedAt.UTC()}
}

func gstProblems(p gst.Problems, bad []FieldProblem) error {
	for _, x := range p {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

func parseDay(field, s string, bad *[]FieldProblem) *time.Time {
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		*bad = append(*bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		return nil
	}
	return &d
}

func registerGST(a *API) {
	codes := append([]string{storage.CodeOverlap, storage.CodeOrganisation, storage.CodeRetry, storage.CodeLockTimeout}, GSTCodes...)

	register(a, authz.ListGSTRegistrations, huma.Operation{
		OperationID: "listGSTRegistrations", Method: http.MethodGet, Path: "/v1/organisation/gst-registrations", Tags: []string{"Organisation"},
		Summary:     "List the GST registrations",
		Description: "The organisation's GST registrations with their effective dates, by the date each starts, a page at a time (FR-ORG-03). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *gstListInput) (*gstListOutput, error) {
		p, _ := authn.From(ctx)
		var all []GSTRegistration
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			regs, err := u.GSTRegistrations().List(ctx)
			for _, r := range regs {
				all = append(all, gstOf(r))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(r GSTRegistration) string { return r.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []GSTRegistration{}
		}
		return &gstListOutput{Body: GSTRegistrationPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.CreateGSTRegistration, huma.Operation{
		OperationID: "createGSTRegistration", Method: http.MethodPost, Path: "/v1/organisation/gst-registrations", Tags: []string{"Organisation"},
		DefaultStatus: http.StatusCreated, Summary: "Record a GST registration",
		Description: "Records that the organisation is GST-registered from a date, with its registration number, return frequency and quarters, " +
			"and its accounting basis (FR-ORG-03). A registration may not overlap another (409). Side effects: the registration and its audit " +
			"event. Approval policy: the Owner approves once the approval engine arrives (P1). Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, codes...),
		func(ctx context.Context, in *gstCreateInput) (*gstWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (GSTRegistration, error) {
				var bad []FieldProblem
				n := storage.NewGSTRegistration{GSTNumber: strings.TrimSpace(in.Body.GSTNumber), ReturnFrequency: in.Body.ReturnFrequency,
					QuarterStartMonth: in.Body.QuarterStartMonth, AccountingBasis: in.Body.AccountingBasis, RecordedAt: a.deps.Clock.Now()}
				if d := parseDay("body.registered_from", in.Body.RegisteredFrom, &bad); d != nil {
					n.RegisteredFrom = *d
				}
				if in.Body.DeregisteredFrom != "" {
					n.DeregisteredFrom = parseDay("body.deregistered_from", in.Body.DeregisteredFrom, &bad)
				}
				if n.ReturnFrequency == "" {
					n.ReturnFrequency = storage.ReturnQuarterly
				}
				if n.QuarterStartMonth == 0 {
					n.QuarterStartMonth = 1
				}
				if n.AccountingBasis == "" {
					n.AccountingBasis = storage.BasisInvoice
				}
				if err := gstProblems(gst.Check(n), bad); err != nil {
					return GSTRegistration{}, err
				}
				r, err := u.GSTRegistrations().Create(ctx, n)
				return gstOf(r), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &gstWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &gstWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/organisation/gst-registrations/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetGSTRegistration, huma.Operation{
		OperationID: "getGSTRegistration", Method: http.MethodGet, Path: "/v1/organisation/gst-registrations/{registration_id}", Tags: []string{"Organisation"},
		Summary:     "Get a GST registration",
		Description: "Returns one GST registration with its ETag (FR-ORG-03). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *GSTPath) (*gstReadOutput, error) {
		id, err := parseID("registration_id", in.RegistrationID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out gstReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.GSTRegistrations().Get(ctx, id)
			out.Body, out.ETag = gstOf(r), etag(r.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateGSTRegistration, huma.Operation{
		OperationID: "updateGSTRegistration", Method: http.MethodPatch, Path: "/v1/organisation/gst-registrations/{registration_id}", Tags: []string{"Organisation"},
		Summary: "Change a GST registration",
		Description: "Changes the fields sent and keeps the rest; \"\" clears the deregistration date. Deregistering is recording " +
			"deregistered_from (FR-ORG-03). A change may not make it overlap another registration (409). Side effects: the next version " +
			"and its audit event. Needs If-Match with the registration's ETag. Approval policy: the Owner approves once the approval engine " +
			"arrives (P1). Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion}, codes...),
		func(ctx context.Context, in *gstUpdateInput) (*gstWriteOutput, error) {
			id, err := parseID("registration_id", in.RegistrationID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GSTRegistration, error) {
				cur, err := u.GSTRegistrations().Get(ctx, id)
				if err != nil {
					return GSTRegistration{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return GSTRegistration{}, err
				}
				c := in.Body
				n := storage.NewGSTRegistration{RegisteredFrom: cur.RegisteredFrom, DeregisteredFrom: cur.DeregisteredFrom, GSTNumber: cur.GSTNumber,
					ReturnFrequency: cur.ReturnFrequency, QuarterStartMonth: cur.QuarterStartMonth, AccountingBasis: cur.AccountingBasis,
					RecordedAt: a.deps.Clock.Now()}
				var bad []FieldProblem
				if c.RegisteredFrom != nil {
					if d := parseDay("body.registered_from", *c.RegisteredFrom, &bad); d != nil {
						n.RegisteredFrom = *d
					}
				}
				if c.DeregisteredFrom != nil {
					n.DeregisteredFrom = nil
					if strings.TrimSpace(*c.DeregisteredFrom) != "" {
						n.DeregisteredFrom = parseDay("body.deregistered_from", *c.DeregisteredFrom, &bad)
					}
				}
				if c.GSTNumber != nil {
					n.GSTNumber = strings.TrimSpace(*c.GSTNumber)
				}
				if c.ReturnFrequency != nil {
					n.ReturnFrequency = *c.ReturnFrequency
				}
				if c.QuarterStartMonth != nil {
					n.QuarterStartMonth = *c.QuarterStartMonth
				}
				if c.AccountingBasis != nil {
					n.AccountingBasis = *c.AccountingBasis
				}
				if err := gstProblems(gst.Check(n), bad); err != nil {
					return GSTRegistration{}, err
				}
				r, err := u.GSTRegistrations().Update(ctx, id, cur.Version, n)
				return gstOf(r), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &gstWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &gstWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})

	register(a, authz.GetGSTStatus, huma.Operation{
		OperationID: "getGSTStatus", Method: http.MethodGet, Path: "/v1/organisation/gst-status", Tags: []string{"Organisation"},
		Summary: "Get the GST status on a date",
		Description: "Whether the organisation is GST-registered on a date, under which registration, and the return period that contains the " +
			"date, from the registrations' effective dates, return frequency and quarters (FR-ORG-03). Read-only; no side effects.",
	}, nil, func(ctx context.Context, in *gstStatusInput) (*gstStatusOutput, error) {
		d, err := time.Parse(time.DateOnly, in.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		p, _ := authn.From(ctx)
		out := GSTStatus{Date: in.Date}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			regs, err := u.GSTRegistrations().List(ctx)
			if err != nil {
				return err
			}
			if r, ok := gst.StatusOn(regs, d); ok {
				reg := gstOf(r)
				out.Registered, out.Registration = true, &reg
				if start, end, ok := gst.ReturnPeriod(r, d); ok {
					out.ReturnPeriod = &GSTPeriod{StartsOn: start.Format(time.DateOnly), EndsOn: end.Format(time.DateOnly)}
				}
			}
			return nil
		})
		return &gstStatusOutput{Body: out}, err
	})
}
