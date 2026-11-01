package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Reconciliations, the Auto-reconciled list and the cash position (FR-BNK-10, FR-BNK-13, FR-BNK-14; p1-bank-import-
// reconcile task 9.1, design D9): a money account's reconciliation for a period prepared under C on "Bank reconciliation
// sign-off" and signed by another person under A, stepped up (FM-5, SOD-5); what the system reconciled by itself, to
// undo with unreconcileBankLine; and each money account's cash on a day.

// BankReconciliation is a money account's reconciliation for a period.
type BankReconciliation struct {
	ID                string     `json:"id" format:"uuid" doc:"The reconciliation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c10"`
	MoneyAccountID    string     `json:"money_account_id" format:"uuid" doc:"The money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	PeriodID          string     `json:"period_id" format:"uuid" doc:"The period." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a50"`
	StatementBalance  Money      `json:"statement_balance" doc:"The statement's balance at the period's end."`
	BookBalance       Money      `json:"book_balance" doc:"The account's balance in the books at the period's end, in its currency."`
	Unreconciled      Money      `json:"unreconciled" doc:"The lines booked by then that the books do not hold yet: unreconciled or possible duplicates."`
	UnreconciledCount int        `json:"unreconciled_count" doc:"How many." example:"3"`
	Difference        Money      `json:"difference" doc:"What they leave unexplained: statement − book − unreconciled. Zero when it matches."`
	LedgerRevision    int64      `json:"ledger_revision" doc:"The ledger revision it saw (ENG-46): a later posting to the account in the period means preparing it again." example:"42"`
	PreparedBy        string     `json:"prepared_by" doc:"Who prepared it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	PreparedAt        time.Time  `json:"prepared_at" doc:"When." example:"2026-11-03T09:00:00Z"`
	SignedBy          string     `json:"signed_by,omitempty" doc:"Who signed it off: never its preparer." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a02"`
	SignedAt          *time.Time `json:"signed_at,omitempty" doc:"When." example:"2026-11-04T09:00:00Z"`
	Version           int        `json:"version" doc:"Its version, as its ETag." example:"1"`
	StatementDate     string     `json:"statement_date,omitempty" format:"date" doc:"On preparing: the day the statement balance is of." example:"2026-10-31"`
	Lines             []BankLine `json:"unreconciled_lines" nullable:"false" doc:"On preparing and reading one: the lines it counts as unreconciled, earliest first."`
}

// AutoReconciliation is a line the system reconciled by itself, and the rule it used.
type AutoReconciliation struct {
	Line        BankLine `json:"line" doc:"The line, with its match: its basis says how (exact, transfer or rule)."`
	RuleName    string   `json:"rule_name,omitempty" doc:"The rule, when a rule did it." example:"AWS to hosting"`
	BatchNumber string   `json:"batch_number,omitempty" doc:"The payment batch, when the line matched one of its payments, which the line named (rev 0.160)." example:"PB-0003"`
}

// AccountName is a money account's name, as a list names it (FR-INT-13).
type AccountName string

// Schema implements huma.SchemaProvider.
func (AccountName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A money account's name.", Examples: []any{"Wise USD"}}
}

// CashAccount is one money account's cash on a day.
type CashAccount struct {
	MoneyAccountID string `json:"money_account_id" format:"uuid" doc:"The money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	Name           string `json:"name" doc:"Its name." example:"DBS current"`
	Kind           string `json:"kind" enum:"bank,card,wallet,payment_gateway,cash" doc:"Its kind." example:"bank"`
	Balance        Money  `json:"balance" doc:"Its balance in its currency on the day."`
	Value          *Money `json:"value,omitempty" doc:"Its value in the base currency at the day's rate; absent in the base currency, or with no rate."`
	Rate           string `json:"rate,omitempty" doc:"The rate used, base currency per unit." example:"1.3472"`
	RateDate       string `json:"rate_date,omitempty" format:"date" doc:"The day the rate is of." example:"2026-10-31"`
}

// CashPosition is the organisation's cash on a day (FR-BNK-13).
type CashPosition struct {
	Date     string        `json:"date" format:"date" doc:"The day." example:"2026-10-31"`
	Accounts []CashAccount `json:"accounts" nullable:"false" doc:"Each open money account, by currency then name."`
	Total    Money         `json:"total" doc:"The total in the base currency of those with a value."`
	NoRate   []AccountName `json:"no_rate" nullable:"false" doc:"The accounts in another currency with no rate for the day, left out of the total."`
}

type ReconciliationPath struct {
	MoneyAccountPath
	PeriodID string `path:"period_id" format:"uuid" doc:"The period's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a50"`
}

type prepareReconciliationInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		MoneyAccountID   string `json:"money_account_id" format:"uuid" doc:"The money account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
		PeriodID         string `json:"period_id" format:"uuid" doc:"The period." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a50"`
		StatementBalance *Money `json:"statement_balance,omitempty" doc:"The balance an e-statement shows at the period's end, entered: refused in Founder mode, where it comes only from an imported statement or feed (FM-5). Left out, the closing balance of the latest statement imported up to the period's end."`
	}
}

type signReconciliationInput struct {
	ReconciliationPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type reconciliationOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The reconciliation's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankReconciliation
}

type reconciliationsInput struct {
	PageQuery
	PeriodID string `query:"period_id" required:"true" format:"uuid" doc:"The period." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a50"`
}

type reconciliationsOutput struct {
	Body struct {
		Items      []BankReconciliation `json:"items" nullable:"false" doc:"The period's reconciliations."`
		NextCursor string               `json:"next_cursor,omitempty" doc:"The next page's cursor; absent on the last page." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c10"`
	}
}

type autoReconciliationsInput struct {
	PageQuery
	Since string `query:"since" format:"date" doc:"From this day; left out, the last 30 days." example:"2026-10-01"`
}

type autoReconciliationsOutput struct {
	Body struct {
		Items      []AutoReconciliation `json:"items" nullable:"false" doc:"What the system reconciled and still stands, latest first."`
		NextCursor string               `json:"next_cursor,omitempty" doc:"The next page's cursor; absent on the last page." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b70"`
	}
}

type cashInput struct {
	Date string `query:"date" format:"date" doc:"The day; left out, today." example:"2026-10-31"`
}

type cashOutput struct {
	Body CashPosition
}

func reconciliationOf(r storage.BankReconciliation) BankReconciliation {
	return BankReconciliation{ID: r.ID.String(), MoneyAccountID: r.MoneyAccountID.String(), PeriodID: r.PeriodID.String(),
		StatementBalance: moneyOf(r.StatementBalance, r.Currency), BookBalance: moneyOf(r.BookBalance, r.Currency),
		Unreconciled: moneyOf(r.UnreconciledTotal, r.Currency), UnreconciledCount: r.UnreconciledCount, Difference: moneyOf(r.Difference, r.Currency),
		LedgerRevision: r.LedgerRevision, PreparedBy: r.PreparedBy, PreparedAt: r.PreparedAt.UTC(), SignedBy: r.SignedBy, SignedAt: r.SignedAt,
		Version: r.Version, Lines: []BankLine{}}
}

func registerBankReconciliation(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		CodeNotFound, storage.CodeBanking, storage.CodeVersion}, locks...)
	paths := func(in ReconciliationPath, prefix string) (uuid.UUID, uuid.UUID, error) {
		account, err := parseID(prefix+"money_account_id", in.MoneyAccountID)
		if err != nil {
			return account, uuid.Nil, err
		}
		period, err := parseID(prefix+"period_id", in.PeriodID)
		return account, period, err
	}

	register(a, authz.GetBankReconciliation, huma.Operation{
		OperationID: "getBankReconciliation", Method: http.MethodGet, Path: "/v1/money-accounts/{money_account_id}/reconciliations/{period_id}",
		Tags: []string{"Banking"}, Summary: "Get a money account's reconciliation for a period",
		Description: "Returns the reconciliation as prepared, with its ETag, and the lines unreconciled at the period's end now (FR-BNK-10). " +
			"Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ReconciliationPath) (*reconciliationOutput, error) {
		account, period, err := paths(*in, "")
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BankReconciliation
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.BankMatching().Reconciliation(ctx, account, period)
			if err != nil {
				return err
			}
			out = reconciliationOf(r)
			per, err := u.Periods().Get(ctx, period)
			if err != nil {
				return err
			}
			lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &account, To: &per.EndsOn,
				Statuses: []string{storage.LineUnreconciled, storage.LinePossibleDuplicate}})
			for _, l := range lines {
				out.Lines = append(out.Lines, bankLineOf(l))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &reconciliationOutput{Status: http.StatusOK, ETag: etag(out.Version), Body: out}, nil
	})

	register(a, authz.PrepareBankReconciliation, huma.Operation{
		OperationID: "prepareBankReconciliation", Method: http.MethodPost, Path: "/v1/bank-reconciliations",
		Tags: []string{"Banking"}, Summary: "Prepare a money account's reconciliation",
		Description: "Prepares a money account's reconciliation for a period, or prepares it again while unsigned (FR-BNK-10): the statement's " +
			"balance at the period's end, the book balance from the balance service, the lines the books do not hold yet, and the difference " +
			"they leave. Another person signs it off. Side effects: the reconciliation and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, append([]string{banking.CodeNoStatement, banking.CodeTyped, banking.CodeCurrency}, writes...), func(ctx context.Context, in *prepareReconciliationInput) (*reconciliationOutput, error) {
		account, period, err := paths(ReconciliationPath{MoneyAccountPath: MoneyAccountPath{MoneyAccountID: in.Body.MoneyAccountID}, PeriodID: in.Body.PeriodID}, "body.")
		if err != nil {
			return nil, err
		}
		r := banking.ReconcileRequest{MoneyAccountID: account, PeriodID: period, At: a.deps.Clock.Now()}
		if in.Body.StatementBalance != nil {
			m, bad := moneyIn(*in.Body.StatementBalance, "body.statement_balance")
			if bad != nil {
				return nil, invalid([]FieldProblem{*bad})
			}
			r.StatementBalance = &m
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankReconciliation, error) {
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return BankReconciliation{}, err
			}
			r.Founder = mode == team.Founder
			done, err := banking.Prepare(ctx, u, r)
			if err != nil {
				return BankReconciliation{}, err
			}
			out := reconciliationOf(done.BankReconciliation)
			out.StatementDate = dateOf(done.StatementDate)
			for _, l := range done.Unreconciled {
				out.Lines = append(out.Lines, bankLineOf(l))
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &reconciliationOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.SignBankReconciliation, huma.Operation{
		OperationID: "signBankReconciliation", Method: http.MethodPost, Path: "/v1/money-accounts/{money_account_id}/reconciliations/{period_id}/sign",
		Tags: []string{"Banking"}, Summary: "Sign off a money account's reconciliation",
		Description: "Signs off a prepared reconciliation (FR-BNK-10, FM-5, SOD-5): by a person who did not prepare it, who passed step-up in " +
			"the last 5 minutes, while its period is open; once signed it never changes. The close needs every money account with activity " +
			"in the period signed off at the ledger revision it saw (FR-GL-07). Needs If-Match with its ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...), func(ctx context.Context, in *signReconciliationInput) (*reconciliationOutput, error) {
		account, period, err := paths(in.ReconciliationPath, "")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil {
			return nil, problem(CodeSessionRequired, "Signing off a reconciliation needs a person signed in.", "Sign in to Fyniq as yourself and sign it off there.")
		}
		if !p.SteppedUp(a.deps.Clock.Now()) {
			return nil, problem(CodeStepUpRequired, "Signing off a reconciliation needs step-up authentication in the last 5 minutes (FM-5).",
				"Confirm it's you with your passkey or authenticator app, then try again.")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankReconciliation, error) {
			r, err := u.BankMatching().Reconciliation(ctx, account, period)
			if err != nil {
				return BankReconciliation{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(r.Version)); err != nil {
				return BankReconciliation{}, err
			}
			signed, err := banking.SignOff(ctx, u, r.ID, r.Version, p.StepUpAt, a.deps.Clock.Now())
			return reconciliationOf(signed), err
		})
		if err != nil {
			return nil, err
		}
		return &reconciliationOutput{Status: w.Status, ETag: etag(w.Body.Version), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListBankReconciliations, huma.Operation{
		OperationID: "listBankReconciliations", Method: http.MethodGet, Path: "/v1/bank-reconciliations", Tags: []string{"Banking"},
		Summary: "List a period's reconciliations",
		Description: "Every money account's reconciliation for a period, prepared or signed off, a page at a time (FR-BNK-10): what the " +
			"close's checks read. Read-only.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *reconciliationsInput) (*reconciliationsOutput, error) {
		period, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := &reconciliationsOutput{}
		out.Body.Items = []BankReconciliation{}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.BankMatching().Reconciliations(ctx, period)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(r storage.BankReconciliation) string { return r.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, r := range all {
				out.Body.Items = append(out.Body.Items, reconciliationOf(r))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.ListAutoReconciliations, huma.Operation{
		OperationID: "listAutoReconciliations", Method: http.MethodGet, Path: "/v1/auto-reconciliations", Tags: []string{"Banking"},
		Summary: "List what the system reconciled",
		Description: "The lines the system reconciled by itself that still stand, latest first, a page at a time (FR-BNK-14): exact matches, " +
			"a released batch's payments (rev 0.160), transfers and lines an approved rule categorised, each with its basis, rule or batch. Undo one with unreconcileBankLine while its " +
			"period is open; a weekly sample goes to the review log. Read-only.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *autoReconciliationsInput) (*autoReconciliationsOutput, error) {
		since, bad := optionalDate("since", in.Since)
		if bad != nil {
			return nil, invalid([]FieldProblem{*bad})
		}
		if since == nil {
			d := a.deps.Clock.Now().UTC().AddDate(0, 0, -30)
			since = &d
		}
		p, _ := authn.From(ctx)
		out := &autoReconciliationsOutput{}
		out.Body.Items = []AutoReconciliation{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{SystemOnly: true, Since: since})
			if err != nil {
				return err
			}
			for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 { // latest first
				matches[i], matches[j] = matches[j], matches[i]
			}
			if matches, out.Body.NextCursor, err = page(matches, func(m storage.BankMatch) string { return m.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			names := map[uuid.UUID]string{}
			for _, m := range matches {
				l, err := u.Banking().Line(ctx, m.LineID)
				if err != nil {
					return err
				}
				x, err := lineWithMatch(ctx, u, l)
				if err != nil {
					return err
				}
				item := AutoReconciliation{Line: x}
				if m.RuleID != nil {
					if _, ok := names[*m.RuleID]; !ok {
						if r, err := u.BankMatching().Rule(ctx, *m.RuleID); err == nil {
							names[*m.RuleID] = r.Name
						}
					}
					item.RuleName = names[*m.RuleID]
				}
				if m.Kind == storage.MatchRecorded && m.DocumentID != nil {
					rel, err := u.PaymentBatches().ReleaseOfPayment(ctx, *m.DocumentID)
					if err != nil {
						return err
					}
					if rel != nil {
						batch, err := u.Documents().Get(ctx, rel.BatchID)
						if err != nil {
							return err
						}
						item.BatchNumber = batch.Number
					}
				}
				out.Body.Items = append(out.Body.Items, item)
			}
			return nil
		})
		return out, err
	})

	register(a, authz.GetCashPosition, huma.Operation{
		OperationID: "getCashPosition", Method: http.MethodGet, Path: "/v1/cash-position", Tags: []string{"Banking"},
		Summary: "Get the cash position",
		Description: "Each open money account's balance in its currency on a day, its value in the base currency at the latest rate the " +
			"cascade has for that day (ENG-40) with the rate's day, and the total; accounts with no rate are named, not guessed (FR-BNK-13). " +
			"Read-only.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *cashInput) (*cashOutput, error) {
		day, bad := optionalDate("date", in.Date)
		if bad != nil {
			return nil, invalid([]FieldProblem{*bad})
		}
		p, _ := authn.From(ctx)
		out := &cashOutput{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if day == nil {
				t, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				day = &t
			}
			c, err := banking.Cash(ctx, u, *day)
			if err != nil {
				return err
			}
			out.Body = CashPosition{Date: dateOf(c.Day), Accounts: []CashAccount{}, Total: moneyOut(c.Total), NoRate: typed[AccountName](c.Missing)}
			for _, r := range c.Rows {
				x := CashAccount{MoneyAccountID: r.Account.ID.String(), Name: r.Account.Name, Kind: r.Account.Kind, Balance: moneyOut(r.Balance)}
				if r.Value != nil {
					v := moneyOut(*r.Value)
					x.Value = &v
				}
				if r.Rate != nil {
					x.Rate, x.RateDate = r.Rate.String(), dateOf(r.RateOf)
				}
				out.Body.Accounts = append(out.Body.Accounts, x)
			}
			return nil
		})
		return out, err
	})
}
