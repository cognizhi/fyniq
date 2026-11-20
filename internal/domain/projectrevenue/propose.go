package projectrevenue

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectreport"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Stable codes (ENG-23).
const (
	CodeProject  = "PRJ-REV-01" // the project is not a fixed-fee project in the base currency, or has no election of revenue over time
	CodeEstimate = "PRJ-REV-02" // the estimate or the fee is not usable
	CodeMonth    = "PRJ-REV-03" // the month is not one that can be proposed now
	CodeAccounts = "PRJ-REV-04" // the contract asset, contract liability or revenue account is not set up
	CodeNothing  = "PRJ-REV-05" // the month's journal would move nothing
)

// The settings that name the accounts the journal posts to.
const (
	SettingAsset     = "projects.contract_asset_account"
	SettingLiability = "projects.contract_liability_account"
	SettingRevenue   = "projects.revenue_over_time_account"
)

// Error is a refusal in plain words.
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message + " (" + e.Fix + ")" }

// Report is a project's revenue position for a month, as it would be proposed.
type Report struct {
	Election                 storage.ProjectRevenueElection
	Month                    time.Time
	Fee, Estimate            money.Money
	CostToDate               money.Money // what progress is measured by: the costs posted less those left out
	CostPosted, Excluded     money.Money // the project's posted costs, and the part of them the accountant left out of progress
	Billed, Held             money.Money
	Position                 Position
	Asset, Liability, Income storage.Account // the accounts the journal posts to
	Runs                     []storage.ProjectRevenueRun
}

func accountOf(ctx context.Context, u storage.UnitOfWork, key string, day time.Time) (storage.Account, error) {
	def, _ := settings.Lookup(key)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return storage.Account{}, err
	}
	code := settings.On(def, changes, day).Value
	a, err := u.Accounts().ByCode(ctx, code)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && (!a.Active || a.Group)) {
		return storage.Account{}, &Error{CodeAccounts, "The account " + code + " (" + def.Label + ") is not an active account that takes postings.", "Set it under organisation settings, or add the account to the chart"}
	}
	return a, err
}

// Of works out the project's position for a month (its first day) from the posted ledger and the milestones: the fee is the milestones' total, the cost to date the
// project's costs posted to the end of the month, the revenue held the project's posted revenue, and what was invoiced is that revenue less the earlier journals' adjustments.
func Of(ctx context.Context, u storage.UnitOfWork, project uuid.UUID, month, today time.Time) (Report, error) {
	var r Report
	pr, err := u.Projects().Get(ctx, project)
	if err != nil {
		return r, err
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return r, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return r, err
	}
	if pr.BillingMethod != storage.BillingFixedFee || pr.Currency != base.Code {
		return r, &Error{CodeProject, "Revenue over time is for a fixed-fee project in the organisation's base currency " + base.Code + ".", "Bill the project as a fixed fee in " + base.Code + ", or keep recognising each milestone as it is invoiced"}
	}
	if r.Election, err = u.ProjectRevenue().Election(ctx, project); errors.Is(err, ports.ErrNotFound) || (err == nil && r.Election.Method != storage.RevenueOverTime) {
		return r, &Error{CodeProject, "The project is not elected for revenue over time.", "Elect it first, with its estimated total cost"}
	} else if err != nil {
		return r, err
	}
	if !month.Equal(time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)) || month.After(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		return r, &Error{CodeMonth, "The month is the first day of a month that has begun.", "Choose a month up to this one"}
	}
	r.Month = month
	if r.Estimate, err = money.ParseStored(string(*r.Election.EstimatedCost), base); err != nil {
		return r, &Error{CodeEstimate, "The estimated total cost is not an amount in " + base.Code + ".", "Elect the project again with its estimated total cost"}
	}
	ms, err := u.Billing().Milestones(ctx, project)
	if err != nil {
		return r, err
	}
	r.Fee = money.Zero(base)
	for _, m := range ms {
		if m.Kind != storage.MilestoneFixedFee {
			continue
		}
		a, err := money.ParseStored(string(m.Amount), base)
		if err != nil {
			return r, err
		}
		if r.Fee, err = r.Fee.Add(a); err != nil {
			return r, err
		}
	}
	if r.Fee.IsZero() {
		return r, &Error{CodeEstimate, "The project has no milestones, so it has no fee to recognise.", "Add the milestones that make up its fixed fee"}
	}
	if r.Runs, err = u.ProjectRevenue().Runs(ctx, project); err != nil {
		return r, err
	}
	var priorAdj = money.Zero(base)
	// The revenue the ledger holds includes each posted journal up to this month's: those adjustments are not invoicing. An earlier month's journal that is not
	// posted yet is not in the ledger, so this month cannot be worked out until it is posted or discarded.
	for _, run := range r.Runs {
		if run.Month.After(month) {
			continue
		}
		doc, err := u.Documents().Get(ctx, run.JournalID)
		if err != nil {
			return r, err
		}
		if doc.Status != storage.DocumentPosted {
			if run.Month.Before(month) {
				return r, &Error{CodeMonth, "The journal proposed for " + run.Month.Format("January 2006") + " is not posted yet, so this month cannot be worked out.", "Approve and post it, or discard it, first"}
			}
			continue
		}
		adj, err := money.ParseStored(string(run.Adjustment), base)
		if err != nil {
			return r, err
		}
		if priorAdj, err = priorAdj.Add(adj); err != nil {
			return r, err
		}
	}
	first := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	byProject, _, err := projectreport.LedgerByProject(ctx, u, base, first, month)
	if err != nil {
		return r, err
	}
	l, ok := byProject[project]
	if !ok {
		l = projectreport.Ledger{Revenue: money.Zero(base), DirectCosts: money.Zero(base), PayrollTagged: money.Zero(base)}
	}
	if r.CostPosted, err = l.DirectCosts.Add(l.PayrollTagged); err != nil {
		return r, err
	}
	if r.Excluded, err = money.ParseStored(string(r.Election.ExcludedCost), base); err != nil {
		return r, err
	}
	if c, _ := r.Excluded.Cmp(r.CostPosted); c > 0 {
		return r, &Error{CodeEstimate, "The costs left out of progress, " + r.Excluded.String() + ", are more than the " + r.CostPosted.String() + " posted to the project.", "Lower what is left out when you elect the project again"}
	}
	if r.CostToDate, err = r.CostPosted.Sub(r.Excluded); err != nil {
		return r, err
	}
	r.Held = l.Revenue
	if r.Billed, err = r.Held.Sub(priorAdj); err != nil {
		return r, err
	}
	if r.Position, err = Work(r.Fee, r.Estimate, r.CostToDate, r.Billed, r.Held); err != nil {
		return r, &Error{CodeEstimate, strings.TrimSpace(err.Error()), "Check the milestones and the estimated total cost"}
	}
	return r, nil
}

// Propose makes the month's journal as a draft for a person to approve and records the run: Dr or Cr the contract asset and the contract liability by what the position moves
// them by, and the opposite on the revenue account, every line carrying the project. Nothing is posted here.
func Propose(ctx context.Context, u storage.UnitOfWork, project uuid.UUID, month, today, now time.Time) (Report, storage.Document, error) {
	r, err := Of(ctx, u, project, month, today)
	if err != nil {
		return r, storage.Document{}, err
	}
	for _, run := range r.Runs {
		if run.Month.Equal(month) {
			return r, storage.Document{}, &Error{CodeMonth, "A journal is proposed for " + month.Format("January 2006") + " already.", "Open it from the project; reverse it if it was wrong"}
		}
		if run.Month.After(month) {
			return r, storage.Document{}, &Error{CodeMonth, "A later month is proposed already, so months go in order.", "Choose a month after " + run.Month.Format("January 2006")}
		}
	}
	if r.Position.AssetChange.IsZero() && r.Position.LiabilityChange.IsZero() {
		return r, storage.Document{}, &Error{CodeNothing, "The journal would move nothing this month.", "Nothing is proposed until the progress or the invoicing changes"}
	}
	if r.Asset, err = accountOf(ctx, u, SettingAsset, month); err != nil {
		return r, storage.Document{}, err
	}
	if r.Liability, err = accountOf(ctx, u, SettingLiability, month); err != nil {
		return r, storage.Document{}, err
	}
	if r.Income, err = accountOf(ctx, u, SettingRevenue, month); err != nil {
		return r, storage.Document{}, err
	}
	pj, err := u.Projects().Get(ctx, project)
	if err != nil {
		return r, storage.Document{}, err
	}
	tag := []uuid.UUID{project}
	var lines []ledger.JournalLine
	add := func(acct storage.Account, debit money.Money, why string) {
		if !debit.IsZero() {
			lines = append(lines, ledger.JournalLine{AccountID: acct.ID, Amount: debit, Description: why, Values: tag})
		}
	}
	// A debit is positive: the asset rises by its change, the liability (a credit balance) rises by crediting it, and revenue takes the rest.
	add(r.Asset, r.Position.AssetChange, "Contract asset")
	add(r.Liability, r.Position.LiabilityChange.Neg(), "Contract liability")
	adj, err := r.Position.AssetChange.Sub(r.Position.LiabilityChange)
	if err != nil {
		return r, storage.Document{}, err
	}
	add(r.Income, adj.Neg(), "Revenue recognised over time")
	end := month.AddDate(0, 1, -1)
	memo := "Revenue over time for " + pj.Code + ", " + month.Format("January 2006") + ": " + r.Position.Progress + "% of the estimated cost (cost-to-cost)"
	doc, err := journal.CreateIn(ctx, u, journal.Draft{Date: end, Memo: memo, Lines: lines})
	if err != nil {
		return r, storage.Document{}, err
	}
	if _, err = u.ProjectRevenue().Record(ctx, storage.NewProjectRevenueRun{ProjectID: project, Month: month, JournalID: doc.ID,
		Adjustment: storage.Decimal(r.Position.Adjustment.String()), Earned: storage.Decimal(r.Position.Earned.String()), At: now}); err != nil {
		return r, storage.Document{}, err
	}
	return r, doc, nil
}
