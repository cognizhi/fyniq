package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a ledger entry, traced to its source (FR-UX-04)

// LedgerEntryLine is one line of a ledger entry.
type LedgerEntryLine struct {
	LineNo      int    `json:"line_no" doc:"Its place in the entry." example:"1"`
	AccountID   string `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code        string `json:"code" doc:"The account's code." example:"6120"`
	Name        string `json:"name" doc:"The account's name." example:"Software subscriptions"`
	Description string `json:"description,omitempty" doc:"The line's description." example:"GitHub Team, September"`
	Amount      Money  `json:"amount" doc:"In the line's currency: debit positive, credit negative."`
	Base        Money  `json:"base" doc:"In the base currency, as posted."`
	// FR-DIM-02.
	DimensionValueIDs []DimensionValueRef `json:"dimension_value_ids,omitempty" doc:"The line's dimension values (listDimensionValues)."`
}

// LedgerEntrySource is where an entry comes from (ENG-03): its source, and the document it is when it is one.
type LedgerEntrySource struct {
	Type    string `json:"type" doc:"The source type (ENG-03), such as manual_journal, bill or bank_transaction." example:"bill"`
	ID      string `json:"id" doc:"The source's ID, to open it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Version int    `json:"version" doc:"The source's version that posted the entry." example:"1"`
	// The document, when the source is one of the organisation's documents.
	DocumentType   string `json:"document_type,omitempty" doc:"The document's type, when the source is a document; absent otherwise." example:"bill"`
	Number         string `json:"number,omitempty" doc:"The document's number, once it has one." example:"BILL-0905"`
	Status         string `json:"status,omitempty" doc:"The document's status now." example:"posted"`
	Date           string `json:"date,omitempty" format:"date" doc:"The document's date." example:"2026-09-05"`
	Party          string `json:"party,omitempty" doc:"The customer's or supplier's name, for a document that names one." example:"GitHub, Inc."`
	CurrentVersion int    `json:"current_version,omitempty" doc:"The document's version now: later than version when it was changed and posted again since." example:"1"`
	Link           string `json:"link,omitempty" doc:"Where the document opens in the web app, when it has a page." example:"/purchases/bills/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
}

// LedgerEntryView is a ledger entry with its lines and its source: the Trace panel's ledger line, source and history
// (FR-UX-04), read as the general ledger is.
type LedgerEntryView struct {
	ID             string            `json:"id" format:"uuid" doc:"The entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	PostingDate    string            `json:"posting_date" format:"date" doc:"The date it posted on." example:"2026-09-05"`
	LedgerRevision int64             `json:"ledger_revision" doc:"The ledger revision it made (ENG-46)." example:"18402"`
	Memo           string            `json:"memo,omitempty" doc:"Its memo." example:"GitHub Team, September"`
	Reverses       string            `json:"reverses,omitempty" format:"uuid" doc:"The entry it reverses, for a reversing entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d81"`
	Source         LedgerEntrySource `json:"source" doc:"Where it comes from."`
	Lines          []LedgerEntryLine `json:"lines" nullable:"false" doc:"Its lines, in order."`
}

// LedgerEntryPath names a ledger entry.
type LedgerEntryPath struct {
	EntryID string `path:"entry_id" format:"uuid" doc:"The entry, as a general ledger line names it (entry_id)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
}

type ledgerEntryOutput struct{ Body LedgerEntryView }

func registerLedgerEntries(a *API) {
	register(a, authz.GetLedgerEntry, huma.Operation{
		OperationID: "getLedgerEntry", Method: http.MethodGet, Path: "/v1/ledger/entries/{entry_id}", Tags: []string{"Reports"},
		Summary: "Get a ledger entry and its source",
		Description: "A ledger entry with its lines and where it comes from (FR-UX-04, the Trace panel): its source's type, ID and the " +
			"version that posted it, and, when the source is a document, its type, number, status, date, customer or supplier and " +
			"version now. Read as the general ledger is, so it needs an unqualified view of the reports; an entry the purge after " +
			"retention removed is not found (AUD-10). The document's history is the audit trail's, for those who may read it " +
			"(listAuditEvents with record_type=document and its ID). Read-only; no side effects.",
	}, []string{CodeInvalid, CodeNotFound}, func(ctx context.Context, in *LedgerEntryPath) (*ledgerEntryOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.EntryID)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "entry_id", Message: "is not an entry's ID", Fix: "Send a general ledger line's entry_id"}})
		}
		var out ledgerEntryOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			e, err := u.Ledger().Entry(ctx, id)
			if err != nil {
				return err
			}
			out.Body = LedgerEntryView{ID: e.ID.String(), PostingDate: dateOf(e.PostingDate), LedgerRevision: e.LedgerRevision, Memo: e.Memo,
				Source: LedgerEntrySource{Type: e.Source.Type, ID: e.Source.ID, Version: e.Source.Version}, Lines: make([]LedgerEntryLine, 0, len(e.Lines))}
			if e.ReversesEntryID != nil {
				out.Body.Reverses = e.ReversesEntryID.String()
			}
			accounts := map[uuid.UUID]storage.Account{}
			for _, l := range e.Lines {
				acc, ok := accounts[l.AccountID]
				if !ok {
					if acc, err = u.Accounts().Get(ctx, l.AccountID); err != nil {
						return err
					}
					accounts[l.AccountID] = acc
				}
				cur, err := money.Lookup(l.Currency)
				if err != nil {
					return err
				}
				amount, err := money.ParseStored(string(l.Amount), cur)
				if err != nil {
					return err
				}
				b, err := money.ParseStored(string(l.BaseAmount), base)
				if err != nil {
					return err
				}
				out.Body.Lines = append(out.Body.Lines, LedgerEntryLine{LineNo: l.LineNo, AccountID: acc.ID.String(), Code: acc.Code, Name: acc.Name,
					Description: l.Description, Amount: Money{Amount: amount.String(), Currency: cur.Code}, Base: Money{Amount: b.String(), Currency: base.Code},
					DimensionValueIDs: valueRefs(l.DimensionValues)})
			}
			return entrySourceDocument(ctx, u, &out.Body.Source)
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// entrySourceDocument fills in the document an entry's source is, when it is one of the organisation's documents; a
// source that is not, such as an account merge's, keeps its type and ID alone.
func entrySourceDocument(ctx context.Context, u storage.UnitOfWork, s *LedgerEntrySource) error {
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return nil
	}
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var payload struct {
		Date       string `json:"date"`
		CustomerID string `json:"customer_id"`
		SupplierID string `json:"supplier_id"`
	}
	_ = json.Unmarshal(doc.Payload, &payload) // a payload without these fields leaves them empty
	s.DocumentType, s.Number, s.Status, s.Date, s.CurrentVersion = string(doc.Type), doc.Number, string(doc.Status), payload.Date, doc.Version
	s.Link = linkOf(doc.Type, doc.ID.String(), false)
	party := payload.CustomerID
	if party == "" {
		party = payload.SupplierID
	}
	if cid, err := uuid.Parse(party); err == nil {
		c, err := u.Contacts().Get(ctx, cid)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		s.Party = c.LegalName
	}
	return nil
}
