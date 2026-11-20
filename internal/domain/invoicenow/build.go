package invoicenow

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var postCode = regexp.MustCompile(`\b[0-9]{6}\b`)

// AddressOf splits an address written as one text into the line and the post code PINT-SG asks for: a Singapore post code
// is six digits. An address with none keeps its text as line 1, and the rules (BR-106, BR-107) say what is missing.
func AddressOf(text, country string) Address {
	text = strings.TrimSpace(text)
	a := Address{Country: country}
	if loc := postCode.FindStringIndex(text); loc != nil {
		a.PostCode = text[loc[0]:loc[1]]
		text = strings.Trim(strings.TrimSpace(text[:loc[0]]+text[loc[1]:]), " ,")
	}
	if i := strings.Index(text, ","); i > 0 && len(text) > 60 {
		a.Line1, a.Line2 = strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+1:])
	} else {
		a.Line1 = text
	}
	if a.Country == "" && a.Line1 != "" {
		a.Country = "SG"
	}
	return a
}

// percent is a GST rate held as a fraction, such as 0.09, in percent: 9.
func percent(rate string) string {
	d, _, err := apd.NewFromString(rate)
	if err != nil {
		return rate
	}
	var out apd.Decimal
	if _, err := apd.BaseContext.WithPrecision(20).Mul(&out, d, apd.New(100, 0)); err != nil {
		return rate
	}
	var red apd.Decimal
	if _, _, err := apd.BaseContext.WithPrecision(20).Reduce(&red, &out); err != nil {
		return out.Text('f')
	}
	return red.Text('f')
}

// FromInvoice makes the PINT-SG document of a posted invoice or credit note, read as the engine posted it: the lines'
// nets, the GST of each code under RND-GST-EXCL, and the totals. A document in another currency than SGD takes its GST and its totals in Singapore dollars from the posting's base amounts, so the
// document and the ledger agree; one with a deposit deducted, or whose base currency is not SGD, is refused. The document UUID is the one the
// submissions keep for it.
func FromInvoice(ctx context.Context, u storage.UnitOfWork, doc storage.Document, documentUUID uuid.UUID, base money.Currency) (Document, error) {
	p, problems, err := invoice.Prepare(ctx, u, doc, base)
	if err != nil {
		return Document{}, err
	}
	if len(problems) > 0 {
		return Document{}, fmt.Errorf("the invoice cannot be read for InvoiceNow: %v", problems)
	}
	inv := p.Invoice
	var sgdTax, sgdNet, sgdGross *money.Money
	if inv.Currency.Code != "SGD" {
		// the totals in Singapore dollars are the posting's own base amounts, so the document and the ledger agree (BR-113-GST-SG)
		if base.Code != "SGD" || len(inv.Deposits) > 0 {
			return Document{}, &RefusedError{Findings: []Finding{{Rule: "BR-113-GST-SG", Field: "currency",
				Message: "the totals in Singapore dollars of this invoice cannot be taken from its posting (a base currency that is not SGD, or a deposit deducted): send it by the tax agent's route"}}}
		}
		settings, err := journal.LoadSettings(ctx, u)
		if err != nil {
			return Document{}, err
		}
		entry, err := ledger.PostInvoice(inv, settings)
		if err != nil {
			return Document{}, err
		}
		tax, gross := money.Zero(base), money.Zero(base)
		for _, l := range entry.Lines {
			amount := l.Base
			if amount.Sign() < 0 {
				amount = amount.Neg()
			}
			switch settings.Accounts[l.AccountID].Role {
			case ledger.RoleGSTControl:
				if tax, err = tax.Add(amount); err != nil {
					return Document{}, err
				}
			case ledger.RoleAccountsReceivable:
				if gross, err = gross.Add(amount); err != nil {
					return Document{}, err
				}
			}
		}
		net, err := gross.Sub(tax)
		if err != nil {
			return Document{}, err
		}
		sgdTax, sgdNet, sgdGross = &tax, &net, &gross
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return Document{}, err
	}
	regs, err := u.GSTRegistrations().List(ctx)
	if err != nil {
		return Document{}, err
	}
	gstNo := ""
	for _, r := range regs {
		if !r.RegisteredFrom.After(inv.Date) && (r.DeregisteredFrom == nil || r.DeregisteredFrom.After(inv.Date)) {
			gstNo = r.GSTNumber
		}
	}
	v, ok, err := gst.PackOn(ctx, u, "sg-invoicenow", inv.Date)
	if err != nil {
		return Document{}, err
	}
	if !ok {
		return Document{}, ErrNoPack
	}
	esCat, err := Setting(ctx, u, SettingESCategory, inv.Date)
	if err != nil {
		return Document{}, err
	}
	d := Document{Number: doc.Number, UUID: documentUUID.String(), Issued: inv.Date, Due: p.DueDate, Currency: inv.Currency,
		Seller: Party{Name: org.Profile.LegalName, UEN: org.Profile.UEN, GST: gstNo, Address: AddressOf(org.Profile.RegisteredAddress, "SG")},
		Buyer:  Party{Name: p.Customer.LegalName, UEN: p.Customer.UENOrTaxID, Address: AddressOf(p.Customer.Address, p.Customer.Country)},
		Net:    p.Totals.Net, TaxTotal: p.Totals.GST, Gross: p.Totals.Total, SGDTax: sgdTax, SGDNet: sgdNet, SGDGross: sgdGross}
	if sch, id, ok := strings.Cut(p.Customer.PeppolID, ":"); ok {
		d.Buyer.EndpointScheme, d.Buyer.EndpointID = sch, id
	}
	switch {
	case invoice.IsCreditNote(doc):
		d.Kind = CreditNote
		d.Note = strings.TrimSpace(p.Draft.Notes)
		if p.Draft.Credits != uuid.Nil {
			if prior, err := u.Documents().Get(ctx, p.Draft.Credits); err == nil {
				d.Preceded = prior.Number
			}
		}
	default:
		d.Kind = Invoice
	}
	type group struct{ cat, rate string }
	nets := map[group]money.Money{}
	var order []group
	for i, l := range inv.Lines {
		c, ok := pack.InvoiceNow(v, l.GSTCode)
		if !ok || c.Direction != "supply" {
			return Document{}, &RefusedError{Findings: []Finding{{Rule: "BR-47-GST-SG", Field: fmt.Sprintf("lines[%d].gst_code", i),
				Message: fmt.Sprintf("the GST code %q is not a supply code: it has no GST category to send", l.GSTCode)}}}
		}
		cat := c.Category
		switch c.Scope {
		case pack.ScopeChoose:
			if esCat == "none" || esCat == "" {
				return Document{}, &RefusedError{Findings: []Finding{{Rule: "BR-47-GST-SG", Field: fmt.Sprintf("lines[%d].gst_code", i),
					Message: "an exempt line has no GST category until the tax agent chooses ES33 or ESN33 in the setting " + SettingESCategory}}}
			}
			cat = esCat
		case pack.ScopeNotReported:
			cat = c.Category
			if cat == "" {
				cat = l.GSTCode
			}
		}
		rate := percent(l.GSTRate)
		if l.GSTRate == "" {
			rate = "0"
		}
		g := group{cat, rate}
		if _, ok := nets[g]; !ok {
			order = append(order, g)
			nets[g] = money.Zero(inv.Currency)
		}
		sum, err := nets[g].Add(l.Net)
		if err != nil {
			return Document{}, err
		}
		nets[g] = sum
		q := p.Draft.Lines[i].Quantity.String()
		d.Lines = append(d.Lines, Line{ID: fmt.Sprint(i + 1), Name: lineName(l.Description, i), Quantity: q, Unit: "C62", NetPrice: p.Draft.Lines[i].UnitPrice.String(),
			Net: l.Net, Category: cat, Rate: rate})
	}
	for _, g := range order {
		tax := money.Zero(inv.Currency)
		for _, s := range p.Totals.Subtotals {
			if percent(s.Rate) == g.rate && s.Code != "" {
				if c, ok := pack.InvoiceNow(v, s.Code); ok && (c.Category == g.cat || c.Scope == pack.ScopeChoose) {
					if sum, err := tax.Add(s.GST); err == nil {
						tax = sum
					}
				}
			}
		}
		d.Tax = append(d.Tax, TaxSubtotal{Category: g.cat, Rate: g.rate, Taxable: nets[g], Tax: tax})
	}
	return d, nil
}

func lineName(desc string, i int) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return fmt.Sprintf("Line %d", i+1)
	}
	return desc
}
