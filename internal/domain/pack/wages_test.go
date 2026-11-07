package pack_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

func shipped(t *testing.T, dir string) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func month(t *testing.T, s string) time.Time {
	t.Helper()
	m, err := time.ParseInLocation("2006-01", s, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// refused loads the shipped version in dir with edits and wants the code among its problems.
func refused(t *testing.T, dir string, edits map[string]string, code string) {
	t.Helper()
	_, err := pack.Load(fixtureOf(t, dir, edits), dir)
	var ps pack.Problems
	if !errors.As(err, &ps) {
		t.Fatalf("%v loaded; want %s", edits, code)
	}
	for _, p := range ps {
		if p.Code == code {
			return
		}
	}
	t.Errorf("%v: got %v; want %s", edits, ps, code)
}

// req: ENG-30
// req: QA-ENG-08
// req: FR-PLN-03
// The start-up and partial exemptions take each tier's share of the band after the tiers before it, exactly, and the
// rebate follows IRAS's rule for the cash grant; a YA the version does not hold is not guessed.
func TestTheCorporateTaxPack(t *testing.T) {
	v := shipped(t, "sg-cit/2026.1")
	for _, c := range []struct {
		scheme, chargeable, want string
	}{
		{pack.SchemeStartUp, "199999.99", "124999.995"}, // 75,000 + 50% of 99,999.99
		{pack.SchemeStartUp, "-500.00", "0"},            // a loss exempts nothing
		{pack.SchemePartial, "10000.01", "7500.005"},
	} {
		got, err := pack.CITExemption(v, c.scheme, 2026, dec(t, c.chargeable))
		if err != nil || got.Cmp(dec(t, c.want)) != 0 {
			t.Errorf("%s on %s: got %v, %v; want %s", c.scheme, c.chargeable, got, err, c.want)
		}
	}
	if _, err := pack.CITExemption(v, "other", 2026, dec(t, "1.00")); !errors.Is(err, pack.ErrNoYA) {
		t.Errorf("an unknown scheme: got %v; want ErrNoYA", err)
	}
	if got, err := pack.CITRebate(v, 2026, dec(t, "0"), true); err != nil || got.Sign() != 0 {
		t.Errorf("no tax: got %v, %v; want no rebate (the cash grant is paid apart, IRAS's Example 1)", got, err)
	}
	if got, err := pack.CITRebate(v, 2020, dec(t, "100000.00"), true); err != nil || got.Cmp(dec(t, "15000.00")) != 0 {
		t.Errorf("YA 2020 had no cash grant: got %v, %v; want the capped 15000.00", got, err)
	}
	for _, ya := range []int{2021, 2022, 2023, 2027} {
		if _, err := pack.CITRebate(v, ya, dec(t, "1000.00"), false); !errors.Is(err, pack.ErrNoYA) {
			t.Errorf("YA %d has no rebate: got %v", ya, err)
		}
	}
	const dir = "sg-cit/2026.1"
	refused(t, dir, map[string]string{dir + "/exemptions.csv": "sute,2020,,2,→sute,2020,,3,"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/exemptions.csv": "pte,2020,,1,10000.00,0.75→pte,2020,,1,10000,0.75"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/rebates.csv": "2025,0.50→2026,0.50"}, pack.CodeDates)
	refused(t, dir, map[string]string{dir + "/rates.csv": "2020,,0.17→2020,,17"}, pack.CodeValue)
	// A wrong rate fails its hand-pinned case, so it cannot be activated (ENG-32).
	wrong, err := pack.Load(fixtureOf(t, dir, map[string]string{dir + "/rebates.csv": "2026,0.50,40000.00→2026,0.40,30000.00"}), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Failed(pack.RunPinned(wrong))) == 0 {
		t.Error("YA 2026's rebate as Budget 2026 first announced it (40%, $30,000) passed the pinned cases of the enhanced rebate")
	}
	if ps := pack.CheckActivation(wrong, pack.Activation{ProposedBy: uuid.New(), ApprovedBy: uuid.New(), Approval: uuid.New()}); len(ps) == 0 {
		t.Error("a version failing its pinned cases was activated")
	}
}

// req: ENG-30
// req: ENG-31
// req: QA-ENG-08
// req: FR-PLN-02
// The rates are chosen by the month the wages are earned in, the band and the wage; the graduated range and earlier
// months are not held, so no rate is guessed for them.
func TestTheCPFPack(t *testing.T) {
	v := shipped(t, "sg-cpf/2026.1")
	for _, c := range []struct {
		month       string
		age         int
		wage        string
		er, ee      string
		notHeld     bool
		description string
	}{
		{"2026-12", 60, "5000.00", "0.16", "0.18", false, "60 is in above 55 to 60"},
		{"2026-12", 61, "5000.00", "0.125", "0.125", false, "61 is above 60"},
		{"2027-06", 65, "5000.00", "0.13", "0.13", false, "65 is in above 60 to 65, from 2027"},
		{"2027-06", 70, "5000.00", "0.09", "0.075", false, "70 is in above 65 to 70"},
		{"2026-06", 30, "750.01", "0.17", "0.20", false, "a cent above $750"},
		{"2026-06", 30, "500.00", "", "", true, "the graduated range"},
	} {
		got, err := pack.CPFRatesOn(v, month(t, c.month), pack.GroupSCSPR3, c.age, dec(t, c.wage))
		switch {
		case c.notHeld:
			if !errors.Is(err, pack.ErrNotHeld) {
				t.Errorf("%s: got %v, %v; want ErrNotHeld", c.description, got, err)
			}
		case err != nil || got.Employer.Cmp(dec(t, c.er)) != 0 || got.Employee.Cmp(dec(t, c.ee)) != 0:
			t.Errorf("%s: got %v, %v; want %s / %s", c.description, got, err, c.er, c.ee)
		}
	}
	if _, err := pack.CPFRatesOn(v, month(t, "2026-06"), "spr1", 30, dec(t, "5000.00")); !errors.Is(err, pack.ErrNotHeld) {
		t.Errorf("a first-year permanent resident: got %v; want ErrNotHeld until sg-cpf@2026.2", err)
	}
	const dir = "sg-cpf/2026.1"
	refused(t, dir, map[string]string{dir + "/rates.csv": "sc_spr3,55,60,750.00,2026-01-01,2026-12-31→sc_spr3,55,60,750.00,2026-01-01,2027-01-31"}, pack.CodeDates)
	refused(t, dir, map[string]string{dir + "/rates.csv": "sc_spr3,65,70→sc_spr3,70,65"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/ceilings.csv": "ow_monthly,→aw_monthly,"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/rates.csv": ",0.09,0.075,→,9,7.5,"}, pack.CodeValue)
}

// req: ENG-30
// req: QA-ENG-08
// req: FR-PLN-02
// The levy is the rate of the wages within its minimum and maximum, exactly; no wages, no levy.
func TestTheSDLPack(t *testing.T) {
	v := shipped(t, "sg-sdl/2026.1")
	for wages, want := range map[string]string{"0": "0", "799.99": "2.00", "800.00": "2.00", "1234.56": "3.0864", "4500.01": "11.25"} {
		got, err := pack.SDLOn(v, month(t, "2026-06"), dec(t, wages))
		if err != nil || got.Cmp(dec(t, want)) != 0 {
			t.Errorf("SDL on %s: got %v, %v; want %s", wages, got, err, want)
		}
	}
	const dir = "sg-sdl/2026.1"
	refused(t, dir, map[string]string{dir + "/levy.csv": ",2.00,11.25,→,12.00,11.25,"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/levy.csv": "2026-01-01,,→2026-13-01,,"}, pack.CodeDates)
}
