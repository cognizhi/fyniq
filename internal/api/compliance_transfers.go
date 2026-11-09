package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	compliancedomain "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// TransferFlowView is one of Fyniq's flows of personal data to a provider, from its catalogue, and whether the
// organisation's register covers it (CMP-PD-06, rev 0.244).
type TransferFlowView struct {
	Key         string         `json:"key" doc:"The flow." example:"email"`
	What        string         `json:"what" doc:"What the flow is." example:"Email sent through the deployment's email providers: Zoho ZeptoMail or Resend"`
	Data        string         `json:"data" doc:"The personal data it carries." example:"Recipients' names and email addresses, and the messages"`
	Classes     []DataClassKey `json:"classes" nullable:"false" doc:"The inventory's classes it carries."`
	CrossBorder bool           `json:"cross_border" doc:"true when it leaves Singapore, so it needs a transfer assessment." example:"true"`
	Note        string         `json:"note" doc:"Where it goes, and what is still to verify." example:"The email providers keep mail outside Singapore (FR-INT-21)"`
	CoveredBy   string         `json:"covered_by,omitempty" format:"uuid" doc:"The open transfer that assesses it; absent when none does." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

// DataClassKey names a class of the personal-data inventory, as getDataInventory lists them.
type DataClassKey string

// Schema documents the key.
func (DataClassKey) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A class of getDataInventory's classes.", Examples: []any{"members"}}
}

// TransferView is the latest version of an overseas transfer, assessed and recorded (CMP-PD-06).
type TransferView struct {
	TransferID   string        `json:"transfer_id" format:"uuid" doc:"The transfer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Version      int           `json:"version" doc:"Its version; the ETag carries it for If-Match." example:"1"`
	Flow         string        `json:"flow,omitempty" doc:"The catalogue flow it covers; absent for one of the company's own." example:"email"`
	Recipient    string        `json:"recipient" doc:"Who receives the data." example:"Zoho Corporation (ZeptoMail)"`
	Countries    string        `json:"countries" doc:"Where the recipient processes it." example:"United States, India"`
	Data         string        `json:"data" doc:"The personal data transferred." example:"Recipients' names and email addresses, and the messages"`
	Purpose      string        `json:"purpose" doc:"Why." example:"To deliver the company's email"`
	Basis        TransferBasis `json:"basis" doc:"What protects the data abroad."`
	AssessedOn   string        `json:"assessed_on" format:"date" doc:"When it was assessed." example:"2026-11-09"`
	AssessedBy   string        `json:"assessed_by" doc:"Who assessed it." example:"Ann Lim, Data Protection Officer"`
	Evidence     string        `json:"evidence" doc:"Where the contract, rules or certificate are kept." example:"The provider's data processing addendum, in the shared drive"`
	NextReviewOn string        `json:"next_review_on" format:"date" doc:"When it is reviewed next." example:"2027-11-09"`
	ReviewDue    bool          `json:"review_due" doc:"true from 30 days before the next review, while open." example:"false"`
	Overdue      bool          `json:"overdue" doc:"true once the next review has passed, while open." example:"false"`
	State        string        `json:"state" enum:"open,retired" doc:"open while the transfer goes on; retired once it stops." example:"open"`
	SavedBy      string        `json:"saved_by" doc:"Who saved this version, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	SavedAt      string        `json:"saved_at" format:"date-time" doc:"When." example:"2026-11-09T02:00:00Z"`
}

// TransferBasis is what protects personal data abroad under the transfer limitation obligation.
type TransferBasis string

// Schema documents the bases.
func (TransferBasis) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"contract", "binding_corporate_rules", "other_instrument", "certification", "consent"},
		Description: "contract: a contract with the recipient; binding_corporate_rules: rules binding the recipient's group; other_instrument: " +
			"another law or legally binding instrument; certification: a specified certification such as APEC CBPR; consent: the person's consent.",
		Examples: []any{"contract"}}
}

// TransferInput is what a transfer says.
type TransferInput struct {
	Flow         string        `json:"flow,omitempty" pattern:"^[a-z][a-z0-9-]{0,39}$" doc:"The catalogue flow it covers, from getTransfers' flows; leave it out for one of the company's own. A revision keeps the flow." example:"email"`
	Recipient    string        `json:"recipient" minLength:"1" maxLength:"300" doc:"Who receives the data." example:"Zoho Corporation (ZeptoMail)"`
	Countries    string        `json:"countries" minLength:"1" maxLength:"300" doc:"Where the recipient processes it." example:"United States, India"`
	Data         string        `json:"data" minLength:"1" maxLength:"500" doc:"The personal data transferred." example:"Recipients' names and email addresses, and the messages"`
	Purpose      string        `json:"purpose" minLength:"1" maxLength:"1000" doc:"Why." example:"To deliver the company's email"`
	Basis        TransferBasis `json:"basis" doc:"What protects the data abroad."`
	AssessedOn   string        `json:"assessed_on" format:"date" doc:"When it was assessed." example:"2026-11-09"`
	AssessedBy   string        `json:"assessed_by" minLength:"1" maxLength:"300" doc:"Who assessed it." example:"Ann Lim, Data Protection Officer"`
	Evidence     string        `json:"evidence" minLength:"1" maxLength:"1000" doc:"Where the contract, rules or certificate are kept." example:"The provider's data processing addendum, in the shared drive"`
	NextReviewOn string        `json:"next_review_on" format:"date" doc:"When it is reviewed next: after the assessment." example:"2027-11-09"`
	State        string        `json:"state,omitempty" enum:"open,retired" doc:"open, the default, or retired once the transfer stops." example:"open"`
}

// TransferRegister is the organisation's overseas transfers and Fyniq's flows they cover (CMP-PD-06).
type TransferRegister struct {
	Flows     []TransferFlowView `json:"flows" nullable:"false" doc:"Fyniq's flows of personal data to providers, from its catalogue: the same for every organisation."`
	Transfers []TransferView     `json:"transfers" nullable:"false" doc:"The organisation's transfers at their latest versions, oldest first."`
}

type (
	transferRegisterOutput struct{ Body TransferRegister }
	// PDTransferPath names a transfer (exported: huma reads only an exported embedded struct).
	PDTransferPath struct {
		TransferID string `path:"transfer_id" format:"uuid" doc:"The transfer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	transferCreateInput struct {
		KeyHeader
		PreferHeader
		Body TransferInput
	}
	transferReviseInput struct {
		PDTransferPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body TransferInput
	}
	pdTransferOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The transfer's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     TransferView
	}
)

func transferView(t storage.Transfer, today time.Time) TransferView {
	open := t.State == "open"
	return TransferView{TransferID: t.TransferID.String(), Version: t.Version, Flow: t.Flow, Recipient: t.Recipient, Countries: t.Countries,
		Data: t.Data, Purpose: t.Purpose, Basis: TransferBasis(t.Basis), AssessedOn: t.AssessedOn.Format(time.DateOnly), AssessedBy: t.AssessedBy,
		Evidence: t.Evidence, NextReviewOn: t.NextReviewOn.Format(time.DateOnly),
		ReviewDue: open && !today.Before(t.NextReviewOn.AddDate(0, 0, -compliancedomain.TransferReviewWindow)),
		Overdue:   open && today.After(t.NextReviewOn), State: t.State, SavedBy: t.SavedBy, SavedAt: t.SavedAt.UTC().Format(time.RFC3339)}
}

func transferETag(version int) string { return etag("v" + strconv.Itoa(version)) }

// pdTransferOf reads a transfer's input against the catalogue's flows; the version, the actor and the time come from
// the caller.
func pdTransferOf(in TransferInput, flows []compliance.Flow) (storage.Transfer, error) {
	t := storage.Transfer{Flow: in.Flow, Recipient: strings.TrimSpace(in.Recipient), Countries: strings.TrimSpace(in.Countries),
		Data: strings.TrimSpace(in.Data), Purpose: strings.TrimSpace(in.Purpose), Basis: string(in.Basis), AssessedBy: strings.TrimSpace(in.AssessedBy),
		Evidence: strings.TrimSpace(in.Evidence), State: cmpOr(in.State, "open")}
	var bad []FieldProblem
	for _, f := range []struct{ field, v string }{{"recipient", t.Recipient}, {"countries", t.Countries}, {"data", t.Data}, {"purpose", t.Purpose},
		{"assessed_by", t.AssessedBy}, {"evidence", t.Evidence}} {
		if f.v == "" {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + f.field, Message: "is empty", Fix: "Say it, in a few words"})
		}
	}
	if t.Flow != "" && !slices.ContainsFunc(flows, func(f compliance.Flow) bool { return f.Key == t.Flow }) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.flow", Message: "is not one of Fyniq's flows", Fix: "Use a key from getTransfers' flows, or leave it out"})
	}
	var err error
	if t.AssessedOn, err = time.Parse(time.DateOnly, in.AssessedOn); err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.assessed_on", Message: "is not a date", Fix: "Give the day, as 2026-11-09"})
	}
	if t.NextReviewOn, err = time.Parse(time.DateOnly, in.NextReviewOn); err != nil || !t.NextReviewOn.After(t.AssessedOn) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.next_review_on", Message: "is not a day after the assessment",
			Fix: "Give the day of the next review, after assessed_on; a year on is usual"})
	}
	if len(bad) > 0 {
		return t, invalid(bad)
	}
	return t, nil
}

func (a *API) transferRegister(ctx context.Context, u storage.UnitOfWork) (TransferRegister, error) {
	inv, err := compliance.LoadInventory()
	if err != nil {
		return TransferRegister{}, err
	}
	ts, err := u.Compliance().Transfers(ctx)
	if err != nil {
		return TransferRegister{}, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return TransferRegister{}, err
	}
	out := TransferRegister{Flows: []TransferFlowView{}, Transfers: []TransferView{}}
	covered := map[string]string{}
	for _, t := range ts {
		out.Transfers = append(out.Transfers, transferView(t, today))
		if t.State == "open" && t.Flow != "" {
			covered[t.Flow] = t.TransferID.String()
		}
	}
	for _, f := range inv.Flows {
		classes := make([]DataClassKey, 0, len(f.Classes))
		for _, c := range f.Classes {
			classes = append(classes, DataClassKey(c))
		}
		out.Flows = append(out.Flows, TransferFlowView{Key: f.Key, What: f.What, Data: f.Data, Classes: classes, CrossBorder: f.CrossBorder,
			Note: f.Note, CoveredBy: covered[f.Key]})
	}
	return out, nil
}

func registerPDTransfers(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeTransfer}, locks...)
	register(a, authz.GetTransfers, exempt(huma.Operation{
		OperationID: "getTransfers", Method: http.MethodGet, Path: "/v1/compliance/transfers", Tags: []string{"Compliance"},
		Summary: "Get the overseas transfers of personal data",
		Description: "The transfer register (CMP-PD-06): Fyniq's flows of personal data to providers, from its catalogue, each that " +
			"leaves Singapore with the open transfer that assesses it; and the organisation's transfers, each with its recipient, " +
			"countries, data, purpose, basis, assessment, evidence and next review. Read-only; no side effects.",
	}, "cursor", "The register is a handful of flows and transfers, read whole as one register beside the inventory."), locks,
		func(ctx context.Context, _ *struct{}) (*transferRegisterOutput, error) {
			p, _ := authn.From(ctx)
			out := &transferRegisterOutput{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out.Body, err = a.transferRegister(ctx, u)
				return err
			})
			return out, err
		})

	register(a, authz.CreateTransfer, huma.Operation{
		OperationID: "createTransfer", Method: http.MethodPost, Path: "/v1/compliance/transfers", Tags: []string{"Compliance"},
		Summary:     "Record an overseas transfer's assessment",
		Description: "Records an overseas transfer of personal data and its assessment (CMP-PD-06), at version 1: for one of Fyniq's flows, or one of the company's own. Side effects: the transfer, the audit event transfer.saved. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *transferCreateInput) (*pdTransferOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (TransferView, error) {
			inv, err := compliance.LoadInventory()
			if err != nil {
				return TransferView{}, err
			}
			t, err := pdTransferOf(in.Body, inv.Flows)
			if err != nil {
				return TransferView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return TransferView{}, err
			}
			t.Version, t.SavedBy, t.SavedAt = 1, uploader(ctx), now
			saved, err := u.Compliance().SaveTransfer(ctx, t)
			return transferView(saved, today), err
		})
		if err != nil {
			return nil, err
		}
		return &pdTransferOutput{Status: w.Status, ETag: transferETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReviseTransfer, huma.Operation{
		OperationID: "reviseTransfer", Method: http.MethodPut, Path: "/v1/compliance/transfers/{transfer_id}", Tags: []string{"Compliance"},
		Summary:     "Change an overseas transfer",
		Description: "Saves the transfer's next version (CMP-PD-06): a review with its new next review day, or its retirement when the transfer stops. It keeps the flow it covers. Earlier versions are kept. Side effects: the version, the audit event transfer.saved. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *transferReviseInput) (*pdTransferOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TransferView, error) {
			ts, err := u.Compliance().Transfers(ctx)
			if err != nil {
				return TransferView{}, err
			}
			i := slices.IndexFunc(ts, func(t storage.Transfer) bool { return t.TransferID.String() == in.TransferID })
			if i < 0 {
				return TransferView{}, problem(CodeNotFound, "There is no transfer "+in.TransferID+".", "Use a transfer_id from getTransfers.")
			}
			if err := checkIfMatch(in.IfMatch, transferETag(ts[i].Version)); err != nil {
				return TransferView{}, err
			}
			inv, err := compliance.LoadInventory()
			if err != nil {
				return TransferView{}, err
			}
			t, err := pdTransferOf(in.Body, inv.Flows)
			if err != nil {
				return TransferView{}, err
			}
			if t.Flow != ts[i].Flow {
				return TransferView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.flow", Message: "is not the flow the transfer covers",
					Fix: "Keep its flow; for another flow, retire this transfer and record a new one"}})
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return TransferView{}, err
			}
			t.TransferID, t.Version, t.SavedBy, t.SavedAt = ts[i].TransferID, ts[i].Version+1, uploader(ctx), now
			saved, err := u.Compliance().SaveTransfer(ctx, t)
			return transferView(saved, today), err
		})
		if err != nil {
			return nil, err
		}
		return &pdTransferOutput{Status: w.Status, ETag: transferETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
