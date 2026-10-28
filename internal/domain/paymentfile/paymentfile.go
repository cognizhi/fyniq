// Package paymentfile writes a released payment batch as a file (FR-BNK-12; PRD-001 rev 0.156; p1-payments-out design
// D6): one writer per format, behind one interface. Until the bank is chosen and its own file is built, the batch is
// paid from a payment list keyed into the bank (the owner's decision of 28 Oct 2026). A file holds full account numbers:
// it is written only inside the release's unit of work and sealed there (design D7).
package paymentfile

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// File is what a payment file holds: the batch, the account it is paid from, and each payment.
type File struct {
	Batch       string // its number, such as PB-0003
	PaymentDate time.Time
	Method      string // fast or giro
	Debit       Debit
	Payments    []Payment
}

// Debit is the account the batch is paid from.
type Debit struct {
	Name, Identifier string
}

// Payment is one payee's payment.
type Payment struct {
	Payee       string // the supplier's legal name, or the person's name
	Kind        string // supplier or claimant
	BIC         string
	AccountName string // the name on the account, as the bank holds it
	Number      string // the full account number
	Amount      money.Money
	Reference   string // what the payee's statement shows: the batch and the documents it pays
}

// Out is a written file.
type Out struct {
	Name      string
	MediaType string
	Data      []byte
}

// Writer writes a batch in one format.
type Writer interface {
	// Name is the writer and its version, as a release records it, such as payment-list@1. A new layout is a new version.
	Name() string
	Write(f File) (Out, error)
}

// PaymentList is the payment list (payment-list@1, rev 0.156): a CSV (UTF-8, CRLF, RFC 4180 quoting) with a header row,
// one row per payment, and a closing row with the count and total, to key into a bank's bulk-payment screen.
type PaymentList struct{}

// Name is the writer's name and version.
func (PaymentList) Name() string { return "payment-list@1" }

var listHeader = []string{"Payee", "Payee type", "Bank (SWIFT/BIC)", "Name on account", "Account number", "Amount", "Currency", "Payment date",
	"Method", "Reference"}

// Write writes the list. A cell a spreadsheet would read as a formula, one that starts with =, +, -, @, a tab or a carriage
// return, is written with a leading apostrophe, so opening the list never runs it (CWE-1236).
func (PaymentList) Write(f File) (Out, error) {
	if len(f.Payments) == 0 {
		return Out{}, fmt.Errorf("paymentfile: batch %s has no payment", f.Batch)
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	day := f.PaymentDate.Format(time.DateOnly)
	method := strings.ToUpper(f.Method)
	if err := w.Write(listHeader); err != nil {
		return Out{}, err
	}
	total := money.Zero(f.Payments[0].Amount.Currency())
	for _, p := range f.Payments {
		var err error
		if total, err = total.Add(p.Amount); err != nil {
			return Out{}, err
		}
		row := []string{p.Payee, p.Kind, p.BIC, p.AccountName, p.Number, p.Amount.String(), p.Amount.Currency().Code, day, method, p.Reference}
		if err := w.Write(inert(row)); err != nil {
			return Out{}, err
		}
	}
	closing := []string{"Total", "", "", "", "", total.String(), total.Currency().Code, day, method,
		fmt.Sprintf("%d payments from %s %s", len(f.Payments), f.Debit.Name, f.Debit.Identifier)}
	if err := w.Write(inert(closing)); err != nil {
		return Out{}, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return Out{}, err
	}
	return Out{Name: f.Batch + "-payment-list.csv", MediaType: "text/csv", Data: b.Bytes()}, nil
}

// inert prefixes each cell that a spreadsheet would read as a formula with an apostrophe.
func inert(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		if c != "" && strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			c = "'" + c
		}
		out[i] = c
	}
	return out
}
