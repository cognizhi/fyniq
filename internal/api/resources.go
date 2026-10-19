package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ value formats (FR-INT-10)

// Money is an amount as a decimal string with its currency, never a JSON number (ENG-12).
type Money struct {
	Amount   string `json:"amount" pattern:"^-?(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"An exact decimal: debits positive, credits negative; at most the currency's decimals." example:"1250.00"`
	Currency string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The ISO 4217 currency code." example:"SGD"`
}

// TransformSchema implements huma.SchemaTransformer: the money-string pattern for each currency, by its
// minor units (ENG-44 rev 0.24, FR-INT-13). Decimal places count as written, so "12345.0" is not a JPY
// amount and "100" is a USD one. The schema says so to clients and models; the API checks the same
// rule itself, with a field error naming the currency.
func (Money) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	byUnits := map[int][]string{}
	for code, units := range money.Currencies() {
		byUnits[units] = append(byUnits[units], code)
	}
	for _, units := range slices.Sorted(maps.Keys(byUnits)) {
		codes := byUnits[units]
		slices.Sort(codes)
		pattern := `^-?(0|[1-9][0-9]{0,15})`
		if units > 0 {
			pattern += fmt.Sprintf(`(\.[0-9]{1,%d})?`, units)
		}
		pattern += `$`
		s.AllOf = append(s.AllOf, &huma.Schema{Extensions: map[string]any{
			"if":   map[string]any{"required": []string{"currency"}, "properties": map[string]any{"currency": map[string]any{"enum": codes}}},
			"then": map[string]any{"properties": map[string]any{"amount": map[string]any{"pattern": pattern}}},
		}})
	}
	return s
}

func moneyOf(amount storage.Decimal, currency string) Money {
	m := Money{Amount: string(amount), Currency: currency}
	if cur, err := money.Lookup(currency); err == nil {
		if v, err := money.ParseStored(string(amount), cur); err == nil {
			m.Amount = v.String() // at the currency's minor units: "100.50", not "100.5000"
		}
	}
	return m
}

func dateOf(t time.Time) string { return t.Format(time.DateOnly) }

// trimDecimal drops the zeros a NUMERIC column pads a rate with: "1.3472000000" is "1.3472", and
// "1.0000000000" is "1".
func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

// ------------------------------------------------------------------ organisation

// Organisation is the caller's organisation.
type Organisation struct {
	ID             string `json:"id" format:"uuid" doc:"The organisation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d5e"`
	Name           string `json:"name" doc:"Its name." example:"Northwind Analytics Pte. Ltd."`
	BaseCurrency   string `json:"base_currency" doc:"The currency the books are kept in." example:"SGD"`
	BaseMinorUnits int    `json:"base_minor_units" doc:"How many decimals the base currency has." example:"2"`
	Version        int    `json:"version" doc:"The record's version; it also makes the ETag." example:"1"`
}

type organisationOutput struct {
	ETag string `header:"ETag" doc:"The organisation's version, for If-Match."`
	Body Organisation
}

func registerOrganisation(a *API) {
	register(a, authz.GetOrganisation, huma.Operation{
		OperationID: "getOrganisation", Method: http.MethodGet, Path: "/v1/organisation", Tags: []string{"Organisation"},
		Summary: "Get your organisation",
		Description: "Returns the organisation your session is in: its name and base currency, which every screen shows. " +
			"Read-only; no side effects. Every member of the organisation may call it.",
	}, nil, func(ctx context.Context, _ *struct{}) (*organisationOutput, error) {
		p, _ := authn.From(ctx)
		var out organisationOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			out.Body = Organisation{ID: o.ID.String(), Name: o.Name, BaseCurrency: o.BaseCurrency, BaseMinorUnits: o.BaseMinorUnits, Version: o.Version}
			out.ETag = etag(o.Version)
			return err
		})
		return &out, err
	})
}

// ------------------------------------------------------------------ accounts

// Account is an account of the chart of accounts.
type Account struct {
	ID       string  `json:"id" format:"uuid" doc:"The account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code     string  `json:"code" doc:"The account code." example:"6100"`
	Name     string  `json:"name" doc:"The account name." example:"Rent"`
	Type     string  `json:"type" enum:"asset,liability,equity,revenue,expense" doc:"The account type." example:"expense"`
	ParentID *string `json:"parent_id" format:"uuid" required:"true" nullable:"true" doc:"The group account it sits under, or null." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Group    bool    `json:"group" doc:"A group account is a heading: postings go to the accounts under it." example:"false"`
	Active   bool    `json:"active" doc:"An inactive account takes no new postings." example:"true"`
	Version  int     `json:"version" doc:"The record's version; it also makes the ETag." example:"1"`
}

func accountOf(a storage.Account) Account {
	out := Account{ID: a.ID.String(), Code: a.Code, Name: a.Name, Type: string(a.Type), Group: a.Group, Active: a.Active, Version: a.Version}
	if a.ParentID != nil {
		s := a.ParentID.String()
		out.ParentID = &s
	}
	return out
}

// AccountPage is one page of accounts, by code.
type AccountPage struct {
	Items      []Account `json:"items" doc:"The accounts on this page, by code."`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjYxMDAifQ"`
}

type accountsInput struct {
	PageQuery
}

type accountsOutput struct {
	Body AccountPage
}

type accountInput struct {
	AccountID string `path:"account_id" format:"uuid" doc:"The account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
}

type accountOutput struct {
	ETag string `header:"ETag" doc:"The account's version, for If-Match."`
	Body Account
}

func registerAccounts(a *API) {
	register(a, authz.ListAccounts, huma.Operation{
		OperationID: "listAccounts", Method: http.MethodGet, Path: "/v1/accounts", Tags: []string{"Chart of accounts"},
		Summary:     "List the chart of accounts",
		Description: "Returns the organisation's accounts by code, a page at a time (cursor pagination). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *accountsInput) (*accountsOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := accountsOutput{Body: AccountPage{Items: []Account{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.Accounts().Page(ctx, after, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].Code)
			}
			for _, acc := range page {
				out.Body.Items = append(out.Body.Items, accountOf(acc))
			}
			return nil
		})
		return &out, err
	})
	register(a, authz.GetAccount, huma.Operation{
		OperationID: "getAccount", Method: http.MethodGet, Path: "/v1/accounts/{account_id}", Tags: []string{"Chart of accounts"},
		Summary:     "Get an account",
		Description: "Returns one account with its ETag. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *accountInput) (*accountOutput, error) {
		id, err := parseID("account_id", in.AccountID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out accountOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			acc, err := u.Accounts().Get(ctx, id)
			out.Body, out.ETag = accountOf(acc), etag(acc.Version)
			return err
		})
		return &out, err
	})
}

// ------------------------------------------------------------------ manual journals

// JournalLineDraft is a line of a journal as entered.
type JournalLineDraft struct {
	AccountID    string `json:"account_id" format:"uuid" doc:"The account to post to: a leaf account, active." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Amount       Money  `json:"amount" doc:"Debit positive, credit negative, in the line's currency."`
	FXRate       string `json:"fx_rate,omitempty" pattern:"^(0|[1-9][0-9]{0,9})(\\.[0-9]{1,10})?$" doc:"Only for a line not in the base currency: base currency per unit of the line's currency. A base-currency line takes none; posting refuses one (ENG-03)." example:"1.3472"`
	FXRateSource string `json:"fx_rate_source,omitempty" maxLength:"100" doc:"Where the rate came from. A rate you send is the document's, so its source is document, set for you when you leave it out; any other source is refused (ENG-03, ENG-40)." example:"document"`
	Description  string `json:"description,omitempty" maxLength:"500" doc:"What the line is for." example:"October office rent"`
}

// RateSourceDocument is the source of a rate typed on the document (ENG-40).
const RateSourceDocument = "document"

// JournalDraft is a manual journal as entered. A draft is checked only for its amounts; posting
// checks the rest (it must balance, its accounts must take postings, its date must be in an open
// period).
type JournalDraft struct {
	Date  string             `json:"date,omitempty" format:"date" doc:"The date the journal belongs to." example:"2026-10-31"`
	Memo  string             `json:"memo,omitempty" maxLength:"1000" doc:"What the journal is for." example:"October rent accrual"`
	Lines []JournalLineDraft `json:"lines" maxItems:"1000" doc:"Debits positive, credits negative."`
}

// Journal is a manual journal: its current version and status.
type Journal struct {
	ID      string             `json:"id" format:"uuid" doc:"The journal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Number  string             `json:"number,omitempty" doc:"Its number from the journal sequence: given when it posts, or when it is saved if the sequence is not gap-free; absent before (FR-ORG-04)." example:"JNL-0042"`
	Status  string             `json:"status" enum:"draft,posted,reversed" doc:"Draft (✎), posted, or reversed by a reversing entry." example:"draft"`
	Version int                `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Date    string             `json:"date,omitempty" format:"date" doc:"The date it belongs to." example:"2026-10-31"`
	Memo    string             `json:"memo,omitempty" doc:"What it is for." example:"October rent accrual"`
	Lines   []JournalLineDraft `json:"lines" doc:"The lines as entered."`
}

// EntryLine is a posted line.
type EntryLine struct {
	LineNo       int    `json:"line_no" doc:"The line's number in the entry." example:"1"`
	AccountID    string `json:"account_id" format:"uuid" doc:"The account posted to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Amount       Money  `json:"amount" doc:"In the line's currency: debit positive, credit negative."`
	FXRate       string `json:"fx_rate" doc:"Base currency per unit of the line's currency (1 for the base currency)." example:"1"`
	FXRateSource string `json:"fx_rate_source" doc:"Where the rate came from." example:"base"`
	BaseAmount   Money  `json:"base_amount" doc:"In the base currency, computed once at posting (RND-FX)."`
	Description  string `json:"description,omitempty" doc:"What the line is for." example:"October office rent"`
}

// Entry is a posted journal entry: append-only, corrected only by reversal.
type Entry struct {
	ID              string      `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	LedgerRevision  int64       `json:"ledger_revision" doc:"The organisation's ledger revision this entry made (ENG-46)." example:"42"`
	PostingDate     string      `json:"posting_date" format:"date" doc:"The date it posts on." example:"2026-10-31"`
	PeriodID        string      `json:"period_id" format:"uuid" doc:"The accounting period it falls in." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	PostingIdentity string      `json:"posting_identity" doc:"What was posted, under which rule: the same thing never posts twice." example:"manual_journal:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70:v1:journal@1"`
	RuleVersion     string      `json:"rule_version" doc:"The posting rule and its version (ENG-03)." example:"journal@1"`
	ReversesEntryID string      `json:"reverses_entry_id,omitempty" format:"uuid" doc:"For a reversal, the entry it reverses." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d81"`
	Memo            string      `json:"memo,omitempty" doc:"The entry's memo." example:"October rent accrual"`
	RecordedAt      time.Time   `json:"recorded_at" doc:"When it was recorded." example:"2026-10-31T09:30:00Z"`
	Lines           []EntryLine `json:"lines" doc:"Its lines."`
}

// JournalPosting is a journal after a post or a reversal, with the entry it made.
type JournalPosting struct {
	Journal Journal `json:"journal" doc:"The journal after the command."`
	Entry   Entry   `json:"entry" doc:"The entry the command posted: the journal's entry, or its reversing entry."`
}

func journalOf(doc storage.Document) (Journal, error) {
	saved, err := journal.ReadSaved(doc)
	if err != nil {
		return Journal{}, err
	}
	out := Journal{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, Date: saved.Date, Memo: saved.Memo,
		Lines: make([]JournalLineDraft, 0, len(saved.Lines))}
	for _, l := range saved.Lines {
		out.Lines = append(out.Lines, JournalLineDraft{AccountID: l.AccountID.String(),
			Amount: Money{Amount: l.Amount.String(), Currency: l.Amount.Currency().Code},
			FXRate: l.FXRate, FXRateSource: l.FXRateSource, Description: l.Description})
	}
	return out, nil
}

func journalETag(j Journal) string { return etag(j.Version, j.Status) }

func entryOf(e storage.Entry) Entry {
	out := Entry{ID: e.ID.String(), LedgerRevision: e.LedgerRevision, PostingDate: dateOf(e.PostingDate), PeriodID: e.PeriodID.String(),
		PostingIdentity: e.PostingIdentity, RuleVersion: e.RuleVersion, Memo: e.Memo, RecordedAt: e.RecordedAt.UTC(),
		Lines: make([]EntryLine, 0, len(e.Lines))}
	if e.ReversesEntryID != nil {
		out.ReversesEntryID = e.ReversesEntryID.String()
	}
	for _, l := range e.Lines {
		out.Lines = append(out.Lines, EntryLine{LineNo: l.LineNo, AccountID: l.AccountID.String(), Amount: moneyOf(l.Amount, l.Currency),
			FXRate: trimDecimal(string(l.FXRate)), FXRateSource: l.FXRateSource, BaseAmount: moneyOf(l.BaseAmount, l.BaseCurrency), Description: l.Description})
	}
	return out
}

// draftOf reads a draft from the request, with every invalid field at once. Rates are base currency
// per unit of the line's currency.
func draftOf(in JournalDraft, base money.Currency) (journal.Draft, error) {
	var d journal.Draft
	var fields []FieldProblem
	if in.Date != "" {
		t, err := time.Parse(time.DateOnly, in.Date)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		d.Date = t
	}
	d.Memo = in.Memo
	for i, l := range in.Lines {
		at := func(f string) string { return "body.lines[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(l.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("account_id"), Message: "is not an ID", Fix: "Send the account's ID"})
		}
		line := ledger.JournalLine{AccountID: id, Description: l.Description}
		cur, err := money.Lookup(l.Amount.Currency)
		if err != nil {
			fields = append(fields, FieldProblem{Code: money.CodeCurrency, Field: at("amount.currency"), Message: "is not a currency Fyniq knows",
				Fix: "Send an ISO 4217 code such as SGD"})
			continue
		}
		if line.Amount, err = money.ParseField(l.Amount.Amount, cur, at("amount.amount")); err != nil {
			fields = append(fields, fieldOf(err, at("amount.amount")))
			continue
		}
		if l.FXRate != "" || l.FXRateSource != "" {
			// A rate the client sends is typed on the document (ENG-03, ENG-40): its source is "document".
			source := cmp.Or(l.FXRateSource, RateSourceDocument)
			if source != RateSourceDocument {
				fields = append(fields, FieldProblem{Code: money.CodeRate, Field: at("fx_rate_source"),
					Message: fmt.Sprintf("a rate you send is the document's, so its source is %s, not %q", RateSourceDocument, source),
					Fix:     "Leave fx_rate_source out, or send document"})
				continue
			}
			rate, err := money.ParseRate(l.FXRate, cur, base, source)
			if err != nil {
				fields = append(fields, fieldOf(err, at("fx_rate")))
				continue
			}
			line.Rate = &rate
		}
		d.Lines = append(d.Lines, line)
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

func invalid(fields []FieldProblem) *Problem {
	return problem(CodeInvalid, strconv.Itoa(len(fields))+" field(s) are not valid; each is listed in errors.",
		"Correct every field listed in errors and send the request again.", fields...)
}

// fieldOf turns a money error into a field problem at path (fx_rate_source keeps its own name).
func fieldOf(err error, path string) FieldProblem {
	var me *money.Error
	if errors.As(err, &me) {
		if me.Field == "fx_rate_source" {
			path = strings.TrimSuffix(path, "fx_rate") + "fx_rate_source"
		}
		return FieldProblem{Code: me.Code, Field: path, Message: me.Message, Fix: cmp.Or(me.Fix, "Correct the value")}
	}
	return FieldProblem{Code: CodeInvalid, Field: path, Message: err.Error(), Fix: "Correct the value"}
}

// parseID reads an ID from the path (huma has checked its format already).
func parseID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return id, invalid([]FieldProblem{{Code: CodeInvalid, Field: "path." + field, Message: "is not an ID", Fix: "Send the record's ID"}})
	}
	return id, nil
}

// JournalPath names a journal in the path.
type JournalPath struct {
	JournalID string `path:"journal_id" format:"uuid" doc:"The journal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
}

type createJournalInput struct {
	KeyHeader
	PreferHeader
	Body JournalDraft
}

type journalOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The journal's version and status, for If-Match."`
	Location string `header:"Location" doc:"The journal's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Journal
}

// JournalDrafts are the drafts a bulk request saves.
type JournalDrafts struct {
	Items []JournalDraft `json:"items" minItems:"1" maxItems:"250" doc:"Up to 250 drafts, each saved on its own (FR-INT-11)."`
}

type bulkCreateJournalsInput struct {
	KeyHeader
	PreferHeader
	Body JournalDrafts
}

type bulkOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: every item was checked and none was saved (FR-INT-14)."`
	Body     BulkResults
}

type journalReadOutput struct {
	ETag string `header:"ETag" doc:"The journal's version and status, for If-Match."`
	Body Journal
}

type postJournalInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

// Reversal is how a posted journal is reversed.
type Reversal struct {
	Date   string `json:"date" format:"date" doc:"The date of the reversing entry; it must be in an open period." example:"2026-11-01"`
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is reversed; the audit trail records it (FR-GL-12)." example:"Posted to the wrong account"`
}

type reverseJournalInput struct {
	JournalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type postingOutput struct {
	ETag     string `header:"ETag" doc:"The journal's new version and status, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what posting would write (FR-INT-14)."`
	Body     JournalPosting
}

func registerJournals(a *API) {
	register(a, authz.CreateJournal, huma.Operation{
		OperationID: "createJournal", Method: http.MethodPost, Path: "/v1/journals", Tags: []string{"Journals"}, DefaultStatus: http.StatusCreated,
		Summary: "Save a draft journal",
		Description: "Saves a manual journal as a draft (✎) at version 1. Side effects: a draft document and its audit event; nothing " +
			"posts until postJournal. A draft is checked only for its amounts and currencies. Idempotent with Idempotency-Key.",
	}, []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *createJournalInput) (*journalOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Journal, error) {
				org, err := u.Organisation(ctx)
				if err != nil {
					return Journal{}, err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return Journal{}, err
				}
				d, err := draftOf(in.Body, base)
				if err != nil {
					return Journal{}, err
				}
				doc, err := a.journals.Create(ctx, p.Organisation, d)
				if err != nil {
					return Journal{}, err
				}
				return journalOf(doc)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed { // a preview reserves nothing: no version to match, no URL to follow
				return &journalOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &journalOutput{Status: w.Status, ETag: journalETag(w.Body), Location: "/v1/journals/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.BulkCreateJournals, huma.Operation{
		OperationID: "bulkCreateJournals", Method: http.MethodPost, Path: "/v1/journals:bulk", Tags: []string{"Journals"},
		Summary: "Save up to 250 draft journals",
		Description: "Saves each draft on its own, as createJournal does, and answers with one result per item in the order sent " +
			"(FR-INT-11): 201 with the draft's ID and ETag, or the item's problem details. An item that is refused takes no other " +
			"item with it, and is never left out. A request whose items break the schema is refused whole, with every invalid field " +
			"listed. Side effects: a draft document and its audit event per saved item. Idempotent with Idempotency-Key, which covers " +
			"the whole request; with Prefer: return=preview every item is checked and none is saved.",
		MaxBodyBytes: 8 << 20,
	}, []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, money.CodeRate, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *bulkCreateJournalsInput) (*bulkOutput, error) {
			p, _ := authn.From(ctx)
			w, err := bulk(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, len(in.Body.Items),
				func(ctx context.Context, u storage.UnitOfWork, i int) (BulkResult, error) {
					org, err := u.Organisation(ctx)
					if err != nil {
						return BulkResult{}, err
					}
					base, err := money.Lookup(org.BaseCurrency)
					if err != nil {
						return BulkResult{}, err
					}
					d, err := draftOf(in.Body.Items[i], base)
					if err != nil {
						return BulkResult{}, err
					}
					doc, err := a.journals.Create(ctx, p.Organisation, d)
					if err != nil {
						return BulkResult{}, err
					}
					j, err := journalOf(doc)
					return BulkResult{Status: http.StatusCreated, ID: j.ID, ETag: journalETag(j)}, err
				})
			if err != nil {
				return nil, err
			}
			return &bulkOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.GetJournal, huma.Operation{
		OperationID: "getJournal", Method: http.MethodGet, Path: "/v1/journals/{journal_id}", Tags: []string{"Journals"},
		Summary:     "Get a journal",
		Description: "Returns a manual journal's current version and status, with its ETag. Read-only; no side effects.",
	}, []string{CodeNotFound, journal.CodeNotJournal, journal.CodeBadDraft}, func(ctx context.Context, in *JournalPath) (*journalReadOutput, error) {
		id, err := parseID("journal_id", in.JournalID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var j Journal
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			j, err = journalOf(doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &journalReadOutput{ETag: journalETag(j), Body: j}, nil
	})

	postingCodes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		journal.CodeNotJournal, journal.CodeBadDraft, storage.CodeUnbalanced, storage.CodeImmutable, storage.CodePeriodClosed,
		storage.CodeAccountNotPost, storage.CodeAmount, storage.CodeDocumentStatus, storage.CodeDuplicate, storage.CodeRetry, storage.CodeLockTimeout,
		storage.CodeShareCapital}
	postingCodes = append(postingCodes, LedgerCodes...)

	register(a, authz.PostJournal, huma.Operation{
		OperationID: "postJournal", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/post", Tags: []string{"Journals"},
		Summary: "Post a draft journal",
		Description: "Posts a draft: the posting engine turns its current version into a balanced entry, written with the journal's " +
			"change to posted in one transaction (ENG-02). Side effects: a journal entry, a new ledger revision, balances, audit events. " +
			"Needs If-Match with the draft's ETag. Approval policy: the Accountant's amount limit and SOD-3 apply from P1. " +
			"Idempotent with Idempotency-Key; the same version never posts twice.",
	}, append([]string{journal.CodeNotDraft}, postingCodes...), func(ctx context.Context, in *postJournalInput) (*postingOutput, error) {
		return a.posting(ctx, in.JournalID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			return a.journals.Post(ctx, p.Organisation, id)
		})
	})

	register(a, authz.ReverseJournal, huma.Operation{
		OperationID: "reverseJournal", Method: http.MethodPost, Path: "/v1/journals/{journal_id}/reverse", Tags: []string{"Journals"},
		Summary: "Reverse a posted journal",
		Description: "Voids a posted journal with a reversing entry on the date given (FR-GL-12); posted history never changes. " +
			"Side effects: the reversing entry, a new ledger revision, balances, audit events with the reason. Needs If-Match with the " +
			"journal's ETag. Approval policy: in Founder mode voiding needs a second person (FM-1) from P1. Idempotent with Idempotency-Key.",
	}, append([]string{journal.CodeNotPosted, CodeMalformed, CodeTooLarge, CodeMediaType}, postingCodes...), func(ctx context.Context, in *reverseJournalInput) (*postingOutput, error) {
		date, err := time.Parse(time.DateOnly, in.Body.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		return a.posting(ctx, in.JournalID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Entry, error) {
			return a.journals.Reverse(ctx, p.Organisation, id, date, in.Body.Reason)
		})
	})
}

// posting runs a command that posts for a journal: If-Match is checked against the journal locked in
// the same unit of work, so nothing can change it between the check and the command.
func (a *API) posting(ctx context.Context, rawID, key, ifMatch, prefer string, request any,
	command func(ctx context.Context, p authn.Principal, id uuid.UUID) (storage.Entry, error)) (*postingOutput, error) {
	id, err := parseID("journal_id", rawID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ifMatch) == "" {
		return nil, checkIfMatch(ifMatch, "")
	}
	p, _ := authn.From(ctx)
	w, err := write(ctx, a, key, prefer, request, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (JournalPosting, error) {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return JournalPosting{}, err
		}
		before, err := journalOf(doc)
		if err != nil {
			return JournalPosting{}, err
		}
		if err := checkIfMatch(ifMatch, journalETag(before)); err != nil {
			return JournalPosting{}, err
		}
		entry, err := command(ctx, p, id)
		if err != nil {
			return JournalPosting{}, err
		}
		after, err := u.Documents().Get(ctx, id)
		if err != nil {
			return JournalPosting{}, err
		}
		j, err := journalOf(after)
		return JournalPosting{Journal: j, Entry: entryOf(entry)}, err
	})
	if err != nil {
		return nil, err
	}
	if w.Previewed {
		return &postingOutput{Applied: applied(w), Body: w.Body}, nil
	}
	return &postingOutput{ETag: journalETag(w.Body.Journal), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
}
