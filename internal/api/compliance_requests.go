package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PDRequestView is an access or correction request (CMP-PD-04, rev 0.244) with its steps.
type PDRequestView struct {
	ID         string              `json:"id" format:"uuid" doc:"The request." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Kind       string              `json:"kind" enum:"access,correction" doc:"access: to see their personal data; correction: to correct it." example:"access"`
	Subject    string              `json:"subject" doc:"Whose data: member:<user id>, contact:<contact id>, or person for someone else." example:"member:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	Requester  string              `json:"requester,omitempty" doc:"Who asked and how to reach them, when not a member asking for themselves." example:"Ada Tan, ada@example.com"`
	Details    string              `json:"details" doc:"What they asked for." example:"A copy of the personal data you hold about me."`
	ReceivedOn string              `json:"received_on" format:"date" doc:"When it was received." example:"2026-11-09"`
	DueOn      string              `json:"due_on" format:"date" doc:"When it is to be answered: 30 days after receipt, or the day the written extension names." example:"2026-12-09"`
	Status     string              `json:"status" enum:"received,acknowledged,extended,answered,closed" doc:"Its latest step." example:"acknowledged"`
	Overdue    bool                `json:"overdue" doc:"true when it is not answered and its day has passed." example:"false"`
	Steps      []PDRequestStepView `json:"steps" nullable:"false" doc:"Its steps, oldest first."`
}

// PDRequestStepView is a request's step.
type PDRequestStepView struct {
	Step     string `json:"step" enum:"acknowledged,extended,answered,closed" doc:"The step." example:"answered"`
	At       string `json:"at" format:"date-time" doc:"When." example:"2026-11-20T02:00:00Z"`
	By       string `json:"by" doc:"Who, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Note     string `json:"note,omitempty" doc:"What was said or done." example:"Sent the report by email."`
	NewDueOn string `json:"new_due_on,omitempty" format:"date" doc:"An extension's new day, told to the requester in writing." example:"2027-01-08"`
	Outcome  string `json:"outcome,omitempty" enum:"provided,corrected,partly,refused" doc:"An answer's outcome." example:"provided"`
}

// PDRequestInput records a request from someone other than a member asking for themselves.
type PDRequestInput struct {
	Kind       string `json:"kind" enum:"access,correction" doc:"access or correction." example:"access"`
	Subject    string `json:"subject" pattern:"^((member|contact):[0-9a-f-]{36}|person)$" doc:"Whose data: member:<user id>, contact:<contact id>, or person." example:"contact:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e12"`
	Requester  string `json:"requester" minLength:"1" maxLength:"500" doc:"Who asked and how to reach them." example:"Ada Tan, ada@example.com"`
	Details    string `json:"details" minLength:"1" maxLength:"4000" doc:"What they asked for." example:"A copy of the personal data you hold about me."`
	ReceivedOn string `json:"received_on,omitempty" format:"date" doc:"When it was received; left out, today. Never after today." example:"2026-11-09"`
}

// PDRequestStepInput adds a request's step.
type PDRequestStepInput struct {
	Step     string `json:"step" enum:"acknowledged,extended,answered,closed" doc:"The step." example:"answered"`
	Note     string `json:"note,omitempty" maxLength:"4000" doc:"What was said or done; an extension's written notice." example:"Sent the report by email."`
	NewDueOn string `json:"new_due_on,omitempty" format:"date" doc:"With extended: the day it will be answered, told to the requester in writing within the first 30 days." example:"2027-01-08"`
	Outcome  string `json:"outcome,omitempty" enum:"provided,corrected,partly,refused" doc:"With answered: the outcome." example:"provided"`
}

// MyRequestInput is a member's request about their own personal data.
type MyRequestInput struct {
	Kind    string `json:"kind" enum:"access,correction" doc:"access: to see the personal data the company holds about you; correction: to correct it." example:"access"`
	Details string `json:"details" minLength:"1" maxLength:"4000" doc:"What you ask for; for a correction, what is wrong and what it should say." example:"Please correct my name: it is Ada Tan, not Ada Tam."`
}

// PersonalDataReport is the personal data Fyniq holds about a request's subject, from the inventory's classes.
type PersonalDataReport struct {
	Subject string       `json:"subject" doc:"Whose data." example:"member:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	Items   []ReportItem `json:"items" nullable:"false" doc:"Each value held, with its class."`
	Note    string       `json:"note" doc:"What the report does not cover." example:"The audit trail's history and issued documents are kept as records; ask the Compliance officer for copies."`
}

// ReportItem is a value held about a person.
type ReportItem struct {
	Class string `json:"class" doc:"The inventory's class." example:"members"`
	Field string `json:"field" doc:"What it is." example:"Name"`
	Value string `json:"value" doc:"The value held." example:"Ada Tan"`
}

type (
	pdRequestsOutput struct {
		Body struct {
			Items []PDRequestView `json:"items" nullable:"false" doc:"The requests, oldest first."`
		}
	}
	// PDRequestPath names a request (exported: huma reads only an exported embedded struct).
	PDRequestPath struct {
		RequestID string `path:"request_id" format:"uuid" doc:"The request." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	pdRequestCreateInput struct {
		KeyHeader
		PreferHeader
		Body PDRequestInput
	}
	pdRequestStepInput struct {
		PDRequestPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body PDRequestStepInput
	}
	myRequestInput struct {
		KeyHeader
		PreferHeader
		Body MyRequestInput
	}
	pdRequestOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The request's steps, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PDRequestView
	}
	pdReportOutput struct{ Body PersonalDataReport }
)

func pdRequestETag(v PDRequestView) string { return etag("s" + strconv.Itoa(len(v.Steps))) }

func pdRequestView(r storage.PDRequest, today time.Time) PDRequestView {
	v := PDRequestView{ID: r.ID.String(), Kind: r.Kind, Subject: r.Subject, Requester: r.Requester, Details: r.Details,
		ReceivedOn: r.ReceivedOn.Format(time.DateOnly), Status: "received", Steps: []PDRequestStepView{}}
	answered := false
	for _, s := range r.Steps {
		sv := PDRequestStepView{Step: s.Step, At: s.At.UTC().Format(time.RFC3339), By: s.By, Note: s.Note, Outcome: s.Outcome}
		if s.NewDueOn != nil {
			sv.NewDueOn = s.NewDueOn.Format(time.DateOnly)
		}
		v.Steps, v.Status = append(v.Steps, sv), s.Step
		answered = answered || s.Step == "answered"
	}
	due := controls.RequestDue(r)
	v.DueOn, v.Overdue = due.Format(time.DateOnly), !answered && today.After(due)
	return v
}

func (a *API) onePDRequest(ctx context.Context, u storage.UnitOfWork, id string) (storage.PDRequest, error) {
	all, err := u.Compliance().PDRequests(ctx)
	if err != nil {
		return storage.PDRequest{}, err
	}
	i := slices.IndexFunc(all, func(r storage.PDRequest) bool { return r.ID.String() == id })
	if i < 0 {
		return storage.PDRequest{}, problem(CodeNotFound, "There is no request "+id+".", "Use an ID from listPersonalDataRequests.")
	}
	return all[i], nil
}

// personalDataReport reads what Fyniq holds about a member or a contact.
func (a *API) personalDataReport(ctx context.Context, u storage.UnitOfWork, subject string) (PersonalDataReport, error) {
	out := PersonalDataReport{Subject: subject, Items: []ReportItem{},
		Note: "The audit trail's history and issued documents are kept as records under the law's retention; ask the Compliance officer for copies."}
	kind, id, _ := strings.Cut(subject, ":")
	add := func(class, field, value string) {
		if strings.TrimSpace(value) != "" {
			out.Items = append(out.Items, ReportItem{Class: class, Field: field, Value: value})
		}
	}
	switch kind {
	case "member":
		ms, err := u.Members().List(ctx, a.deps.Clock.Now())
		if err != nil {
			return out, err
		}
		for _, m := range ms {
			if m.UserID.String() != id {
				continue
			}
			add("members", "Name", m.DisplayName)
			add("members", "Joined", m.Since.UTC().Format(time.DateOnly))
			var roles []string
			for _, r := range m.Roles {
				roles = append(roles, r.Role)
			}
			add("members", "Roles", strings.Join(roles, ", "))
		}
	case "contact":
		cid, err := uuid.Parse(id)
		if err != nil {
			return out, nil
		}
		c, err := u.Contacts().Get(ctx, cid)
		if err != nil {
			return out, err
		}
		add("contacts", "Name", c.LegalName)
		add("contacts", "Billing email", c.BillingEmail)
		add("contacts", "Remittance email", c.RemittanceEmail)
		add("contacts", "Phone", c.Phone)
		add("contacts", "Address", c.Address)
	}
	return out, nil
}

func registerPDRequests(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodePDRequest}, locks...)
	answer := func(w written[PDRequestView]) *pdRequestOutput {
		if w.Previewed {
			return &pdRequestOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &pdRequestOutput{Status: w.Status, ETag: pdRequestETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}
	// received records a request and tells whoever keeps the PDPA registers.
	received := func(ctx context.Context, u storage.UnitOfWork, r storage.PDRequest, today time.Time) (PDRequestView, error) {
		saved, err := u.Compliance().RecordPDRequest(ctx, r)
		if err != nil {
			return PDRequestView{}, err
		}
		p, _ := authn.From(ctx)
		if a.deps.Outbox != nil {
			id := saved.ID
			if _, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, notify.Event{Type: notify.PDRequestType, RecordType: "pd_request", RecordID: &id,
				Title: fmt.Sprintf("A personal-data %s request was received; it is due by %s", saved.Kind, saved.DueOn.Format("2 Jan 2006")),
				Link:  "/compliance/data-protection", Audience: notify.Audience{Capability: string(authz.DataProtection), Need: "C"}, At: a.deps.Clock.Now()}); err != nil {
				return PDRequestView{}, err
			}
		}
		return pdRequestView(saved, today), nil
	}

	register(a, authz.ListPDRequests, exempt(huma.Operation{
		OperationID: "listPersonalDataRequests", Method: http.MethodGet, Path: "/v1/compliance/personal-data-requests", Tags: []string{"Compliance"},
		Summary: "List the access and correction requests",
		Description: "Every access and correction request (CMP-PD-04): whose data, who asked, what for, when it was received and is due, " +
			"whether it is overdue, and its steps. Read-only; no side effects.",
	}, "cursor", "A company receives few requests, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*pdRequestsOutput, error) {
			p, _ := authn.From(ctx)
			out := &pdRequestsOutput{}
			out.Body.Items = []PDRequestView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				all, err := u.Compliance().PDRequests(ctx)
				for _, r := range all {
					out.Body.Items = append(out.Body.Items, pdRequestView(r, today))
				}
				return err
			})
			return out, err
		})

	register(a, authz.CreatePDRequest, huma.Operation{
		OperationID: "createPersonalDataRequest", Method: http.MethodPost, Path: "/v1/compliance/personal-data-requests", Tags: []string{"Compliance"},
		Summary:     "Record an access or correction request",
		Description: "The Compliance officer records a request received from a contact or anyone else (CMP-PD-04), due 30 days after receipt. Side effects: the request, the audit event pd_request.received, a notice to those who keep the PDPA registers. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *pdRequestCreateInput) (*pdRequestOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PDRequestView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return PDRequestView{}, err
			}
			on := today
			if in.Body.ReceivedOn != "" {
				d, err := time.Parse(time.DateOnly, in.Body.ReceivedOn)
				if err != nil || d.After(today) {
					return PDRequestView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.received_on", Message: "is not a day up to today",
						Fix: "Send the day it was received, YYYY-MM-DD, today or earlier"}})
				}
				on = d
			}
			return received(ctx, u, storage.PDRequest{Kind: in.Body.Kind, Subject: in.Body.Subject, Requester: strings.TrimSpace(in.Body.Requester),
				Details: strings.TrimSpace(in.Body.Details), ReceivedOn: on, CreatedBy: uploader(ctx), CreatedAt: now}, today)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.StepPDRequest, huma.Operation{
		OperationID: "stepPersonalDataRequest", Method: http.MethodPost, Path: "/v1/compliance/personal-data-requests/{request_id}/steps", Tags: []string{"Compliance"},
		Summary:     "Add a request's next step",
		Description: "Records that a request was acknowledged, extended (with the day told to the requester in writing, within the first 30 days), answered (with its outcome) or closed after its answer (CMP-PD-04). Side effects: the step, the audit event pd_request.<step>. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *pdRequestStepInput) (*pdRequestOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PDRequestView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return PDRequestView{}, err
			}
			r, err := a.onePDRequest(ctx, u, in.RequestID)
			if err != nil {
				return PDRequestView{}, err
			}
			if err := checkIfMatch(in.IfMatch, pdRequestETag(pdRequestView(r, today))); err != nil {
				return PDRequestView{}, err
			}
			s := storage.PDRequestStep{Step: in.Body.Step, At: now, By: uploader(ctx), Note: strings.TrimSpace(in.Body.Note), Outcome: in.Body.Outcome}
			var bad []FieldProblem
			if in.Body.Step == "extended" {
				d, err := time.Parse(time.DateOnly, in.Body.NewDueOn)
				if err != nil {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.new_due_on", Message: "is not a day", Fix: "Send the day you told the requester, YYYY-MM-DD"})
				}
				s.NewDueOn = &d
			} else if in.Body.NewDueOn != "" {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.new_due_on", Message: "is only for an extension", Fix: "Leave it out"})
			}
			if (in.Body.Step == "answered") != (in.Body.Outcome != "") {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.outcome", Message: "goes with an answer, and only with one",
					Fix: "Send the outcome with answered, and leave it out otherwise"})
			}
			if len(bad) > 0 {
				return PDRequestView{}, invalid(bad)
			}
			if err := u.Compliance().StepPDRequest(ctx, r.ID, s); err != nil {
				return PDRequestView{}, err
			}
			after, err := a.onePDRequest(ctx, u, in.RequestID)
			return pdRequestView(after, today), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.GetPersonalDataReport, exempt(huma.Operation{
		OperationID: "getPersonalDataReport", Method: http.MethodGet, Path: "/v1/compliance/personal-data-requests/{request_id}/report", Tags: []string{"Compliance"},
		Summary:     "Report the personal data held about a request's subject",
		Description: "The personal data Fyniq holds about an access request's subject, a member or a contact, read through the inventory's classes, to answer the request (CMP-PD-04). A request about someone else reports nothing. Read-only; the read is audited as a security event.",
	}, "cursor", "One person's report, not a list: a handful of values from the inventory's classes."), append([]string{CodeNotFound}, locks...),
		func(ctx context.Context, in *PDRequestPath) (*pdReportOutput, error) {
			p, _ := authn.From(ctx)
			out := &pdReportOutput{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				r, err := a.onePDRequest(ctx, u, in.RequestID)
				if err != nil {
					return err
				}
				if out.Body, err = a.personalDataReport(ctx, u, r.Subject); err != nil {
					return err
				}
				// Every read of a person's data is audited (AUD-05): whose, and how many values.
				return u.Audit().Record(ctx, storage.AuditRecord{Action: auditedReads["getPersonalDataReport"], ObjectType: "pd_request", ObjectID: r.ID,
					Changes: map[string][2]any{"subject": {nil, r.Subject}, "values": {nil, len(out.Body.Items)}}})
			})
			return out, err
		})

	register(a, authz.ListMyPDRequests, exempt(huma.Operation{
		OperationID: "listMyPersonalDataRequests", Method: http.MethodGet, Path: "/v1/me/personal-data-requests", Tags: []string{"Compliance"},
		Summary:     "List my requests about my personal data",
		Description: "Your own access and correction requests (CMP-PD-04), with when each is due and its steps. Read-only; no side effects.",
	}, "cursor", "A member's own requests are few, read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*pdRequestsOutput, error) {
			p, _ := authn.From(ctx)
			out := &pdRequestsOutput{}
			out.Body.Items = []PDRequestView{}
			me := "member:" + p.UserID.String()
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				all, err := u.Compliance().PDRequests(ctx)
				for _, r := range all {
					if r.Subject == me {
						out.Body.Items = append(out.Body.Items, pdRequestView(r, today))
					}
				}
				return err
			})
			return out, err
		})

	register(a, authz.AskAboutMyPersonalData, huma.Operation{
		OperationID: "askAboutMyPersonalData", Method: http.MethodPost, Path: "/v1/me/personal-data-requests", Tags: []string{"Compliance"},
		Summary:     "Ask to see or correct my personal data",
		Description: "You ask for access to, or correction of, the personal data the company holds about you (CMP-PD-04). It is answered within 30 days, or you are told in writing within 30 days when it will be. Needs a person signed in to Fyniq, never the AI. Side effects: the request, the audit event pd_request.received, a notice to the Compliance officer. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeSessionRequired}, writes...), func(ctx context.Context, in *myRequestInput) (*pdRequestOutput, error) {
		p, err := signedIn(ctx, "Asking about your personal data")
		if err != nil {
			return nil, err
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PDRequestView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return PDRequestView{}, err
			}
			return received(ctx, u, storage.PDRequest{Kind: in.Body.Kind, Subject: "member:" + p.UserID.String(), Details: strings.TrimSpace(in.Body.Details),
				ReceivedOn: today, CreatedBy: uploader(ctx), CreatedAt: now}, today)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})
}
