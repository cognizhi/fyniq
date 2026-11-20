package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- the usage view (FR-ADM-05; p2-mobile-approvals-automation design D7)

func (u *unitOfWork) Usage() storage.Usage { return usage{u} }

type usage struct{ u *unitOfWork }

func (r usage) Months(ctx context.Context, since time.Time) ([]storage.UsageMonth, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	first := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	by := map[time.Time]*storage.UsageMonth{}
	at := func(m time.Time) *storage.UsageMonth {
		k := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
		if by[k] == nil {
			by[k] = &storage.UsageMonth{Month: k}
		}
		return by[k]
	}
	ai, err := r.u.q.UsageAIByMonth(ctx, first)
	if err != nil {
		return nil, mapError(err)
	}
	for _, x := range ai {
		m := at(x.Month)
		m.AICalls += x.Calls
		m.InputTokens += x.InputTokens
		m.OutputTokens += x.OutputTokens
		if x.Currency != "" && x.Cost != "" && x.Cost != "0" {
			m.AICost = append(m.AICost, storage.UsageCost{Currency: x.Currency, Amount: storage.Decimal(x.Cost)})
		}
	}
	ex, err := r.u.q.UsageExportsByMonth(ctx, first)
	if err != nil {
		return nil, mapError(err)
	}
	for _, x := range ex {
		m := at(x.Month)
		m.Exports, m.ExportBytes = x.Exports, x.Bytes
	}
	ev, err := r.u.q.UsageEvidenceByMonth(ctx, first)
	if err != nil {
		return nil, mapError(err)
	}
	for _, x := range ev {
		m := at(x.Month)
		m.EvidenceFiles, m.EvidenceBytes = x.Files, x.Bytes
	}
	out := make([]storage.UsageMonth, 0, len(by))
	for _, m := range by {
		out = append(out, *m)
	}
	// earliest first
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Month.Before(out[j-1].Month); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (r usage) Stored(ctx context.Context) (storage.StoredUsage, error) {
	if err := r.u.check(); err != nil {
		return storage.StoredUsage{}, err
	}
	var out storage.StoredUsage
	ev, err := r.u.q.UsageStoredEvidence(ctx)
	if err != nil {
		return out, mapError(err)
	}
	cp, err := r.u.q.UsageStoredCaptures(ctx)
	if err != nil {
		return out, mapError(err)
	}
	ex, err := r.u.q.UsageStoredExports(ctx)
	if err != nil {
		return out, mapError(err)
	}
	out.EvidenceFiles, out.EvidenceBytes = ev.Files, ev.Bytes
	out.CaptureFiles, out.CaptureBytes = cp.Files, cp.Bytes
	out.ExportFiles, out.ExportBytes = ex.Files, ex.Bytes
	return out, nil
}
