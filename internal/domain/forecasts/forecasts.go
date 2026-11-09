package forecasts

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BufferSetting is the organisation's cash buffer in months of burn (FR-PLN-03, the owner's decision of 8 Nov 2026).
const BufferSetting = "planning.buffer_months"

// Error is a refusal in plain words (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Encode is a plan as a document version keeps it.
func Encode(p ForecastPlan) ([]byte, error) { return json.Marshal(p) }

// Read reads a forecast's version as it was saved.
func Read(doc storage.Document) (ForecastPlan, error) {
	var p ForecastPlan
	err := json.Unmarshal(doc.Payload, &p)
	return p, err
}

// tidy trims a plan's names and gives it nil-free lists.
func tidy(p ForecastPlan) ForecastPlan {
	p.Name = strings.TrimSpace(p.Name)
	for i := range p.Scenarios {
		p.Scenarios[i].Name = strings.TrimSpace(p.Scenarios[i].Name)
	}
	for i := range p.Opex {
		p.Opex[i].Label = strings.TrimSpace(p.Opex[i].Label)
		p.Opex[i].Group = strings.TrimSpace(p.Opex[i].Group)
	}
	for _, l := range []*[]ForecastOpex{&p.Opex} {
		if *l == nil {
			*l = []ForecastOpex{}
		}
	}
	if p.Headcount == nil {
		p.Headcount = []ForecastHire{}
	}
	if p.Capex == nil {
		p.Capex = []ForecastCapex{}
	}
	if p.Funding == nil {
		p.Funding = []ForecastFunding{}
	}
	return p
}

// checkIn is everything wrong with a plan in u: Check, and a report group the chart does not hold, or a last actual
// month after the month before today.
func checkIn(ctx context.Context, u storage.UnitOfWork, p ForecastPlan, base money.Currency, today time.Time) error {
	probs := Check(p, base)
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return err
	}
	groups := map[string]bool{}
	for _, a := range accounts {
		if !a.Group && (a.Type == storage.Expense || a.Type == storage.Revenue) && a.ReportGroup != "" {
			groups[strings.TrimSpace(a.ReportGroup)] = true
		}
	}
	for i, o := range p.Opex {
		if o.Group != "" && !groups[o.Group] {
			probs = append(probs, ledger.Problem{Code: CodePlan, Field: fmt.Sprintf("opex[%d].report_group", i),
				Message: fmt.Sprintf("the chart has no income or expense report group %q", o.Group),
				Fix:     "Name a report group of the chart, such as Sales & marketing, or leave it out"})
		}
	}
	if p.LastActual != "" {
		if m, err := Month(p.LastActual); err == nil && !m.Before(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)) {
			probs = append(probs, ledger.Problem{Code: CodePlan, Field: "last_actual", Message: "the month is not over yet",
				Fix: "Name a month before this one, or leave it out for the last closed month"})
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftIn saves a plan as a draft forecast in u: a sandbox to change and run.
func DraftIn(ctx context.Context, u storage.UnitOfWork, p ForecastPlan, base money.Currency, today time.Time) (storage.Document, error) {
	p = tidy(p)
	if err := checkIn(ctx, u, p, base, today); err != nil {
		return storage.Document{}, err
	}
	b, err := Encode(p)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentForecast, Payload: b})
}

// Drafted reads a forecast not recorded yet, its row locked: not found for any other document.
func Drafted(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentForecast {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "the forecast is recorded, so it changes no more", "Copy it into a new forecast and record that; the latest recorded is Home's"}
	}
	return doc, nil
}

// ReviseIn saves a new version of a forecast not recorded yet, in u.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, p ForecastPlan, base money.Currency, today time.Time) (storage.Document, error) {
	if _, err := Drafted(ctx, u, id); err != nil {
		return storage.Document{}, err
	}
	p = tidy(p)
	if err := checkIn(ctx, u, p, base, today); err != nil {
		return storage.Document{}, err
	}
	b, err := Encode(p)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordIn records a drafted forecast in u: checked again, then the company's own; it posts nothing (FR-PLN-08) and
// changes no more. The latest recorded is the one Home reads.
func RecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, base money.Currency, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	p, err := Read(doc)
	if err != nil {
		return doc, err
	}
	if err := checkIn(ctx, u, p, base, today); err != nil {
		return doc, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// Recorded lists the recorded forecasts, the latest recorded first.
func Recorded(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	var out []storage.Document
	f := storage.DocumentFilter{Type: storage.DocumentForecast, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < f.Limit {
			break
		}
		f.AfterDate, f.AfterID = page[len(page)-1].Date, &page[len(page)-1].ID
	}
	at := func(d storage.Document) time.Time {
		if d.PostedAt != nil {
			return *d.PostedAt
		}
		return time.Time{}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && at(out[j]).After(at(out[j-1])); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// Run is one scenario of a forecast, with or without its funding plan.
type Run struct {
	Scenario    string
	WithFunding bool
	Result      planning.Result
}

// Results are a forecast worked out on a day: its books' figures, the pack versions it read, and its runs.
type Results struct {
	Books  Books
	Packs  []string
	Buffer int // the organisation's buffer, planning.buffer_months, on the day
	Runs   []Run
}

// PacksOn are the sg-cit, sg-cpf and sg-sdl versions a forecast starting on start reads: the organisation's active
// versions in force then, else this release's (ENG-31).
func PacksOn(ctx context.Context, u storage.UnitOfWork, start time.Time) (planning.Packs, []string, error) {
	var out planning.Packs
	var ids []string
	for _, name := range []string{"sg-cit", "sg-cpf", "sg-sdl"} {
		v, ok, err := gst.PackOn(ctx, u, name, start)
		if err != nil {
			return out, nil, err
		}
		if !ok {
			return out, nil, &planning.Error{Code: planning.CodeStatutory, Field: "packs",
				Message: fmt.Sprintf("no %s version is in force on %s", name, start.Format("2 Jan 2006")), Fix: "Activate the " + name + " pack"}
		}
		switch name {
		case "sg-cit":
			out.CITPack = v
		case "sg-cpf":
			out.CPFPack = v
		case "sg-sdl":
			out.SDLPack = v
		}
		ids = append(ids, v.ID)
	}
	return out, ids, nil
}

// BufferOn is the organisation's buffer months on day.
func BufferOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (int, error) {
	def, _ := settings.Lookup(BufferSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(settings.On(def, changes, day).Value)
}

// Work works a checked plan out on today, from the books and the packs: every scenario, or only the one named, each
// with and without its funding plan (FR-PLN-03, design D4, D5). It saves nothing.
func Work(ctx context.Context, u storage.UnitOfWork, p ForecastPlan, base money.Currency, today time.Time, only string) (Results, error) {
	var out Results
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return out, err
	}
	last, err := LastActual(periods, today, p.LastActual)
	if err != nil {
		return out, err
	}
	if out.Books, err = ReadBooks(ctx, u, base, last, p.Horizon); err != nil {
		return out, err
	}
	packs, ids, err := PacksOn(ctx, u, out.Books.Start)
	if err != nil {
		return out, err
	}
	out.Packs = ids
	if out.Buffer, err = BufferOn(ctx, u, today); err != nil {
		return out, err
	}
	in := Inputs(p, base, out.Books.Start, out.Books.Figures, out.Buffer)
	for i, o := range p.Opex {
		if out.Books.SalesGroups[o.Group] {
			in.Opex[i].Group = planning.GroupSalesMarketing
		}
	}
	found := false
	for _, s := range p.Scenarios {
		if only != "" && !strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(only)) {
			continue
		}
		found = true
		sc := ScenarioOf(s)
		for _, with := range []bool{true, false} {
			x := in
			if !with {
				x = planning.WithoutFunding(in)
			}
			r, err := planning.Run(x, sc, packs)
			if err != nil {
				return out, err
			}
			out.Runs = append(out.Runs, Run{Scenario: sc.Name, WithFunding: with, Result: r})
		}
	}
	if !found {
		return out, &Error{CodePlan, fmt.Sprintf("the forecast has no scenario %q", only), "Name one of its scenarios, such as Base"}
	}
	return out, nil
}
