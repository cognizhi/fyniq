package paymentfile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/paymentfile"
)

func sgd(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, money.MustLookup("SGD"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The payment list of a batch (rev 0.156), worked by hand: a header, a supplier's payment and a claimant's, and the
// closing row whose total is the batch's, to the byte; a payee whose name a spreadsheet would run as a formula is
// written inert, and a name with a comma or a quote is quoted as RFC 4180 says.
// req: FR-BNK-12
// req: banking/bank-transactions A payment list until the bank's file (rev 0.156)
func TestThePaymentListIsWrittenToTheByte(t *testing.T) {
	f := paymentfile.File{Batch: "PB-0003", PaymentDate: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), Method: "fast",
		Debit: paymentfile.Debit{Name: "DBS SGD current", Identifier: "072-123456-7"},
		Payments: []paymentfile.Payment{
			{Payee: "Northwind Analytics Pte. Ltd.", Kind: "supplier", BIC: "DBSSSGSG", AccountName: "Northwind Analytics Pte. Ltd.", Number: "0123456789",
				Amount: sgd(t, "381.50"), Reference: "PB-0003 BILL-0041 BILL-0042 BILL-0043"},
			{Payee: "Tan, Ah \"Kow\"", Kind: "claimant", BIC: "OCBCSGSG", AccountName: "TAN AH KOW", Number: "601234567001", Amount: sgd(t, "27.30"),
				Reference: "PB-0003 CLM-0007"},
			{Payee: "=HYPERLINK(\"http://evil.example\",\"x\")", Kind: "supplier", BIC: "UOVBSGSG", AccountName: "-Evil", Number: "998877665",
				Amount: sgd(t, "1.00"), Reference: "PB-0003 BILL-0044"},
		}}
	out, err := paymentfile.PaymentList{}.Write(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "Payee,Payee type,Bank (SWIFT/BIC),Name on account,Account number,Amount,Currency,Payment date,Method,Reference\r\n" +
		"Northwind Analytics Pte. Ltd.,supplier,DBSSSGSG,Northwind Analytics Pte. Ltd.,0123456789,381.50,SGD,2026-10-31,FAST,PB-0003 BILL-0041 BILL-0042 BILL-0043\r\n" +
		"\"Tan, Ah \"\"Kow\"\"\",claimant,OCBCSGSG,TAN AH KOW,601234567001,27.30,SGD,2026-10-31,FAST,PB-0003 CLM-0007\r\n" +
		"\"'=HYPERLINK(\"\"http://evil.example\"\",\"\"x\"\")\",supplier,UOVBSGSG,'-Evil,998877665,1.00,SGD,2026-10-31,FAST,PB-0003 BILL-0044\r\n" +
		"Total,,,,,409.80,SGD,2026-10-31,FAST,3 payments from DBS SGD current 072-123456-7\r\n"
	if string(out.Data) != want {
		t.Errorf("the list:\n%q\nwant:\n%q", out.Data, want)
	}
	if out.Name != "PB-0003-payment-list.csv" || out.MediaType != "text/csv" || (paymentfile.PaymentList{}).Name() != "payment-list@1" {
		t.Errorf("the file's name and type: %s %s", out.Name, out.MediaType)
	}
	if strings.Contains(string(out.Data), ",=HYPERLINK") {
		t.Error("a formula is written live")
	}
	if _, err := (paymentfile.PaymentList{}).Write(paymentfile.File{Batch: "PB-0004"}); err == nil {
		t.Error("an empty batch was written")
	}
}
