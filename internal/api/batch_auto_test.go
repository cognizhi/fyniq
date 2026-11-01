//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// namedLineOn is a waiting line of amount on m whose description is text, as the bank prints the reference keyed in.
func (e *env) namedLineOn(m storage.MoneyAccount, amount, text string) storage.BankLine {
	e.t.Helper()
	var lines []storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, lines, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 21), PeriodTo: porttest.Day(10, 21), Currency: "SGD", At: e.clock.Now(),
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 21), Amount: storage.Decimal(amount), Description: text,
				Fingerprint: strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")}}})
		return err
	})
	return lines[0]
}

// A line that names a released batch's number, for the amount of exactly one of its unmatched payments, reconciles to
// that payment by itself, within the line cap, and queues its supplier's remittance advice as a person's match does
// (FR-BNK-14, rev 0.160; design D1). Two payments of the same amount, a line that does not name the batch, one above
// the cap and one for a whole batch's total stay in the queue; a batch paid from another account is not matched; and
// what the system matched is undone like any other.
// req: FR-BNK-14
// req: FR-BNK-12
// req: banking/bank-transactions A batch's payment matches by itself (rev 0.160)
func TestABatchPaymentReconcilesByItself(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	northwind, harbour, big := e.vendor("Northwind Analytics Pte Ltd"), e.vendor("Harbour Supplies Pte Ltd"), e.vendor("Big Ticket Pte Ltd")
	if r := e.call(http.MethodPatch, "/v1/contacts/"+northwind, accountant, map[string]any{"remittance_email": "payments@northwind.example"}, "If-Match", "*"); r.Status != http.StatusOK {
		t.Fatalf("setting the remittance email = %d: %s", r.Status, r.Raw)
	}
	e.vendorAccount(northwind, "0123456789")
	e.vendorAccount(harbour, "2223334445")
	e.vendorAccount(big, "3334445556")
	// Northwind 109.00; Harbour two bills of 54.50, paid as one payment of 109.00 too; and Big Ticket 654.00, above the cap.
	rel, m := e.releasedBatch("bill:"+e.billOf(northwind, "100.00", "9.00"), "bill:"+e.billOf(harbour, "50.00", "4.50"),
		"bill:"+e.billOf(harbour, "50.00", "4.50"), "bill:"+e.billOf(big, "600.00", "54.00"))
	number := rel.str("number")
	reconcileNow := func() map[string]any {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", accountant, map[string]any{"money_account_id": m.ID.String()})
		if r.Status != http.StatusOK {
			t.Fatalf("reconcile now = %d: %s", r.Status, r.Raw)
		}
		return r.Body
	}
	statusOf := func(l storage.BankLine) string {
		var x storage.BankLine
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			x, err = u.Banking().Line(ctx, l.ID)
			return err
		})
		return x.Status
	}

	// Two payments of 109.00 in the batch: the line names the batch but could be either, so a person decides.
	twice := e.namedLineOn(m, "-109.00", "FAST "+number+" BILL-0001")
	if got := reconcileNow(); got["reconciled"].(float64) != 0 || statusOf(twice) != storage.LineUnreconciled {
		t.Fatalf("a line two payments could be: %v, %s", got, statusOf(twice))
	}
	// A line for the batch's total, and one above the cap, stay too; a line not naming the batch matches nothing.
	total := e.namedLineOn(m, "-872.00", "GIRO "+number)
	above := e.namedLineOn(m, "-654.00", "GIRO "+number+" BILL-0004")
	reconcileNow()
	if statusOf(total) != storage.LineUnreconciled || statusOf(above) != storage.LineUnreconciled {
		t.Fatalf("the total and the payment above the cap: %s, %s", statusOf(total), statusOf(above))
	}

	// Once the person matches Harbour's payment, the line naming the batch can only be Northwind's: it reconciles by
	// itself, as an exact match of the payment, and Northwind's advice is queued.
	harbourPay := paymentOf(t, rel, "Harbour Supplies Pte Ltd")["payment_id"].(string)
	hl := e.lineOn(m, "-109.00", nil)
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+hl.ID.String()+"/match-recorded", accountant, map[string]any{"document_id": harbourPay}, "If-Match", etag(hl.Version)); r.Status != http.StatusOK {
		t.Fatalf("matching Harbour's payment = %d: %s", r.Status, r.Raw)
	}
	if got := reconcileNow(); got["reconciled"].(float64) != 1 || statusOf(twice) == storage.LineUnreconciled {
		t.Fatalf("the line naming the batch: %v, %s", got, statusOf(twice))
	}
	nw := paymentOf(t, rel, "Northwind Analytics Pte Ltd")["payment_id"].(string)
	var auto storage.BankMatch
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc := uuid.MustParse(nw)
		ms, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{DocumentID: &doc})
		if len(ms) != 1 || ms[0].Basis != storage.BasisExact || ms[0].Kind != storage.MatchRecorded || ms[0].LineID != twice.ID {
			t.Fatalf("Northwind's match: %+v", ms)
		}
		auto = ms[0]
		return err
	})
	if n := e.runRemittances(); n != 2 {
		t.Errorf("%d remittance jobs, want Harbour's (no email) and Northwind's", n)
	}
	g := e.call(http.MethodGet, "/v1/payment-batches/"+rel.str("id"), accountant, nil)
	if p := paymentOf(t, g, "Northwind Analytics Pte Ltd"); p["remittance"] != "sent" || p["paid"] != true {
		t.Errorf("Northwind's payment after the system matched it: %v", p)
	}
	if l := e.call(http.MethodGet, "/v1/auto-reconciliations", accountant, nil); !strings.Contains(l.Raw, twice.ID.String()) {
		t.Errorf("the auto-reconciled list lacks the line: %s", l.Raw)
	}

	// A batch paid from another account is not this line's: its own account's lines are.
	other, om := e.releasedBatch("bill:"+e.billOf(northwind, "10.00", "0.90"), "bill:"+e.billOf(harbour, "20.00", "1.80"))
	e.namedLineOn(m, "-10.90", "FAST "+other.str("number"))
	if got := reconcileNow(); got["reconciled"].(float64) != 0 {
		t.Errorf("a line on another account matched %v", got)
	}
	// On the batch's own account, a line for its total under the cap is still never matched by itself; the payment's
	// line is, its number in lower case.
	whole := e.namedLineOn(om, "-32.70", "GIRO "+other.str("number"))
	if l := e.namedLineOn(om, "-10.90", "fast "+strings.ToLower(other.str("number"))); true {
		r := e.call(http.MethodPost, "/v1/bank-lines:auto-reconcile", accountant, map[string]any{"money_account_id": om.ID.String()})
		if r.Status != http.StatusOK || r.Body["reconciled"].(float64) != 1 || statusOf(l) == storage.LineUnreconciled {
			t.Errorf("the batch's own account, its number in lower case: %d %s", r.Status, r.Raw)
		}
		if statusOf(whole) != storage.LineUnreconciled {
			t.Errorf("a line for the batch's total was matched: %s", statusOf(whole))
		}
	}

	// Undone, the line goes back to the queue and the payment is unmatched; nothing is reversed, as nothing was posted.
	var line storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		line, err = u.Banking().Line(ctx, auto.LineID)
		return err
	})
	if u := e.call(http.MethodPost, "/v1/bank-lines/"+line.ID.String()+"/unreconcile", accountant, map[string]any{"reason": "Not this payment"}, "If-Match", etag(line.Version)); u.Status != http.StatusOK {
		t.Fatalf("undoing the system's match = %d: %s", u.Status, u.Raw)
	}
	if statusOf(line) != storage.LineUnreconciled {
		t.Errorf("the undone line is %s", statusOf(line))
	}
}
