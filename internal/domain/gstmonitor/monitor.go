package gstmonitor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Status is what the monitor found on a day.
type Status string

// The monitor's statuses (design D4).
const (
	StatusRegistered  Status = "registered"   // registered for GST on the day: the monitor has nothing to test
	StatusLiable      Status = "liable"       // a test is met: the business must apply to register
	StatusNear        Status = "near"         // the calendar year so far reached the warning percentage of the threshold
	StatusBelow       Status = "below"        // neither
	StatusNotAssessed Status = "not_assessed" // an input is missing, which Reason names
)

// WarningSetting is the setting of the warning percentage (the owner's decision of 7 Nov 2026).
const WarningSetting = "gst.threshold_warning_percent"

// Dates are when a business liable by a test must apply by and is registered from, by the sg-deadlines pack (D2);
// nil where the pack in force gives none.
type Dates struct {
	ApplyBy, StartsOn *time.Time
}

// YearTest is a calendar year's taxable supplies against the threshold: the year so far while it runs.
type YearTest struct {
	Year     int
	Supplies Supplies
	Exceeded bool // more than the threshold
	Dates
}

// ForecastTest is the forecast in force against the threshold.
type ForecastTest struct {
	Recorded
	Exceeded bool
	Dates
}

// Result is the monitor on a day.
type Result struct {
	On           time.Time
	Status       Status
	Reason       string // why it is not assessed, or since when it is registered
	Registration *storage.GSTRegistration
	Threshold    money.Money // zero while not assessed
	Warning      string      // the warning percentage, such as "80"
	LastYear     *YearTest
	ThisYear     *YearTest
	Share        string // the year so far as a percentage of the threshold (RND-PERCENT), for showing only
	Forecast     *ForecastTest
	Forecasts    []Recorded // every recorded forecast, the latest first
}

// Monitor tests, on today, whether the organisation must register for GST (FR-TAX-05; design D4): last calendar year's
// taxable supplies and this year's so far against the threshold of the sg-gst version in force today (the
// retrospective test), and the forecast in force (the prospective test). It warns once the year so far reaches the
// warning percentage. A registered organisation is not tested.
func Monitor(ctx context.Context, u storage.UnitOfWork, today time.Time) (Result, error) {
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	out := Result{On: today, Status: StatusNotAssessed}
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return out, err
	}
	if r, ok := gst.StatusOn(regs, today); ok {
		out.Status, out.Registration = StatusRegistered, &r
		out.Reason = "Registered for GST from " + r.RegisteredFrom.Format("2 Jan 2006")
		return out, nil
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return out, err
	}
	if base != SGD {
		out.Reason = fmt.Sprintf("The base currency is %s; the threshold is in Singapore dollars", base)
		return out, nil
	}
	v, err := gst.Version(ctx, u, today)
	if err != nil {
		return out, err
	}
	amount, cur, ok := pack.Limit(v, pack.LimitRegistrationThreshold, today)
	if !ok || cur != SGD.Code {
		out.Reason = fmt.Sprintf("%s holds no registration threshold in force on %s", v.ID, today.Format("2 Jan 2006"))
		return out, nil
	}
	if out.Threshold, err = money.ParseStored(amount, SGD); err != nil {
		return out, err
	}
	def, _ := settings.Lookup(WarningSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return out, err
	}
	out.Warning = settings.On(def, changes, today).Value
	dv, hasDates, err := gst.PackOn(ctx, u, "sg-deadlines", today)
	if err != nil {
		return out, err
	}
	dates := func(apply, starts string, anchor time.Time) Dates {
		var d Dates
		if !hasDates {
			return d
		}
		if due, err := pack.DueOn(dv, apply, anchor, nil); err == nil {
			d.ApplyBy = &due.On
		}
		if due, err := pack.DueOn(dv, starts, anchor, nil); err == nil {
			d.StartsOn = &due.On
		}
		return d
	}
	year := func(y int, to time.Time) (*YearTest, error) {
		from := time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
		s, err := TaxableSupplies(ctx, u, SGD, from, to)
		if err != nil {
			return nil, err
		}
		c, err := s.Total.Cmp(out.Threshold)
		if err != nil {
			return nil, err
		}
		t := &YearTest{Year: y, Supplies: s, Exceeded: c > 0}
		if t.Exceeded {
			t.Dates = dates("gst_register_retrospective", "gst_starts_retrospective", time.Date(y, time.December, 31, 0, 0, 0, 0, time.UTC))
		}
		return t, nil
	}
	if out.LastYear, err = year(today.Year()-1, time.Date(today.Year()-1, time.December, 31, 0, 0, 0, 0, time.UTC)); err != nil {
		return out, err
	}
	if out.ThisYear, err = year(today.Year(), today); err != nil {
		return out, err
	}
	if out.Share, err = rounding.Percent(out.ThisYear.Supplies.Total, out.Threshold); err != nil {
		return out, err
	}
	if out.Forecasts, err = Forecasts(ctx, u); err != nil {
		return out, err
	}
	if f, ok := InForce(out.Forecasts, today); ok {
		c, err := f.Value.Cmp(out.Threshold)
		if err != nil {
			return out, err
		}
		out.Forecast = &ForecastTest{Recorded: f, Exceeded: c > 0}
		if out.Forecast.Exceeded {
			out.Forecast.Dates = dates("gst_register_prospective", "gst_starts_prospective", f.ForecastOn)
		}
	}
	near, err := Reaches(out.ThisYear.Supplies.Total, out.Threshold, out.Warning)
	if err != nil {
		return out, err
	}
	switch {
	case out.LastYear.Exceeded || out.ThisYear.Exceeded || (out.Forecast != nil && out.Forecast.Exceeded):
		out.Status = StatusLiable
	case near:
		out.Status = StatusNear
	default:
		out.Status = StatusBelow
	}
	return out, nil
}

var exact = apd.BaseContext.WithPrecision(60)

// Reaches reports whether supplies reach percent of threshold, exactly: supplies × 100 ≥ threshold × percent. The
// percentage shown is rounded; this comparison never is.
func Reaches(supplies, threshold money.Money, percent string) (bool, error) {
	p, _, err := apd.NewFromString(percent)
	if err != nil {
		return false, fmt.Errorf("the warning percentage %q: %w", percent, err)
	}
	if p.Negative {
		return false, errors.New("the warning percentage is negative")
	}
	var left, right apd.Decimal
	if _, err := exact.Mul(&left, supplies.Decimal(), apd.New(100, 0)); err != nil {
		return false, err
	}
	if _, err := exact.Mul(&right, threshold.Decimal(), p); err != nil {
		return false, err
	}
	return left.Cmp(&right) >= 0, nil
}
