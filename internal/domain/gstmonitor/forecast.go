// Package gstmonitor is the GST registration monitor (PRD-001 FR-TAX-05, C.1 #37; p1-gst design D3 and D4): the taxable
// supplies of a calendar year read from the posted documents, the forecast of the next 12 months a person records with
// its evidence, and the two tests against the sg-gst pack's threshold, with the warning the owner chose at 80% of it
// (rev 0.208). It reads the ledger and the documents, so package gst, which the documents import, does not.
package gstmonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The monitor's codes.
const (
	CodeForecast   = "GST-30" // a forecast that does not fit: its day, value or basis
	CodeNoEvidence = "GST-31" // a forecast recorded without its evidence
	CodeRecorded   = "GST-32" // a forecast recorded already changes no more
)

// MaxBasis is the longest basis a forecast keeps.
const MaxBasis = 1000

// Error is a refusal in plain words (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// SGD is the currency of the threshold and of a forecast.
var SGD = money.MustLookup("SGD")

// Forecast is the taxable supplies a person expects in the 12 months from ForecastOn (design D4), with its basis. IRAS
// asks for supporting documents, such as signed contracts or confirmed orders, so it is recorded only with its evidence
// attached; a later one recorded supersedes it.
type Forecast struct {
	Date       time.Time // the day it was drafted
	ForecastOn time.Time // the day of the forecast: its 12 months run from it
	Value      money.Money
	Basis      string
}

type forecastPayload struct {
	Date       string      `json:"date"`
	ForecastOn string      `json:"forecast_on"`
	Value      money.Money `json:"value"`
	Basis      string      `json:"basis"`
}

// Until is the last day of the forecast's 12 months.
func (f Forecast) Until() time.Time { return f.ForecastOn.AddDate(1, 0, -1) }

// Encode is a forecast as a document version keeps it.
func Encode(f Forecast) ([]byte, error) {
	return json.Marshal(forecastPayload{Date: f.Date.Format(time.DateOnly), ForecastOn: f.ForecastOn.Format(time.DateOnly), Value: f.Value, Basis: f.Basis})
}

// Read reads a forecast's version as it was saved.
func Read(doc storage.Document) (Forecast, error) {
	var p forecastPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Forecast{}, err
	}
	out := Forecast{Value: p.Value, Basis: p.Basis}
	var err error
	if out.ForecastOn, err = time.Parse(time.DateOnly, p.ForecastOn); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// Check is what is wrong with a forecast on its own, every problem at once (ENG-22): no day or one after today, a
// value that is not Singapore dollars above zero, or a basis blank or too long.
func Check(f Forecast, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeForecast, Field: field, Message: msg, Fix: fix})
	}
	switch {
	case f.ForecastOn.IsZero():
		add("forecast_on", "the forecast's day is missing", "Enter the day the forecast was made")
	case !today.IsZero() && f.ForecastOn.After(today):
		add("forecast_on", "the forecast is dated after today", "Enter the day it was made, today or earlier")
	}
	switch {
	case f.Value.Currency() != SGD:
		add("value", fmt.Sprintf("the value is in %s", f.Value.Currency()), "Enter the value in Singapore dollars, as the threshold is")
	case f.Value.Sign() <= 0:
		add("value", "the value is not above zero", "Enter the taxable supplies expected in the 12 months, such as 1200000.00")
	}
	switch n := utf8.RuneCountInString(strings.TrimSpace(f.Basis)); {
	case n == 0:
		add("basis", "the basis is blank", "Say what the forecast rests on, such as the signed contracts with their customers")
	case n > MaxBasis:
		add("basis", fmt.Sprintf("the basis is longer than %d characters", MaxBasis), "Shorten it; attach the contracts as evidence")
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftIn saves a forecast to record in u: a draft, to attach its evidence to.
func DraftIn(ctx context.Context, u storage.UnitOfWork, f Forecast, today time.Time) (storage.Document, error) {
	f.Date = today
	f.Basis = strings.TrimSpace(f.Basis)
	if probs := Check(f, today); probs != nil {
		return storage.Document{}, probs
	}
	b, err := Encode(f)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentGSTForecast, Payload: b})
}

// Drafted reads a forecast not recorded yet, its row locked: not found for any other document.
func Drafted(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentGSTForecast {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "the forecast is recorded, so it changes no more", "Record a new forecast; the latest recorded is the one the monitor reads"}
	}
	return doc, nil
}

// ReviseIn saves a new version of a forecast not recorded yet, in u.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, f Forecast, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	was, err := Read(doc)
	if err != nil {
		return doc, err
	}
	f.Date, f.Basis = was.Date, strings.TrimSpace(f.Basis)
	if probs := Check(f, today); probs != nil {
		return doc, probs
	}
	b, err := Encode(f)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordIn records a drafted forecast in u: checked again, then recorded once a file is attached (IRAS's supporting
// documents). From then on it changes no more.
func RecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	f, err := Read(doc)
	if err != nil {
		return doc, err
	}
	if probs := Check(f, time.Time{}); probs != nil {
		return doc, probs
	}
	n, err := attached(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if n == 0 {
		return doc, &Error{CodeNoEvidence, "the forecast has no evidence attached",
			"Attach what it rests on, such as the signed contracts or confirmed orders IRAS asks for, then record it"}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// attached counts the files attached to a document and not removed.
func attached(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	n, after := 0, uuid.Nil
	for {
		page, err := u.Evidence().OfDocument(ctx, id, after, 200)
		if err != nil {
			return 0, err
		}
		for _, e := range page {
			if e.State != storage.EvidenceRemoved {
				n++
			}
			after = e.ID
		}
		if len(page) < 200 {
			return n, nil
		}
	}
}

// Recorded is a recorded forecast with its document.
type Recorded struct {
	Doc storage.Document
	Forecast
}

// Forecasts lists the recorded forecasts, the latest recorded first.
func Forecasts(ctx context.Context, u storage.UnitOfWork) ([]Recorded, error) {
	var out []Recorded
	f := storage.DocumentFilter{Type: storage.DocumentGSTForecast, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, doc := range page {
			x, err := Read(doc)
			if err != nil {
				return nil, err
			}
			out = append(out, Recorded{Doc: doc, Forecast: x})
		}
		if len(page) < f.Limit {
			break
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}
	latestFirst(out)
	return out, nil
}

// latestFirst orders forecasts by when they were recorded, the latest first; by their day where that is unknown.
func latestFirst(rs []Recorded) {
	at := func(r Recorded) time.Time {
		if r.Doc.PostedAt != nil {
			return *r.Doc.PostedAt
		}
		return r.Date
	}
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && at(rs[j]).After(at(rs[j-1])); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// InForce is the latest recorded forecast whose 12 months hold day, or false.
func InForce(rs []Recorded, day time.Time) (Recorded, bool) {
	for _, r := range rs {
		if !day.Before(r.ForecastOn) && !day.After(r.Until()) {
			return r, true
		}
	}
	return Recorded{}, false
}
