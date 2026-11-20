package invoicenow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The settings the requirement reads (p2-invoicenow design D5, D6).
const (
	SettingApplies    = "invoicenow.applies"
	SettingESCategory = "invoicenow.es_category"
)

// ErrNoPack is the population's answer while the organisation has no sg-invoicenow version in force on the day: which
// documents the requirement covers is not known, so the gate is not assessed with that reason, never empty.
var ErrNoPack = errors.New("no sg-invoicenow version is active on the return's last day")

// Entry is a document of a return period that the requirement may make Fyniq submit (FR-TAX-10).
type Entry struct {
	Item
	Direction    string // sale or purchase
	Counterparty uuid.UUID
	// Excluded is set when every line the return reports is an excluded transaction by the pack's rule, such as an import
	// permit: Reason says which. No person records it, and it never needs a submission.
	Excluded bool
	Reason   Reason
	// NeedsChoice marks a document with an exempt-supply line while the tax agent has not chosen ES33 or ESN33.
	NeedsChoice bool
}

// Population reads the posted sales (invoices and credit notes) and purchases (bills) of the days from to to, and keeps
// those with a line the return reports: the lines' GST codes decide, by the sg-invoicenow version in force on to (an
// invoice with one line in scope is submitted whole: IRAS e-Tax Guide 7.4). Opening items, which precede the books, are
// left out.
func Population(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) ([]Entry, error) {
	v, ok, err := gst.PackOn(ctx, u, "sg-invoicenow", to)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoPack
	}
	choice, err := Setting(ctx, u, SettingESCategory, to)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, t := range []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote, storage.DocumentBill} {
		filter := storage.DocumentFilter{Type: t, Status: storage.DocumentPosted, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Limit: 200}
		for {
			page, err := u.Documents().Page(ctx, filter)
			if err != nil {
				return nil, err
			}
			for _, doc := range page {
				e, ok, err := classify(v, base, doc, choice)
				if err != nil {
					return nil, err
				}
				if ok {
					out = append(out, e)
				}
			}
			if len(page) < filter.Limit {
				break
			}
			last := page[len(page)-1]
			filter.AfterDate, filter.AfterID = last.Date, &last.ID
		}
	}
	return out, nil
}

// classify is a document's entry, or false when no line of it is in the return.
func classify(v pack.Version, base money.Currency, doc storage.Document, choice string) (Entry, bool, error) {
	var codes []string
	e := Entry{Item: Item{DocumentID: doc.ID.String(), Number: doc.Number}}
	switch doc.Type {
	case storage.DocumentBill:
		d, err := bill.Read(doc, base)
		if err != nil {
			return e, false, err
		}
		if d.OpeningItem {
			return e, false, nil
		}
		for _, l := range d.Lines {
			codes = append(codes, l.GSTCode)
		}
		e.Direction, e.Kind, e.Date, e.Counterparty = "purchase", "purchase", d.Date, d.SupplierID
	default:
		d, err := invoice.Read(doc, base)
		if err != nil {
			return e, false, err
		}
		if d.OpeningItem {
			return e, false, nil
		}
		for _, l := range d.Lines {
			codes = append(codes, l.GSTCode)
		}
		e.Direction, e.Kind, e.Date, e.Counterparty = "sale", "sale", d.Date, d.CustomerID
		if invoice.IsCreditNote(doc) {
			e.Kind = "credit_note"
		}
	}
	inScope, excludedReason := false, Reason("")
	for _, c := range codes {
		r, ok := pack.InvoiceNow(v, c)
		if !ok {
			return e, false, fmt.Errorf("the GST code %q has no treatment in %s", c, v.ID)
		}
		switch r.Scope {
		case pack.ScopeSubmit:
			inScope = true
		case pack.ScopeChoose:
			inScope = true
			e.NeedsChoice = choice == "none" || choice == ""
		case pack.ScopeExclude:
			excludedReason = Reason(r.Reason)
		}
	}
	switch {
	case inScope:
		return e, true, nil
	case excludedReason != "":
		e.Excluded, e.Reason = true, excludedReason
		return e, true, nil
	}
	return e, false, nil
}

// Setting is a setting's value on a day.
func Setting(ctx context.Context, u storage.UnitOfWork, key string, on time.Time) (string, error) {
	def, ok := settings.Lookup(key)
	if !ok {
		return "", fmt.Errorf("no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return "", err
	}
	return settings.On(def, changes, on).Value, nil
}

// GateResult is the gate for a return period with what it was read from.
type GateResult struct {
	Result
	From, To time.Time
	// NoPack is the reason the gate is not assessed when the organisation has no sg-invoicenow version active.
	NoPack bool
	// Entries are the population: its documents, in date order.
	Entries []Entry
	// Exclusions are the recorded exclusions by document, and Rules the exclusions the pack's rule makes.
	Recorded map[string]storage.InvoiceExclusion
}

// Gate is FR-TAX-11's gate for the days from to to: every document of the population has an acknowledged latest
// submission, an exclusion (recorded, or by the pack's rule) or an approved contingency that covers the period. While the
// requirement is not marked as applying, the gate is not assessed and blocks nothing.
func Gate(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) (GateResult, error) {
	out := GateResult{From: from, To: to, Recorded: map[string]storage.InvoiceExclusion{}}
	applies, err := Setting(ctx, u, SettingApplies, to)
	if err != nil {
		return out, err
	}
	entries, err := Population(ctx, u, base, from, to)
	if errors.Is(err, ErrNoPack) {
		out.NoPack = true
		out.Result = Evaluate(false, nil, nil, nil, nil)
		return out, nil
	}
	if err != nil {
		return out, err
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return compareStrings(a.Number, b.Number)
	})
	out.Entries = entries
	ids := make([]uuid.UUID, 0, len(entries))
	for _, e := range entries {
		id, err := uuid.Parse(e.DocumentID)
		if err != nil {
			return out, err
		}
		ids = append(ids, id)
	}
	excl, err := u.InvoiceNow().Exclusions(ctx, ids)
	if err != nil {
		return out, err
	}
	excluded := map[string]Reason{}
	for _, x := range excl {
		excluded[x.DocumentID.String()] = Reason(x.Reason)
		out.Recorded[x.DocumentID.String()] = x
	}
	var items []Item
	for _, e := range entries {
		if e.Excluded {
			if _, ok := excluded[e.DocumentID]; !ok {
				excluded[e.DocumentID] = e.Reason
			}
			continue
		}
		items = append(items, e.Item)
	}
	subs, err := u.InvoiceNow().Latest(ctx, ids)
	if err != nil {
		return out, err
	}
	var latest []Submission
	for _, s := range subs {
		latest = append(latest, Submission{DocumentID: s.DocumentID.String(), Version: s.Version, Status: Status(s.Status), Ack: s.IRASAck})
	}
	cs, err := u.InvoiceNow().Contingencies(ctx, from, to)
	if err != nil {
		return out, err
	}
	var cont *Contingency
	for _, c := range cs {
		// it covers the whole period, and has its approver (a pending request lets nothing through)
		if c.ApprovedBy != "" && !c.PeriodFrom.After(from) && !c.PeriodTo.Before(to) {
			cont = &Contingency{RequestedBy: c.RequestedBy, ApprovedBy: c.ApprovedBy, Reason: c.Reason, Evidence: 1}
			break
		}
	}
	out.Result = Evaluate(applies == "true", items, latest, excluded, cont)
	return out, nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
