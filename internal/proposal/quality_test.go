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

// The edit-before-approve rate, the reversals after approval and the breakdown per task type and approver (PRD-001 AI-33, rev 0.563; p2-ai-expansion):
// Ann approves three bills (one edited, one undone) and a task whole; Bob approves a bill whole and edits a task; one bill is rejected. So of 6
// approvals two were edited (a third) and one was undone (a sixth); the bills' median decision is that of their five decisions.
// req: AI-33
func TestApprovalQualityCountsEditsAndReversalsPerTaskTypeAndApprover(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	ann, bob := uuid.New(), uuid.New()
	d := func(sec int) *time.Time { x := at.Add(time.Duration(sec) * time.Second); return &x }
	row := func(typ, status string, by *uuid.UUID, sec int, whole bool, undone bool) storage.AIQualityRow {
		r := storage.AIQualityRow{ID: uuid.New(), TaskType: typ, Status: status, ProposedAt: at, DecidedBy: by, DecidedAt: d(sec), ApprovedWhole: whole, FirstViewedAt: d(sec - 60)}
		if undone {
			r.UndoneAt = d(sec + 3600)
		}
		return r
	}
	q := MeasureQuality([]storage.AIQualityRow{
		row("ap.draft_bill", storage.ProposalExecuted, &ann, 100, true, false),
		row("ap.draft_bill", storage.ProposalExecuted, &ann, 200, false, false), // edited
		row("ap.draft_bill", storage.ProposalExecuted, &ann, 300, true, true),   // undone
		row("sec.create_task", storage.ProposalExecuted, &ann, 50, true, false),
		row("ap.draft_bill", storage.ProposalExecuted, &bob, 400, true, false),
		row("sec.create_task", storage.ProposalExecuted, &bob, 80, false, false), // edited
		row("ap.draft_bill", storage.ProposalRejected, &bob, 500, false, false),
	})
	if q.Approved != 6 || q.Edited != 2 || q.Reversed != 1 || q.EditedShare != 2.0/6 || q.ReversedShare != 1.0/6 {
		t.Fatalf("the counts: %+v", q)
	}
	bills := q.TaskTypes[0]
	if bills.TaskType != "ap.draft_bill" || bills.Count != 5 || bills.Approved != 4 || bills.Edited != 1 || bills.Reversed != 1 || bills.MedianDecisionSeconds != 300 {
		t.Errorf("the bills: %+v", bills)
	}
	tasks := q.TaskTypes[1]
	if tasks.Approved != 2 || tasks.Edited != 1 || tasks.Reversed != 0 || tasks.MedianDecisionSeconds != 65 {
		t.Errorf("the tasks: %+v", tasks)
	}
	// Ann approved 4 (one edited), Bob 2 (one edited); per task type and approver, the most first, ties by task type.
	if len(q.PerApprover) != 2 || q.PerApprover[0].User != ann || q.PerApprover[0].Edited != 1 || q.PerApprover[1].Edited != 1 {
		t.Errorf("per approver: %+v", q.PerApprover)
	}
	want := []struct {
		user                       uuid.UUID
		typ                        string
		approved, edited, reversed int
	}{{ann, "ap.draft_bill", 3, 1, 1}, {bob, "ap.draft_bill", 1, 0, 0}, {ann, "sec.create_task", 1, 0, 0}, {bob, "sec.create_task", 1, 1, 0}}
	if len(q.PerApproverType) != 4 {
		t.Fatalf("per approver and task type: %+v", q.PerApproverType)
	}
	for _, w := range want {
		found := false
		for _, x := range q.PerApproverType {
			found = found || x.User == w.user && x.TaskType == w.typ && x.Approved == w.approved && x.Edited == w.edited && x.Reversed == w.reversed
		}
		if !found {
			t.Errorf("missing %+v among %+v", w, q.PerApproverType)
		}
	}
	if q.PerApproverType[0].Approved != 3 {
		t.Errorf("the most first: %+v", q.PerApproverType[0])
	}
}

// A page the AI proposes holds no figure (AI-59): money, a percentage, a decimal, an identifier and a whole number of a thousand or more are refused, however
// they are written; plain prose, small numbers, ordinals and years are not.
// req: AI-59
func TestAProposedPageHoldsNoFigure(t *testing.T) {
	for _, bad := range []string{"We spent S$4,800 on hosting", "Revenue was SGD 52000", "about 12% more", "12 per cent more", "a margin of 61.4", "the invoice 01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90",
		"NRIC S1234567D", "account 123456789012", "we have 4800 users", "paid 5,000 in March", "$40", "40 USD", "€9"} {
		if NoFigures(bad) == "" {
			t.Errorf("%q passed as free of figures", bad)
		}
	}
	for _, ok := range []string{"File the claim within 14 days of the purchase", "Step 3: attach the receipt", "From 2026 the review is quarterly", "Ask the finance controller; see the cash KPI for the balance",
		"Approvals go to two people", "Use the 2nd form"} {
		if why := NoFigures(ok); why != "" {
			t.Errorf("%q refused: %s", ok, why)
		}
	}
}
