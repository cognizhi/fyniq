package clock

import (
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/runtime"
)

// req: DEV-05
func TestFrozenClockStaysPut(t *testing.T) {
	at := time.Date(2026, 10, 13, 9, 12, 0, 0, Singapore)
	c := NewFrozen(at)
	if !c.Now().Equal(at) || !c.Now().Equal(at) {
		t.Fatalf("frozen clock moved: %v", c.Now())
	}
	c.Advance(90 * time.Minute)
	if want := at.Add(90 * time.Minute); !c.Now().Equal(want) {
		t.Fatalf("Advance: got %v, want %v", c.Now(), want)
	}
}

// req: DEV-05
func TestBusinessDateKeepsTimeOfDay(t *testing.T) {
	base := NewFrozen(time.Date(2026, 10, 13, 9, 12, 30, 0, Singapore))
	b := BusinessDate{base: base, date: time.Date(2026, 12, 31, 0, 0, 0, 0, Singapore)}
	if got, want := b.Now(), time.Date(2026, 12, 31, 9, 12, 30, 0, Singapore); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// req: DEV-05
func TestBusinessDateHonouredOutsideProduction(t *testing.T) {
	for _, env := range []runtime.Env{runtime.EnvLocal, runtime.EnvCI, runtime.EnvDev, runtime.EnvStaging} {
		c, err := New(env, "2026-12-31")
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		if y, m, d := c.Now().In(Singapore).Date(); y != 2026 || m != 12 || d != 31 {
			t.Fatalf("%s: business date not applied: %v", env, c.Now())
		}
	}
}

// req: DEV-05
func TestBusinessDateRefusedInProduction(t *testing.T) {
	_, err := New(runtime.EnvProduction, "2026-12-31")
	var re *runtime.Error
	if !errors.As(err, &re) || re.Code != "CLOCK-01" {
		t.Fatalf("want CLOCK-01 refusal, got %v", err)
	}
}

func TestBusinessDateMustBeADate(t *testing.T) {
	_, err := New(runtime.EnvLocal, "31/12/2026")
	var re *runtime.Error
	if !errors.As(err, &re) || re.Code != "CLOCK-02" {
		t.Fatalf("want CLOCK-02, got %v", err)
	}
}

func TestNoBusinessDateIsTheSystemClock(t *testing.T) {
	c, err := New(runtime.EnvProduction, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(System); !ok {
		t.Fatalf("want System clock, got %T", c)
	}
}
