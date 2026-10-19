package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the standard chart (FR-COA-01)

// CodeChartLoaded refuses the standard chart for an organisation that has accounts already.
const CodeChartLoaded = "API-CHART-01"

// ChartLoaded is what loading the standard chart did.
type ChartLoaded struct {
	Accounts int `json:"accounts" doc:"How many accounts the chart now holds." example:"86"`
}

type loadChartInput struct {
	KeyHeader
	PreferHeader
}

type loadChartOutput struct {
	Status   int
	Location string `header:"Location" doc:"The chart of accounts."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ChartLoaded
}

func registerChart(a *API) {
	register(a, authz.LoadStandardChart, huma.Operation{
		OperationID: "loadStandardChart", Method: http.MethodPost, Path: "/v1/accounts:load-standard-chart", Tags: []string{"Chart of accounts"},
		DefaultStatus: http.StatusCreated, Summary: "Load the standard chart of accounts",
		Description: "Loads the standard Singapore start-up chart into an organisation that has no account yet: 86 accounts with their " +
			"report groups, cash-flow lines, functions, GST codes, flags and guidance (FR-COA-01, FR-COA-02; PRD-001 rev 0.31, provisional " +
			"until the accountant's review). Refused once the organisation has any account. Side effects: the accounts and an audit event " +
			"for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeChartLoaded, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeDuplicate, storage.CodeRetry,
		storage.CodeLockTimeout},
		func(ctx context.Context, in *loadChartInput) (*loadChartOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ChartLoaded, error) {
				n, err := u.Accounts().Count(ctx)
				if err != nil {
					return ChartLoaded{}, err
				}
				if n > 0 {
					return ChartLoaded{}, problem(CodeChartLoaded, fmt.Sprintf("The organisation has %d accounts already.", n),
						"Change the chart account by account; the standard chart loads only into an organisation with none.")
				}
				accounts, err := chart.Standard()
				if err != nil {
					return ChartLoaded{}, err
				}
				for _, acc := range accounts {
					if _, err := u.Accounts().Create(ctx, acc.NewAccount); err != nil {
						return ChartLoaded{}, err
					}
				}
				return ChartLoaded{Accounts: len(accounts)}, nil
			})
			if err != nil {
				return nil, err
			}
			out := &loadChartOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.Location, out.Replayed = "/v1/accounts", replayedHeader(w.Replayed)
			}
			return out, nil
		})
}

// ------------------------------------------------------------------ editing the chart (FR-COA-01…05)

// AccountDraft is a new account.
type AccountDraft struct {
	Code           string `json:"code" maxLength:"20" doc:"Its code: 1 to 20 letters, digits, dots or hyphens; it never changes." example:"6115"`
	Name           string `json:"name" maxLength:"200" doc:"Its name." example:"Co-working memberships"`
	Type           string `json:"type" doc:"asset, liability, equity, revenue or expense; it never changes." example:"expense"`
	ParentID       string `json:"parent_id,omitempty" doc:"The group account it sits under (FR-COA-05)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Group          bool   `json:"group,omitempty" doc:"A group account is a heading: postings go only to the accounts under it." example:"false"`
	Role           string `json:"role,omitempty" doc:"A system role (FR-COA-03); it never changes. Usually left empty: the standard chart sets them." example:"suspense"`
	ReportGroup    string `json:"report_group,omitempty" maxLength:"60" doc:"Where the statements show it." example:"Premises & office"`
	CashFlowLine   string `json:"cash_flow_line,omitempty" maxLength:"60" doc:"Its cash-flow line, for balance-sheet accounts." example:"Payables & accruals"`
	Function       string `json:"function,omitempty" doc:"COGS, R&D, S&M, G&A or Other." example:"G&A"`
	MoneyAccount   bool   `json:"money_account,omitempty" doc:"A bank, cash, card or wallet account money moves through." example:"false"`
	DefaultGSTCode string `json:"default_gst_code,omitempty" doc:"The GST code a line on it starts with." example:"TX"`
	Currency       string `json:"currency,omitempty" doc:"A bank account's currency." example:"SGD"`
	Control        bool   `json:"control,omitempty" doc:"Has a sub-ledger." example:"false"`
	PayrollCost    bool   `json:"payroll_cost,omitempty" doc:"Carries payroll cost." example:"false"`
	Guidance       string `json:"guidance,omitempty" maxLength:"500" doc:"Shown when someone picks the account." example:"Hot-desk and co-working memberships."`
}

// AccountChanges change an account: a field left out keeps its value. Its code, type and role never change.
type AccountChanges struct {
	Name           *string `json:"name,omitempty" maxLength:"200" doc:"Its name." example:"Rent & co-working"`
	ParentID       *string `json:"parent_id,omitempty" doc:"The group account it sits under; \"\" moves it to the top." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Group          *bool   `json:"group,omitempty" doc:"Whether it is a heading; an account with postings stays a leaf." example:"false"`
	Active         *bool   `json:"active,omitempty" doc:"false blocks new postings (FR-COA-04); a system account stays active." example:"false"`
	ReportGroup    *string `json:"report_group,omitempty" maxLength:"60" doc:"Where the statements show it." example:"Premises & office"`
	CashFlowLine   *string `json:"cash_flow_line,omitempty" maxLength:"60" doc:"Its cash-flow line." example:"Payables & accruals"`
	Function       *string `json:"function,omitempty" doc:"COGS, R&D, S&M, G&A or Other; \"\" clears it." example:"G&A"`
	MoneyAccount   *bool   `json:"money_account,omitempty" doc:"A bank, cash, card or wallet account." example:"true"`
	DefaultGSTCode *string `json:"default_gst_code,omitempty" doc:"The GST code a line on it starts with; \"\" clears it." example:"TX"`
	Currency       *string `json:"currency,omitempty" doc:"A bank account's currency; \"\" clears it." example:"USD"`
	Control        *bool   `json:"control,omitempty" doc:"Changes only with an approval, which arrives with the approval engine: refused until then." example:"false"`
	PayrollCost    *bool   `json:"payroll_cost,omitempty" doc:"Changes only with an approval, like the control flag: refused until then." example:"false"`
	Guidance       *string `json:"guidance,omitempty" maxLength:"500" doc:"Shown when someone picks the account; \"\" clears it." example:"Office rent and co-working memberships."`
}

type createAccountInput struct {
	KeyHeader
	PreferHeader
	Body AccountDraft
}

type updateAccountInput struct {
	AccountPath
	IfMatchHeader
	PreferHeader
	Body AccountChanges
}

// AccountPath names an account in a path.
type AccountPath struct {
	AccountID string `path:"account_id" format:"uuid" doc:"The account's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
}

type accountWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The account's version, for If-Match."`
	Location string `header:"Location" doc:"The account's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Account
}

// AccountCodes are an account's rules (internal/domain/chart).
var AccountCodes = []string{chart.CodeAccountCode, chart.CodeText, chart.CodeType, chart.CodeFunction, chart.CodeGSTCode,
	chart.CodeCurrency, chart.CodeRole, chart.CodeProtected, chart.CodeSystem}

func chartProblems(p []chart.Problem, bad []FieldProblem) error {
	for _, x := range p {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

func registerAccountEditing(a *API) {
	codes := append([]string{storage.CodeAccountFixed, storage.CodeAccountTree, storage.CodeRetry, storage.CodeLockTimeout}, AccountCodes...)

	register(a, authz.CreateAccount, huma.Operation{
		OperationID: "createAccount", Method: http.MethodPost, Path: "/v1/accounts", Tags: []string{"Chart of accounts"},
		DefaultStatus: http.StatusCreated, Summary: "Add an account",
		Description: "Adds an account to the chart (FR-COA-01, FR-COA-02), as a leaf or as a group under which others sit (FR-COA-05). Its " +
			"code, type and role never change. Side effects: the account and its audit event. Approval policy: the Owner and the " +
			"Controller approve chart changes once the approval engine arrives (P1). Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodeDuplicate}, codes...),
		func(ctx context.Context, in *createAccountInput) (*accountWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Account, error) {
				b := in.Body
				n := storage.NewAccount{Code: strings.TrimSpace(b.Code), Name: strings.TrimSpace(b.Name), Type: storage.AccountType(b.Type),
					Group: b.Group, Role: storage.AccountRole(b.Role), AccountAttributes: storage.AccountAttributes{
						ReportGroup: strings.TrimSpace(b.ReportGroup), CashFlowLine: strings.TrimSpace(b.CashFlowLine), Function: b.Function,
						MoneyAccount: b.MoneyAccount, DefaultGSTCode: b.DefaultGSTCode, Currency: b.Currency, Control: b.Control,
						PayrollCost: b.PayrollCost, Guidance: strings.TrimSpace(b.Guidance)}}
				var bad []FieldProblem
				if b.ParentID != "" {
					id, err := uuid.Parse(b.ParentID)
					if err != nil {
						bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.parent_id", Message: "is not an account ID", Fix: "Send the group account's ID"})
					} else {
						n.ParentID = &id
					}
				}
				if err := chartProblems(chart.CheckAccount(n), bad); err != nil {
					return Account{}, err
				}
				acc, err := u.Accounts().Create(ctx, n)
				return accountOf(acc), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &accountWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &accountWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/accounts/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.UpdateAccount, huma.Operation{
		OperationID: "updateAccount", Method: http.MethodPatch, Path: "/v1/accounts/{account_id}", Tags: []string{"Chart of accounts"},
		Summary: "Change an account",
		Description: "Changes the fields sent and keeps the rest: the name, the group it sits under, whether it is a group, whether it is " +
			"active (FR-COA-04: an inactive account takes no new postings; a system account stays active, FR-COA-03), and the FR-COA-02 " +
			"attributes. Its code, type and role never change. The control and payroll-cost flags change only with an approval, which " +
			"arrives with the approval engine: refused until then. Side effects: the next version and its audit event. Needs If-Match " +
			"with the account's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion}, codes...),
		func(ctx context.Context, in *updateAccountInput) (*accountWriteOutput, error) {
			id, err := parseID("account_id", in.AccountID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Account, error) {
				cur, err := u.Accounts().Get(ctx, id)
				if err != nil {
					return Account{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return Account{}, err
				}
				b := in.Body
				c := storage.AccountChange{Name: cur.Name, ParentID: cur.ParentID, Group: cur.Group, Active: cur.Active, AccountAttributes: cur.AccountAttributes}
				text := func(dst *string, v *string) {
					if v != nil {
						*dst = strings.TrimSpace(*v)
					}
				}
				flag := func(dst *bool, v *bool) {
					if v != nil {
						*dst = *v
					}
				}
				text(&c.Name, b.Name)
				text(&c.ReportGroup, b.ReportGroup)
				text(&c.CashFlowLine, b.CashFlowLine)
				text(&c.Function, b.Function)
				text(&c.DefaultGSTCode, b.DefaultGSTCode)
				text(&c.Currency, b.Currency)
				text(&c.Guidance, b.Guidance)
				flag(&c.Group, b.Group)
				flag(&c.Active, b.Active)
				flag(&c.MoneyAccount, b.MoneyAccount)
				flag(&c.Control, b.Control)
				flag(&c.PayrollCost, b.PayrollCost)
				var bad []FieldProblem
				if b.ParentID != nil {
					c.ParentID = nil
					if p := strings.TrimSpace(*b.ParentID); p != "" {
						pid, err := uuid.Parse(p)
						if err != nil {
							bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.parent_id", Message: "is not an account ID", Fix: "Send the group account's ID, or \"\" for the top"})
						} else {
							c.ParentID = &pid
						}
					}
				}
				if err := chartProblems(chart.CheckChange(cur, c), bad); err != nil {
					return Account{}, err
				}
				acc, err := u.Accounts().Update(ctx, id, cur.Version, c)
				return accountOf(acc), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &accountWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &accountWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
