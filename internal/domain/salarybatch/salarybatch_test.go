package salarybatch

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The legal moves of a payment line are Nemura's (PRD-002 NM-PAY-10): the first event may be any state, paid goes back only to returned, an unknown outcome is resolved only to paid,
// rejected or returned, and a payment that came back may be scheduled again.
// req: FR-PPL-12
func TestALinkMovesOnlyAsPaymentsDo(t *testing.T) {
	for _, c := range []struct {
		from, to string
		ok       bool
	}{
		{storage.LineScheduled, storage.LineReleased, true}, {storage.LineReleased, storage.LinePaid, true}, {storage.LinePaid, storage.LineReturned, true},
		{storage.LinePaid, storage.LineRejected, false}, {storage.LinePaid, storage.LineScheduled, false}, {storage.LineUnknown, storage.LineScheduled, false},
		{storage.LineUnknown, storage.LinePaid, true}, {storage.LineRejected, storage.LineScheduled, true}, {storage.LineReleased, storage.LineScheduled, false},
		{storage.LineSent, storage.LineReleased, false},
	} {
		if got := CanMove(c.from, c.to); got != c.ok {
			t.Errorf("%s to %s: %v, want %v", c.from, c.to, got, c.ok)
		}
	}
	if !BlocksInstruction(storage.LineUnknown) || BlocksInstruction(storage.LineRejected) {
		t.Error("only an unknown outcome blocks a new instruction")
	}
}

// What an approval binds is the lines' amounts and bank-detail versions, in no order of arrival, and neither a name nor an account: a revised line moves the digest, and the same lines
// in another order do not (FR-PPL-14, FR-PPL-17).
// req: FR-PPL-14
func TestTheApprovedDigestFollowsTheLinesAndNamesNobody(t *testing.T) {
	a := storage.SalaryLine{LineRef: uuid.New(), LineVersion: 1, PersonID: uuid.New(), Amount: "100.0000", BankDetailID: uuid.New(), BankDetailVersion: 1, BankDetailDigest: strings.Repeat("a", 64), Sealed: []byte("Ana 0123456789")}
	b := storage.SalaryLine{LineRef: uuid.New(), LineVersion: 1, PersonID: uuid.New(), Amount: "250.5000", BankDetailID: uuid.New(), BankDetailVersion: 2, BankDetailDigest: strings.Repeat("b", 64), Sealed: []byte("Ben 99")}
	d := LinesDigest([]storage.SalaryLine{a, b})
	if d != LinesDigest([]storage.SalaryLine{b, a}) {
		t.Error("the digest depends on the order the lines arrived in")
	}
	a2 := a
	a2.BankDetailVersion = 2
	if d == LinesDigest([]storage.SalaryLine{a2, b}) {
		t.Error("a new bank-detail version does not move the digest")
	}
	a3 := a
	a3.Sealed = []byte("another name and number")
	if d != LinesDigest([]storage.SalaryLine{a3, b}) {
		t.Error("the sealed content, which the approver may not see, is in the digest")
	}
	total, err := Total([]storage.SalaryLine{a, b})
	if err != nil || total.String() != "350.50" {
		t.Errorf("total %v %v", total, err)
	}
}

// A batch is paid only when every line is paid: a release is not paid, and a part paid is paid in part (FR-PPL-12).
// req: FR-PPL-12
func TestABatchIsPaidOnlyWhenEveryLineIs(t *testing.T) {
	x, y := storage.SalaryLine{LineRef: uuid.New()}, storage.SalaryLine{LineRef: uuid.New()}
	lines := []storage.SalaryLine{x, y}
	batch := storage.SalaryBatch{Status: storage.SalaryApproved}
	at := func(sx, sy string) string {
		return Overall(batch, map[uuid.UUID]storage.SalaryLineEvent{x.LineRef: {State: sx}, y.LineRef: {State: sy}}, lines)
	}
	for _, c := range []struct{ x, y, want string }{
		{"scheduled", "scheduled", "approved"}, {"released", "scheduled", "released_in_part"}, {"released", "sent", "released"}, {"paid", "released", "paid_in_part"},
		{"paid", "paid", "paid"}, {"accepted", "accepted", "released"}, {"rejected", "released", "released"},
	} {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("%s and %s: %s, want %s", c.x, c.y, got, c.want)
		}
	}
	if got := Overall(storage.SalaryBatch{Status: storage.SalaryCancelled}, nil, lines); got != "cancelled" {
		t.Errorf("cancelled: %s", got)
	}
}
