package evalsuite

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/seed"
)

// FromSeed works out a golden fact's value from the sample company itself, in integer cents and without the engine or
// the semantic layer (golden rule 3), so a suite's expected values are checked against something other than what they
// test. account_movement is an account's debits less its credits in the days; revenue is the revenue accounts' (codes
// 4…) credits less their debits. The first journal counts as seedload posts it: on its day, and reversed the day after.
func FromSeed(c seed.Company, w Want) (string, error) {
	from, err1 := time.Parse(time.DateOnly, w.From)
	to, err2 := time.Parse(time.DateOnly, w.To)
	if err1 != nil || err2 != nil {
		return "", fmt.Errorf("%s: its days are not dates", w.Measure)
	}
	var in func(code string) bool
	sign := int64(1)
	switch w.Measure {
	case "account_movement":
		in = func(code string) bool { return code == w.Account }
	case "revenue":
		in, sign = func(code string) bool { return strings.HasPrefix(code, "4") }, -1
	default:
		return "", fmt.Errorf("the seed does not work out %s yet", w.Measure)
	}
	var cents int64
	add := func(day time.Time, l seed.Line, by int64) error {
		if day.Before(from) || day.After(to) || !in(l.Account) {
			return nil
		}
		dr, err := centsOf(l.Debit)
		if err != nil {
			return err
		}
		cr, err := centsOf(l.Credit)
		if err != nil {
			return err
		}
		cents += by * sign * (dr - cr)
		return nil
	}
	for i, j := range c.Journals {
		day, err := time.Parse(time.DateOnly, j.Date)
		if err != nil {
			return "", fmt.Errorf("journal %s: %w", j.ID, err)
		}
		for _, l := range j.Lines {
			if err := add(day, l, 1); err != nil {
				return "", err
			}
			if i == 0 {
				if err := add(day.AddDate(0, 0, 1), l, -1); err != nil {
					return "", err
				}
			}
		}
	}
	neg := ""
	if cents < 0 {
		neg, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", neg, cents/100, cents%100), nil
}

// centsOf reads an amount of two decimals, such as 126927.84, as cents.
func centsOf(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	whole, frac, _ := strings.Cut(s, ".")
	frac = (frac + "00")[:2]
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return n, nil
}
