//go:build local

package local

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// RateFiles is the local daily rate feed (FR-GL-05, ADR-T4): a directory with one CSV file per date, named
// 2026-10-16.csv, whose header is base,currency,quote,form and each row one published rate, such as
// SGD,USD,1.2953,base-per-unit or SGD,JPY,0.8521,base-per-100. A date with no file published nothing. The production
// feed (MAS) arrives at the cloud gate; this one is for local mode and tests, with synthetic rates only.
type RateFiles struct {
	Dir string
}

var _ ports.RateFeed = RateFiles{}

// Name implements ports.RateFeed: the source every rate from these files records.
func (RateFiles) Name() string { return "Local file" }

// Fetch implements ports.RateFeed.
func (f RateFiles) Fetch(_ context.Context, day time.Time) ([]ports.RateQuote, error) {
	path := filepath.Join(f.Dir, day.Format(time.DateOnly)+".csv")
	file, err := os.Open(path) // #nosec G304 -- the configured rates directory and a date: local mode only
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if got := strings.Join(rows[0], ","); got != "base,currency,quote,form" {
		return nil, fmt.Errorf("%s: the header is %q, not base,currency,quote,form", path, got)
	}
	out := make([]ports.RateQuote, 0, len(rows)-1)
	for i, r := range rows[1:] {
		if len(r) != 4 {
			return nil, fmt.Errorf("%s: row %d has %d fields, not 4", path, i+2, len(r))
		}
		out = append(out, ports.RateQuote{Base: strings.TrimSpace(r[0]), Currency: strings.TrimSpace(r[1]), Quote: strings.TrimSpace(r[2]),
			Form: strings.TrimSpace(r[3])})
	}
	return out, nil
}
