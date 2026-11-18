package paymentfile

import (
	"fmt"
	"slices"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ which file a money account gives
//
// Each money account says which file a batch paying from it gives (FR-BNK-12, rev 0.157; p1-payment-files design D1,
// D2): the payment list, for any bank, by default; Wise's batch payment file for a Wise account; DBS IDEAL's UFF for a DBS
// account. Each is built when its specification is in hand (the owner's decision of 28 Oct 2026): until then it is
// known, shown as not available yet, and refused. Airwallex takes no file, since its new batches must be the .xlsx
// downloaded from the account: an Airwallex account gives the payment list until its payout API (P2; design D5).

// The codes of setting and using a money account's payment file.
const (
	CodeProvider = "PAYFILE-01" // the file is not one the account's bank or provider takes
	CodeNotBuilt = "PAYFILE-02" // the file is not built yet: its specification is not in hand
)

// Error is a payment file refused for an account, in plain words (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Format is a payment file a money account may be set to.
type Format struct {
	Key   string // as the money account keeps it
	Words string // what the screens call it
	// Provider is the bank or provider whose accounts take it, by its key in banking's list; "" for any.
	Provider, ProviderName string
	// Awaits is, until it is built, the specification it is written from.
	Awaits string
}

// formats are the files the owner decided on (rev 0.157), in the order the screens offer them.
var formats = []Format{
	{Key: storage.PaymentFileList, Words: "the payment list"},
	{Key: storage.PaymentFileWise, Words: "Wise's batch payment file", Provider: "wise", ProviderName: "Wise",
		Awaits: "Wise's template for paying SGD to bank accounts"},
	{Key: storage.PaymentFileDBS, Words: "DBS IDEAL's UFF", Provider: "dbs", ProviderName: "DBS",
		Awaits: "DBS's specification, DBS UFF Version 2.2"},
}

// Formats are the payment files Fyniq knows, built or not.
func Formats() []Format { return slices.Clone(formats) }

// Lookup is a payment file by its key.
func Lookup(key string) (Format, bool) {
	for _, f := range formats {
		if f.Key == key {
			return f, true
		}
	}
	return Format{}, false
}

// Writers are the built files' writers, by the file's key.
type Writers map[string]Writer

// Built are the writers built so far: the payment list. Wise's batch payment file and DBS IDEAL's UFF join once their
// specifications are in hand (p1-payment-files tasks 2.1 and 3.1).
func Built() Writers { return Writers{storage.PaymentFileList: PaymentList{}} }

// Choice is a payment file as an account may or may not be set to it.
type Choice struct {
	Format
	Offered bool
	Reason  string // why it is not offered; "" when it is
}

// Choices are every payment file, offered or not, for an account at provider ("" for one not on banking's list).
func (w Writers) Choices(provider string) []Choice {
	out := make([]Choice, 0, len(formats))
	for _, f := range formats {
		c := Choice{Format: f, Offered: true}
		if err := w.Check(f.Key, provider); err != nil {
			c.Offered, c.Reason = false, err.Message
		}
		out = append(out, c)
	}
	return out
}

// Check refuses a payment file an account at provider cannot give: one its bank or provider does not take, or one not
// built yet (design D2). The bank's own check comes first, so the refusal says what would stay true once it is built.
func (w Writers) Check(key, provider string) *Error {
	f, ok := Lookup(key)
	if !ok {
		return &Error{Code: CodeProvider, Message: fmt.Sprintf("Payment file: %q is not a payment file Fyniq writes", key),
			Fix: "Choose payment-list, wise-batch or dbs-uff"}
	}
	if f.Provider != "" && f.Provider != provider {
		return &Error{Code: CodeProvider, Message: fmt.Sprintf("Payment file: %s is only for a %s account, and this account is not at %s",
			f.Words, f.ProviderName, f.ProviderName), Fix: fmt.Sprintf("Choose the payment list, or set the account's bank to %s if it is at %s",
			f.ProviderName, f.ProviderName)}
	}
	if w[key] == nil {
		return &Error{Code: CodeNotBuilt, Message: fmt.Sprintf("Payment file: %s is not available yet; Fyniq writes it once %s is in hand",
			f.Words, f.Awaits), Fix: "Keep the payment list for now: its batches are keyed into the bank"}
	}
	return nil
}

// Writer is the writer of a payment file an account at provider gives, or why it gives none (design D1, D2). An account
// keeps the payment list when it was never set.
func (w Writers) Writer(key, provider string) (Writer, *Error) {
	if key == "" {
		key = storage.PaymentFileList
	}
	if err := w.Check(key, provider); err != nil {
		return nil, err
	}
	return w[key], nil
}
