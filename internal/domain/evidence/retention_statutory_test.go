package evidence

import (
	"testing"
	"time"
)

// statutoryMinimums are the latest of the periods the law asks a record of a financial year to be kept for (PRD-001 §14.2,
// "Record keeping"; dates as the PRD records them from ACRA, Feb 2026, and IRAS, Jan and Sep 2026): the Companies Act's
// s199, five years after the financial year's end; income tax's, five years from the relevant Year of Assessment, which
// IRAS's examples work out as the 31 December five years after the year of the financial year's end; and GST's, five years
// from the end of the period, a period never ending after the financial year does.
func statutoryMinimums(fyEnd time.Time) (companiesAct, incomeTax, gst time.Time) {
	companiesAct = fyEnd.AddDate(5, 0, 0)
	incomeTax = time.Date(fyEnd.Year()+5, 12, 31, 0, 0, 0, 0, fyEnd.Location())
	gst = fyEnd.AddDate(5, 0, 0)
	return
}

// The default retention, 7 years after the financial year's end, covers every statutory period for every month a
// financial year can end in; IRAS's two examples (the year to 31 December 2018 and the year to 30 September 2018) are both
// kept until 31 December 2023 by the income tax rule, and the default keeps them longer.
// req: compliance/singapore-statutory Record keeping and retention Statutory retention periods are covered
// req: compliance/singapore-statutory Record keeping and retention IRAS income tax examples
// req: NFR-RET-01
func TestTheDefaultRetentionCoversEveryStatutoryPeriod(t *testing.T) {
	sgt := time.FixedZone("SGT", 8*3600)
	end := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, sgt) }

	// IRAS's examples, as the PRD records them.
	for name, fyEnd := range map[string]time.Time{"the year to 31 December 2018": end(2018, 12, 31), "the year to 30 September 2018": end(2018, 9, 30)} {
		if _, tax, _ := statutoryMinimums(fyEnd); !tax.Equal(end(2023, 12, 31)) {
			t.Errorf("%s: income tax keeps records until %s; IRAS's example says 31 December 2023", name, tax.Format(time.DateOnly))
		}
	}

	// Every month a year can start in, over several years: a record committed anywhere in the year is kept past each period.
	for start := 1; start <= 12; start++ {
		for year := 2024; year <= 2030; year++ {
			at := time.Date(year, time.Month(start), 15, 12, 0, 0, 0, sgt)
			keep := RetainUntil(start, RecordsYears(), at, sgt)
			fyEnd := time.Date(at.Year(), time.Month(start), 1, 0, 0, 0, 0, sgt).AddDate(1, 0, -1)
			companies, tax, gst := statutoryMinimums(fyEnd)
			for name, minimum := range map[string]time.Time{"the Companies Act": companies, "income tax": tax, "GST": gst} {
				if keep.Before(minimum) {
					t.Errorf("a record of the year starting %d/%d is kept until %s, before %s's %s", start, year, keep.Format(time.DateOnly), name, minimum.Format(time.DateOnly))
				}
			}
		}
	}
	if RecordsYears() != 7 {
		t.Errorf("the default is %d years after the financial year's end; PRD-001 §14.2 says 7", RecordsYears())
	}
}
