package planning

import (
	"errors"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Packs are the statutory figures from the organisation's active versions of sg-cit, sg-cpf and sg-sdl.
type Packs struct {
	CITPack, CPFPack, SDLPack pack.Version
}

// fullRateWage is a wage above S$750: the forecast applies the full rate to every salary with CPF, as the workbook.
var fullRateWage = dec("750.01")

// fullRateAge is the age the forecast takes for everyone: the band of 55 and below, as the workbook.
const fullRateAge = 55

func (p Packs) EmployerCPF(month time.Time) (rate, ceiling *apd.Decimal, err error) {
	r, err := pack.CPFRatesOn(p.CPFPack, month, pack.GroupSCSPR3, fullRateAge, fullRateWage)
	if err != nil {
		return nil, nil, err
	}
	ceiling, err = pack.CPFCeiling(p.CPFPack, pack.CeilingOWMonthly, month)
	return r.Employer, ceiling, err
}

func (p Packs) SDL(month time.Time, wages *apd.Decimal) (*apd.Decimal, error) {
	return pack.SDLOn(p.SDLPack, month, wages)
}

func (p Packs) TaxRate(ya int) (*apd.Decimal, error) { return pack.CITRate(p.CITPack, ya) }

func (p Packs) Exemption(scheme string, ya int, chargeable *apd.Decimal) (*apd.Decimal, error) {
	return pack.CITExemption(p.CITPack, scheme, ya, chargeable)
}

// Rebate is the YA's rebate for a company not counted as eligible for the cash grant: the percentage of the tax, at
// most the cap (the cash grant is part of the same cap, paid apart); none for a YA without one.
func (p Packs) Rebate(ya int, tax *apd.Decimal) (*apd.Decimal, error) {
	r, err := pack.CITRebate(p.CITPack, ya, tax, false)
	if errors.Is(err, pack.ErrNoYA) {
		return apd.New(0, 0), nil
	}
	return r, err
}
