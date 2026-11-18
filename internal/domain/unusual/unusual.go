// Package unusual is FM-6 at launch (PRD-001 rev 0.287): in Founder mode, the journals and payments of the last week
// that rules find unusual go into the weekly review (FM-3) as "unusual" entries, each with its reason, for a founder to
// review. It is advisory: it flags, and never approves, holds or changes anything. The AI's explanation of each follows
// after go-live, as a scheduled agent.
package unusual

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the nightly work that reviews each organisation's last week.
const Job = "review.unusual"

// Reviewer is the scheduler's principal that logs unusual items: the job review.unusual.
var Reviewer = storage.Actor{Type: "scheduler", ID: Job}

// The rules' bounds.
const (
	lookback   = 7 * 24 * time.Hour   // what a night looks at: a missed night is still caught, and each item is logged once
	history    = 180 * 24 * time.Hour // the manual journals the usual is worked out from
	minHistory = 5                    // fewer journals than this have no usual yet
	backdated  = 30                   // days between a journal's date and its posting
	afterBank  = 14 * 24 * time.Hour  // a payment this soon after its payee's bank details changed
	duplicate  = 7 * 24 * time.Hour   // the same amount to the same payee within this
)

var (
	timesUsual = apd.New(3, 0)     // a manual journal above three times the median
	floor      = apd.New(1000, 0)  // and at least this, in the base currency
	roundFrom  = apd.New(10000, 0) // a round sum: a whole number of thousands from this
	thousand   = apd.New(1000, 0)
)

// The rules, each a fixed start of its finding's summary, so each item is logged once a rule.
const (
	ruleLarge     = "Well above the usual: "
	ruleRound     = "A round sum: "
	ruleBackdated = "Back-dated: "
	ruleAfterBank = "Soon after a bank change: "
	ruleDuplicate = "A possible duplicate: "
	ruleFirst     = "A first payment: "
)

// Finding is an unusual item: what it is, why, and who posted it.
type Finding struct {
	ObjectType, ObjectKey string
	Summary               string
	Actor                 *uuid.UUID
}

// Review logs the organisation's unusual items of the last week in Founder mode, each once a rule, and gives how many
// it logged. In another mode it logs none: Standard mode's separation of duties stands in for it.
func Review(ctx context.Context, store storage.Store, org uuid.UUID, now time.Time) (int, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Reviewer, Reason: "Unusual items for the weekly review (FM-6, FM-3)"})
	n := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		modes, err := u.TeamModes().List(ctx)
		if err != nil {
			return err
		}
		if len(modes) > 0 && modes[len(modes)-1].Mode != team.Founder {
			return nil
		}
		found, err := Find(ctx, u, now)
		if err != nil {
			return err
		}
		logged, err := u.ReviewLog().List(ctx)
		if err != nil {
			return err
		}
		for _, f := range found {
			rule, _, _ := strings.Cut(f.Summary, ": ")
			if slices.ContainsFunc(logged, func(e storage.ReviewLogEntry) bool {
				return e.Kind == storage.ReviewUnusual && e.ObjectType == f.ObjectType && e.ObjectKey == f.ObjectKey && strings.HasPrefix(e.Summary, rule+": ")
			}) {
				continue
			}
			if f.Actor != nil { // a poster who is no member of the organisation is not named: anyone may review it
				if _, err := u.Members().Membership(ctx, *f.Actor); errors.Is(err, ports.ErrNotFound) {
					f.Actor = nil
				} else if err != nil {
					return err
				}
			}
			if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewUnusual, ObjectType: f.ObjectType, ObjectKey: f.ObjectKey,
				Summary: f.Summary, Actor: f.Actor, LoggedAt: now}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ReviewAll reviews every organisation.
func ReviewAll(ctx context.Context, store storage.Store, now time.Time) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, org := range orgs {
		n, err := Review(ctx, store, org, now)
		total += n
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return total, errors.Join(errs...)
}

// Find is the unusual journals and payments posted in the week to now.
func Find(ctx context.Context, u storage.UnitOfWork, now time.Time) ([]Finding, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	journals, err := postings(ctx, u, ledger.SourceManualJournal, now.Add(-history-lookback))
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return nil, err
	}
	out, err := unusualJournals(journals, now, base, loc)
	if err != nil {
		return nil, err
	}
	payments, err := unusualPayments(ctx, u, now, base)
	if err != nil {
		return nil, err
	}
	return append(out, payments...), nil
}

// posting is a posted document's entry: the document, when it was posted (as the application's clock said, as
// approvals are decided) and what it debits in the base currency.
type posting struct {
	entry  storage.Entry
	doc    storage.Document
	at     time.Time
	debits *apd.Decimal
}

// postings are the documents of a source type posted since a time and still posted, in the order they posted, with
// their entries: the ledger's entries recorded since then, reversals aside.
func postings(ctx context.Context, u storage.UnitOfWork, source string, since time.Time) ([]posting, error) {
	var out []posting
	for after := int64(0); ; {
		page, err := u.Ledger().RecordedSince(ctx, []string{source}, since, after, 1000)
		if err != nil {
			return nil, err
		}
		for _, e := range page {
			after = e.LedgerRevision
			id, err := uuid.Parse(e.Source.ID)
			if e.ReversesEntryID != nil || err != nil {
				continue
			}
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return nil, err
			}
			if doc.Status != storage.DocumentPosted || doc.PostedAt == nil || doc.PostedAt.Before(since) || doc.Version != e.Source.Version {
				continue // voided, or an earlier version an edit replaced
			}
			d, err := debits(e)
			if err != nil {
				return nil, err
			}
			out = append(out, posting{e, doc, *doc.PostedAt, d})
		}
		if len(page) < 1000 {
			slices.SortStableFunc(out, func(a, b posting) int { return a.at.Compare(b.at) })
			return out, nil
		}
	}
}

// debits is what an entry debits in the base currency.
func debits(e storage.Entry) (*apd.Decimal, error) {
	sum := new(apd.Decimal)
	for _, l := range e.Lines {
		var d apd.Decimal
		if _, _, err := d.SetString(string(l.BaseAmount)); err != nil {
			return nil, err
		}
		if d.Sign() > 0 {
			if _, err := exact.Add(sum, sum, &d); err != nil {
				return nil, err
			}
		}
	}
	return sum, nil
}

var exact = apd.BaseContext.WithPrecision(34)

// postedBy is the person who posted a document, when it names one ("user:<id>").
func postedBy(doc storage.Document) *uuid.UUID {
	kind, id, ok := strings.Cut(doc.PostedBy, ":")
	if !ok || kind != "user" {
		return nil
	}
	if u, err := uuid.Parse(id); err == nil {
		return &u
	}
	return nil
}

// shown writes an amount of the currency with thousands separators, at its scale: "12000.0000" is "12,000.00" in SGD.
// The ledger keeps amounts at their currency's scale, so nothing is rounded; an amount that would be is written in full.
func shown(d *apd.Decimal, c money.Currency) string {
	text := d.Text('f')
	var at apd.Decimal
	if res, err := exact.Quantize(&at, d, int32(-c.MinorUnits)); err == nil && res&apd.Inexact == 0 { // #nosec G115 -- 0 to 4
		text = at.Text('f')
	}
	whole, frac, _ := strings.Cut(text, ".")
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign, whole = "-", whole[1:]
	}
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		return sign + b.String() + "." + frac
	}
	return sign + b.String()
}

func label(doc storage.Document, what string) string {
	if doc.Number != "" {
		return what + " " + doc.Number
	}
	return "A " + strings.ToLower(what)
}

func unusualJournals(all []posting, now time.Time, base money.Currency, loc *time.Location) ([]Finding, error) {
	var out []Finding
	for i, p := range all {
		if now.Sub(p.at) > lookback || p.at.After(now) {
			continue
		}
		add := func(summary string) {
			out = append(out, Finding{ObjectType: approval.ObjectJournal, ObjectKey: p.doc.ID.String(), Summary: summary, Actor: postedBy(p.doc)})
		}
		name := label(p.doc, "Journal")
		// Well above the usual: more than three times the median of the half year's manual journals before it.
		var before []*apd.Decimal
		for _, q := range all[:i] {
			if p.at.Sub(q.at) <= history {
				before = append(before, q.debits)
			}
		}
		if len(before) >= minHistory && p.debits.Cmp(floor) >= 0 {
			slices.SortFunc(before, func(a, b *apd.Decimal) int { return a.Cmp(b) })
			median := before[len(before)/2]
			limit := new(apd.Decimal)
			if _, err := exact.Mul(limit, median, timesUsual); err != nil {
				return nil, err
			}
			if p.debits.Cmp(limit) > 0 {
				add(fmt.Sprintf("%s%s debits %s %s, more than three times the company's usual manual journal (%s %s, the median of the half year before)",
					ruleLarge, name, shown(p.debits, base), base, shown(median, base), base))
			}
		}
		// A round sum: a whole number of thousands, from ten thousand.
		if p.debits.Cmp(roundFrom) >= 0 {
			rem := new(apd.Decimal)
			if _, err := exact.Rem(rem, p.debits, thousand); err == nil && rem.IsZero() {
				add(fmt.Sprintf("%s%s debits exactly %s %s", ruleRound, name, shown(p.debits, base), base))
			}
		}
		// Back-dated: posted more than 30 days after its date, into a month that may be reported already.
		y, m, d := p.at.In(loc).Date()
		posted := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if days := int(posted.Sub(p.entry.PostingDate).Hours() / 24); days > backdated {
			add(fmt.Sprintf("%s%s is dated %s and was posted %d days later, on %s", ruleBackdated, name,
				p.entry.PostingDate.Format("2 Jan 2006"), days, posted.Format("2 Jan 2006")))
		}
	}
	return out, nil
}

// payment is a supplier payment, with its payee and what it paid as recorded.
type payment struct {
	posting
	supplier uuid.UUID
	paid     string // the amount with its currency
}

func unusualPayments(ctx context.Context, u storage.UnitOfWork, now time.Time, base money.Currency) ([]Finding, error) {
	recent, err := postings(ctx, u, ledger.SourceBillPayment, now.Add(-lookback-duplicate))
	if err != nil {
		return nil, err
	}
	var all []payment
	for _, p := range recent {
		d, err := billpayment.Read(p.doc, base)
		if err != nil {
			continue
		}
		total, err := d.Total()
		if err != nil {
			continue
		}
		all = append(all, payment{p, d.SupplierID, total.String() + " " + total.Currency().Code})
	}
	if len(all) == 0 {
		return nil, nil
	}
	changed, err := bankChanges(ctx, u)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	name := func(id uuid.UUID) string {
		if n, ok := names[id]; ok {
			return n
		}
		n := "the supplier"
		if c, err := u.Contacts().Get(ctx, id); err == nil {
			n = c.LegalName
		}
		names[id] = n
		return n
	}
	var out []Finding
	for i, p := range all {
		if now.Sub(p.at) > lookback || p.at.After(now) {
			continue
		}
		add := func(summary string) {
			out = append(out, Finding{ObjectType: approval.ObjectPayment, ObjectKey: p.doc.ID.String(), Summary: summary, Actor: postedBy(p.doc)})
		}
		what := label(p.doc, "Payment")
		// Soon after a bank change: the payee's bank details were approved in the 14 days before it.
		for _, at := range changed[p.supplier] {
			if gap := p.at.Sub(at); gap >= 0 && gap <= afterBank {
				add(fmt.Sprintf("%s%s pays %s to %s %d day(s) after its bank details changed", ruleAfterBank, what, p.paid, name(p.supplier),
					int(gap.Hours()/24)))
				break
			}
		}
		// A possible duplicate: the same amount to the same payee in the 7 days before it.
		for _, q := range all[:i] {
			if q.supplier == p.supplier && q.paid == p.paid && p.at.Sub(q.at) <= duplicate {
				add(fmt.Sprintf("%s%s pays %s to %s, as %s did on %s", ruleDuplicate, what, p.paid, name(p.supplier), label(q.doc, "payment"),
					q.at.Format("2 Jan 2006")))
				break
			}
		}
		// A first payment: nothing was paid to the payee before.
		first, err := firstPayment(ctx, u, p)
		if err != nil {
			return nil, err
		}
		if first {
			add(fmt.Sprintf("%s%s is the first payment to %s: %s", ruleFirst, what, name(p.supplier), p.paid))
		}
	}
	return out, nil
}

// bankChanges are when each supplier's approved bank details changes were approved, as the application's clock said.
func bankChanges(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]time.Time, error) {
	approved, err := u.Approvals().List(ctx, "approved")
	if err != nil {
		return nil, err
	}
	decided := map[string]time.Time{}
	for _, a := range approved {
		if a.ObjectType == approval.ObjectPayeeBankChange && a.DecidedAt != nil {
			decided[a.ObjectKey] = *a.DecidedAt
		}
	}
	out := map[uuid.UUID][]time.Time{}
	f := storage.DocumentFilter{Type: storage.DocumentPayeeBankChange, Status: storage.DocumentPosted, Limit: 500}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, doc := range page {
			on, ok := decided[doc.ID.String()]
			if !ok && doc.PostedAt != nil {
				on, ok = *doc.PostedAt, true
			}
			s, err := payee.Read(doc)
			if err != nil || !ok {
				continue
			}
			if p, err := s.Payee(); err == nil && p.ContactID != uuid.Nil {
				out[p.ContactID] = append(out[p.ContactID], on)
			}
		}
		if len(page) < f.Limit {
			return out, nil
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}
}

// firstPayment says whether no posted payment to the supplier came before this one.
func firstPayment(ctx context.Context, u storage.UnitOfWork, p payment) (bool, error) {
	docs, err := u.Documents().OfContact(ctx, p.supplier, 1000)
	if err != nil {
		return false, err
	}
	for _, d := range docs {
		if d.ID != p.doc.ID && d.Type == storage.DocumentBillPayment && d.Status == storage.DocumentPosted && d.PostedAt != nil &&
			d.PostedAt.Before(p.at) {
			return false, nil
		}
	}
	return true, nil
}
