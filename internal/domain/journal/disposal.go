package journal

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AssetDisposals records the sale or write-off of an asset, or of a share of its cost, and voids one (FR-FA-03;
// p1-fixed-assets design D4): each in one unit of work with the asset locked, the disposal, its number (DSP-, gap-free),
// its entry from asset_disposal@1, the reversal of the asset's months of depreciation dated on or after it, and the
// asset's next version (ENG-02, ENG-47).
type AssetDisposals struct {
	Store storage.Store
	// Clock is the organisation's clock: after a partial disposal, or a void, the asset's months due by today post at once.
	Clock interface{ Now() time.Time }
	// AfterPosting runs inside the unit of work after the disposal's entry (or its reversal) is written and the document
	// updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// DisposalDraft is a disposal as recorded: the asset, the day it was sold or written off, the share of its cost (nil for
// the whole asset), and its proceeds: money received (the chart's money account, the amount with any GST in it, its GST
// code), an issued invoice's line on the disposal clearing account, or none.
type DisposalDraft struct {
	AssetID        uuid.UUID
	Date           time.Time
	Cost           *money.Money
	Proceeds       string
	MoneyAccountID *uuid.UUID
	Gross          *money.Money
	GSTCode        string
	InvoiceID      *uuid.UUID
	InvoiceLine    int
	Notes          string
}

// CheckedDisposal is a disposal as recording it would post it, written nowhere: the disposal, the asset's next version,
// the months of depreciation it reverses, and its entry.
type CheckedDisposal struct {
	Disposal asset.Disposal
	Next     asset.Asset
	Reverse  []storage.Entry
	Entry    ledger.Entry
	Problems ledger.Problems
}

// RecordedDisposal is a recorded disposal, or its void: the disposal, its entry, and the asset's version it made.
type RecordedDisposal struct {
	Disposal storage.Document
	Entry    storage.Entry
	Asset    storage.Document
}

// settingAccount is the account of the chart whose code a setting holds on a day: nil when the setting is not set or the
// chart has no such account.
func settingAccount(ctx context.Context, u storage.UnitOfWork, key string, on time.Time) (*uuid.UUID, error) {
	def, ok := settings.Lookup(key)
	if !ok {
		return nil, fmt.Errorf("no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, key)
	if err != nil {
		return nil, err
	}
	code := settings.On(def, changes, on).Value
	if code == "" {
		return nil, nil
	}
	acc, err := u.Accounts().ByCode(ctx, code)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &acc.ID, nil
}

// depreciationEntries are an asset's months of depreciation that nothing reversed, in revision order.
func depreciationEntries(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) ([]storage.Entry, error) {
	entries, err := u.Ledger().OfSource(ctx, id)
	if err != nil {
		return nil, err
	}
	reversed := map[uuid.UUID]bool{}
	for _, e := range entries {
		if e.ReversesEntryID != nil {
			reversed[*e.ReversesEntryID] = true
		}
	}
	var out []storage.Entry
	for _, e := range entries {
		if e.Source.Type == ledger.SourceAssetDepreciation && e.ReversesEntryID == nil && !reversed[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}

// depreciated is what an entry of depreciation debited 6610: its month's part.
func depreciated(e storage.Entry, base money.Currency) (money.Money, error) {
	x, err := fromStorage(e, base)
	if err != nil {
		return money.Money{}, err
	}
	for _, l := range x.Lines {
		if l.Base.Sign() > 0 {
			return l.Base, nil
		}
	}
	return money.Zero(base), nil
}

// invoiceProceeds reads the line of an issued invoice that a disposal's proceeds are: a line on the clearing account, not
// spread, and what the invoice's entry credited the account for it, in the base currency (design D4).
func invoiceProceeds(ctx context.Context, u storage.UnitOfWork, d DisposalDraft, clearing uuid.UUID, s ledger.Settings) (money.Money, *ledger.Problem, error) {
	refuse := func(msg, fix string) (money.Money, *ledger.Problem, error) {
		return money.Money{}, &ledger.Problem{Code: asset.CodeProceeds, Field: "invoice_id", Message: msg, Fix: fix}, nil
	}
	doc, err := u.Documents().Lock(ctx, *d.InvoiceID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return refuse("the invoice does not exist in this organisation", "Choose the issued invoice to the buyer")
	case err != nil:
		return money.Money{}, nil, err
	case doc.Type != storage.DocumentInvoice || doc.Status != storage.DocumentPosted:
		return refuse(fmt.Sprintf("%s is not an issued invoice", cmp.Or(doc.Number, "the document")), "Issue the invoice to the buyer first, then record the disposal")
	}
	inv, err := invoice.Read(doc, s.Base)
	if err != nil {
		return money.Money{}, nil, err
	}
	if d.InvoiceLine < 1 || d.InvoiceLine > len(inv.Lines) {
		return refuse(fmt.Sprintf("%s has no line %d", doc.Number, d.InvoiceLine), "Choose the invoice's line for the asset")
	}
	k := 0
	for i, l := range inv.Lines[:d.InvoiceLine] {
		if l.AccountID != clearing || l.Spread(inv.Date) != nil {
			if i == d.InvoiceLine-1 {
				return refuse(fmt.Sprintf("line %d of %s is not on the disposal clearing account", d.InvoiceLine, doc.Number),
					"Invoice the asset's sale to the disposal clearing account, unspread")
			}
			continue
		}
		k++
	}
	stored, err := u.Ledger().ByIdentity(ctx, InvoiceIdentityOf(doc))
	if err != nil {
		return money.Money{}, nil, err
	}
	e, err := fromStorage(stored, s.Base)
	if err != nil {
		return money.Money{}, nil, err
	}
	for _, l := range e.Lines {
		if l.AccountID != clearing {
			continue
		}
		if k--; k == 0 {
			if l.Base.Sign() >= 0 {
				break
			}
			return l.Base.Neg(), nil, nil
		}
	}
	return refuse(fmt.Sprintf("line %d of %s credits nothing to the disposal clearing account", d.InvoiceLine, doc.Number), "Invoice the asset's sale to it")
}

// prepareDisposal reads a disposal as recording it would post it (design D4), the asset doc locked: every month of
// depreciation before its month posted, those of its month and after reversed, the share of the cost with its
// accumulated depreciation and residual value, the proceeds and the accounts it posts to; and the asset's next version.
func (s AssetDisposals) prepareDisposal(ctx context.Context, u storage.UnitOfWork, doc storage.Document, d DisposalDraft, set ledger.Settings,
	today time.Time) (CheckedDisposal, error) {
	a, err := asset.Read(doc)
	if err != nil {
		return CheckedDisposal{}, err
	}
	if doc.Status != storage.DocumentPosted {
		return CheckedDisposal{}, &asset.Error{Code: asset.CodeVoided, Message: fmt.Sprintf("%s was voided", cmp.Or(doc.Number, "the asset")),
			Fix: "Register its line again, if it is an asset after all"}
	}
	if a.Disposed {
		return CheckedDisposal{}, &asset.Error{Code: asset.CodeDisposed, Message: fmt.Sprintf("nothing of %s is left: it was disposed of", cmp.Or(doc.Number, "the asset")),
			Fix: "Void the disposal first, if it was a mistake"}
	}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) { probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix}) }
	on := dateOf(d.Date)
	m := asset.Month(on)
	switch {
	case d.Date.IsZero():
		add(asset.CodeDisposalDate, "date", "the disposal has no date", "Enter the day the asset was sold or written off")
	case on.Before(a.PurchasedOn):
		add(asset.CodeDisposalDate, "date", fmt.Sprintf("the disposal is dated %s, before the asset was bought on %s", on.Format(time.DateOnly), a.PurchasedOn.Format(time.DateOnly)),
			"Date it on or after the purchase")
	case on.After(today):
		add(asset.CodeDisposalDate, "date", fmt.Sprintf("the disposal is dated %s, after today", on.Format(time.DateOnly)), "Record a disposal once it has happened")
	case doc.Version > 1 && m.Before(a.From):
		add(asset.CodeDisposalDate, "date", fmt.Sprintf("the asset changed from %s, after the disposal's month", a.From.Format("January 2006")),
			fmt.Sprintf("Date the disposal in or after %s", a.From.Format("January 2006")))
	}
	if len(probs) > 0 {
		return CheckedDisposal{Problems: probs}, nil
	}

	// What has posted: every month before the disposal's must be, and those of its month and after are reversed.
	entries, err := depreciationEntries(ctx, u, doc.ID)
	if err != nil {
		return CheckedDisposal{}, err
	}
	out := CheckedDisposal{}
	before := a.Carried
	posted := map[string]bool{}
	for _, e := range entries {
		posted[e.PostingIdentity] = true
		if e.Source.Version != doc.Version {
			continue
		}
		if !asset.Month(e.PostingDate).Before(m) {
			out.Reverse = append(out.Reverse, e)
			continue
		}
		part, err := depreciated(e, set.Base)
		if err != nil {
			return CheckedDisposal{}, err
		}
		before, _ = before.Add(part)
	}
	ds, err := depreciationOf(doc)
	if err != nil {
		return CheckedDisposal{}, err
	}
	for _, x := range ds {
		if x.Month.Before(m) && !posted[x.Identity(ledger.RuleAssetDepreciation)] {
			add(asset.CodeDepreciationDue, "date", fmt.Sprintf("the depreciation of %s is due and not posted", x.Month.Format("January 2006")),
				"Open its period if it is closed, and record the disposal once it has posted")
			break
		}
	}

	// The share of the cost, with its accumulated depreciation and residual value.
	x := asset.Disposal{AssetID: doc.ID, AssetVersion: doc.Version, Date: on, Whole: true, Cost: a.Cost, Before: before, Proceeds: d.Proceeds,
		AssetAccountID: a.AccountID, Notes: d.Notes}
	if d.Cost != nil && !d.Cost.Equal(a.Cost) {
		if over, _ := d.Cost.Cmp(a.Cost); d.Cost.Currency() != set.Base || d.Cost.Sign() <= 0 || over > 0 {
			add(asset.CodeDisposalShare, "cost", fmt.Sprintf("a share of %s %s of a cost of %s %s", d.Cost, d.Cost.Currency(), a.Cost, set.Base),
				"Enter the part of the cost disposed of, above zero and below the cost, or leave it out to dispose of the whole asset")
		} else {
			x.Whole, x.Cost = false, *d.Cost
		}
	}
	if x.Accumulated, x.Residual, err = asset.Share(a, x.Cost, before); err != nil {
		return CheckedDisposal{}, err
	}

	// The proceeds.
	switch d.Proceeds {
	case asset.ProceedsNone:
	case asset.ProceedsMoney:
		x.MoneyAccountID, x.Gross, x.GSTCode = d.MoneyAccountID, d.Gross, d.GSTCode
		if d.MoneyAccountID == nil || d.Gross == nil {
			add(asset.CodeProceeds, "money_account_id", "money received names no money account or amount", "Choose the account the money came into, and the amount")
			break
		}
		version, err := gst.Version(ctx, u, on)
		if err != nil {
			return CheckedDisposal{}, err
		}
		regs, err := u.GSTRegistrations().List(ctx)
		if err != nil {
			return CheckedDisposal{}, err
		}
		if _, registered := gst.StatusOn(regs, on); registered {
			code, ok := pack.Code(version, d.GSTCode)
			standard, _ := pack.RateOn(version, on)
			switch {
			case !ok || code.Direction != pack.DirSupply:
				add(asset.CodeProceeds, "gst_code", fmt.Sprintf("%q is not a GST code for supplies", d.GSTCode),
					"Choose the GST code of the sale, usually SR: the sale of a business asset is a taxable supply")
			case code.StandardRated:
				x.GSTRate = standard
			case code.ValueBox != 0:
				x.GSTRate = "0"
			}
		}
	case asset.ProceedsInvoice:
		if d.InvoiceID == nil {
			add(asset.CodeProceeds, "invoice_id", "an invoiced sale names no invoice", "Choose the issued invoice to the buyer and its line")
			break
		}
		clearing, err := settingAccount(ctx, u, asset.ClearingSetting, on)
		if err != nil {
			return CheckedDisposal{}, err
		}
		if clearing == nil {
			add(asset.CodeProceeds, "invoice_id", "the disposal clearing account is not set, or not in the chart",
				"Ask the accountant to name it in Settings › Organisation (assets.disposal_clearing_account), then record the disposal")
			break
		}
		net, p, err := invoiceProceeds(ctx, u, d, *clearing, set)
		if err != nil {
			return CheckedDisposal{}, err
		}
		if p != nil {
			probs = append(probs, *p)
			break
		}
		taken, err := disposalOfInvoiceLine(ctx, u, *d.InvoiceID, d.InvoiceLine)
		if err != nil {
			return CheckedDisposal{}, err
		}
		if taken != "" {
			add(asset.CodeProceeds, "invoice_id", fmt.Sprintf("the invoice's line %d is already the proceeds of %s", d.InvoiceLine, taken), "Void that disposal first, or choose another line")
			break
		}
		x.InvoiceID, x.InvoiceLine, x.InvoiceNet, x.ClearingAccountID = d.InvoiceID, d.InvoiceLine, &net, clearing
	default:
		add(asset.CodeProceeds, "proceeds", fmt.Sprintf("%q is not a kind of proceeds", d.Proceeds), "Send money, invoice or none")
	}
	disposal, err := settingAccount(ctx, u, asset.DisposalSetting, on)
	if err != nil {
		return CheckedDisposal{}, err
	}
	if disposal != nil {
		x.DisposalAccountID = *disposal
	}

	// The asset's next version: nothing left of it, or what is left depreciating from the disposal's month.
	n := a
	n.From, n.DisposalID = m, nil
	if x.Whole {
		zero := money.Zero(set.Base)
		n.Cost, n.Residual, n.Carried, n.Disposed = zero, zero, zero, true
	} else {
		n.Cost, _ = a.Cost.Sub(x.Cost)
		n.Residual, _ = a.Residual.Sub(x.Residual)
		n.Carried, _ = before.Sub(x.Accumulated)
	}
	out.Disposal, out.Next, out.Problems = x, n, probs
	if len(probs) > 0 {
		return out, nil
	}
	e, err := ledger.PostAssetDisposal(x.Rule(storage.Document{Number: "DSP-…"}, assetMemo(doc, a), set.Base), set)
	if ps, ok := err.(ledger.Problems); ok {
		out.Problems = append(out.Problems, ps...)
	} else if err != nil {
		return CheckedDisposal{}, err
	}
	out.Entry = e
	return out, nil
}

// disposalOfInvoiceLine is the number of the posted disposal whose proceeds are an invoice's line, "" when none is.
func disposalOfInvoiceLine(ctx context.Context, u storage.UnitOfWork, invoiceID uuid.UUID, line int) (string, error) {
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentAssetDisposal, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return "", err
		}
		for _, doc := range docs {
			x, err := asset.ReadDisposal(doc)
			if err != nil {
				return "", err
			}
			if x.InvoiceID != nil && *x.InvoiceID == invoiceID && x.InvoiceLine == line {
				return cmp.Or(doc.Number, doc.ID.String()), nil
			}
		}
		if len(docs) < 500 {
			return "", nil
		}
		afterDate, afterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
	}
}

// Check runs the rules over a disposal as it would be recorded on today, and writes nothing.
func (s AssetDisposals) Check(ctx context.Context, u storage.UnitOfWork, d DisposalDraft, today time.Time) (CheckedDisposal, error) {
	doc, err := u.Documents().Get(ctx, d.AssetID)
	if err != nil {
		return CheckedDisposal{}, err
	}
	if !asset.IsAsset(doc) {
		return CheckedDisposal{}, ports.ErrNotFound
	}
	set, err := loadSettings(ctx, u)
	if err != nil {
		return CheckedDisposal{}, err
	}
	return s.prepareDisposal(ctx, u, doc, d, set, today)
}

// afterNow posts an asset's months due by the organisation's today once u commits, as FixedAssets does.
func (s AssetDisposals) afterNow(u storage.UnitOfWork, org, id uuid.UUID) {
	FixedAssets{Store: s.Store, Clock: s.Clock}.depreciateNow(u, org, id)
}

// Record records a disposal in u, the caller's unit of work, on today (FR-FA-03): the disposal, its number (DSP-), the
// reversal of the asset's months of depreciation on or after its month, each on its own date, its entry, the asset's
// next version and the disposal's change to posted. One the rules refuse writes nothing and answers every problem; a
// month to reverse in a closed period refuses it.
func (s AssetDisposals) Record(ctx context.Context, u storage.UnitOfWork, d DisposalDraft, today time.Time) (RecordedDisposal, error) {
	doc, err := u.Documents().Lock(ctx, d.AssetID) // ENG-47 step 1: the asset, before anything of it changes
	if err != nil {
		return RecordedDisposal{}, err
	}
	if !asset.IsAsset(doc) {
		return RecordedDisposal{}, ports.ErrNotFound
	}
	set, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedDisposal{}, err
	}
	c, err := s.prepareDisposal(ctx, u, doc, d, set, today)
	if err != nil {
		return RecordedDisposal{}, err
	}
	if len(c.Problems) > 0 {
		return RecordedDisposal{}, c.Problems
	}
	b, err := asset.EncodeDisposal(c.Disposal)
	if err != nil {
		return RecordedDisposal{}, err
	}
	dsp, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAssetDisposal, Payload: b})
	if err != nil {
		return RecordedDisposal{}, err
	}
	if dsp.Number, err = u.Sequences().Number(ctx, dsp.ID, storage.DocumentAssetDisposal); err != nil {
		return RecordedDisposal{}, err
	}
	reason := fmt.Sprintf("Disposed of on %s (%s)", c.Disposal.Date.Format(time.DateOnly), dsp.Number)
	for _, stored := range c.Reverse { // a disposal recorded late: the months it reaches back over
		original, err := fromStorage(stored, set.Base)
		if err != nil {
			return RecordedDisposal{}, err
		}
		reversal, err := ledger.Reverse(original, original.PostingDate, reason)
		if err != nil {
			return RecordedDisposal{}, err
		}
		if _, err := u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return RecordedDisposal{}, err
		}
	}
	a, err := asset.Read(doc)
	if err != nil {
		return RecordedDisposal{}, err
	}
	e, err := ledger.PostAssetDisposal(c.Disposal.Rule(dsp, assetMemo(doc, a), set.Base), set)
	if err != nil {
		return RecordedDisposal{}, err
	}
	out := RecordedDisposal{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(e, nil)); err != nil {
		return RecordedDisposal{}, err
	}
	next := c.Next
	next.DisposalID = &dsp.ID
	nb, err := asset.Encode(next)
	if err != nil {
		return RecordedDisposal{}, err
	}
	if out.Asset, err = u.Documents().Edit(ctx, doc.ID, nb, ""); err != nil {
		return RecordedDisposal{}, err
	}
	if err := u.Documents().SetStatus(ctx, dsp.ID, storage.DocumentPosted); err != nil {
		return RecordedDisposal{}, err
	}
	dsp.Status = storage.DocumentPosted
	out.Disposal = dsp
	org, err := u.Organisation(ctx)
	if err != nil {
		return RecordedDisposal{}, err
	}
	if !c.Disposal.Whole {
		s.afterNow(u, org.ID, doc.ID)
	}
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, dsp, out.Entry)
}

// Void undoes a posted disposal in u (FR-GL-12; design D4): a reversing entry dated date, on or after the disposal, in
// an open period, and the asset's next version as it stood before the disposal, carrying what had posted before its
// month, from which its months post again. A disposal is voided only while the asset's version is the one it made and
// none of that version's months has posted.
func (s AssetDisposals) Void(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, date time.Time, reason string) (RecordedDisposal, error) {
	dsp, err := u.Documents().Get(ctx, id)
	if err != nil {
		return RecordedDisposal{}, err
	}
	x, err := asset.ReadDisposal(dsp)
	if err != nil {
		return RecordedDisposal{}, err
	}
	doc, err := u.Documents().Lock(ctx, x.AssetID) // the asset before its disposal, as recording one locks them
	if err != nil {
		return RecordedDisposal{}, err
	}
	if dsp, err = u.Documents().Lock(ctx, id); err != nil {
		return RecordedDisposal{}, err
	}
	refuse := func(msg, fix string) (RecordedDisposal, error) {
		return RecordedDisposal{}, &asset.Error{Code: asset.CodeDisposalVoid, Message: msg, Fix: fix}
	}
	if dsp.Status != storage.DocumentPosted {
		return refuse(fmt.Sprintf("the disposal is %s, not posted", dsp.Status), "Only a posted disposal is voided")
	}
	cur, err := asset.Read(doc)
	if err != nil {
		return RecordedDisposal{}, err
	}
	if cur.DisposalID == nil || *cur.DisposalID != id {
		return refuse(fmt.Sprintf("%s changed after the disposal", cmp.Or(doc.Number, "the asset")), "Void a disposal only before its asset changes again")
	}
	entries, err := depreciationEntries(ctx, u, doc.ID)
	if err != nil {
		return RecordedDisposal{}, err
	}
	for _, e := range entries {
		if e.Source.Version == doc.Version {
			return refuse(fmt.Sprintf("what is left of %s has depreciated since: %s posted", cmp.Or(doc.Number, "the asset"), e.PostingDate.Format("January 2006")),
				"Void a partial disposal before its asset's next month posts; record a correcting journal with your accountant otherwise")
		}
	}
	date = dateOf(date)
	if date.Before(x.Date) {
		return refuse(fmt.Sprintf("the void is dated %s, before the disposal's %s", date.Format(time.DateOnly), x.Date.Format(time.DateOnly)), "Date the void on or after the disposal")
	}
	set, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedDisposal{}, err
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.AssetDisposalIdentity(dsp.ID, dsp.Version, ledger.RuleAssetDisposal))
	if err != nil {
		return RecordedDisposal{}, err
	}
	original, err := fromStorage(stored, set.Base)
	if err != nil {
		return RecordedDisposal{}, err
	}
	reversal, err := ledger.Reverse(original, date, reason)
	if err != nil {
		return RecordedDisposal{}, err
	}
	out := RecordedDisposal{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
		return RecordedDisposal{}, err
	}
	was, err := u.Documents().Version(ctx, doc.ID, x.AssetVersion)
	if err != nil {
		return RecordedDisposal{}, err
	}
	restored, err := asset.Read(was)
	if err != nil {
		return RecordedDisposal{}, err
	}
	restored.Carried, restored.From, restored.DisposalID, restored.Disposed = x.Before, asset.Month(x.Date), nil, false
	b, err := asset.Encode(restored)
	if err != nil {
		return RecordedDisposal{}, err
	}
	if out.Asset, err = u.Documents().Edit(ctx, doc.ID, b, ""); err != nil {
		return RecordedDisposal{}, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
		return RecordedDisposal{}, err
	}
	dsp.Status = storage.DocumentReversed
	out.Disposal = dsp
	org, err := u.Organisation(ctx)
	if err != nil {
		return RecordedDisposal{}, err
	}
	s.afterNow(u, org.ID, doc.ID)
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, dsp, out.Entry)
}

// replayAssetDisposal posts a stored disposal again under the rule that posted e (ENG-06), from what its version kept,
// with the asset named as it was at the version disposed of.
func replayAssetDisposal(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	x, err := asset.ReadDisposal(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	was, err := u.Documents().Version(ctx, x.AssetID, x.AssetVersion)
	if err != nil {
		return ledger.Entry{}, err
	}
	a, err := asset.Read(was)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayAssetDisposal(e.RuleVersion, x.Rule(doc, assetMemo(was, a), s.Base), s)
}
