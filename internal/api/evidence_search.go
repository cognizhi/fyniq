package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ evidence search (FR-DOC-10, rev 0.184)
//
// Every file across the organisation's records, found by activity, evidence type, the record's date and uploader, and
// only on the records whose Appendix D rows the person views. The missing-evidence list is Checks' C.2 #30.

// EvidenceActivity is a kind of record files are attached to, as a search names it.
type EvidenceActivity struct {
	Key   string
	Label string
	Row   authz.Capability
	Types []storage.DocumentType
}

// EvidenceActivities are the kinds of record a search covers, in the order a filter offers them.
var EvidenceActivities = []EvidenceActivity{
	{"bill", "Bills and vendor credits", authz.Bills, []storage.DocumentType{storage.DocumentBill, storage.DocumentVendorCredit}},
	{"invoice", "Invoices and credit notes", authz.Invoices, []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote}},
	{"journal", "Journals", authz.Journals, []storage.DocumentType{storage.DocumentJournal}},
	{"bank", "Bank transactions and agreements", authz.BankTransactions, []storage.DocumentType{storage.DocumentBankTransaction, storage.DocumentBankAgreement}},
	{"claim", "Expense claims", authz.ExpenseClaims, []storage.DocumentType{storage.DocumentExpenseClaim}},
	{"vendor_bank_change", "Changes of suppliers' bank details", authz.VendorBankDetails, []storage.DocumentType{storage.DocumentPayeeBankChange}},
	{"person_bank_change", "Changes of people's bank accounts", authz.PaymentBatches, []storage.DocumentType{storage.DocumentClaimantBankChange}},
	{"founders", "Founders' money", authz.DirectorAccounts, []storage.DocumentType{storage.DocumentPreIncorporationCost, storage.DocumentDirectorPayChoice,
		storage.DocumentDirectorFee, storage.DocumentExemptStatus, storage.DocumentDirectorSettlement}},
	{"owner_succession", "Owner successions", authz.UsersAndRoles, []storage.DocumentType{storage.DocumentOwnerSuccession}},
}

// ActivityKey names a kind of record an evidence search covers.
type ActivityKey string

// Schema implements huma.SchemaProvider.
func (ActivityKey) Schema(huma.Registry) *huma.Schema {
	enum := make([]any, len(EvidenceActivities))
	for i, a := range EvidenceActivities {
		enum[i] = a.Key
	}
	return &huma.Schema{Type: "string", Description: "A kind of record files are attached to.", Enum: enum, Examples: []any{"bill"}}
}

// activityOf is the activity a document type belongs to.
func activityOf(t storage.DocumentType) string {
	for _, a := range EvidenceActivities {
		if slices.Contains(a.Types, t) {
			return a.Key
		}
	}
	return ""
}

// EvidenceRecord is the record a file is attached to.
type EvidenceRecord struct {
	Activity string `json:"activity" enum:"bill,invoice,journal,bank,claim,vendor_bank_change,person_bank_change,founders,owner_succession" doc:"The kind of record." example:"bill"`
	Type     string `json:"type" doc:"The record's document type, such as bill or vendor_credit." example:"bill"`
	ID       string `json:"id" format:"uuid" doc:"The record's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Number   string `json:"number,omitempty" doc:"Its number, once it has one." example:"BILL-0012"`
	Date     string `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-20"`
	Status   string `json:"status" doc:"Its status, such as draft or posted." example:"posted"`
}

// EvidenceFound is a file a search found, with its record.
type EvidenceFound struct {
	ID           string         `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	FileName     string         `json:"file_name" doc:"As the uploader named it." example:"AU-100.pdf"`
	EvidenceType string         `json:"evidence_type" doc:"What the file is." example:"invoice"`
	MediaType    string         `json:"media_type" doc:"Its type, from its content." example:"application/pdf"`
	Size         int64          `json:"size_bytes" doc:"Its size in bytes." example:"18432"`
	DataClass    string         `json:"data_class" enum:"general,personal,bank,payroll,identity" doc:"The personal data it holds (FR-DOC-09)." example:"general"`
	UnmaskedNRIC bool           `json:"unmasked_nric,omitempty" doc:"true when its text shows a Singapore NRIC or FIN in full (FR-DOC-09)." example:"false"`
	UploadedBy   string         `json:"uploaded_by" doc:"Who attached it: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UploadedAt   time.Time      `json:"uploaded_at" doc:"When." example:"2026-10-20T09:15:00Z"`
	State        string         `json:"state" enum:"attached,removed,final" doc:"Attached, or final: committed to the retention-locked store." example:"final"`
	Record       EvidenceRecord `json:"record" doc:"The record it is attached to."`
}

type searchEvidenceInput struct {
	PageQuery
	Activity     string `query:"activity" enum:"bill,invoice,journal,bank,claim,vendor_bank_change,person_bank_change,founders,owner_succession" doc:"Only files on this kind of record; every kind you may see when left out." example:"bill"`
	EvidenceType string `query:"evidence_type" maxLength:"100" doc:"Only files whose evidence type contains this, case aside." example:"invoice"`
	From         string `query:"from" format:"date" doc:"Only files on records dated on or after this day." example:"2026-10-01"`
	To           string `query:"to" format:"date" doc:"Only files on records dated on or before this day." example:"2026-10-31"`
	UploadedBy   string `query:"uploaded_by" maxLength:"220" doc:"Only files this actor attached, such as user:<user id>." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type searchEvidenceOutput struct {
	Body struct {
		Items      []EvidenceFound `json:"items" nullable:"false" doc:"The files on this page, newest record date first."`
		Activities []ActivityKey   `json:"activities" nullable:"false" doc:"The kinds of record you may search."`
		NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMjAifQ"`
	}
}

// searchable are the activities p may search: those whose Appendix D row they view without a qualifier.
func searchable(p authn.Principal) []EvidenceActivity {
	var out []EvidenceActivity
	for _, a := range EvidenceActivities {
		if authz.Decide(authz.Policy{Operation: "view", Capability: a.Row, Need: authz.View, Unqualified: true}, p.Roles, p.AI()).Allowed {
			out = append(out, a)
		}
	}
	return out
}

func registerEvidenceSearch(a *API) {
	register(a, authz.SearchEvidence, huma.Operation{
		OperationID: "searchEvidence", Method: http.MethodGet, Path: "/v1/evidence", Tags: []string{"Evidence"},
		Summary: "Search the evidence",
		Description: "Every file attached across the organisation's records, removed ones aside, newest record date first, a page at a " +
			"time (FR-DOC-10; PRD-001 rev 0.184): by activity, evidence type, the record's date and uploader. Only files on records " +
			"whose Appendix D rows you view are listed, and activities lists the kinds you may search. The missing-evidence list is " +
			"Checks' C.2 #30 (getCheck evidence.missing). Read-only.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *searchEvidenceInput) (*searchEvidenceOutput, error) {
		p, _ := authn.From(ctx)
		out := &searchEvidenceOutput{}
		out.Body.Items, out.Body.Activities = []EvidenceFound{}, []ActivityKey{}
		var types []storage.DocumentType
		for _, x := range searchable(p) {
			out.Body.Activities = append(out.Body.Activities, ActivityKey(x.Key))
			if in.Activity == "" || in.Activity == x.Key {
				types = append(types, x.Types...)
			}
		}
		var bad []FieldProblem
		for _, d := range []struct{ field, value string }{{"query.from", in.From}, {"query.to", in.To}} {
			if d.value != "" {
				if _, err := time.Parse(time.DateOnly, d.value); err != nil {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: d.field, Message: "is not a date", Fix: "Send YYYY-MM-DD"})
				}
			}
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		if len(types) == 0 {
			return out, nil // nothing they may see, or a kind they may not
		}
		f := storage.EvidenceSearch{Types: types, Kind: strings.TrimSpace(in.EvidenceType), From: in.From, To: in.To, UploadedBy: strings.TrimSpace(in.UploadedBy)}
		cur, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		if cur != "" {
			date, id, ok := strings.Cut(cur, "|")
			after, err := uuid.Parse(id)
			if !ok || err != nil {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor.")
			}
			f.AfterDate, f.AfterID = date, &after
		}
		size := in.PageSize
		if size <= 0 {
			size = 100
		}
		f.Limit = size + 1
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			hits, err := u.Evidence().Search(ctx, f)
			if err != nil {
				return err
			}
			if len(hits) > size {
				hits = hits[:size]
				last := hits[len(hits)-1]
				out.Body.NextCursor = encodeCursor(last.DocumentDate + "|" + last.ID.String())
			}
			for _, h := range hits {
				out.Body.Items = append(out.Body.Items, EvidenceFound{ID: h.ID.String(), FileName: h.FileName, EvidenceType: h.EvidenceType,
					MediaType: h.MediaType, Size: h.Size, DataClass: string(h.DataClass), UnmaskedNRIC: h.NRICFlagged, UploadedBy: h.UploadedBy,
					UploadedAt: h.UploadedAt.UTC(), State: string(h.State), Record: EvidenceRecord{Activity: activityOf(h.DocumentType),
						Type: string(h.DocumentType), ID: h.DocumentID.String(), Number: h.DocumentNumber, Date: h.DocumentDate, Status: string(h.DocumentStatus)}})
			}
			return nil
		})
		return out, err
	})
}
