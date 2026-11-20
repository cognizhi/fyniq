package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the tenant usage and cost view (FR-ADM-05; p2-mobile-approvals-automation design D7)
//
// What the organisation used of AI, storage and exports, a month at a time, from what the platform already keeps: the AI calls (tokens, and the cost in each currency a price used), the
// report exports made ready and the files uploaded as evidence, plus what is held in storage now. It is read-only and keeps nothing of its own.

// UsageCost is an exact amount in one currency.
type UsageCost struct {
	Currency string `json:"currency" doc:"The currency of the price the calls used." example:"USD"`
	Amount   string `json:"amount" pattern:"^-?[0-9]+(\\.[0-9]+)?$" doc:"The exact amount, as a decimal string." example:"12.34500000"`
}

// UsageFiles is a count of files and their size.
type UsageFiles struct {
	Files int64 `json:"files" doc:"How many." example:"42"`
	Bytes int64 `json:"bytes" doc:"Their size in bytes." example:"1048576"`
}

// UsageMonth is one month's use.
type UsageMonth struct {
	Month        string      `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The calendar month (UTC), as YYYY-MM." example:"2026-11"`
	AICalls      int64       `json:"ai_calls" doc:"Attempts of a model call that started in the month, served or not." example:"120"`
	InputTokens  int64       `json:"input_tokens" doc:"Input tokens of those calls." example:"350000"`
	OutputTokens int64       `json:"output_tokens" doc:"Output tokens of those calls." example:"41000"`
	AICost       []UsageCost `json:"ai_cost" nullable:"false" doc:"What the calls cost, exactly, in each currency their prices used; empty when none was priced."`
	Exports      UsageFiles  `json:"exports" doc:"Report exports made ready in the month, and their size."`
	Evidence     UsageFiles  `json:"evidence" doc:"Files uploaded as evidence in the month."`
}

// UsageStored is what the organisation holds in storage now.
type UsageStored struct {
	Evidence   UsageFiles `json:"evidence" doc:"Evidence files."`
	Captures   UsageFiles `json:"captures" doc:"Captured documents, such as receipts and emailed bills."`
	Exports    UsageFiles `json:"exports" doc:"Report exports that are ready."`
	TotalBytes int64      `json:"total_bytes" doc:"All of them together, in bytes." example:"5242880"`
}

// Usage is the organisation's use.
type Usage struct {
	Months []UsageMonth `json:"months" nullable:"false" doc:"Every month from the first asked for to the current one, oldest first, with zeros where nothing was used."`
	Stored UsageStored  `json:"stored" doc:"What is held in storage now."`
}

type usageInput struct {
	Months int `query:"months" minimum:"1" maximum:"24" default:"6" doc:"How many months to show, ending with the current one." example:"6"`
}

type usageOutput struct{ Body Usage }

func registerUsage(a *API) {
	register(a, authz.GetUsage, huma.Operation{
		OperationID: "getUsage", Method: http.MethodGet, Path: "/v1/usage", Tags: []string{"Organisation"},
		Summary: "The organisation's usage and cost",
		Description: "What the organisation used of AI, storage and exports (FR-ADM-05): for each of the last months (1 to 24, default 6), the model calls with their tokens and their exact cost in " +
			"each currency a price used, the report exports made ready and the evidence files uploaded, and what is held in storage now. The months are calendar months in UTC. " +
			"Read-only; no side effects. For the Owner, the Administrator and the DPO; never the AI.",
	}, []string{storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *usageInput) (*usageOutput, error) {
		p, _ := authn.From(ctx)
		n := in.Months
		if n < 1 {
			n = 6
		}
		var out Usage
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			now := a.deps.Clock.Now().UTC()
			since := time.Date(now.Year(), now.Month()-time.Month(n-1), 1, 0, 0, 0, 0, time.UTC)
			rows, err := u.Usage().Months(ctx, since)
			if err != nil {
				return err
			}
			byMonth := map[string]storage.UsageMonth{}
			for _, r := range rows {
				byMonth[r.Month.Format("2006-01")] = r
			}
			out.Months = make([]UsageMonth, 0, n)
			for i := 0; i < n; i++ {
				m := since.AddDate(0, i, 0)
				r := byMonth[m.Format("2006-01")]
				cost := make([]UsageCost, 0, len(r.AICost))
				for _, c := range r.AICost {
					cost = append(cost, UsageCost{Currency: c.Currency, Amount: string(c.Amount)})
				}
				out.Months = append(out.Months, UsageMonth{Month: m.Format("2006-01"), AICalls: r.AICalls, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, AICost: cost,
					Exports: UsageFiles{Files: r.Exports, Bytes: r.ExportBytes}, Evidence: UsageFiles{Files: r.EvidenceFiles, Bytes: r.EvidenceBytes}})
			}
			s, err := u.Usage().Stored(ctx)
			out.Stored = UsageStored{Evidence: UsageFiles{Files: s.EvidenceFiles, Bytes: s.EvidenceBytes}, Captures: UsageFiles{Files: s.CaptureFiles, Bytes: s.CaptureBytes},
				Exports: UsageFiles{Files: s.ExportFiles, Bytes: s.ExportBytes}, TotalBytes: s.EvidenceBytes + s.CaptureBytes + s.ExportBytes}
			return err
		})
		return &usageOutput{Body: out}, err
	})
}
