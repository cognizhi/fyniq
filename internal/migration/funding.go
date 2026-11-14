package migration

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Funding sheet's kinds (design D5, task 4.6; rev 0.262).
const (
	KindShareIssue   = "share_issue"
	KindInstrument   = "instrument"
	KindSubscription = "subscription"
)

// fundingNote is what the import's equity records name as their source.
const fundingNote = "The cap table of the workbook's Funding sheet (FR-MIG-01)"

// classOf is the share-capital account of the Fyniq chart a cap table security maps onto: the one whose name says it,
// preference or ordinary, of the accounts the mapping gives the share-capital role.
func (m Mapping) classOf(security string) string {
	pref := strings.Contains(strings.ToLower(security), "pref")
	var first string
	for _, a := range m.Accounts {
		if a.Role != string(storage.RoleShareCapital) {
			continue
		}
		if first == "" {
			first = a.Fyniq
		}
		if strings.Contains(strings.ToLower(a.Name), "pref") == pref {
			return a.Fyniq
		}
	}
	return first
}

// subscribedOn is the day a Transactions row put a holder's money for shares into the books, if one does: a row to a
// share-capital account naming them.
func (w *Workbook) subscribedOn(m Mapping, holder string) (Transaction, bool) {
	for _, x := range w.Transactions {
		if m.role(x.Category) == string(storage.RoleShareCapital) && strings.EqualFold(personName(x.Contact), personName(holder)) {
			return x, true
		}
	}
	return Transaction{}, false
}

// signedOn is the day a SAFE's money came in, as a Transactions row to its account naming its investor shows it.
func (w *Workbook) signedOn(m Mapping, investor string) (time.Time, bool) {
	for _, x := range w.Transactions {
		if m.equity(x.Category) && m.role(x.Category) != string(storage.RoleShareCapital) && strings.EqualFold(personName(x.Contact), personName(investor)) {
			return x.Date, true
		}
	}
	return time.Time{}, false
}

// priceOf is a holding's price per share: what was paid for it divided by its shares, exact.
func priceOf(h Holding) (string, error) {
	if h.Paid == nil || h.Shares == nil || h.Shares.IsZero() {
		return "", Refused{{Code: CodeValue, Field: "shares", Message: fmt.Sprintf("%s's holding has no shares or amount paid", h.Holder),
			Fix: "Enter its shares and the amount paid on the Funding sheet"}}
	}
	out := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(30).Quo(out, h.Paid, h.Shares); err != nil {
		return "", err
	}
	out.Reduce(out)
	return out.Text('f'), nil
}

// funding is the Funding sheet's records (design D5, task 4.6; rev 0.262) [accountant to confirm]: each holding issued
// before the books start in the register of members, with no entry, as the opening balances hold its share capital
// (FR-EQ-08); and each SAFE as an instrument (FR-EQ-18), which the 2700 lines name. A holding the Transactions sheet
// subscribed for after the books start is allotted with its row instead (subscriptionRecord); the option pool is no
// holding, kept in the archive.
func (w *Workbook) funding(m Mapping, path string) []Record {
	start := w.BooksStart(path)
	var out []Record
	for _, h := range w.Holdings {
		if esop(h) {
			continue
		}
		on := start.AddDate(0, 0, -1) // the workbook keeps no allotment dates
		if x, ok := w.subscribedOn(m, h.Holder); ok {
			if !x.Date.Before(start) {
				continue // allotted with its row, in the history
			}
			on = x.Date
		}
		out = append(out, Record{Key: fmt.Sprintf("share-issue:%d", h.Row), Kind: KindShareIssue, Origin: h.Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				holder, err := contactOf(e, h.Holder)
				if err != nil {
					return Made{}, err
				}
				class, err := account(ctx, u, "class", m.classOf(h.Security))
				if err != nil {
					return Made{}, err
				}
				price, err := priceOf(h)
				if err != nil {
					return Made{}, err
				}
				s, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return Made{}, err
				}
				o, err := u.Organisation(ctx)
				if err != nil {
					return Made{}, err
				}
				c, err := equity.CheckBeforeBooks(ctx, u, equity.Issue{HolderID: holder, ClassAccountID: class.ID, Shares: h.Shares.Text('f'), Price: price,
					Consideration: storage.ConsiderationCash, ResolutionRefs: fundingNote, EffectiveOn: on}, s, o.Profile.BooksStart)
				if err != nil {
					return Made{}, err
				}
				classID, err := equity.DefaultClass(ctx, u, class.ID)
				if err != nil {
					return Made{}, err
				}
				x, err := equity.RecordIssue(ctx, u, storage.ShareIssue{ID: uuid.Must(uuid.NewV7()), HolderID: holder, ClassAccountID: class.ID, ClassID: classID,
					Shares: storage.Decimal(c.Shares.Text('f')), Price: storage.Decimal(c.Price.String()), Currency: c.Amount.Currency().Code,
					Amount: storage.Decimal(c.Amount.String()), Consideration: storage.ConsiderationCash, ResolutionRefs: fundingNote, EffectiveOn: on,
					RecordedBy: e.Person, RecordedAt: e.Now}, nil)
				return Made{Type: "share_issue", ID: x.ID}, err
			}})
	}
	preference := false
	for _, h := range w.Holdings {
		preference = preference || strings.Contains(strings.ToLower(h.Security), "pref")
	}
	for _, x := range w.Instruments {
		signed, ok := w.signedOn(m, x.Investor)
		if !ok {
			signed = start.AddDate(0, 0, -1)
		}
		out = append(out, Record{Key: fmt.Sprintf("instrument:%d", x.Row), Kind: KindInstrument, Origin: x.Origin,
			make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
				holder, err := contactOf(e, x.Investor)
				if err != nil {
					return Made{}, err
				}
				s, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return Made{}, err
				}
				amt, err := amount(x.Amount, s.Base, false, "amount")
				if err != nil {
					return Made{}, err
				}
				security := "Ordinary"
				if preference {
					security = "Preference"
				}
				class, err := account(ctx, u, "class", m.classOf(security))
				if err != nil {
					return Made{}, err
				}
				// [accountant to confirm] A SAFE with a cap is a post-money SAFE, one with a discount alone a discount SAFE, else an
				// MFN SAFE; it converts at a priced round into the preference shares when the cap table has any.
				form := equity.FormMFNSAFE
				switch {
				case x.Cap != nil && !x.Cap.IsZero():
					form = equity.FormPostMoneySAFE
				case x.Discount != nil && !x.Discount.IsZero():
					form = equity.FormDiscountSAFE
				}
				i := equity.Instrument{Form: form, HolderID: holder, Amount: amt, SignedOn: signed, MFN: form == equity.FormMFNSAFE, Discount: percent(x.Discount),
					Trigger: "priced_round", ClassAccountID: class.ID, Denominator: "post_money_capitalisation", PriceDecimals: 4,
					Note: fmt.Sprintf("From the workbook's Funding sheet, row %d (FR-MIG-01)", x.Row), Date: e.Now}
				if x.Cap != nil && !x.Cap.IsZero() {
					i.ValuationCap = x.Cap.Text('f')
				}
				doc, err := equity.DraftInstrumentIn(ctx, u, i, s)
				if err != nil {
					return Made{}, err
				}
				doc, err = equity.RecordImportedIn(ctx, u, storage.DocumentInstrument, doc.ID)
				return Made{Type: "document", ID: doc.ID}, err
			}})
	}
	return out
}

// instrumentOf is the SAFE an earlier record of the Funding sheet made for an investor.
func (w *Workbook) instrumentOf(e *Env, investor string) (uuid.UUID, error) {
	for _, x := range w.Instruments {
		if strings.EqualFold(personName(x.Investor), personName(investor)) {
			if id, ok := e.Made(fmt.Sprintf("instrument:%d", x.Row)); ok {
				return id, nil
			}
		}
	}
	return uuid.Nil, Refused{{Code: CodeImport, Field: "instrument", Message: fmt.Sprintf("names %q, whose SAFE the Funding sheet does not hold", investor),
		Fix: "Add the SAFE to the Funding sheet's SAFEs and convertible notes"}}
}

// subscriptionRecord imports money a subscriber paid for shares after the books start (FR-EQ-17, B.7): its line on
// share application monies in the subscriber's name, then the cash issue its holding on the Funding sheet makes, approved
// and effective on the row's day, which moves the monies to the class's share capital (INV-13).
func (w *Workbook) subscriptionRecord(m Mapping, x Transaction) Record {
	return Record{Key: "transaction:" + fmt.Sprint(x.Row), Kind: KindSubscription, Origin: x.Origin,
		make: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error) {
			line, err := lineOf(ctx, u, m, x.Bank, lineRef(x.Row, ""))
			if err != nil {
				return Made{}, err
			}
			holder, err := contactOf(e, x.Contact)
			if err != nil {
				return Made{}, err
			}
			var h *Holding
			for i := range w.Holdings {
				if strings.EqualFold(personName(w.Holdings[i].Holder), personName(x.Contact)) && !esop(w.Holdings[i]) {
					h = &w.Holdings[i]
				}
			}
			if h == nil {
				return Made{}, Refused{{Code: CodeImport, Field: "contact", Message: fmt.Sprintf("pays %q for shares the Funding sheet's cap table does not hold", x.Contact),
					Fix: "Add the holding to the Funding sheet's cap table"}}
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return Made{}, err
			}
			var monies uuid.UUID
			for id, a := range s.Accounts {
				if a.SubscriberAccount() && a.Active && !a.Group {
					monies = id
				}
			}
			if monies == uuid.Nil {
				return Made{}, Refused{{Code: CodeMapping, Field: "category", Message: "the chart has no share application monies account for the subscription",
					Fix: "Add 2040 Share application monies to the chart"}}
			}
			d := banktransaction.Draft{LineID: line.ID, ContactID: &holder, Reference: strings.TrimSpace(x.Ref), Parts: []banktransaction.Part{{AccountID: monies,
				Description: x.Description}}}
			if _, err := (journal.BankTransactions{Store: e.Store, Clock: e.Clock}).Categorise(ctx, e.Org, journal.Categorisation{Draft: d,
				Basis: storage.BasisPerson, At: e.Now}); err != nil {
				return Made{}, err
			}
			class, err := account(ctx, u, "class", m.classOf(h.Security))
			if err != nil {
				return Made{}, err
			}
			price, err := priceOf(*h)
			if err != nil {
				return Made{}, err
			}
			doc, _, err := equity.DraftTransactionIn(ctx, u, equity.Transaction{Kind: equity.KindIssueCash, HolderID: holder, ClassAccountID: class.ID,
				Shares: h.Shares.Text('f'), Price: price, ResolutionRefs: fundingNote, Note: cmp.Or(x.Description, "Subscription"), Date: x.Date}, s)
			if err != nil {
				return Made{}, err
			}
			if _, err := equity.ApproveImportedIn(ctx, u, doc.ID, x.Date, s); err != nil {
				return Made{}, err
			}
			if _, err := (journal.EquityTransactions{}).EffectiveIn(ctx, u, doc.ID, journal.EquityEffective{EffectiveOn: x.Date, Today: e.Now,
				RecordedBy: e.Person}); err != nil {
				return Made{}, err
			}
			return Made{Type: "document", ID: doc.ID}, nil
		}}
}
