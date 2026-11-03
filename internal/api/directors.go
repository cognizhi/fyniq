package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// The director accounts (PRD-001 §8.22, FR-EQ-01, FR-EQ-14; p1-founders-money design D1, D4 and D5): what the company
// owes each director or shareholder on 2500 Loan from director / shareholder and 2120 Deferred remuneration payable,
// and what they owe it, from the balance service; each person's statement by origin; and the member who signs in as
// them, linked under dual control (FM-1).

// The director accounts' own refusals: a principal that is not a person signed in as themselves asking for their own
// account (FR-EQ-14), and the member linked or unlinked approving the link (FM-1).
const (
	CodeNotLinked = "FOUNDER-03"
	CodeOwnLink   = "FOUNDER-04"
)

// DirectorBalance is a person's balance on one director's account.
type DirectorBalance struct {
	AccountID string `json:"account_id" format:"uuid" doc:"The director's account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Code      string `json:"code" doc:"Its code." example:"2500"`
	Name      string `json:"name" doc:"Its name." example:"Loan from director / shareholder"`
	Role      string `json:"role" enum:"director-loan,deferred-remuneration" doc:"director-loan for 2500, money lent and costs paid personally; deferred-remuneration for 2120, pay and fees not yet paid (B.7)." example:"director-loan"`
	Balance   Money  `json:"balance" doc:"In the base currency, as the ledger holds it: negative (a credit) is what the company owes the person, positive what they owe it."`
}

// DirectorAccount is a director or shareholder with their balances (FR-EQ-01).
type DirectorAccount struct {
	ContactID    string            `json:"contact_id" format:"uuid" doc:"The person's contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d65"`
	LegalName    string            `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	Director     bool              `json:"director" doc:"A director." example:"true"`
	Shareholder  bool              `json:"shareholder" doc:"A shareholder." example:"true"`
	MemberUserID string            `json:"member_user_id,omitempty" format:"uuid" doc:"The member who signs in as them, when linked (linkContactMember)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Active       bool              `json:"active" doc:"The contact is active." example:"true"`
	Balances     []DirectorBalance `json:"balances" doc:"One per director's account of the chart, in code order."`
	Net          Money             `json:"net" doc:"Their balances together, as the ledger holds them: negative is owed to the person; positive means the person owes the company, which the balance sheet shows under 1250 Staff & director advances (FR-EQ-11)."`
	InDebit      bool              `json:"in_debit" doc:"The net is a debit: the person owes the company (FR-EQ-11)." example:"false"`
}

// DirectorAccountList is every director's and shareholder's account as at a day.
type DirectorAccountList struct {
	AsAt     string            `json:"as_at" format:"date" doc:"The day the balances are at, inclusive." example:"2026-10-31"`
	Currency string            `json:"currency" doc:"The base currency the balances are in." example:"SGD"`
	Items    []DirectorAccount `json:"items" doc:"The contacts marked director or shareholder on this page, in the order they were added."`
	Unkeyed  Money             `json:"unkeyed" doc:"What the director's accounts hold that names no person: zero, but for lines posted before the accounts named their person, which the period close names (INV-07)."`
	// NextCursor is the next page's cursor (FR-INT-10).
	NextCursor string `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// OriginTotal is what one origin moved on a director's account in a statement's period.
type OriginTotal struct {
	Origin string `json:"origin" enum:"lent,paid_personally,pay,fee,paid_out,converted,waived,other" doc:"Where the amounts came from (FR-EQ-01)." example:"lent"`
	Amount Money  `json:"amount" doc:"Their sum, as the ledger holds it: negative is owed to the person."`
}

// DirectorStatementAccount is one director's account in a person's statement.
type DirectorStatementAccount struct {
	AccountID string        `json:"account_id" format:"uuid" doc:"The director's account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Code      string        `json:"code" doc:"Its code." example:"2500"`
	Name      string        `json:"name" doc:"Its name." example:"Loan from director / shareholder"`
	Opening   Money         `json:"opening" doc:"The balance before the statement's first day."`
	ByOrigin  []OriginTotal `json:"by_origin" doc:"The movements by origin, in the order origins first moved."`
	Closing   Money         `json:"closing" doc:"The balance at the statement's last day: the opening and the movements."`
}

// DirectorMovement is a line of a person's statement.
type DirectorMovement struct {
	EntryID     string `json:"entry_id" format:"uuid" doc:"The journal entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	Date        string `json:"date" format:"date" doc:"Its posting date." example:"2026-10-15"`
	AccountID   string `json:"account_id" format:"uuid" doc:"The director's account it moved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Origin      string `json:"origin" enum:"lent,paid_personally,pay,fee,paid_out,converted,waived,other" doc:"Where the amount came from (FR-EQ-01): from its source, or as entered on a journal or opening balance line." example:"lent"`
	Amount      Money  `json:"amount" doc:"In the base currency, as the ledger holds it: negative is owed to the person."`
	SourceType  string `json:"source_type" doc:"The document it came from." example:"bank_transaction"`
	SourceID    string `json:"source_id" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Number      string `json:"number,omitempty" doc:"Its number, when it has one." example:"JNL-0042"`
	Memo        string `json:"memo,omitempty" doc:"The entry's memo." example:"Loan from Mei Lin"`
	Description string `json:"description,omitempty" doc:"The line's description." example:"Cash lent for October payroll"`
	Reversal    bool   `json:"reversal" doc:"The line reverses an earlier one." example:"false"`
}

// DirectorStatement is a person's statement from one day to another (FR-EQ-01).
type DirectorStatement struct {
	ContactID string                     `json:"contact_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d65"`
	LegalName string                     `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	From      string                     `json:"from" format:"date" doc:"Its first day." example:"2026-01-01"`
	To        string                     `json:"to" format:"date" doc:"Its last day." example:"2026-10-31"`
	Currency  string                     `json:"currency" doc:"The base currency." example:"SGD"`
	Accounts  []DirectorStatementAccount `json:"accounts" doc:"Each director's account: opening, movements by origin, closing."`
	Movements []DirectorMovement         `json:"movements" doc:"Every line in the period, in posting order."`
}

// MemberLink is the member a director or shareholder is to sign in as, or none.
type MemberLink struct {
	MemberUserID string `json:"member_user_id,omitempty" format:"uuid" doc:"The member; left out or empty unlinks them." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type directorListInput struct {
	PageQuery
	AsAt string `query:"as_at" format:"date" doc:"The day the balances are at; the organisation's today when left out." example:"2026-10-31"`
}

type myDirectorInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day the balances are at; the organisation's today when left out." example:"2026-10-31"`
}

type directorStatementInput struct {
	ContactPath
	From string `query:"from" required:"true" format:"date" doc:"The statement's first day." example:"2026-01-01"`
	To   string `query:"to" required:"true" format:"date" doc:"Its last day." example:"2026-10-31"`
}

type directorExportInput struct {
	ContactPath
	From   string `query:"from" required:"true" format:"date" doc:"The statement's first day." example:"2026-01-01"`
	To     string `query:"to" required:"true" format:"date" doc:"Its last day." example:"2026-10-31"`
	Format string `query:"format" enum:"pdf,csv" default:"pdf" doc:"pdf: the statement to print or send for a balance confirmation, from the render service; csv: its movements, plain decimals with an ASCII minus." example:"pdf"`
}

type memberLinkInput struct {
	ContactPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body MemberLink
}

// MyDirectorAccount is the director or shareholder the caller signs in as, if any (FR-EQ-14).
type MyDirectorAccount struct {
	Linked  bool             `json:"linked" doc:"A director or shareholder is linked to you (linkContactMember); false: Money I put in has nothing to show." example:"true"`
	Account *DirectorAccount `json:"account,omitempty" doc:"Their account, when linked."`
}

type directorListOutput struct{ Body DirectorAccountList }
type myDirectorOutput struct{ Body MyDirectorAccount }
type directorStatementOutput struct{ Body DirectorStatement }

// DirectorCodes are the refusals of the director accounts' commands.
var DirectorCodes = []string{director.CodeNotPerson, director.CodeDates, CodeNotLinked}


func directorAccountOf(p director.Person, accounts []storage.Account, base money.Currency) DirectorAccount {
	out := DirectorAccount{ContactID: p.Contact.ID.String(), LegalName: p.Contact.LegalName, Director: p.Contact.Director,
		Shareholder: p.Contact.Shareholder, MemberUserID: idOrEmpty(p.Contact.MemberUserID), Active: p.Contact.Active,
		Balances: make([]DirectorBalance, 0, len(accounts)), Net: moneyAmount(p.Net), InDebit: p.Net.Sign() > 0}
	for _, a := range accounts {
		b, ok := p.Balances[a.ID]
		if !ok {
			b = money.Zero(base)
		}
		out.Balances = append(out.Balances, DirectorBalance{AccountID: a.ID.String(), Code: a.Code, Name: a.Name, Role: string(a.Role),
			Balance: moneyAmount(b)})
	}
	return out
}

// moneyAmount is an amount as the API shows it.
func moneyAmount(m money.Money) Money { return Money{Amount: m.String(), Currency: m.Currency().Code} }

func directorStatementOf(st director.Statement, base money.Currency) DirectorStatement {
	out := DirectorStatement{ContactID: st.Person.ID.String(), LegalName: st.Person.LegalName, From: dateOf(st.From), To: dateOf(st.To),
		Currency: base.Code, Accounts: make([]DirectorStatementAccount, 0, len(st.Accounts)), Movements: make([]DirectorMovement, 0, len(st.Movements))}
	for _, a := range st.Accounts {
		x := DirectorStatementAccount{AccountID: a.ID.String(), Code: a.Code, Name: a.Name, Opening: moneyAmount(st.Opening[a.ID]),
			Closing: moneyAmount(st.Closing[a.ID]), ByOrigin: []OriginTotal{}}
		totals := map[string]money.Money{}
		var order []string
		for _, m := range st.Movements {
			if m.AccountID != a.ID {
				continue
			}
			t, ok := totals[m.Origin]
			if !ok {
				t = money.Zero(base)
				order = append(order, m.Origin)
			}
			totals[m.Origin], _ = t.Add(m.Amount)
		}
		for _, o := range order {
			x.ByOrigin = append(x.ByOrigin, OriginTotal{Origin: o, Amount: moneyAmount(totals[o])})
		}
		out.Accounts = append(out.Accounts, x)
	}
	for _, m := range st.Movements {
		out.Movements = append(out.Movements, DirectorMovement{EntryID: m.EntryID.String(), Date: dateOf(m.Date), AccountID: m.AccountID.String(),
			Origin: m.Origin, Amount: moneyAmount(m.Amount), SourceType: m.Source.Type, SourceID: m.Source.ID, Number: m.Number, Memo: m.Memo,
			Description: m.Description, Reversal: m.Reversal})
	}
	return out
}

// statementDays reads a statement's first and last days.
func statementDays(from, to string) (time.Time, time.Time, error) {
	var bad []FieldProblem
	f, err := time.Parse(time.DateOnly, from)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "from", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	t, err := time.Parse(time.DateOnly, to)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "to", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	if len(bad) > 0 {
		return f, t, invalid(bad)
	}
	return f, t, nil
}

// directorProblem turns the director package's refusals into problems.
func directorProblem(err error) error {
	var e *director.Error
	if errors.As(err, &e) {
		return problem(e.Code, sentence(e.Message), e.Fix)
	}
	return err
}

// movementOrigins are the origins as a statement's words say them.
var movementOrigins = map[string]string{ledger.OriginLent: "Lent", ledger.OriginPaidPersonally: "Paid personally", ledger.OriginPay: "Pay deferred",
	ledger.OriginFee: "Director's fee", ledger.OriginPaidOut: "Paid out", ledger.OriginConverted: "Converted to shares",
	ledger.OriginWaived: "Waived", ledger.OriginOther: "Other"}

// directorReport is a person's statement as the render service lays it out: per account, the opening balance, the
// movements by origin and the closing balance, signed as owed to the person (FR-EQ-01: for balance confirmations).
func directorReport(s DirectorStatement, author, version string) render.Report {
	r := render.Report{Title: "Statement of account · " + s.LegalName, Author: author, Label: s.Currency,
		Lines:   []string{"Director's and shareholder's account", longDay(s.From) + " to " + longDay(s.To), "Owed to " + s.LegalName + " is shown positive"},
		Columns: []string{"Amount"}}
	owed := func(m Money) string {
		cur, err := money.Lookup(m.Currency)
		if err != nil {
			return m.Amount
		}
		x, err := money.ParseStored(m.Amount, cur)
		if err != nil {
			return m.Amount
		}
		return printed(moneyAmount(x.Neg()))
	}
	for _, a := range s.Accounts {
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowSection, Code: a.Code, Label: a.Name})
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowLine, Label: "Opening balance", Cells: []string{owed(a.Opening)}})
		for _, o := range a.ByOrigin {
			r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowLine, Label: movementOrigins[o.Origin], Cells: []string{owed(o.Amount)}})
		}
		r.Rows = append(r.Rows, render.ReportRow{Kind: render.RowTotal, Label: "Closing balance", Cells: []string{owed(a.Closing)}})
	}
	r.Footer = "Statement of account · Fyniq " + version
	return r
}

func registerDirectors(a *API) {
	codes := append([]string{CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout}, DirectorCodes...)

	register(a, authz.ListDirectorAccounts, huma.Operation{
		OperationID: "listDirectorAccounts", Method: http.MethodGet, Path: "/v1/director-accounts", Tags: []string{"Founders"},
		Summary: "List the director accounts",
		Description: "Every contact marked director or shareholder with their balances as at a day on the director's accounts, 2500 Loan " +
			"from director / shareholder and 2120 Deferred remuneration payable, from the balance service (FR-EQ-01, ENG-04; PRD-001 rev " +
			"0.167). Every line on those accounts names its person, so the balances add up to the accounts; unkeyed shows anything that " +
			"names no one. A net debit means the person owes the company (FR-EQ-11). A page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor}, codes...), func(ctx context.Context, in *directorListInput) (*directorListOutput, error) {
		p, _ := authn.From(ctx)
		var out directorListOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			asAt, err := asAtOf(ctx, a, u, in.AsAt)
			if err != nil {
				return err
			}
			people, accounts, unkeyed, err := director.Accounts(ctx, u, base, asAt)
			if err != nil {
				return directorProblem(err)
			}
			page, next, err := pageOf(people, func(x director.Person) string { return x.Contact.ID.String() }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body = DirectorAccountList{AsAt: dateOf(asAt), Currency: base.Code, Items: make([]DirectorAccount, 0, len(page)),
				Unkeyed: moneyAmount(unkeyed), NextCursor: next}
			for _, x := range page {
				out.Body.Items = append(out.Body.Items, directorAccountOf(x, accounts, base))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetMyDirectorAccount, huma.Operation{
		OperationID: "getMyDirectorAccount", Method: http.MethodGet, Path: "/v1/me/director-account", Tags: []string{"Founders"},
		Summary: "Get your own director account",
		Description: "The director or shareholder you sign in as, with your balances as at a day: Money I put in (FR-EQ-14, FR-EQ-01); " +
			"linked is false when none is linked to you (linkContactMember). Only a person signed in as themselves has one (FOUNDER-03). " +
			"Read-only; no side effects.",
	}, codes, func(ctx context.Context, in *myDirectorInput) (*myDirectorOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
			return nil, problem(CodeNotLinked, "Your own director account is a person's: only a member signed in as themselves has one.",
				"Sign in as yourself")
		}
		var out myDirectorOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			asAt, err := asAtOf(ctx, a, u, in.AsAt)
			if err != nil {
				return err
			}
			c, err := u.Contacts().OfMember(ctx, p.UserID)
			if errors.Is(err, ports.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			person, accounts, err := director.Get(ctx, u, base, c.ID, asAt)
			if err != nil {
				return directorProblem(err)
			}
			x := directorAccountOf(person, accounts, base)
			out.Body = MyDirectorAccount{Linked: true, Account: &x}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetDirectorStatement, huma.Operation{
		OperationID: "getDirectorStatement", Method: http.MethodGet, Path: "/v1/director-accounts/{contact_id}/statement", Tags: []string{"Founders"},
		Summary: "Work out a director's statement",
		Description: "A director's or shareholder's statement from one day to another (FR-EQ-01): for each director's account the balance " +
			"before the first day, the movements by origin (lent, paid personally, pay, fee, paid out, converted, waived, other) and the " +
			"balance at the last day, with every line in posting order. The origin comes from the line's source, or as entered on a " +
			"journal or opening balance line. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, codes...), func(ctx context.Context, in *directorStatementInput) (*directorStatementOutput, error) {
		p, _ := authn.From(ctx)
		var out directorStatementOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out.Body, err = a.directorStatement(ctx, u, in)
			return err
		})
		return &out, err
	})

	register(a, authz.ExportDirectorStatement, huma.Operation{
		OperationID: "exportDirectorStatement", Method: http.MethodGet, Path: "/v1/director-accounts/{contact_id}/statement/export", Tags: []string{"Founders"},
		Summary: "Export a director's statement",
		Description: "The statement getDirectorStatement gives, as a file (FR-EQ-01): a PDF from the render service to print or send for a " +
			"balance confirmation, owed to the person shown positive; or CSV, its movements with plain decimals and an ASCII minus. " +
			"Returned in base64 with its SHA-256. A PDF the render service cannot make now is REPORT-08. Side effects: an audit event " +
			"of the export with its file and hash (FR-RPT-09). Never cached.",
	}, append([]string{CodeNotFound, CodeReportRender}, codes...), func(ctx context.Context, in *directorExportInput) (*reportFileOutput, error) {
		p, _ := authn.From(ctx)
		var s DirectorStatement
		var revision int64
		author := ""
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if s, err = a.directorStatement(ctx, u, &directorStatementInput{ContactPath: in.ContactPath, From: in.From, To: in.To}); err != nil {
				return err
			}
			if revision, err = u.Ledger().Revision(ctx); err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			author = cmpOr(o.Profile.LegalName, o.Name)
			return err
		})
		if err != nil {
			return nil, err
		}
		// The file is written outside the transaction, so the books are not held while the render service lays a PDF out.
		name := "statement-" + fileSlug(s.LegalName) + "-" + s.From + "-to-" + s.To
		var data []byte
		var media string
		switch in.Format {
		case "csv":
			sh := sheet.Sheet{Name: "Statement", Header: []string{"Date", "Account", "Origin", "Source", "Number", "Description", "Amount " + s.Currency}}
			codes := map[string]string{}
			for _, x := range s.Accounts {
				codes[x.AccountID] = x.Code
			}
			for _, m := range s.Movements {
				sh.Rows = append(sh.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(m.Date), sheet.Text(codes[m.AccountID]), sheet.Text(m.Origin),
					sheet.Text(m.SourceType), sheet.Text(m.Number), sheet.Text(cmpOr(m.Description, m.Memo)), sheet.Number(m.Amount.Amount, 2)}})
			}
			data, media, name = sheet.CSV(sh), mediaCSV, name+".csv"
		default:
			if data, err = a.renderReport(ctx, directorReport(s, author, a.deps.Version), a.deps.Clock.Now()); err != nil {
				return nil, err
			}
			media, name = mediaPDF, name+".pdf"
		}
		sum := sha256.Sum256(data)
		id := uuid.Must(uuid.NewV7())
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "director_statement", "contact_id": s.ContactID,
					"format": in.Format, "from": s.From, "to": s.To, "ledger_revision": revision, "rows": len(s.Movements), "file": name,
					"sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: ReportFile{ExportID: id.String(), FileName: name, MediaType: media, SHA256: hex.EncodeToString(sum[:]),
			Size: len(data), Rows: len(s.Movements), Revision: revision, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})

	register(a, authz.LinkContactMember, pendsApproval(a, huma.Operation{
		OperationID: "linkContactMember", Method: http.MethodPut, Path: "/v1/contacts/{contact_id}/member", Tags: []string{"Founders"},
		Summary: "Link a director or shareholder to the member who signs in as them", DefaultStatus: http.StatusAccepted,
		Description: "Asks to link a director or shareholder to a member of the organisation, or with no member to unlink them (FR-EQ-01; " +
			"PRD-001 rev 0.167). The link decides where repayments go and who never approves the person's settlement (FM-9), so it falls " +
			"under FM-1 in every team mode: the answer is 202 with an approval request, which another person who holds A on \"Director " +
			"accounts, settlements, share issues\", not the member linked or unlinked, approves; the database refuses any other change of " +
			"it (STORAGE-PARTY-01). A member signs in as one director or shareholder at most. Asking needs a person signed in who passed " +
			"step-up in the last 5 minutes, and If-Match with the contact's ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, reflect.TypeFor[Contact]()), append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, storage.CodeParty}, codes...),
		func(ctx context.Context, in *memberLinkInput) (*payeeRequestOutput, error) {
			id, err := parseID("contact_id", in.ContactID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			var member *uuid.UUID
			if s := strings.TrimSpace(in.Body.MemberUserID); s != "" {
				m, err := parseID("body.member_user_id", s)
				if err != nil {
					return nil, err
				}
				member = &m
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				c, err := u.Contacts().Get(ctx, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(c.Version)); err != nil {
					return Approval{}, err
				}
				if !c.Director && !c.Shareholder {
					return Approval{}, problem(director.CodeNotPerson, c.LegalName+" is not a director or shareholder, so no member signs in as them.",
						"Mark the contact director or shareholder first")
				}
				if idOrEmpty(c.MemberUserID) == idOrEmpty(member) {
					return Approval{}, problem(CodeInvalid, "The contact has that member already.", "Send another member, or none to unlink them")
				}
				now := a.deps.Clock.Now()
				names, err := memberNames(ctx, u, now)
				if err != nil {
					return Approval{}, err
				}
				after, execute := "none", map[string]any{"contact_id": id.String(), "version": strconv.Itoa(c.Version), "member_user_id": nil}
				if member != nil {
					ms, err := u.Members().List(ctx, now)
					if err != nil {
						return Approval{}, err
					}
					active := false
					for _, m := range ms { // the active members
						active = active || m.UserID == *member
					}
					if !active {
						return Approval{}, problem(CodeInvalid, "The member is not an active member of this organisation.", "Choose an active member")
					}
					if other, err := u.Contacts().OfMember(ctx, *member); err == nil && other.ID != id {
						return Approval{}, problem(storage.CodeParty, names[*member]+" signs in as "+other.LegalName+" already.",
							"Unlink them from "+other.LegalName+" first")
					} else if err != nil && !errors.Is(err, ports.ErrNotFound) {
						return Approval{}, err
					}
					after, execute["member_user_id"] = names[*member], member.String()
				}
				before := "none"
				if c.MemberUserID != nil {
					before = cmpOr(names[*c.MemberUserID], "a former member")
					execute["was"] = c.MemberUserID.String()
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectContactMemberLink, ObjectKey: id.String(),
					Operation: approval.OpContactMemberLink, Summary: fmt.Sprintf("Link %s to the member %s", c.LegalName, after),
					Execute: execute, Diff: []ApprovalDiff{{Field: "member", Before: before, After: after}}})
			})
			if err != nil {
				return nil, err
			}
			return &payeeRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}

// fileSlug is a name as a file's name takes it: lower case, letters and digits, words joined by hyphens.
func fileSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return cmpOr(b.String(), "person")
}

// asAtOf reads an optional day, the organisation's today when it is empty.
func asAtOf(ctx context.Context, a *API, u storage.UnitOfWork, s string) (time.Time, error) {
	if s == "" {
		return orgToday(ctx, a, u)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	return t, nil
}

func (a *API) directorStatement(ctx context.Context, u storage.UnitOfWork, in *directorStatementInput) (DirectorStatement, error) {
	id, err := parseID("contact_id", in.ContactID)
	if err != nil {
		return DirectorStatement{}, err
	}
	from, to, err := statementDays(in.From, in.To)
	if err != nil {
		return DirectorStatement{}, err
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return DirectorStatement{}, err
	}
	st, err := director.StatementOf(ctx, u, base, id, from, to)
	if err != nil {
		return DirectorStatement{}, directorProblem(err)
	}
	return directorStatementOf(st, base), nil
}

// memberLinkDependencies are the contact's version and its member now: the link changes from them (FR-APR-05).
func memberLinkDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	c, err := u.Contacts().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	return c.Version, map[string]string{"contact_version": strconv.Itoa(c.Version), "member": cmpOr(idOrEmpty(c.MemberUserID), "none")}, nil
}

// memberLinkExecutable refuses a signer who is the member linked or unlinked (FM-1, FM-9; the database's FY251).
func memberLinkExecutable(s stored, signer uuid.UUID) error {
	linked, _ := s.Execute["member_user_id"].(string)
	if linked == signer.String() {
		return problem(CodeOwnLink, "You are the member being linked, so another person approves it (FM-1).",
			"Ask another person who holds the approval right on director accounts")
	}
	if was, ok := s.Execute["was"].(string); ok && was == signer.String() {
		return problem(CodeOwnLink, "You are the member being unlinked, so another person approves it (FM-1).",
			"Ask another person who holds the approval right on director accounts")
	}
	return nil
}

// executeMemberLink links or unlinks the member on the request's approval.
func executeMemberLink(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	version, err := strconv.Atoi(fmt.Sprint(s.Execute["version"]))
	if err != nil {
		return err
	}
	var member *uuid.UUID
	if v, _ := s.Execute["member_user_id"].(string); v != "" {
		m, err := uuid.Parse(v)
		if err != nil {
			return err
		}
		member = &m
	}
	_, err = u.Contacts().LinkMember(ctx, id, version, member)
	return err
}
