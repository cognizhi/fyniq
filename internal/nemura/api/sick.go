package api

import (
	"context"
	"encoding/base64"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// SickAsk is a person telling the company they are sick: sick leave is notified, not applied for (NM-LV-12).
type SickAsk struct {
	Kind string `json:"kind" enum:"outpatient,hospitalisation" doc:"Outpatient or hospitalisation sick leave; they share one yearly ceiling." example:"outpatient"`
	From string `json:"from" format:"date" doc:"The first day of absence." example:"2026-05-04"`
	To   string `json:"to" format:"date" doc:"The last day; the same as the first for one day." example:"2026-05-06"`
	Note string `json:"note,omitempty" maxLength:"500" doc:"A note for the people team." example:"Back on Thursday."`
}

// SickNotified is how a notification fell against the year's ceilings.
type SickNotified struct {
	ID     string `json:"id" format:"uuid" doc:"The notification." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
	Days   string `json:"days" doc:"The working days of the absence." example:"3.00"`
	Paid   string `json:"paid_days" doc:"Days within the year's ceiling: paid sick leave." example:"3.00"`
	Unpaid string `json:"unpaid_days" doc:"Days beyond the ceiling, or before three months' service, which the people team decides on." example:"0.00"`
	Late   bool   `json:"late" doc:"Whether it was told more than 48 hours after the first day: it is recorded and accepted." example:"false"`
}

// SickNotification is a notification as it is listed.
type SickNotification struct {
	SickNotified
	PersonID    string `json:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Person      string `json:"person_name" doc:"Their name." example:"Ben Lim"`
	Kind        string `json:"kind" enum:"outpatient,hospitalisation" doc:"The kind of sick leave." example:"outpatient"`
	From        string `json:"from" format:"date" doc:"The first day." example:"2026-05-04"`
	To          string `json:"to" format:"date" doc:"The last day." example:"2026-05-06"`
	Note        string `json:"note,omitempty" doc:"The person's note." example:"Back on Thursday."`
	Certificate bool   `json:"has_certificate" doc:"Whether a certificate was given." example:"true"`
	Issuer      string `json:"issuer,omitempty" doc:"Who issued the certificate." example:"Raffles Medical Clinic"`
	Verified    bool   `json:"verified" doc:"Whether the people team verified the certificate." example:"false"`
	Treatment   string `json:"unpaid_decision,omitempty" enum:"unpaid,annual" doc:"How the days beyond the ceiling were decided, once the people team has decided." example:"unpaid"`
}

// Sick is sick leave behind the API: the API decides who may call, the service reads and writes in one transaction, keeps the certificate as a restricted document
// and audits.
type Sick interface {
	Notify(ctx context.Context, s Session, a SickAsk) (SickNotified, error)
	List(ctx context.Context, s Session, all, unverified bool) ([]SickNotification, error)
	Certificate(ctx context.Context, s Session, id uuid.UUID, team bool, issuer, contentType string, content []byte) error
	Verify(ctx context.Context, s Session, id uuid.UUID) error
}

func init() {
	IdempotencyExempt["POST /v1/me/sick-leave"] = "a repeat overlaps the first notification's days, which the database refuses (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/sick-leave/{id}/certificate"] = "a certificate is given once: a repeat is refused (NM_LEAVE_CONFLICT)"
	IdempotencyExempt["POST /v1/sick-leave/{id}/verification"] = "a certificate is verified once: a repeat is refused (NM_LEAVE_CONFLICT)"
}

type sickInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   SickAsk
}

type sickNotifiedOutput struct{ Body SickNotified }

type sickListInput struct {
	Cookie     string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Unverified bool   `query:"unverified" doc:"Only notifications whose certificate waits to be verified (the people team)." example:"true"`
}

type sickListOutput struct {
	Body struct {
		Notifications []SickNotification `json:"notifications" doc:"The notifications the caller may read, newest first."`
	}
}

type certificateInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The notification." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
	Body   struct {
		Issuer        string `json:"issuer" minLength:"1" maxLength:"200" doc:"The clinic, hospital or doctor on the certificate." example:"Raffles Medical Clinic"`
		ContentType   string `json:"content_type" doc:"The file's media type." example:"image/jpeg"`
		ContentBase64 string `json:"content_base64" doc:"The photo or e-MC, base64, up to 5 MB." example:"/9j/4AAQ"`
	}
}

// sickRights is what a caller holds on medical certificates and notes (Appendix B): the HR officer and the HR administrator read and verify everyone's; nobody
// else reads another person's, a line manager included, and the Owner sees none of the detail.
func (a *API) sickTeam(ctx context.Context, s Session) (bool, error) {
	return a.rights(ctx, s, capSick, authz.View)
}

const capSick = "Medical certificates and notes"

func (a *API) registerSick() {
	huma.Register(a.API, huma.Operation{
		OperationID: "notifySickLeave", Method: "POST", Path: "/v1/me/sick-leave", Tags: []string{"Leave"}, Summary: "Tell the company you are sick",
		Description: "Records the signed-in person's sick leave: no approval. Each working day is taken against the paid ceiling in force on that day, outpatient and hospitalisation sharing one; days beyond it are recorded as unpaid for the people team to decide on. A notice later than 48 hours after the first day is flagged and accepted. It is audited and the people team is told.",
		Errors:      []int{401, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *sickInput) (*sickNotifiedOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		n, err := a.d.Sick.Notify(ctx, s, in.Body)
		if err != nil {
			return nil, requestsError(err)
		}
		return &sickNotifiedOutput{Body: n}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listSickLeave", Method: "GET", Path: "/v1/sick-leave", Tags: []string{"Leave"}, Summary: "Sick leave notifications the caller may read",
		Description: "Lists the caller's own notifications; the HR officer and the HR administrator read everyone's (Appendix B: Medical certificates and notes). A line manager, Payroll and the Owner read none of another person's: the team calendar shows them \"Away\". It changes nothing.",
		Errors:      []int{401, 409},
	}, func(ctx context.Context, in *sickListInput) (*sickListOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		all, err := a.sickTeam(ctx, s)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Sick.List(ctx, s, all, in.Unverified && all)
		if err != nil {
			return nil, requestsError(err)
		}
		out := &sickListOutput{}
		out.Body.Notifications = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "addSickCertificate", Method: "POST", Path: "/v1/sick-leave/{id}/certificate", Tags: []string{"Leave"}, Summary: "Give the medical certificate",
		Description: "Stores the certificate (a photo or e-MC of up to 5 MB) as a restricted document of the person, seen by the HR officer and the HR administrator only, and records who issued it. The person gives their own; the people team may give it for them. It is given once (NM_LEAVE_CONFLICT). It is audited.",
		Errors:      []int{401, 404, 409, 413, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *certificateInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such sick leave notification in your view.")
		}
		content, err := base64.StdEncoding.DecodeString(in.Body.ContentBase64)
		if err != nil || len(content) == 0 {
			return nil, Err(CodeLeaveInvalid, "content_base64 is not base64 of a file.")
		}
		if len(content) > maxDocument {
			return nil, Err(CodeLeaveInvalid, "The file is larger than 5 MB: attach a smaller one.")
		}
		team, err := a.sickTeam(ctx, s)
		if err != nil {
			return nil, err
		}
		return nil, requestsErrorOrNil(a.d.Sick.Certificate(ctx, s, id, team, in.Body.Issuer, in.Body.ContentType, content))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "verifySickCertificate", Method: "POST", Path: "/v1/sick-leave/{id}/verification", Tags: []string{"Leave"}, Summary: "Verify a medical certificate",
		Description: "The HR officer or the HR administrator records that they verified the certificate, once, and never their own. A manager cannot refuse a certified day the person is entitled to. It is audited.",
		Errors:      []int{401, 403, 404, 409},
	}, func(ctx context.Context, in *requestIDInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if err := a.leaveOn(ctx, s); err != nil {
			return nil, err
		}
		team, err := a.sickTeam(ctx, s)
		if err != nil {
			return nil, err
		}
		if !team {
			return nil, Err(CodeRoleRequired, "Only the HR officer and the HR administrator verify medical certificates.")
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such sick leave notification in your view.")
		}
		return nil, requestsErrorOrNil(a.d.Sick.Verify(ctx, s, id))
	})
}
