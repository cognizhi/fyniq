// Package clock implements the Clock port (DEV-05). It is the only package allowed to call
// time.Now(); internal/archtest enforces that.
package clock

import (
	"fmt"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/runtime"
)

// Singapore is the business time zone (PRD-001: the books run on SGT).
var Singapore = mustLoad("Asia/Singapore")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// System reads the wall clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }

// Frozen is a clock for tests: it stays where it is set until moved.
type Frozen struct {
	mu sync.Mutex
	t  time.Time
}

// NewFrozen returns a clock stopped at t.
func NewFrozen(t time.Time) *Frozen { return &Frozen{t: t} }

// Now returns the frozen time.
func (f *Frozen) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Set moves the clock to t.
func (f *Frozen) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
}

// Advance moves the clock forward by d.
func (f *Frozen) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// BusinessDate keeps the base clock's time of day in Singapore but on a fixed date, so period
// close, CPF age bands and deadlines can be exercised on any date outside production.
type BusinessDate struct {
	base ports.Clock
	date time.Time // midnight SGT of the business date
}

// Now returns the business date at the base clock's Singapore time of day.
func (b BusinessDate) Now() time.Time {
	n := b.base.Now().In(Singapore)
	y, m, d := b.date.Date()
	return time.Date(y, m, d, n.Hour(), n.Minute(), n.Second(), n.Nanosecond(), Singapore)
}

// New returns the clock a service uses. businessDate is FYNIQ_BUSINESS_DATE (YYYY-MM-DD); it is
// refused in production with a start-up error, and honoured everywhere else (DEV-05).
func New(env runtime.Env, businessDate string) (ports.Clock, error) {
	if businessDate == "" {
		return System{}, nil
	}
	if env == runtime.EnvProduction {
		return nil, &runtime.Error{Code: "CLOCK-01", Message: "FYNIQ_BUSINESS_DATE is set in production: remove it; the business date override exists only outside production"}
	}
	d, err := time.ParseInLocation("2006-01-02", businessDate, Singapore)
	if err != nil {
		return nil, &runtime.Error{Code: "CLOCK-02", Message: fmt.Sprintf("FYNIQ_BUSINESS_DATE=%q is not a date: use YYYY-MM-DD, for example 2026-12-31", businessDate)}
	}
	return BusinessDate{base: System{}, date: d}, nil
}
