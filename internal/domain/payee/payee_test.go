package payee_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A change's account is checked as entered (FR-CT-03; PRD-001 rev 0.152): a Singapore account at a Singapore bank by its
// SWIFT/BIC, 6 to 17 digits; an overseas account outside Singapore, 4 to 34 letters and digits, or an IBAN whose check
// digits pass and whose country is the bank's; the BIC's country is the bank's; the name and note fit. Every failed
// rule comes back at once.
// req: FR-CT-03
// req: master-data/contacts One approved account per currency (rev 0.152)
func TestAChangesAccountIsChecked(t *testing.T) {
	sg := payee.Draft{Currency: "SGD", Kind: payee.Singapore, BIC: "DBSSSGSG", BankCountry: "SG", AccountName: "Northwind Pte Ltd", Number: "012-345678-9"}
	gb := payee.Draft{Currency: "GBP", Kind: payee.Overseas, BIC: "NWBKGB2L", BankCountry: "GB", AccountName: "Northwind UK Ltd", Number: "GB82 WEST 1234 5698 7654 32"}
	for name, c := range map[string]struct {
		d     payee.Draft
		codes []string
	}{
		"a Singapore account":           {sg, nil},
		"an IBAN":                       {gb, nil},
		"an overseas account number":    {with(gb, func(d *payee.Draft) { d.Number = "12345678" }), nil},
		"an 11-character BIC":           {with(sg, func(d *payee.Draft) { d.BIC = "DBSSSGSGXXX" }), nil},
		"letters in a Singapore number": {with(sg, func(d *payee.Draft) { d.Number = "01234A6789" }), []string{payee.CodeNumber}},
		"a Singapore number too short":  {with(sg, func(d *payee.Draft) { d.Number = "12345" }), []string{payee.CodeNumber}},
		"an IBAN off by one digit":      {with(gb, func(d *payee.Draft) { d.Number = "GB82WEST12345698765431" }), []string{payee.CodeNumber}},
		"an IBAN of another country":    {with(gb, func(d *payee.Draft) { d.BIC, d.BankCountry = "DEUTDEFF", "DE" }), []string{payee.CodeNumber}},
		"a foreign bank as Singapore":   {with(sg, func(d *payee.Draft) { d.BIC = "NWBKGB2L" }), []string{payee.CodeCountry}},
		"a Singapore bank as overseas":  {with(sg, func(d *payee.Draft) { d.Kind = payee.Overseas }), []string{payee.CodeCountry}},
		"not a BIC":                     {with(sg, func(d *payee.Draft) { d.BIC = "DBS" }), []string{payee.CodeBIC}},
		"an unknown currency":           {with(sg, func(d *payee.Draft) { d.Currency = "XYZ" }), []string{payee.CodeCurrency}},
		"no name":                       {with(sg, func(d *payee.Draft) { d.AccountName = "  " }), []string{payee.CodeName}},
		"everything wrong at once": {payee.Draft{Currency: "SG", Kind: "elsewhere", BIC: "x", BankCountry: "Singapore", Number: "?"},
			[]string{payee.CodeCurrency, payee.CodeCountry, payee.CodeCountry, payee.CodeBIC, payee.CodeName}},
	} {
		var got []string
		for _, p := range payee.Check(c.d, true) {
			got = append(got, p.Code)
		}
		if len(got) != len(c.codes) {
			t.Errorf("%s: %v, want %v", name, got, c.codes)
			continue
		}
		for i := range got {
			if got[i] != c.codes[i] {
				t.Errorf("%s: %v, want %v", name, got, c.codes)
			}
		}
	}
	if n := payee.Normalise(" gb82 west-1234\t5698 7654 32 "); n != "GB82WEST12345698765432" {
		t.Errorf("Normalise = %q", n)
	}
	if !payee.IBAN("GB82WEST12345698765432") || payee.IBAN("GB82WEST12345698765433") || payee.IBAN("0123456789") {
		t.Error("the IBAN check digits")
	}
}

func with(d payee.Draft, change func(*payee.Draft)) payee.Draft {
	change(&d)
	return d
}

// A number is kept sealed for its change, vendor and currency, with its last four characters and a fingerprint of its
// bank and number: the same number at the same bank, however it was typed, has the same fingerprint, and at another bank
// another one; sealed for one change it opens for that change only (SEC-11, FR-CT-04).
// req: SEC-11
// req: FR-CT-04
// asvs: V11.3.3
func TestANumberIsSealedForItsChange(t *testing.T) {
	ctx := context.Background()
	c := fieldseal.Sealer{KEK: fieldseal.AESKEK{Name: "test", Key: bytes.Repeat([]byte{1}, 32)},
		MAC: func(_ context.Context, data []byte) ([]byte, error) {
			h := hmac.New(sha256.New, []byte("fingerprint key"))
			h.Write(data)
			return h.Sum(nil), nil
		}}
	org, change, vendor := uuid.New(), uuid.New(), uuid.New()
	d := payee.Draft{Currency: "SGD", Kind: payee.Singapore, BIC: "DBSSSGSG", BankCountry: "SG", AccountName: "Northwind", Number: "012-345678-9"}
	x, err := payee.NumberOf(ctx, c, org, d)
	if err != nil || x.Last4 != "6789" || x.Kind != "account" {
		t.Fatalf("NumberOf = %+v, %v", x, err)
	}
	typedOtherwise := d
	typedOtherwise.Number = "0123456789"
	y, _ := payee.NumberOf(ctx, c, org, typedOtherwise)
	otherBank := d
	otherBank.BIC = "OCBCSGSG"
	z, _ := payee.NumberOf(ctx, c, org, otherBank)
	if !bytes.Equal(x.Fingerprint, y.Fingerprint) || bytes.Equal(x.Fingerprint, z.Fingerprint) {
		t.Error("the fingerprint is not of the bank and the number")
	}
	sealed, err := x.Seal(ctx, c, org, change, payee.Vendor(vendor), "SGD")
	if err != nil || bytes.Contains(sealed, []byte("0123456789")) {
		t.Fatalf("Seal = %x, %v", sealed, err)
	}
	a := storage.PayeeBankAccount{ChangeID: change, ContactID: vendor, Currency: "SGD", Sealed: sealed}
	if n, err := payee.Open(ctx, c, org, a); err != nil || n != "0123456789" {
		t.Fatalf("Open = %q, %v", n, err)
	}
	a.ChangeID = uuid.New()
	if _, err := payee.Open(ctx, c, org, a); err == nil {
		t.Error("a number opened for another change")
	}
	if _, err := payee.NumberOf(ctx, nil, org, d); err == nil {
		t.Error("no field keys, and a number was kept")
	}

	// A person's number is sealed under its own binding: it opens as the person's, never as a vendor's of the same IDs.
	person := uuid.New()
	mine, err := x.Seal(ctx, c, org, change, payee.Person(person), "SGD")
	if err != nil {
		t.Fatalf("Seal for a person = %v", err)
	}
	p := storage.PayeeBankAccount{ChangeID: change, MemberUserID: person, Currency: "SGD", Sealed: mine}
	if n, err := payee.Open(ctx, c, org, p); err != nil || n != "0123456789" {
		t.Fatalf("Open for a person = %q, %v", n, err)
	}
	asVendor := storage.PayeeBankAccount{ChangeID: change, ContactID: person, Currency: "SGD", Sealed: mine}
	if _, err := payee.Open(ctx, c, org, asVendor); err == nil {
		t.Error("a person's number opened as a vendor's")
	}
}

// A person's own account for reimbursements is a Singapore bank's, in SGD (FR-AP-22; rev 0.154): an overseas account or
// another currency is refused for a person, as a vendor may have them, every problem at once; and a stored person's change
// names the person, never a vendor.
// req: FR-AP-22
// req: purchases/payables Entered by the person
func TestAPersonsAccountIsSingaporeInSGD(t *testing.T) {
	person, vendor := payee.Person(uuid.New()), payee.Vendor(uuid.New())
	sg := payee.Draft{Currency: "SGD", Kind: payee.Singapore, BIC: "DBSSSGSG", BankCountry: "SG", AccountName: "Tan Ah Kow", Number: "0123456789"}
	gb := payee.Draft{Currency: "GBP", Kind: payee.Overseas, BIC: "NWBKGB2L", BankCountry: "GB", AccountName: "Tan Ah Kow", Number: "GB82 WEST 1234 5698 7654 32"}
	if probs := payee.CheckFor(person, sg, true); len(probs) != 0 {
		t.Errorf("a person's Singapore SGD account: %v", probs)
	}
	if probs := payee.CheckFor(vendor, gb, true); len(probs) != 0 {
		t.Errorf("a vendor's overseas account: %v", probs)
	}
	probs := payee.CheckFor(person, gb, true)
	if len(probs) != 2 || probs[0].Code != payee.CodePerson || probs[0].Field != "kind" || probs[1].Field != "currency" {
		t.Errorf("a person's overseas GBP account: %v", probs)
	}
	usd := with(sg, func(d *payee.Draft) { d.Currency = "USD" })
	if probs := payee.CheckFor(person, usd, true); len(probs) != 1 || probs[0].Code != payee.CodePerson {
		t.Errorf("a person's USD account at a Singapore bank: %v", probs)
	}
	b, err := payee.EncodeFor(person, sg, payee.Number{Last4: "6789", Fingerprint: []byte{1}, Kind: "account"})
	if err != nil || bytes.Contains(b, []byte("contact_id")) || !bytes.Contains(b, []byte(person.PersonID.String())) ||
		bytes.Contains(b, []byte("0123456789")) {
		t.Fatalf("a person's stored change = %s, %v", b, err)
	}
	s, err := payee.Read(storage.Document{Type: storage.DocumentClaimantBankChange, Payload: b})
	if err != nil {
		t.Fatal(err)
	}
	if who, err := s.Payee(); err != nil || who != person || !who.IsPerson() || who.DocumentType() != storage.DocumentClaimantBankChange {
		t.Errorf("the stored change's payee = %+v, %v", who, err)
	}
}
