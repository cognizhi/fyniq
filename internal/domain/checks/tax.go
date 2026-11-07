package checks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/gstmonitor"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the GST checks' items.
const viewGST = "GST return, filing"

// The checks of GST and the statutory calendar (p1-gst).
var taxChecks = []Definition{
	{Key: "gst.registration-threshold", Ref: "C.1 #37", Area: "Tax", Worst: Warning, Title: "The GST registration threshold is not reached or expected",
		Explain: "IRAS asks a business to register for GST when its taxable supplies are more than $1 million in a calendar year " +
			"(apply by 30 January, registered from 1 March), or are expected to be in the next 12 months (apply within 30 days of the " +
			"forecast). The check warns when a test is met, and earlier, once the year's taxable supplies so far reach the warning " +
			"percentage of the threshold (80% unless changed).",
		Rules: "FR-TAX-05", Owner: "accountant", View: viewGST,
		Triggers: []string{"document", "journal_entry", "gst_registration", "organisation_setting"}, Run: registrationThreshold},
	{Key: "gst.return-due", Ref: "C.2 #13", Area: "Tax", Worst: Due, Title: "Every GST return is filed by its due day",
		Explain: "A GST F5 return and its payment are due one month after the end of the accounting period (IRAS). Each period that " +
			"has ended without a filing recorded on the statutory calendar, due within 7 days or past, is listed. Record the " +
			"filing with IRAS's acknowledgement on the calendar.",
		Rules: "FR-TAX-07, CMP-02", Owner: "accountant", View: viewCalendar,
		Triggers: []string{"document", "gst_registration", "organisation_setting"}, Run: returnsDue},
	{Key: "compliance.deadlines", Ref: "C.2 #15", Area: "Compliance", Worst: Due, Title: "No statutory deadline is missed",
		Explain: "ECI, Form C-S/C, the annual return, the AGM or the statements to members, employment income (IR8A, AIS), CPF and " +
			"applying for GST registration, from the statutory calendar: each due within 14 days or past, with nothing recorded, is " +
			"listed. The registers' own deadlines are on C.2 #22 and C.2 #27, and GST returns on C.2 #13.",
		Rules: "FR-TAX-07, CMP-02", Owner: "accountant", View: viewCalendar,
		Triggers: []string{"document", "gst_registration", "organisation_setting", "organisation"}, Run: deadlinesDue},
}

// The Appendix D row whose view opens the calendar's items.
const viewCalendar = "Statutory calendar: deadlines, and filings recorded as done (rev 0.211)"

// calendarItem is an item of the calendar as a check lists it, linked to the calendar.
func calendarItem(it deadlines.Item, today time.Time) Item {
	detail := ""
	switch it.Status {
	case deadlines.Overdue:
		detail = fmt.Sprintf("Due on %s, %d day(s) ago, with nothing recorded", it.Due.Format("2 Jan 2006"), int(today.Sub(it.Due).Hours()/24))
	case deadlines.NotAssessed:
		detail = "Not assessed: " + it.Reason
	default:
		detail = fmt.Sprintf("Due on %s, in %d day(s)", it.Due.Format("2 Jan 2006"), int(it.Due.Sub(today).Hours()/24))
	}
	if it.Why != "" {
		detail += ". " + it.Why
	}
	out := Item{Kind: "statutory_deadline", ID: it.Key, Label: it.Title, Detail: detail, Version: string(it.Status)}
	if it.Status == deadlines.NotAssessed {
		out.Severity = string(Warning)
	}
	return out
}

// C.2 #13: each GST return period ended with no filing recorded, due within 7 days or past (design D9).
func returnsDue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(regs) == 0 {
		return Fine("Not registered for GST: no return is due"), nil
	}
	c, err := deadlines.Read(ctx, u, e.Today, e.Today.AddDate(0, 0, 7), e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, it := range c.Items {
		if it.Obligation == "sg-deadlines" {
			return Missing("an sg-deadlines version in force", ""), nil
		}
		if it.Obligation != "gst_f5" || (it.Status != deadlines.Overdue && it.Status != deadlines.DueSoon && it.Status != deadlines.NotAssessed) {
			continue
		}
		if !it.Anchor.Before(e.Today) {
			continue // the period has not ended
		}
		items = append(items, calendarItem(it, e.Today))
	}
	if len(items) == 0 {
		return Fine("No GST return is due within 7 days or past without its filing recorded"), nil
	}
	return Found(Due, items, fmt.Sprintf("%d GST return(s) to file", len(items)),
		"File each return with IRAS, then record it on the statutory calendar with IRAS's acknowledgement"), nil
}

// C.2 #15: each of the calendar's items other than GST returns and the registers', due within 14 days or past with
// nothing recorded, overdue first; an item not assessed is listed first and named (design D9).
func deadlinesDue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
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
		case it.Obligation == "financial_year":
			return Missing("the books start and the first financial year's end", "Settings › Organisation"), nil
		case it.Register || it.Obligation == "gst_f5":
			continue
		case it.Status == deadlines.Overdue || it.Status == deadlines.DueSoon:
			due = true
		case it.Status != deadlines.NotAssessed:
			continue
		}
		items = append(items, calendarItem(it, e.Today))
	}
	if len(items) == 0 {
		return Fine("No statutory deadline is due within 14 days or past without its filing recorded"), nil
	}
	status := Warning
	if due {
		status = Due
	}
	slices.SortStableFunc(items, func(a, b Item) int { return strings.Compare(a.Severity, b.Severity) * -1 })
	return Found(status, items, fmt.Sprintf("%d statutory deadline(s) to meet", len(items)),
		"File or pay each one, then record it on the statutory calendar with its acknowledgement, or as not required with the reason"), nil
}

// dayText writes a date as the checks do, or says the pack gives none.
func dayText(t *time.Time) string {
	if t == nil {
		return "the day the pack gives"
	}
	return t.Format("2 Jan 2006")
}

// C.1 #37: the GST registration monitor on today (design D4, D9): ⚠ when a test is met while the organisation is not
// registered, or when the year so far reaches the warning percentage; a document whose lines could not be read for their
// codes is listed for review, as it is left out of the figure.
func registrationThreshold(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	m, err := gstmonitor.Monitor(ctx, u, e.Today)
	if err != nil {
		return Result{}, err
	}
	switch m.Status {
	case gstmonitor.StatusRegistered:
		return Fine(m.Reason + ": the monitor has nothing to test"), nil
	case gstmonitor.StatusNotAssessed:
		return Missing(m.Reason, gstmonitor.WarningSetting), nil
	}
	threshold := "S$" + m.Threshold.String()
	var items []Item
	if t := m.LastYear; t.Exceeded {
		items = append(items, Item{Kind: "gst_monitor", ID: "last_year", Label: fmt.Sprintf("%d: taxable supplies S$%s", t.Year, t.Supplies.Total),
			Detail:  fmt.Sprintf("More than the %s threshold: apply to register by %s; registered from %s (IRAS)", threshold, dayText(t.ApplyBy), dayText(t.StartsOn)),
			Version: t.Supplies.Total.String()})
	}
	if t := m.ThisYear; t.Exceeded {
		items = append(items, Item{Kind: "gst_monitor", ID: "this_year", Label: fmt.Sprintf("%d so far: taxable supplies S$%s", t.Year, t.Supplies.Total),
			Detail:  fmt.Sprintf("Already more than the %s threshold: apply to register by %s; registered from %s (IRAS)", threshold, dayText(t.ApplyBy), dayText(t.StartsOn)),
			Version: t.Supplies.Total.String()})
	} else if m.Status == gstmonitor.StatusNear || (m.Status == gstmonitor.StatusLiable && !t.Exceeded && mustWarn(m)) {
		items = append(items, Item{Kind: "gst_monitor", ID: "this_year", Label: fmt.Sprintf("%d so far: taxable supplies S$%s", t.Year, t.Supplies.Total),
			Detail: fmt.Sprintf("%s%% of the %s threshold, at or above the warning at %s%%: record a forecast if more sales are certain, and get ready to register",
				m.Share, threshold, m.Warning), Version: t.Supplies.Total.String()})
	}
	if f := m.Forecast; f != nil && f.Exceeded {
		items = append(items, Item{Kind: "gst_monitor", ID: "forecast", Label: fmt.Sprintf("Forecast of %s: S$%s", f.ForecastOn.Format("2 Jan 2006"), f.Value),
			Detail: fmt.Sprintf("More than the %s threshold in the 12 months to %s: apply to register by %s; registered from %s (IRAS)", threshold,
				f.Until().Format("2 Jan 2006"), dayText(f.ApplyBy), dayText(f.StartsOn)), Version: f.Doc.ID.String()})
	}
	for _, t := range []*gstmonitor.YearTest{m.LastYear, m.ThisYear} {
		for _, id := range t.Supplies.Unmatched {
			items = append(items, Item{Kind: "document", ID: id.String(), Label: fmt.Sprintf("A sales document of %d", t.Year),
				Detail:   "Its entry's lines do not match its own, so its GST codes cannot be read: it is left out of the taxable supplies. Ask for help",
				Severity: string(Warning)})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("This year's taxable supplies so far, S$%s, are %s%% of the %s threshold; no recorded forecast is above it",
			m.ThisYear.Supplies.Total, m.Share, threshold)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d point(s) on GST registration to review", len(items)),
		"Apply to register with IRAS by the day given, or record why the test is not met; then record the registration in Settings › Organisation"), nil
}

// mustWarn is whether the year so far reaches the warning percentage while a test is met another way.
func mustWarn(m gstmonitor.Result) bool {
	near, err := gstmonitor.Reaches(m.ThisYear.Supplies.Total, m.Threshold, m.Warning)
	return err == nil && near
}
