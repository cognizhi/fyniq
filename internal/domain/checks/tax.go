package checks

import (
	"context"
	"fmt"
	"time"

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
			Detail: fmt.Sprintf("More than the %s threshold: apply to register by %s; registered from %s (IRAS)", threshold, dayText(t.ApplyBy), dayText(t.StartsOn)),
			Version: t.Supplies.Total.String()})
	}
	if t := m.ThisYear; t.Exceeded {
		items = append(items, Item{Kind: "gst_monitor", ID: "this_year", Label: fmt.Sprintf("%d so far: taxable supplies S$%s", t.Year, t.Supplies.Total),
			Detail: fmt.Sprintf("Already more than the %s threshold: apply to register by %s; registered from %s (IRAS)", threshold, dayText(t.ApplyBy), dayText(t.StartsOn)),
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
				Detail: "Its entry's lines do not match its own, so its GST codes cannot be read: it is left out of the taxable supplies. Ask for help",
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
