package api

import (
	"context"
	"encoding/hex"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/salesdoc"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// ------------------------------------------------------------- an invoice as a document (FR-AR-05; p1-sales task 5.1)
//
// An invoice renders with the template that applies to its customer (FR-TPL-05), filled from its current version: a
// tax invoice with the IRAS particulars once the organisation is GST-registered on its date, its totals in SGD when it
// is in another currency, and a simplified tax invoice up to S$1,000 when the organisation chose them.

// InvoiceDocument is an invoice rendered as a document.
type InvoiceDocument struct {
	Template      AppliedTemplate `json:"template" doc:"The template version it rendered with: the customer's, the organisation's default, or the platform's starter."`
	PDF           []byte          `json:"pdf" doc:"The document, as PDF/A-2a and PDF/UA-1, base64." example:"JVBERi0xLjc="`
	SHA256        string          `json:"sha256" doc:"The PDF's SHA-256, in hex: the same version and template always give the same bytes." example:"a18760db1dd480808a9d4b2fd9b460608c4312c8998317e5ef444d83c0061bda"`
	TaxInvoice    bool            `json:"tax_invoice" doc:"Whether it is a tax invoice: the organisation is GST-registered on its date." example:"true"`
	Simplified    bool            `json:"simplified" doc:"Whether it is a simplified tax invoice: sales.simplified_tax_invoices is on and its total with GST is at most S$1,000.00." example:"false"`
	Required      []Particular    `json:"required_particulars" nullable:"false" doc:"The IRAS particulars it must show (FR-AR-05); none before the organisation is GST-registered. A draft leaves out number: it takes one when it is issued."`
	Missing       []Particular    `json:"missing_particulars" nullable:"false" doc:"Those the rendered document lacks, such as the organisation's address when its profile has none; empty when none is missing."`
	InvoiceNumber string          `json:"invoice_number,omitempty" doc:"The invoice's number; a draft has none." example:"INV-0042"`
}

type invoiceDocumentOutput struct{ Body InvoiceDocument }

func registerInvoiceDocuments(a *API) {
	register(a, authz.GetInvoiceDocument, huma.Operation{
		OperationID: "getInvoiceDocument", Method: http.MethodGet, Path: "/v1/invoices/{invoice_id}/document", Tags: []string{"Sales"},
		Summary: "Render an invoice as a document",
		Description: "Renders an invoice's current version with the template that applies to its customer (FR-TPL-05) through the " +
			"render service, and finds the IRAS particulars in it (FR-AR-05): once the organisation is GST-registered on its date it " +
			"is a tax invoice with the supplier's name, address and GST number, the number and date, the customer's name and address, " +
			"each line's description and GST rate, the total before GST, the GST and the total with GST; in another currency also its " +
			"totals in SGD at its rate; with sales.simplified_tax_invoices on and a total with GST of at most S$1,000.00, a simplified " +
			"tax invoice. A draft renders without a number. Read-only; no side effects.",
	}, []string{CodeNotFound, invoice.CodeNotInvoice, invoice.CodeBadDraft, salesdoc.CodeNotInvoice, salesdoc.CodeUnready, doctemplate.CodeRenderer},
		func(ctx context.Context, in *InvoicePath) (*invoiceDocumentOutput, error) {
			id, err := parseID("invoice_id", in.InvoiceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			var out InvoiceDocument
			var data salesdoc.Invoice
			var applied doctemplate.Applied
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				doc, err := u.Documents().Get(ctx, id)
				if err != nil {
					return err
				}
				if !invoice.IsInvoice(doc) {
					return &invoice.Error{Code: invoice.CodeNotInvoice, Message: "the document is not an invoice", Fix: "Open it with its own module"}
				}
				s, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return err
				}
				if data, err = salesdoc.InvoiceData(ctx, u, doc, s); err != nil {
					return err
				}
				d, err := invoice.Read(doc, s.Base)
				if err != nil {
					return err
				}
				applied, err = doctemplate.Applying(ctx, u, doctemplate.KindDocument, doctemplate.Invoice, &d.CustomerID)
				out.InvoiceNumber = doc.Number
				return err
			})
			if err != nil {
				return nil, err
			}
			res, err := a.renderInvoice(ctx, applied.Content, data)
			if err != nil {
				return nil, err
			}
			out.Template = AppliedTemplate{Kind: doctemplate.KindDocument, DocumentType: doctemplate.Invoice, Source: applied.Source, Version: applied.Version,
				Hash: hex.EncodeToString(applied.Hash)}
			if applied.TemplateID != nil {
				out.Template.Template = &ChosenTemplate{ID: applied.TemplateID.String(), Name: applied.Name, PublishedVersion: applied.Version}
			}
			out.PDF, out.SHA256 = res.PDF, res.SHA256
			out.TaxInvoice, out.Simplified = data.Data.GSTRegistered, data.Data.Simplified
			out.Required, out.Missing = []Particular{}, []Particular{}
			numbered := out.InvoiceNumber != "" // a draft takes its number when it is issued
			for _, x := range render.Required(doctemplate.Invoice, data.Data) {
				if numbered || x != render.PNumber {
					out.Required = append(out.Required, Particular(x))
				}
			}
			for _, x := range res.Missing {
				if numbered || x != render.PNumber {
					out.Missing = append(out.Missing, Particular(x))
				}
			}
			return &invoiceDocumentOutput{Body: out}, nil
		})
}

// renderInvoice renders an invoice's data with a template, finding its particulars, at the time it was issued.
func (a *API) renderInvoice(ctx context.Context, c doctemplate.Content, d salesdoc.Invoice) (render.Response, error) {
	if a.deps.Renderer == nil {
		return render.Response{}, problem(doctemplate.CodeRenderer, "No render service is configured, so the invoice cannot be rendered.",
			"Ask the operator to configure the render service")
	}
	return a.renderDocumentAt(ctx, doctemplate.Invoice, c, d.Data, d.Created)
}
