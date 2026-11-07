package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/search"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ global search (FR-UX-02, rev 0.190)
//
// Documents, contacts, accounts and items, found by text or amount, each kind only for whoever views its Appendix D row
// (SEC-34): the results, their counts and their details. An Employee finds their own claims.

// searchGroup is a kind of record a search finds, as its filter names it.
type searchGroup struct {
	Key   string
	Label string
	Types []storage.DocumentType // its document types; none for contacts, accounts and items
	Row   authz.Capability
}

// searchGroups are the kinds of record, in the order the filters show them.
var searchGroups = []searchGroup{
	{"invoices", "Invoices and credit notes", []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote}, authz.Invoices},
	{"bills", "Bills and vendor credits", []storage.DocumentType{storage.DocumentBill, storage.DocumentVendorCredit}, authz.Bills},
	{"journals", "Journals", []storage.DocumentType{storage.DocumentJournal}, authz.Journals},
	{"claims", "Expense claims", []storage.DocumentType{storage.DocumentExpenseClaim}, authz.ExpenseClaims},
	{"quotes", "Quotes and sales orders", []storage.DocumentType{storage.DocumentQuote, storage.DocumentSalesOrder}, authz.Invoices},
	{"purchase_orders", "Purchase orders", []storage.DocumentType{storage.DocumentPurchaseOrder}, authz.Bills},
	{"contacts", "Contacts", nil, authz.ContactsAndItems},
	{"accounts", "Accounts", nil, authz.ChartOfAccounts},
	{"items", "Items", nil, authz.ContactsAndItems},
}

// SearchGroupKey names a kind of record a search finds.
type SearchGroupKey string

// Schema implements huma.SchemaProvider.
func (SearchGroupKey) Schema(huma.Registry) *huma.Schema {
	enum := make([]any, len(searchGroups))
	for i, g := range searchGroups {
		enum[i] = g.Key
	}
	return &huma.Schema{Type: "string", Description: "A kind of record a search finds.", Enum: enum, Examples: []any{"invoices"}}
}

// SearchResult is one record a search found.
type SearchResult struct {
	Group  SearchGroupKey `json:"group" doc:"Its kind."`
	Type   string         `json:"type" doc:"Its type: a document's type, such as invoice or bill, or contact, account or item." example:"invoice"`
	ID     string         `json:"id" format:"uuid" doc:"The record." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Title  string         `json:"title" doc:"What it is: a document's number and party, a contact's name, an account's or item's code and name." example:"INV-0012 · Acme Analytics Pte Ltd"`
	Detail string         `json:"detail,omitempty" doc:"Where the text was found, such as the memo or the line that holds it; a contact's UEN or email." example:"PO-1180, October platform fee"`
	Date   string         `json:"date,omitempty" format:"date" doc:"A document's date." example:"2026-10-14"`
	Status string         `json:"status,omitempty" doc:"A document's status, such as draft or posted; an inactive record's inactive." example:"posted"`
	Amount *Money         `json:"amount,omitempty" doc:"A posted document's largest live line, in its currency: an invoice's or bill's total."`
	Link   string         `json:"link" doc:"Where in the web app it opens." example:"/sales/invoices/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
}

// linkOf is where a found record opens in the web app; a claim opens as the caller's own, or for review.
func linkOf(t storage.DocumentType, id string, own bool) string {
	switch t {
	case storage.DocumentInvoice, storage.DocumentCreditNote:
		return "/sales/invoices/" + id
	case storage.DocumentBill, storage.DocumentVendorCredit:
		return "/purchases/bills/" + id
	case storage.DocumentJournal:
		return "/accounting/journals/" + id
	case storage.DocumentQuote, storage.DocumentSalesOrder:
		return "/sales/quotes/" + id
	case storage.DocumentPurchaseOrder:
		return "/purchases/orders/" + id
	case storage.DocumentExpenseClaim:
		if own {
			return "/me/claims/" + id
		}
		return "/purchases/claims/" + id
	}
	return ""
}

// SearchGroupCount is a kind of record the caller may search, with how many it found.
type SearchGroupCount struct {
	Key   SearchGroupKey `json:"key" doc:"The kind."`
	Label string         `json:"label" doc:"Its name, for a filter." example:"Invoices and credit notes"`
	Count int            `json:"count" doc:"How many it found, up to the limit." example:"3"`
	More  bool           `json:"more" doc:"true when it found more than the limit: refine the query." example:"false"`
}

// SearchResults are what a search found.
type SearchResults struct {
	Query   string             `json:"query" doc:"The query, as read." example:"> 5k"`
	Results []SearchResult     `json:"results" nullable:"false" doc:"The records found: each kind's newest first, in the filters' order; only group's when it is given."`
	Groups  []SearchGroupCount `json:"groups" nullable:"false" doc:"The kinds of record the caller may search, each with what it found."`
}

type searchInput struct {
	Q     string         `query:"q" required:"true" minLength:"1" maxLength:"200" doc:"What to find: text such as a reference (INV-0138), a name or words of a note; or an amount, exact (1,234.56) or above or below a bound (> 5k, <= 2.5m)." example:"> 5k"`
	Group SearchGroupKey `query:"group" doc:"Only this kind of record's results, up to 100; every kind's, up to 20 each, when left out."`
}

type searchOutput struct{ Body SearchResults }

// searchLimits are how many of each kind a search returns: with no group, and for the group asked for.
const (
	searchEach = 20
	searchOne  = 100
)

// detailOf is where text was found in a document: its memo, reference, notes or title, a line's description or
// merchant, or its party; the memo or reference when the text is not in any.
func detailOf(payload []byte, party, text string) string {
	var p struct {
		Memo      string `json:"memo"`
		Reference string `json:"reference"`
		Supplier  string `json:"supplier_ref"`
		Notes     string `json:"notes"`
		Title     string `json:"title"`
		Lines     []struct {
			Description string `json:"description"`
			Merchant    string `json:"merchant"`
		} `json:"lines"`
	}
	_ = json.Unmarshal(payload, &p) // a document's own payload: unreadable, it has no detail
	found := func(s string) bool { return text != "" && strings.Contains(strings.ToLower(s), strings.ToLower(text)) }
	for _, s := range []string{p.Reference, p.Supplier, p.Memo, p.Title, p.Notes} {
		if found(s) {
			return s
		}
	}
	for _, l := range p.Lines {
		for _, s := range []string{l.Description, l.Merchant} {
			if found(s) {
				return s
			}
		}
	}
	if found(party) {
		return ""
	}
	return strings.Join(strings.Fields(p.Reference+" "+p.Supplier+" "+p.Memo), " ")
}

func registerSearch(a *API) {
	register(a, authz.SearchRecords, huma.Operation{
		OperationID: "searchRecords", Method: http.MethodGet, Path: "/v1/search", Tags: []string{"Search"},
		Summary: "Search the records",
		Description: "Finds documents, contacts, accounts and items (FR-UX-02; PRD-001 rev 0.190). Text is looked for, case aside, in a " +
			"document's number, reference, memo, notes, title, lines and party, and in a contact's name, UEN or tax ID and emails, an " +
			"account's or item's code and name. An amount, exact (1,234.56) or above or below a bound (> 5k), is looked for, without its " +
			"sign, in a document's live posted lines, in each line's currency; a bare whole number is looked for as both. Each kind of " +
			"record is searched only by whoever views its Appendix D row without a qualifier, and an Employee finds their own claims, so " +
			"results, counts and details never show a record the caller may not see (SEC-34). Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *searchInput) (*searchOutput, error) {
		p, _ := authn.From(ctx)
		q := search.Parse(in.Q)
		out := &searchOutput{Body: SearchResults{Query: in.Q, Results: []SearchResult{}, Groups: []SearchGroupCount{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, g := range searchGroups {
				var claimant *uuid.UUID
				switch {
				case views(p, g.Row):
				case g.Row == authz.ExpenseClaims && p.UserID != uuid.Nil &&
					authz.Decide(authz.Policy{Operation: "view", Capability: g.Row, Need: authz.View}, p.Roles, p.AI()).Allowed:
					claimant = &p.UserID // their own claims only
				default:
					continue
				}
				if g.Types == nil && q.HasAmount() && !q.Either {
					out.Body.Groups = append(out.Body.Groups, SearchGroupCount{Key: SearchGroupKey(g.Key), Label: g.Label})
					continue // contacts, accounts and items have no amount
				}
				limit := searchEach
				if in.Group != "" {
					limit = searchOne
				}
				found, err := a.searchGroup(ctx, u, g, q, claimant, p.UserID, limit+1)
				if err != nil {
					return err
				}
				count := SearchGroupCount{Key: SearchGroupKey(g.Key), Label: g.Label, Count: min(len(found), limit), More: len(found) > limit}
				out.Body.Groups = append(out.Body.Groups, count)
				if in.Group == "" || string(in.Group) == g.Key {
					out.Body.Results = append(out.Body.Results, found[:count.Count]...)
				}
			}
			return nil
		})
		return out, err
	})
}

// searchGroup finds up to limit records of g for q in u; claimant, when set, limits claims to theirs.
func (a *API) searchGroup(ctx context.Context, u storage.UnitOfWork, g searchGroup, q search.Query, claimant *uuid.UUID, me uuid.UUID,
	limit int) ([]SearchResult, error) {
	key := SearchGroupKey(g.Key)
	var out []SearchResult
	switch g.Key {
	case "contacts":
		cs, err := u.Contacts().Search(ctx, q.Text, limit)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			detail := c.UENOrTaxID
			for _, e := range []string{c.BillingEmail, c.RemittanceEmail} {
				if e != "" && strings.Contains(strings.ToLower(e), strings.ToLower(q.Text)) {
					detail = e
				}
			}
			r := SearchResult{Group: key, Type: "contact", ID: c.ID.String(), Title: c.LegalName, Detail: detail, Link: "/contacts/" + c.ID.String()}
			if !c.Active {
				r.Status = "inactive"
			}
			out = append(out, r)
		}
		return out, nil
	case "accounts":
		all, err := u.Accounts().List(ctx)
		if err != nil {
			return nil, err
		}
		for _, acc := range all {
			if len(out) < limit && strings.Contains(strings.ToLower(acc.Code+" "+acc.Name), strings.ToLower(q.Text)) {
				r := SearchResult{Group: key, Type: "account", ID: acc.ID.String(), Title: acc.Code + " " + acc.Name, Detail: string(acc.Type),
					Link: "/accounting/chart-of-accounts"}
				if !acc.Active {
					r.Status = "inactive"
				}
				out = append(out, r)
			}
		}
		return out, nil
	case "items":
		all, err := u.Items().Page(ctx, "", 10000)
		if err != nil {
			return nil, err
		}
		for _, it := range all {
			if len(out) < limit && strings.Contains(strings.ToLower(it.Code+" "+it.Name), strings.ToLower(q.Text)) {
				r := SearchResult{Group: key, Type: "item", ID: it.ID.String(), Title: it.Code + " " + it.Name, Detail: it.Description, Link: "/items"}
				if !it.Active {
					r.Status = "inactive"
				}
				out = append(out, r)
			}
		}
		return out, nil
	}
	f := storage.DocumentSearch{Types: g.Types, Text: q.Text, Either: q.Either, Min: q.Min, Max: q.Max, MinInclusive: q.MinInclusive,
		MaxInclusive: q.MaxInclusive, Limit: limit, Claimant: claimant}
	hits, err := u.Documents().Search(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		title := h.Number
		if title == "" {
			title = "Draft " + strings.ReplaceAll(string(h.Type), "_", " ")
		}
		if h.Party != "" {
			title += " · " + h.Party
		}
		var whose struct {
			Claimant string `json:"claimant_id"`
		}
		_ = json.Unmarshal(h.Payload, &whose) // a claim names its claimant; other documents none
		r := SearchResult{Group: key, Type: string(h.Type), ID: h.ID.String(), Title: title, Detail: detailOf(h.Payload, h.Party, q.Text),
			Date: h.Date, Status: string(h.Status), Link: linkOf(h.Type, h.ID.String(), whose.Claimant == me.String())}
		if cur, err := money.Lookup(h.Currency); err == nil && h.Amount != "" {
			if m, err := money.ParseStored(h.Amount, cur); err == nil {
				v := moneyOut(m)
				r.Amount = &v
			}
		}
		out = append(out, r)
	}
	return out, nil
}
