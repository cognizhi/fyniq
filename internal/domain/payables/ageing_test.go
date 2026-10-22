package payables

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	sgd  = money.MustLookup("SGD")
	usd  = money.MustLookup("USD")
	asAt = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
)

func m(t *testing.T, s string, c money.Currency) money.Money {
	t.Helper()
	x, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func day(mo time.Month, d int) time.Time { return time.Date(2026, mo, d, 0, 0, 0, 0, time.UTC) }

func openBill(t *testing.T, number string, due time.Time, open, carrying string, cur money.Currency) Item {
	return Item{Kind: KindBill, DocumentID: uuid.New(), Number: number, DueDate: due, Open: m(t, open, cur), Carrying: m(t, carrying, sgd)}
}

func row(r Row) string {
	var b []string
	for _, x := range r.Buckets {
		b = append(b, x.String())
	}
	return fmt.Sprintf("%s %s credits %s total %s carrying %s", r.Currency, strings.Join(b, " "), r.Credits, r.Total, r.Carrying)
}

// Worked by hand at 31 October: a bill due on the day is current; 1 day past due is 1–30; 30 days is 1–30 and 31 is
// 31–60; 61, 90 and 91 days are 61–90, 61–90 and 90+. A supplier credit is apart. A USD bill is its own row, carried at
// its book rate.
// req: FR-AP-06
func TestAgeingBucketsByDaysPastDue(t *testing.T) {
	atlassian := Supplier{ID: uuid.New(), Name: "Atlassian Pty Ltd", Items: []Item{
		openBill(t, "BILL-0001", day(10, 31), "100.00", "100.00", sgd),
		openBill(t, "BILL-0002", day(10, 30), "10.00", "10.00", sgd),
		openBill(t, "BILL-0003", day(10, 1), "20.00", "20.00", sgd),
		openBill(t, "BILL-0004", day(9, 30), "30.00", "30.00", sgd),
		openBill(t, "BILL-0005", day(8, 31), "40.00", "40.00", sgd),
		openBill(t, "BILL-0006", day(8, 2), "50.00", "50.00", sgd),
		openBill(t, "BILL-0007", day(8, 1), "60.00", "60.00", sgd),
		{Kind: KindCredit, DocumentID: uuid.New(), Number: "PAY-0003", Open: m(t, "-25.00", sgd), Carrying: m(t, "-25.00", sgd)},
	}}
	vercel := Supplier{ID: uuid.New(), Name: "Vercel Inc", Items: []Item{openBill(t, "BILL-0008", day(11, 13), "1090.00", "1460.60", usd)}}
	a, err := Age(Ledger{AsAt: asAt, Suppliers: []Supplier{atlassian, vercel}}, sgd)
	if err != nil {
		t.Fatal(err)
	}
	if got := row(a.Suppliers[0].Rows[0]); got != "SGD 100.00 30.00 30.00 90.00 60.00 credits -25.00 total 285.00 carrying 285.00" {
		t.Errorf("Atlassian: %s", got)
	}
	if len(a.Totals) != 2 || row(a.Totals[1]) != "USD 1090.00 0.00 0.00 0.00 0.00 credits 0.00 total 1090.00 carrying 1460.60" {
		t.Errorf("the totals: %+v", a.Totals)
	}
}

// Worked by hand at 31 October, for the next 30 days (to 30 November): BILL-0003 (S$20.00, due 1 Oct) is overdue;
// BILL-0001 (S$100.00, due 31 Oct, the day itself) and BILL-0009 (S$5.00, due 30 Nov, the last day) fall due within;
// BILL-0010 (due 1 Dec) is after; the credit is not taken off; a paid bill is not listed. They come by due date.
// req: FR-AP-06
func TestBillsToPayAndTheCashTheyNeed(t *testing.T) {
	s := Supplier{ID: uuid.New(), Name: "Atlassian Pty Ltd", Items: []Item{
		openBill(t, "BILL-0001", day(10, 31), "100.00", "100.00", sgd),
		openBill(t, "BILL-0003", day(10, 1), "20.00", "20.00", sgd),
		openBill(t, "BILL-0009", day(11, 30), "5.00", "5.00", sgd),
		openBill(t, "BILL-0010", day(12, 1), "7.00", "7.00", sgd),
		openBill(t, "BILL-0011", day(10, 5), "0.00", "0.00", sgd),
		{Kind: KindCredit, DocumentID: uuid.New(), Number: "PAY-0003", Open: m(t, "-25.00", sgd), Carrying: m(t, "-25.00", sgd)},
	}}
	items, req, err := ToPay(Ledger{AsAt: asAt, Suppliers: []Supplier{s}}, sgd, 30)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Number)
	}
	if strings.Join(got, ",") != "BILL-0003,BILL-0001,BILL-0009" {
		t.Errorf("to pay: %v", got)
	}
	if len(req) != 1 || req[0].Overdue.String() != "20.00" || req[0].Within.String() != "105.00" || req[0].Total.String() != "125.00" || req[0].Carrying.String() != "125.00" {
		t.Errorf("the cash requirement: %+v", req)
	}
}
