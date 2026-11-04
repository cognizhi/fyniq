package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// The related-party schedule (FR-EQ-10; p1-founders-money design D14): per director and shareholder, what the company
// owes them at a day by nature, and the year's fees, conversions and waivers, from the balance service. It shows on
// Director accounts, exports as CSV, and the notes to the financial statements (FR-RPT-11) read it.

// RelatedPartyFigures are a row's figures by nature, in the base currency.
type RelatedPartyFigures struct {
	Loan        Money `json:"loan" doc:"What the company owes them on 2500 Loan from director / shareholder at the day: positive owed to them; negative means they owe the company, shown under 1250 Staff & director advances (FR-EQ-11)."`
	DeferredPay Money `json:"deferred_pay" doc:"What the company owes them on 2120 Deferred remuneration payable at the day: pay and fees not yet paid."`
	FeesForYear Money `json:"fees_for_year" doc:"Directors' fees owed to them in the financial year to the day (FR-EQ-04)."`
	Converted   Money `json:"converted" doc:"What they were owed that was converted to shares in the year, effective when ACRA registered it (FR-EQ-06)."`
	Waived      Money `json:"waived" doc:"What they waived of what they were owed in the year (FR-EQ-07)."`
}

// RelatedPartyRow is one director's or shareholder's row of the schedule.
type RelatedPartyRow struct {
	ContactID   string              `json:"contact_id" format:"uuid" doc:"The person's contact." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d65"`
	LegalName   string              `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	Director    bool                `json:"director" doc:"A director." example:"true"`
	Shareholder bool                `json:"shareholder" doc:"A shareholder." example:"true"`
	Figures     RelatedPartyFigures `json:"figures" doc:"Their figures by nature."`
}

// RelatedPartySchedule is the related-party schedule as at a day.
type RelatedPartySchedule struct {
	AsAt     string              `json:"as_at" format:"date" doc:"The day the balances are at, inclusive." example:"2026-12-31"`
	YearFrom string              `json:"year_from" format:"date" doc:"The first day of the year the fees, conversions and waivers are read from: the financial year as_at falls in, or the calendar year when the financial years are not set." example:"2026-01-01"`
	Currency string              `json:"currency" doc:"The base currency." example:"SGD"`
	Rows     []RelatedPartyRow   `json:"rows" doc:"Each director and shareholder with something to disclose, in the order they were added: a schedule, whole, not a page of a list."`
	Totals   RelatedPartyFigures `json:"totals" doc:"The rows' figures added up."`
}

type relatedPartiesInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day the balances are at; the organisation's today when left out." example:"2026-12-31"`
}

type relatedPartiesExportInput struct {
	AsAt   string `query:"as_at" format:"date" doc:"The day the balances are at; the organisation's today when left out." example:"2026-12-31"`
	Format string `query:"format" enum:"csv" default:"csv" doc:"csv: a row per person and a total, plain decimals with an ASCII minus." example:"csv"`
}

type relatedPartiesOutput struct{ Body RelatedPartySchedule }

// relatedParties reads the schedule as at in.
func (a *API) relatedParties(ctx context.Context, u storage.UnitOfWork, asAtText string) (RelatedPartySchedule, error) {
	base, err := baseOf(ctx, u)
	if err != nil {
		return RelatedPartySchedule{}, err
	}
	asAt, err := asAtOf(ctx, a, u, asAtText)
	if err != nil {
		return RelatedPartySchedule{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return RelatedPartySchedule{}, err
	}
	from, rows, err := director.RelatedParties(ctx, u, base, o.Profile, asAt)
	if err != nil {
		return RelatedPartySchedule{}, directorProblem(err)
	}
	out := RelatedPartySchedule{AsAt: dateOf(asAt), YearFrom: dateOf(from), Currency: base.Code, Rows: make([]RelatedPartyRow, 0, len(rows))}
	total := director.RelatedParty{Loan: money.Zero(base), Deferred: money.Zero(base), Fees: money.Zero(base), Converted: money.Zero(base), Waived: money.Zero(base)}
	for _, r := range rows {
		out.Rows = append(out.Rows, RelatedPartyRow{ContactID: r.Contact.ID.String(), LegalName: r.Contact.LegalName, Director: r.Contact.Director,
			Shareholder: r.Contact.Shareholder, Figures: relatedFigures(r)})
		for _, x := range []struct{ to, add *money.Money }{{&total.Loan, &r.Loan}, {&total.Deferred, &r.Deferred}, {&total.Fees, &r.Fees},
			{&total.Converted, &r.Converted}, {&total.Waived, &r.Waived}} {
			if *x.to, err = x.to.Add(*x.add); err != nil {
				return RelatedPartySchedule{}, err
			}
		}
	}
	out.Totals = relatedFigures(total)
	return out, nil
}

func relatedFigures(r director.RelatedParty) RelatedPartyFigures {
	return RelatedPartyFigures{Loan: moneyAmount(r.Loan), DeferredPay: moneyAmount(r.Deferred), FeesForYear: moneyAmount(r.Fees),
		Converted: moneyAmount(r.Converted), Waived: moneyAmount(r.Waived)}
}

func registerRelatedParties(a *API) {
	codes := append([]string{CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout}, DirectorCodes...)

	register(a, authz.GetRelatedParties, huma.Operation{
		OperationID: "getRelatedParties", Method: http.MethodGet, Path: "/v1/director-accounts/related-parties", Tags: []string{"Founders"},
		Summary: "Work out the related-party schedule",
		Description: "The related-party schedule as at a day (FR-EQ-10): for each director and shareholder with something to disclose, " +
			"what the company owes them on 2500 (loan) and 2120 (deferred pay) at the day, negative when they owe it, and in the financial " +
			"year to the day the directors' fees owed to them, what was converted to shares and what they waived, with the totals. From the " +
			"balance service (ENG-04); the notes to the financial statements read it. Read-only; no side effects.",
	}, codes, func(ctx context.Context, in *relatedPartiesInput) (*relatedPartiesOutput, error) {
		p, _ := authn.From(ctx)
		var out relatedPartiesOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out.Body, err = a.relatedParties(ctx, u, in.AsAt)
			return err
		})
		return &out, err
	})

	register(a, authz.ExportRelatedParties, huma.Operation{
		OperationID: "exportRelatedParties", Method: http.MethodGet, Path: "/v1/director-accounts/related-parties/export", Tags: []string{"Founders"},
		Summary: "Export the related-party schedule",
		Description: "The schedule getRelatedParties gives, as CSV (FR-EQ-10): a row per person and a total, plain decimals with an ASCII " +
			"minus. Returned in base64 with its SHA-256. Side effects: an audit event of the export with its file and hash (FR-RPT-09). " +
			"Never cached.",
	}, codes, func(ctx context.Context, in *relatedPartiesExportInput) (*reportFileOutput, error) {
		p, _ := authn.From(ctx)
		var s RelatedPartySchedule
		var revision int64
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if s, err = a.relatedParties(ctx, u, in.AsAt); err != nil {
				return err
			}
			revision, err = u.Ledger().Revision(ctx)
			return err
		})
		if err != nil {
			return nil, err
		}
		sh := sheet.Sheet{Name: "Related parties", Header: []string{"Person", "Director", "Shareholder", "Loan " + s.Currency, "Deferred pay " + s.Currency,
			"Fees for the year " + s.Currency, "Converted to shares " + s.Currency, "Waived " + s.Currency}}
		yes := map[bool]string{true: "Yes", false: "No"}
		figures := func(f RelatedPartyFigures) []sheet.Cell {
			return []sheet.Cell{sheet.Number(f.Loan.Amount, 2), sheet.Number(f.DeferredPay.Amount, 2), sheet.Number(f.FeesForYear.Amount, 2),
				sheet.Number(f.Converted.Amount, 2), sheet.Number(f.Waived.Amount, 2)}
		}
		for _, r := range s.Rows {
			sh.Rows = append(sh.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text(r.LegalName), sheet.Text(yes[r.Director]), sheet.Text(yes[r.Shareholder])},
				figures(r.Figures)...)})
		}
		sh.Rows = append(sh.Rows, sheet.Row{Cells: append([]sheet.Cell{sheet.Text("Total"), sheet.Text(""), sheet.Text("")}, figures(s.Totals)...)})
		data, name := sheet.CSV(sh), "related-parties-"+s.AsAt+".csv"
		sum := sha256.Sum256(data)
		id := uuid.Must(uuid.NewV7())
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "report.exported", ObjectType: "report", ObjectID: id,
				Changes: map[string][2]any{"export": {nil, map[string]any{"report": "related_parties", "format": in.Format, "as_at": s.AsAt,
					"year_from": s.YearFrom, "ledger_revision": revision, "rows": len(s.Rows), "file": name, "sha256": hex.EncodeToString(sum[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &reportFileOutput{Body: ReportFile{ExportID: id.String(), FileName: name, MediaType: mediaCSV, SHA256: hex.EncodeToString(sum[:]),
			Size: len(data), Rows: len(s.Rows), Revision: revision, Content: base64.StdEncoding.EncodeToString(data)}}, nil
	})
}
