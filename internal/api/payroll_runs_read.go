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
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// PayrollRunReceived is one version of a pay run Fyniq received from Nemura and what became of it: posted, or unchanged (a version that changed nothing). It names no person: a pay run reaches Fyniq as totals by
// account and department (PRD-001 FR-PAY-13).
type PayrollRunReceived struct {
	RunID          string               `json:"run_id" format:"uuid" doc:"Nemura's pay run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	Version        int                  `json:"version" doc:"The run's version." example:"1"`
	WageMonth      string               `json:"wage_month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month the wages are for." example:"2026-10"`
	Outcome        string               `json:"outcome" enum:"posted,unchanged" doc:"posted: the version posted a journal entry; unchanged: it changed nothing and only the version was recorded." example:"posted"`
	EntryID        string               `json:"entry_id,omitempty" format:"uuid" doc:"The journal entry it posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f02"`
	MappingVersion int                  `json:"mapping_version" doc:"The version of the account mapping it was calculated against." example:"3"`
	ReceivedAt     string               `json:"received_at" format:"date-time" doc:"When Fyniq received it." example:"2026-10-30T09:00:00+08:00"`
	Posted         []PayrollRunPostLine `json:"posted" nullable:"false" doc:"What the run has posted so far, by account: the signed total after this version, in the base currency."`
}

// PayrollRunPostLine is a total of a run on an account.
type PayrollRunPostLine struct {
	AccountID string `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f03"`
	Amount    string `json:"amount" doc:"The signed total on the account across the run's departments, a decimal string; debits positive." example:"12500.00"`
}

// PayrollRunRefused is a version of a run Fyniq refused and nothing has posted since.
type PayrollRunRefused struct {
	RunID     string `json:"run_id" format:"uuid" doc:"Nemura's pay run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	Version   int    `json:"version" doc:"The version refused." example:"2"`
	Code      string `json:"code" doc:"The refusal's stable code." example:"PPL-14"`
	Message   string `json:"message" doc:"What the code means, in plain words, without a figure or a name." example:"The run was calculated against another account mapping than Fyniq's current one"`
	RefusedAt string `json:"refused_at" format:"date-time" doc:"When it was refused." example:"2026-10-30T09:00:00+08:00"`
}

// PayrollRunsPage is the pay runs received for a month and the runs refused that nothing has posted since.
type PayrollRunsPage struct {
	Items      []PayrollRunReceived `json:"items" nullable:"false" doc:"The versions received for the month, by run and version."`
	Refused    []PayrollRunRefused  `json:"refused" nullable:"false" doc:"The runs refused and not posted since, whatever their month: they never reached a month."`
	NextCursor string               `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

type (
	payrollRunsInput struct {
		PageQuery
		Month string `query:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The wage month, YYYY-MM; left out, the month of the business date." example:"2026-10"`
	}
	payrollRunsOutput struct{ Body PayrollRunsPage }
)

func registerPayrollRunsRead(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListPayrollRuns, huma.Operation{
		OperationID: "listPayrollRuns", Method: http.MethodGet, Path: "/v1/payroll-runs", Tags: []string{"People"},
		Summary: "List the pay runs received from Nemura for a month",
		Description: "The versions of pay runs Fyniq received for a wage month and what became of each (FR-PPL-09, FR-PPL-11): posted or unchanged, the entry it posted, the mapping it was calculated against and its totals by account " +
			"(never by person), a page at a time; and the runs it refused with the code and its plain words, which stay listed until a version posts (check C.1 #19). Read-only; no side effects. Needs the right to view pay runs.",
	}, append([]string{CodeCursor, CodeInvalid}, locks...), func(ctx context.Context, in *payrollRunsInput) (*payrollRunsOutput, error) {
		p, _ := authn.From(ctx)
		month := time.Date(a.deps.Clock.Now().UTC().Year(), a.deps.Clock.Now().UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
		if in.Month != "" {
			m, err := time.Parse("2006-01", in.Month)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.month", Message: "is not a month", Fix: "Send the wage month as YYYY-MM"}})
			}
			month = m
		}
		out := PayrollRunsPage{Items: []PayrollRunReceived{}, Refused: []PayrollRunRefused{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			all, err := u.PayrollRuns().ReceiptsOfMonth(ctx, month)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(x storage.PayrollRunReceipt) string { return x.ID.String() }, in.PageQuery)
			if err != nil {
				return err
			}
			for _, x := range page {
				posted, err := ledger.DecodePayrollPosted(x.Version, x.Totals, base)
				if err != nil {
					return err
				}
				byAccount := map[uuid.UUID]money.Money{}
				for k, v := range posted.Totals {
					if cur, ok := byAccount[k.Account]; ok {
						if v, err = cur.Add(v); err != nil {
							return err
						}
					}
					byAccount[k.Account] = v
				}
				item := PayrollRunReceived{RunID: x.RunID.String(), Version: x.Version, WageMonth: x.WageMonth.Format("2006-01"), Outcome: "unchanged", MappingVersion: x.MappingVersion,
					ReceivedAt: x.ReceivedAt.Format(time.RFC3339), Posted: []PayrollRunPostLine{}}
				if x.EntryID != uuid.Nil {
					item.Outcome, item.EntryID = "posted", x.EntryID.String()
				}
				for acc, v := range byAccount {
					item.Posted = append(item.Posted, PayrollRunPostLine{AccountID: acc.String(), Amount: v.String()})
				}
				slices.SortFunc(item.Posted, func(l, r PayrollRunPostLine) int { return strings.Compare(l.AccountID, r.AccountID) })
				out.Items = append(out.Items, item)
			}
			out.NextCursor = next
			open, err := u.PayrollRuns().OpenRefusals(ctx)
			if err != nil {
				return err
			}
			for _, r := range open {
				pr := problemFor(r.Code)
				out.Refused = append(out.Refused, PayrollRunRefused{RunID: r.RunID.String(), Version: r.Version, Code: r.Code, Message: pr, RefusedAt: r.RefusedAt.Format(time.RFC3339)})
			}
			return nil
		})
		return &payrollRunsOutput{Body: out}, err
	})
}

// problemFor is the plain words of a code: its title in the catalogue, never a figure, a name or a payload.
func problemFor(code string) string {
	if f, ok := codeFacts[code]; ok {
		return f.Title
	}
	return "Fyniq's checks refused the run"
}
