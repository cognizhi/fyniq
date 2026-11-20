package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/taxfilings"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ tax filings (FR-TAX-06, FR-AR-11, ENG-33; p2-tax-filings)

// BadDebtRelief is a written-off debt with its GST relief dates (FR-AR-11).
type BadDebtRelief struct {
	WriteOffID    string `json:"write_off_id" format:"uuid" doc:"The write-off." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	InvoiceID     string `json:"invoice_id" format:"uuid" doc:"The invoice written off." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a02"`
	InvoiceNumber string `json:"invoice_number" doc:"Its number." example:"INV-2026-0007"`
	SupplyDate    string `json:"supply_date" format:"date" doc:"The invoice's date: its tax point." example:"2026-01-15"`
	WrittenOffOn  string `json:"written_off_on" format:"date" doc:"The day the write-off posted from." example:"2027-03-10"`
	Written       Money  `json:"written_off" doc:"What was written off, in the invoice's currency."`
	GST           Money  `json:"gst" doc:"The GST in the part written off, in the invoice's currency, in the invoice's proportion [accountant to confirm]."`
	EligibleFrom  string `json:"eligible_from" format:"date" doc:"The first day a claim may be made." example:"2027-03-10"`
	ClaimBy       string `json:"claim_by" format:"date" doc:"The last day to claim: five years from the supply, the day before the anniversary [accountant to confirm]." example:"2031-01-14"`
	Status        string `json:"status" enum:"not_yet,eligible,expired" doc:"not_yet: the twelve months have not passed; eligible: a claim may be made; expired: it is out of time." example:"eligible"`
	Pack          string `json:"pack" doc:"The sg-tax-filings version the dates come from." example:"sg-tax-filings@2026.1"`
}

// BadDebtReliefQueue is the queue of debts that may carry GST bad debt relief.
type BadDebtReliefQueue struct {
	Today      string          `json:"today" format:"date" example:"2027-03-10"`
	Items      []BadDebtRelief `json:"items" nullable:"false"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// TaxAdjustment is an adjustment to the profit before tax.
type TaxAdjustment struct {
	Kind     string `json:"kind" enum:"non_deductible,capital_allowance_difference,other" doc:"non_deductible: an expense the Act does not allow, added back; capital_allowance_difference: the allowances less the depreciation, negative to take them off." example:"non_deductible"`
	Amount   string `json:"amount" pattern:"^-?[0-9]{1,13}(\\.[0-9]{1,2})?$" doc:"In Singapore dollars: positive adds to the profit, negative takes off." example:"15000.00"`
	Reason   string `json:"reason" minLength:"1" maxLength:"500" doc:"Why: the tax agent signs the working." example:"Client entertainment"`
	Evidence bool   `json:"evidence,omitempty" doc:"Whether evidence is attached to it elsewhere (the working paper does not keep files yet)." example:"true"`
}

// TaxWorkingPaperRequest asks for the tax-adjusted profit working paper of a financial year.
type TaxWorkingPaperRequest struct {
	FinancialYearEnd  string          `json:"financial_year_end" format:"date" doc:"The last day of the financial year, which must be a closed month's end in the books." example:"2026-12-31"`
	Scheme            string          `json:"scheme" enum:"sute,pte" doc:"sute: the start-up exemption, a qualifying company's first three Years of Assessment; pte: the partial exemption." example:"pte"`
	CashGrantEligible bool            `json:"cash_grant_eligible,omitempty" doc:"The company met the cash grant's local employee condition." example:"false"`
	Adjustments       []TaxAdjustment `json:"adjustments,omitempty" maxItems:"100" doc:"What the tax agent adds to or takes off the profit."`
}

// TaxWorkingPaper is the working: from the ledger's profit to the tax payable, and the form the revenue calls for.
type TaxWorkingPaper struct {
	YA                int    `json:"ya" doc:"The Year of Assessment." example:"2027"`
	From              string `json:"from" format:"date" doc:"The financial year's first day." example:"2026-01-01"`
	To                string `json:"to" format:"date" doc:"Its last day." example:"2026-12-31"`
	Revenue           string `json:"revenue" doc:"The year's revenue from the ledger, in Singapore dollars." example:"850000.00"`
	ProfitBeforeTax   string `json:"profit_before_tax" doc:"The year's profit before tax from the ledger." example:"300000.00"`
	Adjustments       string `json:"adjustments" doc:"Their net." example:"0.00"`
	ChargeableIncome  string `json:"chargeable_income" doc:"Never below zero." example:"300000.00"`
	LossForward       string `json:"loss_forward" doc:"The loss the year leaves, when the adjusted profit is below zero." example:"0.00"`
	Exempt            string `json:"exempt" doc:"The exemption of the scheme, exact." example:"102500.00"`
	NormalChargeable  string `json:"normal_chargeable" doc:"What is taxed at the rate." example:"197500.00"`
	Rate              string `json:"rate" doc:"The rate, as a fraction." example:"0.17"`
	TaxBeforeRebate   string `json:"tax_before_rebate" doc:"Exact." example:"33575.00"`
	Rebate            string `json:"rebate" doc:"The Year of Assessment's rebate with the cash grant taken off when eligible." example:"16787.50"`
	TaxPayable        string `json:"tax_payable" doc:"Exact, before the cash grant is paid." example:"16787.50"`
	TaxPayableRounded string `json:"tax_payable_rounded" doc:"To the cent, half away from zero [accountant to confirm]." example:"16787.50"`
	Form              string `json:"form" doc:"The form the revenue calls for." example:"C-S"`
	OptionalForm      string `json:"optional_form,omitempty" doc:"The form a company of that revenue may file instead." example:"C-S"`
	CITPack           string `json:"cit_pack" doc:"The sg-cit version worked with." example:"sg-cit@2026.1"`
	FormsPack         string `json:"forms_pack" doc:"The sg-tax-filings version the form limits come from." example:"sg-tax-filings@2026.1"`
}

// PackChange is a row added, removed or changed between two versions of a pack.
type PackChange struct {
	Table string `json:"table" doc:"The table." example:"limits"`
	Kind  string `json:"kind" enum:"added,removed,changed" doc:"What happened to the row." example:"added"`
	Key   string `json:"key" doc:"The row's first column." example:"registration_threshold"`
	Was   string `json:"was" doc:"The rows it had, each with its columns joined by a vertical bar, rows by a semicolon; empty for an added row." example:"registration_threshold|2026-11-07|1000000.00"`
	Now   string `json:"now" doc:"The rows it has, written the same way; empty for a removed row." example:"registration_threshold|2026-11-07|1000000.00"`
}

// PackUpdate is a newer version of an activated pack the organisation has not accepted, with what it would change (ENG-33).
type PackUpdate struct {
	Pack      string       `json:"pack" example:"sg-gst"`
	Current   string       `json:"current" doc:"The version the organisation has active." example:"sg-gst@2026.2"`
	Available string       `json:"available" doc:"The newest version in force that it has not activated." example:"sg-gst@2026.3"`
	Changes   []PackChange `json:"changes" nullable:"false"`
}

// PackUpdates is the updates available.
type PackUpdates struct {
	Items      []PackUpdate `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

type (
	badDebtReliefInput  struct{ PageQuery }
	badDebtReliefOutput struct{ Body BadDebtReliefQueue }
	packUpdatesInput    struct{ PageQuery }
	taxPaperInput       struct {
		KeyHeader
		PreferHeader
		Body TaxWorkingPaperRequest
	}
	taxPaperOutput struct {
		Status   int
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Body     TaxWorkingPaper
	}
	packUpdatesOutput struct{ Body PackUpdates }
)

func registerTaxFilings(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.GetBadDebtRelief, huma.Operation{
		OperationID: "getBadDebtRelief", Method: http.MethodGet, Path: "/v1/tax/bad-debt-relief", Tags: []string{"Compliance"},
		Summary: "The GST bad debt relief queue",
		Description: "The invoices written off as a bad debt that bore GST, each with the GST in the part written off, when a claim may be made and until when (FR-AR-11): " +
			"from twelve months after the date of supply, or from the write-off when the debtor became insolvent sooner, and within five years of the supply, as the sg-tax-filings pack says. " +
			"The claim goes in the GST return and is not yet recorded here. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *badDebtReliefInput) (*badDebtReliefOutput, error) {
		p, _ := authn.From(ctx)
		out := badDebtReliefOutput{Body: BadDebtReliefQueue{Items: []BadDebtRelief{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			out.Body.Today = dateOf(today)
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			filter := storage.DocumentFilter{Type: storage.DocumentWriteOff, Status: storage.DocumentPosted, Limit: 200}
			for {
				page, err := u.Documents().Page(ctx, filter)
				if err != nil {
					return err
				}
				for _, doc := range page {
					item, ok, err := reliefOf(ctx, u, doc, base, today)
					if err != nil {
						return err
					}
					if ok {
						out.Body.Items = append(out.Body.Items, item)
					}
				}
				if len(page) < filter.Limit {
					return nil
				}
				last := page[len(page)-1]
				filter.AfterDate, filter.AfterID = last.Date, &last.ID
			}
		})
		if err != nil {
			return &out, err
		}
		slices.SortFunc(out.Body.Items, func(x, y BadDebtRelief) int { return strings.Compare(x.ClaimBy+x.WriteOffID, y.ClaimBy+y.WriteOffID) })
		out.Body.Items, out.Body.NextCursor, err = pageOf(out.Body.Items, func(x BadDebtRelief) string { return x.WriteOffID }, in.PageQuery)
		return &out, err
	})

	register(a, authz.WorkTaxPaper, huma.Operation{
		OperationID: "workTaxPaper", Method: http.MethodPost, Path: "/v1/tax/working-paper", Tags: []string{"Compliance"},
		Summary: "Work out a financial year's tax-adjusted profit",
		Description: "The working paper the tax agent needs (FR-TAX-06): the year's profit before tax and revenue from the ledger, the adjustments given with their reasons, the chargeable income, the " +
			"start-up or partial exemption and the Year of Assessment's rebate from the sg-cit pack, the tax, and the form the revenue calls for (sg-tax-filings). Exact decimals, rounded once at the " +
			"end. It saves nothing: it is a calculation the tax agent signs. Idempotent with Idempotency-Key; Prefer: return=preview answers the same.",
	}, append(append([]string{}, writes...), CodeNotFound), func(ctx context.Context, in *taxPaperInput) (*taxPaperOutput, error) {
		end, err := time.Parse(time.DateOnly, in.Body.FinancialYearEnd)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.financial_year_end", Message: "is not a date", Fix: "Send the financial year's last day, YYYY-MM-DD"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (TaxWorkingPaper, error) {
			return a.taxPaper(ctx, u, end, in.Body)
		})
		if err != nil {
			return nil, err
		}
		return &taxPaperOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.ListStatutoryPackUpdates, huma.Operation{
		OperationID: "listStatutoryPackUpdates", Method: http.MethodGet, Path: "/v1/statutory-packs/updates", Tags: []string{"Statutory packs"},
		Summary: "The statutory pack updates available",
		Description: "For each pack the organisation has activated, the newest version in force that it has not activated, with the rows added, removed and changed between the two (ENG-33): " +
			"what accepting the update would do. Nothing re-posts by itself. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *packUpdatesInput) (*packUpdatesOutput, error) {
		p, _ := authn.From(ctx)
		out := packUpdatesOutput{Body: PackUpdates{Items: []PackUpdate{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			vs, err := u.Packs().Versions(ctx)
			if err != nil {
				return err
			}
			acts, err := u.Packs().Activations(ctx)
			if err != nil {
				return err
			}
			loaded := map[string]pack.Version{}
			for _, v := range vs {
				_, act, err := loadedPack(ctx, u, v.ID)
				if err != nil {
					return err
				}
				if _, pv, err := packOf(v, act); err == nil {
					loaded[v.ID] = pv
				}
			}
			active := func(id string) bool {
				return slices.ContainsFunc(acts, func(x storage.PackActivation) bool { return x.VersionID == id })
			}
			inForce := func(v pack.Version) bool {
				return !v.EffectiveFrom.After(today) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(today))
			}
			newer := func(x, y pack.Version) bool {
				return x.EffectiveFrom.After(y.EffectiveFrom) || (x.EffectiveFrom.Equal(y.EffectiveFrom) && (x.Year > y.Year || (x.Year == y.Year && x.N > y.N)))
			}
			names := map[string]bool{}
			for _, v := range loaded {
				names[v.Pack] = true
			}
			for _, name := range slices.Sorted(func(yield func(string) bool) {
				for n := range names {
					if !yield(n) {
						return
					}
				}
			}) {
				var cur, next *pack.Version
				for id, v := range loaded {
					v := v
					switch {
					case v.Pack != name:
					case active(id) && inForce(v) && (cur == nil || newer(v, *cur)):
						cur = &v
					}
				}
				if cur == nil {
					continue
				}
				for id, v := range loaded {
					v := v
					if v.Pack == name && !active(id) && inForce(v) && newer(v, *cur) && (next == nil || newer(v, *next)) {
						next = &v
					}
				}
				if next == nil {
					continue
				}
				up := PackUpdate{Pack: name, Current: cur.ID, Available: next.ID, Changes: []PackChange{}}
				for _, c := range pack.Diff(*cur, *next) {
					up.Changes = append(up.Changes, PackChange{Table: c.Table, Kind: c.Kind, Key: c.Key, Was: strings.Join(rowsText(c.Was), "; "), Now: strings.Join(rowsText(c.Now), "; ")})
				}
				out.Body.Items = append(out.Body.Items, up)
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(out.Body.Items, func(x PackUpdate) string { return x.Pack }, in.PageQuery)
			return err
		})
		return &out, err
	})
}

// rowsText writes each row as its columns joined with a vertical bar.
func rowsText(rows [][]string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.Join(r, "|"))
	}
	return out
}

// reliefOf is a posted write-off's relief, or false when it is not a bad debt that bore GST.
func reliefOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency, today time.Time) (BadDebtRelief, bool, error) {
	w, err := writeoff.Read(doc)
	if err != nil {
		return BadDebtRelief{}, false, err
	}
	if w.Reason != writeoff.ReasonBadDebt {
		return BadDebtRelief{}, false, nil
	}
	inv, err := u.Documents().Get(ctx, w.InvoiceID)
	if err != nil {
		return BadDebtRelief{}, false, err
	}
	p, probs, err := invoice.Prepare(ctx, u, inv, base)
	if err != nil || len(probs) > 0 || !p.Registered || p.Totals.GST.IsZero() || p.Totals.Total.IsZero() {
		return BadDebtRelief{}, false, err
	}
	// the GST in the part written off, in the invoice's proportion of GST to total, to the cent half away from zero
	share, err := proportionOf(w.Amount, p.Totals.GST, p.Totals.Total)
	if err != nil {
		return BadDebtRelief{}, false, err
	}
	v, ok, err := gst.PackOn(ctx, u, "sg-tax-filings", p.Draft.Date)
	if err != nil || !ok {
		return BadDebtRelief{}, false, err
	}
	rel, err := pack.BadDebtRelief(v, p.Draft.Date, w.Date, nil, today)
	if err != nil {
		return BadDebtRelief{}, false, err
	}
	return BadDebtRelief{WriteOffID: doc.ID.String(), InvoiceID: inv.ID.String(), InvoiceNumber: inv.Number, SupplyDate: dateOf(p.Draft.Date), WrittenOffOn: dateOf(w.Date),
		Written: moneyOut(w.Amount), GST: moneyOut(share), EligibleFrom: dateOf(rel.EligibleFrom), ClaimBy: dateOf(rel.ClaimBy), Status: rel.Status, Pack: v.ID}, true, nil
}

// taxPaper works the paper from the ledger's year and the pack.
func (a *API) taxPaper(ctx context.Context, u storage.UnitOfWork, end time.Time, req TaxWorkingPaperRequest) (TaxWorkingPaper, error) {
	var out TaxWorkingPaper
	base, err := baseOf(ctx, u)
	if err != nil {
		return out, err
	}
	if base.Code != "SGD" {
		return out, invalid([]FieldProblem{{Code: CodeInvalid, Field: "financial_year_end", Message: "the books are not in Singapore dollars", Fix: "The working paper is in Singapore dollars"}})
	}
	r, err := a.monthsOf(ctx, u, "year", end.Format(statementMonthForm), 0)
	if err != nil {
		return out, err
	}
	s, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.ProfitAndLoss, All: r.all, First: r.first, Last: r.last, Revision: r.revision})
	if err != nil {
		return out, err
	}
	measure := func(key string) (string, error) {
		for _, b := range s.Blocks {
			if b.Key == key && len(b.Totals) > 0 {
				return b.Totals[0].String(), nil
			}
		}
		return "0.00", nil // a year with no such line has none of it
	}
	pbt, err := measure(financials.KeyProfitBeforeTax)
	if err != nil {
		return out, err
	}
	revenue, err := measure(financials.KeyRevenue)
	if err != nil {
		return out, err
	}
	ya := end.Year() + 1
	citV, ok, err := gst.PackOn(ctx, u, "sg-cit", end)
	if err != nil || !ok {
		return out, invalid([]FieldProblem{{Code: CodeInvalid, Field: "financial_year_end", Message: "no sg-cit version is in force on that day", Fix: "Use a financial year the statutory packs cover"}})
	}
	formsV, ok, err := gst.PackOn(ctx, u, "sg-tax-filings", end)
	if err != nil || !ok {
		return out, invalid([]FieldProblem{{Code: CodeInvalid, Field: "financial_year_end", Message: "no sg-tax-filings version is in force on that day", Fix: "Use a financial year the statutory packs cover"}})
	}
	in := taxfilings.Input{YA: ya, ProfitBeforeTax: pbt, Scheme: req.Scheme, CashGrantEligible: req.CashGrantEligible}
	for _, x := range req.Adjustments {
		in.Adjustments = append(in.Adjustments, taxfilings.Adjustment{Kind: x.Kind, Amount: x.Amount, Reason: x.Reason, Evidence: x.Evidence})
	}
	paper, bad := taxfilings.Work(citV, in)
	if len(bad) > 0 {
		fields := make([]FieldProblem, 0, len(bad))
		for _, b := range bad {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body." + b.Field, Message: b.Message, Fix: "Correct it and ask again"})
		}
		return out, invalid(fields)
	}
	forForm := revenue
	if strings.HasPrefix(strings.TrimSpace(revenue), "-") {
		forForm = "0.00" // a year whose net revenue is negative (credits above sales) is below every limit
	}
	form, optional, err := pack.FilingForm(formsV, forForm)
	if err != nil {
		return out, err
	}
	return TaxWorkingPaper{YA: ya, From: dateOf(r.all[r.first].StartsOn), To: dateOf(end), Revenue: revenue, ProfitBeforeTax: paper.ProfitBeforeTax, Adjustments: paper.Adjustments,
		ChargeableIncome: paper.ChargeableIncome, LossForward: paper.LossForward, Exempt: paper.Exempt, NormalChargeable: paper.NormalChargeable, Rate: paper.Rate,
		TaxBeforeRebate: paper.TaxBeforeRebate, Rebate: paper.Rebate, TaxPayable: paper.TaxPayable, TaxPayableRounded: paper.TaxPayableRounded, Form: form, OptionalForm: optional,
		CITPack: citV.ID, FormsPack: formsV.ID}, nil
}

// proportionOf is amount times part over whole, rounded to the currency's cents half away from zero: the GST in a part of an invoice.
func proportionOf(amount, part, whole money.Money) (money.Money, error) {
	var x apd.Decimal
	c := apd.BaseContext.WithPrecision(40)
	if _, err := c.Mul(&x, amount.Decimal(), part.Decimal()); err != nil {
		return money.Money{}, err
	}
	if _, err := c.Quo(&x, &x, whole.Decimal()); err != nil {
		return money.Money{}, err
	}
	var y apd.Decimal
	rc := apd.BaseContext.WithPrecision(40)
	rc.Rounding = apd.RoundHalfUp
	if _, err := rc.Quantize(&y, &x, -int32(amount.Currency().MinorUnits)); err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(&y, amount.Currency())
}
