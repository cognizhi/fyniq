package checks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/payrollimport"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens payroll's items: pay runs and employee data (FR-PAY-13). The items name a month, a batch or an account, never a person or an amount (FR-PPL-17).
const viewPayroll = "Pay runs, employee data"

var onPayroll = []string{"payroll_run_refusal", "salary_batch", "payroll_arrangement", "payroll_import", "payroll_run_receipt", "person_projection", "journal_entry"}

// The checks of payroll that Fyniq can see for itself (p1-payroll-link task 7.2): off with the organisation's payroll switch.
var payrollChecks = []Definition{
	{Key: "payroll.arrangements-recorded", Ref: "C.2 #45", Area: "Payroll", Worst: Error, Title: "How each month's payroll outputs are met is recorded",
		Explain: "A wage month with a pay run, a salary batch or an imported payroll journal needs, recorded as evidence, how its itemised payslips, key employment terms, CPF, SDL and SHG " +
			"submission and payment, and where they apply the Auto-Inclusion Scheme and IR21 are met (FR-PAY-15). With Nemura, its own issuing of payslips and KETs counts. Record the missing arrangement in Settings, People and HR.",
		Rules: "FR-PAY-15, FR-PAY-08", Owner: "controller", View: viewPayroll, Module: "features.payroll", Triggers: onPayroll, Run: arrangementsRecorded},
	{Key: "payroll.counted-twice", Ref: "C.1 #21", Area: "Payroll", Worst: Warning, Title: "No salary is counted twice",
		Explain: "A wage month whose pay run was posted from Nemura or a provider, and that also has other postings (a journal, a bank line) on the salary accounts the pay run posts to, may count the same " +
			"salaries twice. Check each and move or reverse the one that repeats the pay run.",
		Rules: "FR-PAY-07, FR-PPL-04", Owner: "accountant", View: viewPayroll, Module: "features.payroll", Triggers: onPayroll, Run: salariesCountedTwice},
	{Key: "payroll.pay-lines-cannot-post", Ref: "C.1 #19", Area: "Payroll", Worst: Error, Title: "Every pay run sent from Nemura can post",
		Explain: "A pay run Nemura sent with its proof that Fyniq refused (a locked period, an account the mapping lacks, a department Fyniq does not have, a proof that does not verify) waits in Nemura with the code and posts nothing. " +
			"Each run whose newest refusal no posted version has followed is listed by run and code. Put right what the code says (the plain words are on the run in Nemura), then send it again or approve a new version.",
		Rules: "FR-PPL-04, FR-PPL-09, NM-INT-03", Owner: "controller", View: viewPayroll, Module: "features.payroll", Triggers: onPayroll, Run: payLinesCannotPost},
	{Key: "payroll.pay-lines-to-review", Ref: "C.1 #20", Area: "Payroll", Worst: Warning, Title: "Salary payment lines wait for a decision",
		Explain: "A salary batch that is not cancelled with payment lines that wait: a person held for tax clearance, a person whose bank details changed since the line was approved, or a payment whose outcome is unknown. " +
			"Each batch is listed with how many lines wait and why, never who or how much (FR-PPL-17). Release the free lines, wait for Nemura to lift a hold, approve the batch again or record what became of the payment.",
		Rules: "FR-PPL-12, FR-PPL-13, FR-PPL-14", Owner: "controller", View: viewPayroll, Module: "features.payroll", Triggers: onPayroll, Run: payLinesToReview},
	{Key: "payroll.cpf-due", Ref: "C.2 #14", Area: "Payroll", Worst: Due, Title: "CPF is paid by the 14th",
		Explain: "The CPF contribution of each wage month is due on the 14th of the month after (CPF Board). Each month's, due within 14 days or past with no payment recorded on the statutory calendar, is listed. " +
			"Pay it, then record it on the calendar with the CPF Board's acknowledgement. It is the CPF line of the calendar check C.2 #15, shown here for payroll.",
		Rules: "FR-TAX-07, CMP-02", Owner: "accountant", View: viewCalendar, Triggers: []string{"document", "organisation_setting", "organisation"}, Packs: []string{"sg-deadlines"}, Run: cpfDue},
	{Key: "payroll.held-payment-released", Ref: "C.2 #44", Area: "Payroll", Worst: Error, Title: "No payment to a person under a tax-clearance hold was released",
		Explain: "A salary payment released to a person who was held for tax clearance when it was released (IR21) breaks the hold, which only Nemura's release lifts after IRAS's notice or directive. " +
			"Fyniq refuses such a release; this finds one that went through a hold that reached Fyniq late, or a payment made outside a batch.",
		Rules: "FR-PAY-10, FR-PPL-13", Owner: "controller", View: viewPayroll, Module: "features.payroll", Triggers: onPayroll, Run: heldPaymentReleased},
}

func monthWords(m time.Time) string { return m.Format("January 2006") }

// payrollInNemura says payroll lives in Nemura: the link is active and its source for payroll is Nemura's.
func payrollInNemura(ctx context.Context, u storage.UnitOfWork) (bool, error) {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil || l.State != storage.LinkActive {
		return false, err
	}
	sources, err := u.PeopleLinks().Sources(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range sources {
		if s.Feature == storage.FeaturePayroll {
			return s.Source == storage.SourceNemura, nil
		}
	}
	return false, nil
}

// payrollMonths are the wage months with payroll in the books or on its way: a pay run posted, a salary batch that is not cancelled, a provider's journal imported.
func payrollMonths(ctx context.Context, u storage.UnitOfWork) ([]time.Time, error) {
	set := map[time.Time]bool{}
	months, err := u.PayrollRuns().ReceiptMonths(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range months {
		set[m] = true
	}
	batches, err := u.SalaryBatches().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range batches {
		if b.Status != storage.SalaryCancelled {
			set[b.WageMonth] = true
		}
	}
	imports, err := u.PayrollOutside().Imports(ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range imports {
		set[i.Period] = true
	}
	out := make([]time.Time, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out, nil
}

// C.2 #45: each month with payroll whose gate items (FR-PAY-15) lack an arrangement in force.
func arrangementsRecorded(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	months, err := payrollMonths(ctx, u)
	if err != nil {
		return Result{}, err
	}
	inNemura, err := payrollInNemura(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, m := range months {
		have, err := u.PayrollOutside().Arrangements(ctx, m)
		if err != nil {
			return Result{}, err
		}
		met := map[string]bool{}
		for _, a := range have {
			met[a.Item] = true
		}
		if inNemura {
			met[storage.ArrangePayslips], met[storage.ArrangeKETs] = true, true
		}
		var missing []string
		for _, item := range payrollimport.GateItems {
			if !met[item] {
				missing = append(missing, payrollimport.Words[item])
			}
		}
		if len(missing) > 0 {
			items = append(items, Item{Kind: "payroll_month", ID: m.Format("2006-01"), Label: monthWords(m), Detail: "Not recorded: " + strings.Join(missing, ", "), Version: strings.Join(missing, ",")})
		}
	}
	if len(items) == 0 {
		return Fine("Every month with payroll has how its statutory outputs are met recorded"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d month(s) with payroll have an output with no arrangement recorded", len(items)),
		"Record how each is met, with its evidence, in Settings, People and HR (FR-PAY-15)"), nil
}

// C.1 #21: a wage month with a pay run posted whose salary accounts also have postings that are not the pay run's.
func salariesCountedTwice(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	months, err := u.PayrollRuns().ReceiptMonths(ctx)
	if err != nil {
		return Result{}, err
	}
	mapping, err := u.PayrollRuns().Mapping(ctx)
	if errors.Is(err, ports.ErrNotFound) || len(months) == 0 {
		return Fine("No pay run has been posted, so no salary can be counted twice"), nil
	}
	if err != nil {
		return Result{}, err
	}
	accounts := map[uuid.UUID]string{}
	for k, acc := range mapping.Entries {
		if strings.HasPrefix(k, "department:") {
			accounts[acc] = ""
		}
	}
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, a := range all {
		if _, ok := accounts[a.ID]; ok {
			accounts[a.ID] = a.Code
		}
	}
	var items []Item
	for _, m := range months {
		from, to := m, m.AddDate(0, 1, -1)
		for acc, code := range accounts {
			lines, err := u.Ledger().AccountLines(ctx, acc, from, to, 500)
			if err != nil {
				return Result{}, err
			}
			n := 0
			for _, l := range lines {
				if l.Source.Type != "payroll_run" && l.ReversesEntryID == nil {
					n++
				}
			}
			if n > 0 {
				items = append(items, Item{Kind: "account", ID: acc.String(), Label: code + " in " + monthWords(m), Version: m.Format("2006-01") + "/" + code,
					Detail: fmt.Sprintf("%d posting(s) on this salary account in a month whose pay run is posted, that are not the pay run's", n)})
			}
		}
	}
	if len(items) == 0 {
		return Fine("No salary account of a posted pay run's month has a posting besides the pay run's"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d salary account(s) have postings besides a posted pay run's", len(items)),
		"Check each posting: move or reverse the one that repeats the pay run"), nil
}

// C.2 #44: a release of a salary batch to a person who is held now, whose hold began before the release.
func heldPaymentReleased(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	batches, err := u.SalaryBatches().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, b := range batches {
		releases, err := u.SalaryBatches().Releases(ctx, b.ID)
		if err != nil {
			return Result{}, err
		}
		if len(releases) == 0 {
			continue
		}
		at := map[uuid.UUID]time.Time{}
		for _, r := range releases {
			at[r.ID] = r.ReleasedAt
		}
		lines, err := u.SalaryBatches().Lines(ctx, b.ID)
		if err != nil {
			return Result{}, err
		}
		person := map[uuid.UUID]uuid.UUID{}
		for _, l := range lines {
			person[l.LineRef] = l.PersonID
		}
		rl, err := u.SalaryBatches().ReleaseLines(ctx, b.ID)
		if err != nil {
			return Result{}, err
		}
		n := 0
		for _, x := range rl {
			p, err := u.PeopleLinks().Person(ctx, person[x.LineRef])
			if err != nil {
				return Result{}, err
			}
			if p.Held && p.HoldFrom != nil && !at[x.ReleaseID].Before(*p.HoldFrom) {
				n++
			}
		}
		if n > 0 {
			items = append(items, Item{Kind: "salary_batch", ID: b.ID.String(), Label: fmt.Sprintf("SB-%04d", b.Seq), Version: fmt.Sprint(n),
				Detail: fmt.Sprintf("%d payment(s) were released to people who were under a tax-clearance hold when they were released", n)})
		}
	}
	if len(items) == 0 {
		return Fine("No salary payment was released to a person under a tax-clearance hold"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d salary batch(es) released payments to people under a hold", len(items)),
		"Tell the accountant at once: the hold is lifted only by Nemura after IRAS's notice or directive; ask Nemura for the hold's record and recover what was paid if IRAS requires it"), nil
}

// C.2 #14: the CPF obligations of the statutory calendar due within 14 days or past with nothing recorded (the CPF line of C.2 #15).
func cpfDue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	c, err := deadlines.Read(ctx, u, e.Today, e.Today.AddDate(0, 0, deadlines.SoonDays), e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	due := false
	for _, it := range c.Items {
		switch {
		case it.Obligation == "sg-deadlines":
			return Missing("an sg-deadlines version in force", ""), nil
		case it.Obligation != "cpf":
			continue
		case it.Status == deadlines.Overdue || it.Status == deadlines.DueSoon:
			due = true
		case it.Status != deadlines.NotAssessed:
			continue
		}
		items = append(items, calendarItem(it, e.Today))
	}
	if len(items) == 0 {
		return Fine("No CPF payment is due within 14 days or past without its payment recorded"), nil
	}
	status := Warning
	if due {
		status = Due
	}
	return Found(status, items, fmt.Sprintf("%d CPF payment(s) to make", len(items)), "Pay it, then record it on the statutory calendar with the CPF Board's acknowledgement"), nil
}

// C.1 #19: the pay runs Fyniq refused and nothing posted since, by run and code.
func payLinesCannotPost(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	open, err := u.PayrollRuns().OpenRefusals(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(open) == 0 {
		return Fine("Every pay run sent from Nemura that Fyniq refused has posted since, or none was refused"), nil
	}
	var items []Item
	for _, r := range open {
		items = append(items, Item{Kind: "payroll_run", ID: r.RunID.String(), Label: fmt.Sprintf("Pay run %s, version %d", r.RunID.String()[:8], r.Version),
			Detail: fmt.Sprintf("Refused with %s on %s: nothing has posted since", r.Code, r.RefusedAt.Format("2 Jan 2006")), Version: fmt.Sprint(r.Version)})
	}
	return Found(Error, items, fmt.Sprintf("%d pay run(s) cannot post", len(items)), "Put right what each code says, then send the run again from Nemura or approve a new version"), nil
}

// C.1 #20: the salary batches with payment lines that wait: a person held, bank details changed since the line was approved, or an unknown outcome. Counts and reasons, never a person or an amount.
func payLinesToReview(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	batches, err := u.SalaryBatches().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, b := range batches {
		if b.Status == storage.SalaryCancelled {
			continue
		}
		lines, err := u.SalaryBatches().Lines(ctx, b.ID)
		if err != nil {
			return Result{}, err
		}
		events, err := u.SalaryBatches().Events(ctx, b.ID)
		if err != nil {
			return Result{}, err
		}
		latest := map[uuid.UUID]string{}
		for _, ev := range events {
			latest[ev.LineRef] = ev.State
		}
		held, changed, unknown := 0, 0, 0
		seen := map[uuid.UUID]bool{}
		for _, l := range lines {
			if seen[l.LineRef] {
				continue
			}
			seen[l.LineRef] = true
			switch latest[l.LineRef] {
			case "paid", "rejected", "returned", "released", "sent", "accepted":
				continue
			case "unknown":
				unknown++
				continue
			}
			p, err := u.PeopleLinks().Person(ctx, l.PersonID)
			if err != nil {
				return Result{}, err
			}
			switch {
			case p.Known() && p.Held:
				held++
			case p.Known() && p.BankVersion > l.BankDetailVersion:
				changed++
			}
		}
		if n := held + changed + unknown; n > 0 {
			items = append(items, Item{Kind: "salary_batch", ID: b.ID.String(), Label: fmt.Sprintf("Salary batch SB%04d", b.Seq),
				Detail: fmt.Sprintf("%d line(s) wait: %d held for tax clearance, %d with new bank details, %d with an unknown outcome", n, held, changed, unknown), Version: fmt.Sprint(n)})
		}
	}
	if len(items) == 0 {
		return Fine("No salary payment line waits for a decision"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d salary batch(es) have lines that wait", len(items)), "Release the free lines, wait for Nemura to lift a hold, approve the batch again, or record what became of the payment"), nil
}
