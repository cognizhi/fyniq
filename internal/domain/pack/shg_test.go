package pack_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// req: ENG-30
// req: QA-ENG-08
// req: FR-PLN-02
// The self-help group contributions are chosen by the fund, the month and the wage band, "up to" including its top and
// "above" starting a cent over it; no wages, an unlisted fund and a month before the version are not guessed.
func TestTheSHGPack(t *testing.T) {
	v := shipped(t, "sg-shg/2026.1")
	for _, c := range []struct{ fund, month, wages, want string }{
		{pack.FundCDAC, "2026-06", "0.01", "0.50"},
		{pack.FundCDAC, "2026-06", "7500.00", "2.00"},
		{pack.FundCDAC, "2026-06", "7500.01", "3.00"},
		{pack.FundECF, "2027-03", "10000.00", "16.00"},
		{pack.FundECF, "2027-03", "10000.01", "20.00"},
		{pack.FundMBMF, "2026-06", "3000.00", "6.50"},
		{pack.FundMBMF, "2026-06", "3000.01", "15.00"},
		{pack.FundSINDA, "2026-06", "15000.00", "18.00"},
		{pack.FundSINDA, "2026-06", "15000.01", "30.00"},
	} {
		got, err := pack.SHGOn(v, month(t, c.month), c.fund, dec(t, c.wages))
		if err != nil || got.Cmp(dec(t, c.want)) != 0 {
			t.Errorf("%s on %s in %s: got %v, %v; want %s", c.fund, c.wages, c.month, got, err, c.want)
		}
	}
	for _, c := range []struct{ fund, month, wages string }{
		{pack.FundCDAC, "2026-06", "0"},
		{pack.FundCDAC, "2026-06", "-1.00"},
		{pack.FundCDAC, "2025-12", "3000.00"},
		{"CCC", "2026-06", "3000.00"},
	} {
		if _, err := pack.SHGOn(v, month(t, c.month), c.fund, dec(t, c.wages)); !errors.Is(err, pack.ErrNotHeld) {
			t.Errorf("%+v: got %v; want ErrNotHeld", c, err)
		}
	}
	const dir = "sg-shg/2026.1"
	// A band that does not start where the one before ended, a missing top band, a fund twice and a wrong basis are refused.
	refused(t, dir, map[string]string{dir + "/bands.csv": "CDAC,2000.00,3500.00→CDAC,2100.00,3500.00"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/bands.csv": "CDAC,7500.00,,2026-01-01,,3.00,cpf-shg→CDAC,7500.00,9999.00,2026-01-01,,3.00,cpf-shg"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/bands.csv": "CDAC,,2000.00,2026-01-01,,0.50→CDAC,,2000.00,2026-01-01,,0.5"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/funds.csv": "ECF,Eurasian Association,race→CDAC,Eurasian Association,race"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/funds.csv": "MBMF,Majlis Ugama Islam Singapura (Muis),religion→MBMF,Majlis Ugama Islam Singapura (Muis),faith"}, pack.CodeValue)
	refused(t, dir, map[string]string{dir + "/bands.csv": "CDAC,,2000.00,2026-01-01,,0.50,cpf-shg→CDAC,,2000.00,2026-01-01,,0.50,cpf-unknown"}, pack.CodeSource)
	// A wrong amount fails its hand-pinned case, so it cannot be activated (ENG-32).
	wrong, err := pack.Load(fixtureOf(t, dir, map[string]string{dir + "/bands.csv": "MBMF,3000.00,4000.00,2026-01-01,,15.00→MBMF,3000.00,4000.00,2026-01-01,,6.50"}), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Failed(pack.RunPinned(wrong))) == 0 {
		t.Error("an MBMF band edited to the amount below it passed the pinned cases")
	}
	if ps := pack.CheckActivation(wrong, pack.Activation{ProposedBy: uuid.New(), ApprovedBy: uuid.New(), Approval: uuid.New()}); len(ps) == 0 {
		t.Error("a version failing its pinned cases was activated")
	}
}
