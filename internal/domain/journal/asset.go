package journal

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FixedAssets keeps the fixed asset register (FR-FA-01; p1-fixed-assets design D1 and D2): registering an asset from the
// bill line or bank line that bought it, or one bought before the books start with its opening figures, and changing an
// asset, its next version, which applies to the months not yet posted. Each command is one unit of work: the document,
// its number (FA-, gap-free) and its source, audited together (AUD-01). Registering posts nothing: the cost is already on
// the asset account.
type FixedAssets struct {
	Store storage.Store
}

// AssetDraft is an asset as registered. A sourced asset names its source's document and line (from 1), and takes its
// account, purchase date and cost from that line as its entry posted it; one bought before the books start names its
// account, purchase date, cost and the accumulated depreciation the opening balances carry for it. Start is the month
// depreciation starts: zero for the purchase month.
type AssetDraft struct {
	SourceID           *uuid.UUID
	SourceLine         int
	Name               string
	Notes              string
	AccountID          uuid.UUID
	PurchasedOn        time.Time
	Cost               money.Money
	OpeningAccumulated money.Money
	Residual           money.Money
	LifeMonths         int
	Method             string
	Start              time.Time
}

// AssetChange is a change of a registered asset: its name and notes, its estimates (residual value, useful life), and
// what posted months depend on, which changes only while none has posted: its start, and an opening asset's purchase
// date, cost and opening accumulated depreciation. A nil field keeps what the asset has.
type AssetChange struct {
	Name               *string
	Notes              *string
	Residual           *money.Money
	LifeMonths         *int
	Start              *time.Time
	PurchasedOn        *time.Time
	Cost               *money.Money
	OpeningAccumulated *money.Money
}

// CheckedAsset is an asset version as registering or changing it would save it, written nowhere.
type CheckedAsset struct {
	Asset    asset.Asset
	Problems ledger.Problems
}

// AssetLine is a line of a posted bill or categorised bank line on an asset account: what an asset may be registered
// from, with what its entry put on the account.
type AssetLine struct {
	DocumentID  uuid.UUID
	Type        storage.DocumentType
	Number      string
	Version     int
	Line        int
	Date        time.Time
	AccountID   uuid.UUID
	Description string
	Cost        money.Money
}

// SourceLines are the lines of a posted bill or categorised bank line that put a cost on an asset account of the
// register: each with what its entry, of the document's current version, put there in the base currency.
func SourceLines(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]AssetLine, error) {
	if doc.Status != storage.DocumentPosted || doc.Type != storage.DocumentBill && doc.Type != storage.DocumentBankTransaction {
		return nil, nil
	}
	accounts, err := registerAccounts(ctx, u)
	if err != nil {
		return nil, err
	}
	var out []AssetLine
	add := func(i int, date time.Time, acc uuid.UUID, desc string, l ledger.Line, ok bool) {
		if ok && accounts[acc] && l.Base.Sign() > 0 {
			out = append(out, AssetLine{DocumentID: doc.ID, Type: doc.Type, Number: doc.Number, Version: doc.Version, Line: i + 1, Date: date,
				AccountID: acc, Description: desc, Cost: l.Base})
		}
	}
	if doc.Type == storage.DocumentBill {
		stored, err := u.Ledger().ByIdentity(ctx, ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill))
		if err != nil {
			return nil, err
		}
		p, _, err := bill.PrepareAt(ctx, u, doc, s.Base, stored.RecordedAt)
		if err != nil {
			return nil, err
		}
		e, err := fromStorage(stored, s.Base)
		if err != nil {
			return nil, err
		}
		for i, l := range p.Draft.Lines {
			x, ok := ledger.BillLineCost(p.Bill, e, s, i+1)
			add(i, p.Draft.Date, l.AccountID, l.Description, x, ok)
		}
		return out, nil
	}
	saved, err := banktransaction.Read(doc, s.Base)
	if err != nil {
		return nil, err
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.BankTransactionIdentity(doc.ID, doc.Version, ledger.RuleBankTransaction))
	if err != nil {
		return nil, err
	}
	e, err := fromStorage(stored, s.Base)
	if err != nil {
		return nil, err
	}
	t := banktransaction.Transaction(doc, saved)
	for i, p := range saved.Parts {
		x, ok := ledger.BankPartCost(t, e, s, i+1)
		desc := p.Description
		if desc == "" {
			desc = saved.Memo
		}
		add(i, saved.Date, p.AccountID, desc, x, ok)
	}
	return out, nil
}

// registerAccounts are the chart's accounts an asset sits on (asset.InRegister), by ID.
func registerAccounts(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]bool, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, a := range accounts {
		if asset.InRegister(a) {
			out[a.ID] = true
		}
	}
	return out, nil
}

// booksStart is the organisation's books start, zero when it has none.
func booksStart(ctx context.Context, u storage.UnitOfWork) (time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil || o.Profile.BooksStart == nil {
		return time.Time{}, err
	}
	return dateOf(*o.Profile.BooksStart), nil
}

// accountOf is the chart's account id, nil when it has none.
func accountOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (*storage.Account, error) {
	a, err := u.Accounts().Get(ctx, id)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &a, nil
}

// sourceProblem is a refusal of an asset's source.
func sourceProblem(msg, fix string) ledger.Problems {
	return ledger.Problems{{Code: asset.CodeSource, Field: "source", Message: msg, Fix: fix}}
}

// prepareAsset reads an asset as registering it would save it: a sourced one's line from its document, locked so that
// two registrations of a line, or a void of the document and its registration, wait for each other (ENG-47).
func prepareAsset(ctx context.Context, u storage.UnitOfWork, d AssetDraft, s ledger.Settings) (CheckedAsset, error) {
	start, err := booksStart(ctx, u)
	if err != nil {
		return CheckedAsset{}, err
	}
	zero := money.Zero(s.Base)
	a := asset.Asset{Name: strings.TrimSpace(d.Name), Notes: d.Notes, Residual: d.Residual, LifeMonths: d.LifeMonths, Method: d.Method, Carried: zero}
	if a.Method == "" {
		a.Method = asset.StraightLine
	}
	if a.Residual.Currency() == (money.Currency{}) {
		a.Residual = zero
	}
	if d.SourceID != nil {
		doc, err := u.Documents().Lock(ctx, *d.SourceID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			return CheckedAsset{Asset: a, Problems: sourceProblem("the source does not exist in this organisation", "Choose a posted bill or a categorised bank line")}, nil
		case err != nil:
			return CheckedAsset{}, err
		}
		lines, err := SourceLines(ctx, u, doc, s)
		if err != nil {
			return CheckedAsset{}, err
		}
		i := slices.IndexFunc(lines, func(l AssetLine) bool { return l.Line == d.SourceLine })
		if i < 0 {
			return CheckedAsset{Asset: a, Problems: sourceProblem(fmt.Sprintf("line %d of %s does not put a cost on an asset account", d.SourceLine, cmp.Or(doc.Number, "the document")),
				"Choose a line of a posted bill or categorised bank line on an asset account, such as 1510 Computer equipment")}, nil
		}
		taken, err := u.AssetSources().OfDocument(ctx, doc.ID)
		if err != nil {
			return CheckedAsset{}, err
		}
		if slices.ContainsFunc(taken, func(x storage.AssetSource) bool { return x.Line == d.SourceLine }) {
			return CheckedAsset{Asset: a, Problems: sourceProblem(fmt.Sprintf("line %d of %s is in the register already", d.SourceLine, cmp.Or(doc.Number, "the document")),
				"Open the asset registered from it")}, nil
		}
		l := lines[i]
		a.Source = &asset.Source{DocumentID: doc.ID, Version: doc.Version, Line: l.Line}
		a.AccountID, a.PurchasedOn, a.Cost = l.AccountID, l.Date, l.Cost
		if a.Name == "" {
			a.Name = l.Description
		}
	} else {
		a.AccountID, a.PurchasedOn, a.Cost, a.Carried = d.AccountID, dateOf(d.PurchasedOn), d.Cost, d.OpeningAccumulated
		if a.Cost.Currency() == (money.Currency{}) {
			a.Cost = zero
		}
		if a.Carried.Currency() == (money.Currency{}) {
			a.Carried = zero
		}
	}
	a.Start = asset.Month(a.PurchasedOn)
	if !d.Start.IsZero() {
		a.Start = asset.Month(d.Start)
	}
	a.From = a.Start
	if a.Source == nil && !start.IsZero() && asset.Month(start).After(a.From) {
		a.From = asset.Month(start) // months before the books start are in the opening balances (FR-FA-02)
	}
	acc, err := accountOf(ctx, u, a.AccountID)
	if err != nil {
		return CheckedAsset{}, err
	}
	return CheckedAsset{Asset: a, Problems: asset.Check(a, acc, s.Base, start)}, nil
}

// CheckRegister runs the rules over an asset as registering it would save it, and writes nothing.
func (s FixedAssets) CheckRegister(ctx context.Context, org uuid.UUID, d AssetDraft) (CheckedAsset, error) {
	var out CheckedAsset
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		out, err = prepareAsset(ctx, u, d, settings)
		return err
	})
	return out, err
}

// Register registers an asset in u, the caller's unit of work (FR-FA-01): its document, posted at once, its number (FA-,
// gap-free) and its source. One the rules refuse writes nothing and answers every problem.
func (s FixedAssets) Register(ctx context.Context, u storage.UnitOfWork, d AssetDraft) (storage.Document, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return storage.Document{}, err
	}
	c, err := prepareAsset(ctx, u, d, settings)
	if err != nil {
		return storage.Document{}, err
	}
	if len(c.Problems) > 0 {
		return storage.Document{}, c.Problems
	}
	b, err := asset.Encode(c.Asset)
	if err != nil {
		return storage.Document{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentFixedAsset, Payload: b})
	if err != nil {
		return storage.Document{}, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentFixedAsset); err != nil {
		return storage.Document{}, err
	}
	if x := c.Asset.Source; x != nil {
		if err := u.AssetSources().Add(ctx, storage.AssetSource{AssetID: doc.ID, SourceID: x.DocumentID, SourceVersion: x.Version, Line: x.Line,
			AccountID: c.Asset.AccountID}); err != nil {
			return storage.Document{}, err
		}
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Get(ctx, doc.ID)
}

// Posted is what an asset's depreciation entries posted, net of their reversals: by version, the months and the total.
type Posted struct {
	Months map[int][]time.Time
	Total  map[int]money.Money
	Last   time.Time // the latest month posted under any version, zero when none
}

// PostedOf is what an asset's months of depreciation posted (asset_depreciation@1), net of reversals.
func PostedOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, base money.Currency) (Posted, error) {
	out := Posted{Months: map[int][]time.Time{}, Total: map[int]money.Money{}}
	entries, err := u.Ledger().OfSource(ctx, id)
	if err != nil {
		return out, err
	}
	reversed := map[uuid.UUID]bool{}
	for _, e := range entries {
		if e.ReversesEntryID != nil {
			reversed[*e.ReversesEntryID] = true
		}
	}
	for _, e := range entries {
		if e.Source.Type != ledger.SourceAssetDepreciation || e.ReversesEntryID != nil || reversed[e.ID] {
			continue
		}
		x, err := fromStorage(e, base)
		if err != nil {
			return out, err
		}
		v := e.Source.Version
		m := asset.Month(x.PostingDate)
		out.Months[v] = append(out.Months[v], m)
		total, ok := out.Total[v]
		if !ok {
			total = money.Zero(base)
		}
		for _, l := range x.Lines {
			if l.Base.Sign() > 0 {
				total, _ = total.Add(l.Base)
			}
		}
		out.Total[v] = total
		if m.After(out.Last) {
			out.Last = m
		}
	}
	return out, nil
}

// next is the version after cur, with what cur posted carried into it (design D2): from the first month not posted.
func next(cur asset.Asset, version int, p Posted) asset.Asset {
	n := cur
	if t, ok := p.Total[version]; ok {
		n.Carried, _ = cur.Carried.Add(t)
	}
	if !p.Last.IsZero() && !p.Last.Before(n.From) {
		n.From = p.Last.AddDate(0, 1, 0)
	}
	n.DisposalID = nil
	return n
}

// prepareChange reads a change of an asset as it would save its next version.
func prepareChange(ctx context.Context, u storage.UnitOfWork, doc storage.Document, c AssetChange, s ledger.Settings) (CheckedAsset, error) {
	cur, err := asset.Read(doc)
	if err != nil {
		return CheckedAsset{}, err
	}
	if cur.Disposed {
		return CheckedAsset{}, &asset.Error{Code: asset.CodeDisposed, Message: fmt.Sprintf("nothing of %s is left: it was disposed of", cmp.Or(doc.Number, "the asset")),
			Fix: "Void the disposal first, if it was a mistake"}
	}
	posted, err := PostedOf(ctx, u, doc.ID, s.Base)
	if err != nil {
		return CheckedAsset{}, err
	}
	n := next(cur, doc.Version, posted)
	var probs ledger.Problems
	fixed := func(field, what string) {
		probs = append(probs, ledger.Problem{Code: asset.CodePosted, Field: field,
			Message: fmt.Sprintf("its depreciation has posted up to %s, which depends on its %s", posted.Last.Format("January 2006"), what),
			Fix:     "Change its residual value or life: they apply to the months not posted yet"})
	}
	if c.Name != nil {
		n.Name = strings.TrimSpace(*c.Name)
	}
	if c.Notes != nil {
		n.Notes = *c.Notes
	}
	if c.Residual != nil {
		n.Residual = *c.Residual
	}
	if c.LifeMonths != nil {
		n.LifeMonths = *c.LifeMonths
	}
	none := posted.Last.IsZero()
	if c.Start != nil && !asset.Month(*c.Start).Equal(cur.Start) {
		if !none {
			fixed("start", "start")
		}
		n.Start = asset.Month(*c.Start)
	}
	for _, x := range []struct {
		field string
		set   bool
		apply func()
	}{
		{"purchased_on", c.PurchasedOn != nil && !dateOf(*c.PurchasedOn).Equal(cur.PurchasedOn), func() { n.PurchasedOn = dateOf(*c.PurchasedOn) }},
		{"cost", c.Cost != nil && !c.Cost.Equal(cur.Cost), func() { n.Cost = *c.Cost }},
		{"opening_accumulated", c.OpeningAccumulated != nil && !c.OpeningAccumulated.Equal(cur.Carried), func() { n.Carried = *c.OpeningAccumulated }},
	} {
		if !x.set {
			continue
		}
		if cur.Source != nil {
			probs = append(probs, ledger.Problem{Code: asset.CodeSource, Field: x.field, Message: "the asset takes its " + strings.ReplaceAll(x.field, "_", " ") + " from the line it was registered from",
				Fix: "Correct the bill or bank line instead"})
			continue
		}
		if !none {
			fixed(x.field, strings.ReplaceAll(x.field, "_", " "))
			continue
		}
		x.apply()
	}
	if none {
		start, err := booksStart(ctx, u)
		if err != nil {
			return CheckedAsset{}, err
		}
		n.From = n.Start
		if n.Source == nil && !start.IsZero() && asset.Month(start).After(n.From) {
			n.From = asset.Month(start)
		}
	}
	start, err := booksStart(ctx, u)
	if err != nil {
		return CheckedAsset{}, err
	}
	acc, err := accountOf(ctx, u, n.AccountID)
	if err != nil {
		return CheckedAsset{}, err
	}
	probs = append(probs, asset.Check(n, acc, s.Base, start)...)
	if n.LifeMonths >= 1 && n.End().Before(n.From) && n.LifeMonths != cur.LifeMonths {
		probs = append(probs, ledger.Problem{Code: asset.CodeLife, Field: "life_months",
			Message: fmt.Sprintf("a life of %d months ends in %s, before %s, the first month not posted yet", n.LifeMonths, n.End().Format("January 2006"), n.From.Format("January 2006")),
			Fix:     fmt.Sprintf("Enter a life that ends in or after %s, or dispose of the asset", n.From.Format("January 2006"))})
	}
	return CheckedAsset{Asset: n, Problems: probs}, nil
}

// CheckChange runs the rules over a change of an asset as it would save it, and writes nothing.
func (s FixedAssets) CheckChange(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c AssetChange) (CheckedAsset, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return CheckedAsset{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return CheckedAsset{}, err
	}
	return prepareChange(ctx, u, doc, c, settings)
}

// Change saves the next version of an asset in u (design D2): what earlier versions posted is carried into it, and it
// applies to the months not posted yet. The asset is locked, so its depreciation waits for the change (ENG-47).
func (s FixedAssets) Change(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c AssetChange) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return storage.Document{}, err
	}
	if !asset.IsAsset(doc) {
		return storage.Document{}, &asset.Error{Code: asset.CodeNotAsset, Message: fmt.Sprintf("document %s is a %s, not a fixed asset", doc.ID, doc.Type),
			Fix: "Open it with its own module"}
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return storage.Document{}, err
	}
	checked, err := prepareChange(ctx, u, doc, c, settings)
	if err != nil {
		return storage.Document{}, err
	}
	if len(checked.Problems) > 0 {
		return storage.Document{}, checked.Problems
	}
	b, err := asset.Encode(checked.Asset)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Edit(ctx, id, b, "")
}
