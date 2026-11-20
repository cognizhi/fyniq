package invoicenow

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// Received is a document read from the network: only what a draft bill needs (FR-AP-05), as text exactly as written.
// Reading never posts and never trusts: the figures are the sender's, a person checks the draft.
type Received struct {
	Kind          Kind
	Number        string
	UUID          string
	Issued        string // YYYY-MM-DD
	Due           string
	Currency      string
	Note          string
	Seller        ReceivedParty
	Lines         []ReceivedLine
	Net           string
	TaxTotal      string
	Gross         string
	Specification string
}

// ReceivedParty is the sender, matched to a contact by its endpoint.
type ReceivedParty struct {
	Name           string
	EndpointScheme string
	EndpointID     string
	UEN            string
	GST            string
	PostCode       string
	Country        string
}

// ReceivedLine is one line of a received document.
type ReceivedLine struct {
	ID, Name, Quantity, Unit, Net, Price, Category, Rate string
}

type rxText struct {
	Scheme string `xml:"schemeID,attr"`
	Unit   string `xml:"unitCode,attr"`
	Value  string `xml:",chardata"`
}

type rxParty struct {
	Party struct {
		Endpoint rxText                `xml:"EndpointID"`
		Name     struct{ Name string } `xml:"PartyName"`
		Address  struct {
			Post    string `xml:"PostalZone"`
			Country struct {
				Code string `xml:"IdentificationCode"`
			} `xml:"Country"`
		} `xml:"PostalAddress"`
		Tax struct {
			CompanyID string `xml:"CompanyID"`
		} `xml:"PartyTaxScheme"`
		Legal struct {
			Name      string `xml:"RegistrationName"`
			CompanyID string `xml:"CompanyID"`
		} `xml:"PartyLegalEntity"`
	} `xml:"Party"`
}

type rxLine struct {
	ID       string `xml:"ID"`
	Invoiced rxText `xml:"InvoicedQuantity"`
	Credited rxText `xml:"CreditedQuantity"`
	Net      string `xml:"LineExtensionAmount"`
	Item     struct {
		Name string `xml:"Name"`
		Tax  struct {
			ID      string `xml:"ID"`
			Percent string `xml:"Percent"`
		} `xml:"ClassifiedTaxCategory"`
	} `xml:"Item"`
	Price struct {
		Amount string `xml:"PriceAmount"`
	} `xml:"Price"`
}

type rxDocument struct {
	XMLName       xml.Name
	Customization string  `xml:"CustomizationID"`
	ID            string  `xml:"ID"`
	UUID          string  `xml:"UUID"`
	Issue         string  `xml:"IssueDate"`
	Due           string  `xml:"DueDate"`
	InvoiceType   string  `xml:"InvoiceTypeCode"`
	CreditType    string  `xml:"CreditNoteTypeCode"`
	Note          string  `xml:"Note"`
	Currency      string  `xml:"DocumentCurrencyCode"`
	Seller        rxParty `xml:"AccountingSupplierParty"`
	Tax           struct {
		Amount string `xml:"TaxAmount"`
	} `xml:"TaxTotal"`
	Totals struct {
		Net   string `xml:"TaxExclusiveAmount"`
		Gross string `xml:"TaxInclusiveAmount"`
	} `xml:"LegalMonetaryTotal"`
	InvoiceLines []rxLine `xml:"InvoiceLine"`
	CreditLines  []rxLine `xml:"CreditNoteLine"`
}

// ParseError is a received document Fyniq cannot read: its reason is for the person who sees the failed receipt.
type ParseError struct{ Reason string }

func (e *ParseError) Error() string { return "the received document cannot be read: " + e.Reason }

// Parse reads a received PINT-SG invoice, credit note or debit note. It refuses anything that is not UBL Invoice or
// CreditNote, or whose specification identifier is not Singapore's PINT (rule PEPPOL-EN16931-R004-SG: it starts with the
// identifier). Elements it does not need are ignored.
func Parse(b []byte) (Received, error) {
	if len(b) == 0 || len(b) > 10<<20 {
		return Received{}, &ParseError{Reason: "it is empty or larger than 10 MB"}
	}
	var d rxDocument
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = true
	if err := dec.Decode(&d); err != nil {
		return Received{}, &ParseError{Reason: "it is not well-formed XML: " + err.Error()}
	}
	var k Kind
	var lines []rxLine
	switch d.XMLName.Local {
	case "Invoice":
		k, lines = Invoice, d.InvoiceLines
		if strings.TrimSpace(d.InvoiceType) == "383" {
			k = DebitNote
		}
	case "CreditNote":
		k, lines = CreditNote, d.CreditLines
	default:
		return Received{}, &ParseError{Reason: fmt.Sprintf("the document is a %q, not an Invoice or a CreditNote", d.XMLName.Local)}
	}
	spec := strings.TrimSpace(d.Customization)
	if !strings.HasPrefix(spec, SpecificationID) {
		return Received{}, &ParseError{Reason: fmt.Sprintf("its specification identifier %q is not %s", spec, SpecificationID)}
	}
	if strings.TrimSpace(d.ID) == "" {
		return Received{}, &ParseError{Reason: "it has no invoice number"}
	}
	p := d.Seller.Party
	out := Received{Kind: k, Number: strings.TrimSpace(d.ID), UUID: strings.TrimSpace(d.UUID), Issued: strings.TrimSpace(d.Issue),
		Due: strings.TrimSpace(d.Due), Currency: strings.TrimSpace(d.Currency), Note: strings.TrimSpace(d.Note), Specification: spec,
		Net: strings.TrimSpace(d.Totals.Net), TaxTotal: strings.TrimSpace(d.Tax.Amount), Gross: strings.TrimSpace(d.Totals.Gross),
		Seller: ReceivedParty{Name: strings.TrimSpace(p.Legal.Name), EndpointScheme: p.Endpoint.Scheme, EndpointID: strings.TrimSpace(p.Endpoint.Value),
			UEN: strings.TrimSpace(p.Legal.CompanyID), GST: strings.TrimSpace(p.Tax.CompanyID), PostCode: strings.TrimSpace(p.Address.Post),
			Country: strings.TrimSpace(p.Address.Country.Code)}}
	if out.Seller.Name == "" {
		out.Seller.Name = strings.TrimSpace(p.Name.Name)
	}
	for _, l := range lines {
		q := l.Invoiced
		if k == CreditNote {
			q = l.Credited
		}
		out.Lines = append(out.Lines, ReceivedLine{ID: strings.TrimSpace(l.ID), Name: strings.TrimSpace(l.Item.Name), Quantity: strings.TrimSpace(q.Value), Unit: q.Unit,
			Net: strings.TrimSpace(l.Net), Price: strings.TrimSpace(l.Price.Amount), Category: strings.TrimSpace(l.Item.Tax.ID), Rate: strings.TrimSpace(l.Item.Tax.Percent)})
	}
	return out, nil
}
