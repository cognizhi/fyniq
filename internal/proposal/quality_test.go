package proposal

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The month's approval-quality metrics are computed from the proposals' own records: those made and how they ended, the share approved within ten
// seconds of the decider's first look (or with no look at all) with the whole payload, the median time to a decision, the approvals per approver,
// the share of rejections with a reason and each task type's share.
// req: CTL-AI-07
// req: AI-92
func TestTheMonthsApprovalQualityIsMeasuredFromTheProposals(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	ann, bob := uuid.New(), uuid.New()
	d := func(sec int) *time.Time { x := at.Add(time.Duration(sec) * time.Second); return &x }
	row := func(typ, status string, by *uuid.UUID, decided, viewed *time.Time, whole bool, reason string) storage.AIQualityRow {
		return storage.AIQualityRow{ID: uuid.New(), TaskType: typ, Status: status, ProposedAt: at, DecidedBy: by, DecidedAt: decided, ApprovedWhole: whole, Reason: reason, FirstViewedAt: viewed}
	}
	q := MeasureQuality([]storage.AIQualityRow{
		row("sec.create_task", storage.ProposalExecuted, &ann, d(60), d(55), true, ""),  // opened 5 s before: quick
		row("sec.create_task", storage.ProposalExecuted, &ann, d(120), d(20), true, ""), // a real look: not quick
		row("ap.draft_bill", storage.ProposalExecuted, &bob, d(30), nil, true, ""),      // never opened: quick
		row("ap.draft_bill", storage.ProposalExecuted, &bob, d(40), d(35), false, ""),   // some items: not quick
		row("ap.draft_bill", storage.ProposalRejected, &bob, d(300), d(200), false, "Wrong supplier"),
		row("ap.draft_bill", storage.ProposalRejected, &bob, d(300), d(200), false, ""),
		row("sec.create_task", storage.ProposalExpired, nil, nil, nil, false, ""),
		row("sec.create_task", storage.ProposalInvalid, nil, nil, nil, false, "bad"),
		row("sec.create_task", storage.ProposalStale, &ann, d(90), d(10), true, ""),
	})
	if q.Made != 9 || q.Approved != 5 || q.Rejected != 2 || q.Expired != 1 || q.Invalid != 1 || q.Stale != 1 {
		t.Fatalf("the counts: %+v", q)
	}
	if q.QuickShare != 2.0/5 || q.RejectedWithReasonShare != 0.5 || q.MedianDecisionSeconds != 90 {
		t.Errorf("the shares and the median: quick %v, reasons %v, median %v", q.QuickShare, q.RejectedWithReasonShare, q.MedianDecisionSeconds)
	}
	if len(q.PerApprover) != 2 || q.PerApprover[0].User != ann || q.PerApprover[0].Approved != 3 || q.PerApprover[0].Quick != 1 || q.PerApprover[1].Quick != 1 {
		t.Errorf("per approver: %+v", q.PerApprover)
	}
	if len(q.TaskTypes) != 2 || q.TaskTypes[0].TaskType != "sec.create_task" || q.TaskTypes[0].Count != 5 || q.TaskTypes[1].Count != 4 {
		t.Errorf("per task type: %+v", q.TaskTypes)
	}
	if empty := MeasureQuality(nil); empty.Made != 0 || empty.QuickShare != 0 || empty.PerApprover == nil || empty.TaskTypes == nil {
		t.Errorf("an empty month: %+v", empty)
	}
}
