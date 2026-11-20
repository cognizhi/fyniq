package proposal

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The approval-quality metrics of a month (PRD-001 CTL-AI-07, AI-92; p1-ai-proposals design D6), computed from the proposals' own records. They answer whether
// the people who approve what the AI proposes look at it: a share approved in seconds of first opening its review is the sign of a rubber stamp
// (automation bias, IMDA's agentic-AI framework).

// QuickApproval is how soon after first opening a proposal's review a whole approval counts as made without a look (AI-92).
const QuickApproval = 10 * time.Second

// QualityTaskType is one task type's share of the month.
type QualityTaskType struct {
	TaskType string  `json:"task_type" doc:"The task type." example:"ap.draft_bill"`
	Count    int     `json:"count" doc:"How many of the month's proposals were of it." example:"12"`
	Share    float64 `json:"share" doc:"Of the month's proposals, from 0 to 1." example:"0.4"`
	// (rev 0.544, AI-33) What became of the approvals of this task type.
	Approved              int     `json:"approved" doc:"How many of its proposals were approved." example:"9"`
	Edited                int     `json:"edited" doc:"How many of the approved ones were approved with an item changed or removed (an edit before approving)." example:"2"`
	Reversed              int     `json:"reversed" doc:"How many of the approved ones were undone afterwards (AI-91)." example:"1"`
	MedianDecisionSeconds float64 `json:"median_decision_seconds" doc:"The median time from its proposals to their decisions, in seconds." example:"310"`
}

// QualityApprover is what one person approved in the month.
type QualityApprover struct {
	User     uuid.UUID `json:"user" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Approved int       `json:"approved" doc:"How many proposals they approved in the month." example:"14"`
	Quick    int       `json:"quick" doc:"How many of them within ten seconds of first opening the review, with no item changed, or with no look at all." example:"3"`
	Edited   int       `json:"edited" doc:"How many of them were approved with an item changed or removed (rev 0.544, AI-33)." example:"2"`
}

// QualityApproverTaskType is what one person approved of one task type in the month: the breakdown AI-33 names, per task type and approver.
type QualityApproverTaskType struct {
	User     uuid.UUID `json:"user" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	TaskType string    `json:"task_type" doc:"The task type." example:"ap.draft_bill"`
	Approved int       `json:"approved" doc:"How many they approved." example:"5"`
	Quick    int       `json:"quick" doc:"How many of them within ten seconds of first opening the review with no item changed, or with no look." example:"1"`
	Edited   int       `json:"edited" doc:"How many with an item changed or removed." example:"2"`
	Reversed int       `json:"reversed" doc:"How many were undone afterwards." example:"0"`
}

// Quality is a month's approval-quality metrics.
type Quality struct {
	Made     int `json:"made" doc:"Proposals made in the month." example:"30"`
	Approved int `json:"approved" doc:"Decided by a person's approval." example:"22"`
	Rejected int `json:"rejected" doc:"Rejected by a person." example:"4"`
	Expired  int `json:"expired" doc:"Left undecided until they expired." example:"2"`
	Invalid  int `json:"invalid" doc:"Found invalid when they were made." example:"1"`
	Stale    int `json:"stale" doc:"Approved, then not run because a record had changed." example:"1"`
	// QuickShare is the share of approvals made within QuickApproval of the decider's first look at the review with no item changed, or with no look
	// recorded at all, from 0 to 1; 0 when nothing was approved.
	QuickShare float64 `json:"quick_share" doc:"The share of approvals made within ten seconds of the decider first opening the review with no item changed, or with no look recorded, from 0 to 1." example:"0.18"`
	// MedianDecisionSeconds is the median time from a proposal to the decision, for those decided; 0 when none was.
	MedianDecisionSeconds float64 `json:"median_decision_seconds" doc:"The median time from a proposal to its decision, in seconds." example:"420"`
	// RejectedWithReasonShare is the share of rejections that gave a reason; 0 when nothing was rejected.
	RejectedWithReasonShare float64           `json:"rejected_with_reason_share" doc:"The share of rejections that gave a reason, from 0 to 1." example:"0.75"`
	PerApprover             []QualityApprover `json:"per_approver" nullable:"false" doc:"What each person approved, the most first."`
	TaskTypes               []QualityTaskType `json:"task_types" nullable:"false" doc:"Each task type's share of the month's proposals, the most first."`
	// (rev 0.544, AI-33) The edit-before-approve rate, the reversals after approval and the breakdown per task type and approver.
	Edited          int                       `json:"edited" doc:"Approvals with an item changed or removed before approving (the edit-before-approve count)." example:"4"`
	EditedShare     float64                   `json:"edited_share" doc:"The share of approvals that were edited before approving, from 0 to 1; 0 when nothing was approved." example:"0.18"`
	Reversed        int                       `json:"reversed" doc:"Approved proposals a person undid afterwards (AI-91): reversals after approval." example:"1"`
	ReversedShare   float64                   `json:"reversed_share" doc:"The share of approvals that were undone, from 0 to 1; 0 when nothing was approved." example:"0.05"`
	PerApproverType []QualityApproverTaskType `json:"per_approver_task_type" nullable:"false" doc:"What each person approved of each task type, the most first."`
}

// MeasureQuality computes the month's metrics from the proposals' rows. An approval is a decision with a decider that was not a rejection, an expiry or a
// supersession; it is Quick when the decider opened the review less than QuickApproval before deciding, or never opened it, and approved the whole payload.
func MeasureQuality(rows []storage.AIQualityRow) Quality {
	q := Quality{PerApprover: []QualityApprover{}, TaskTypes: []QualityTaskType{}, PerApproverType: []QualityApproverTaskType{}}
	perUser := map[uuid.UUID]*QualityApprover{}
	types := map[string]*QualityTaskType{}
	typeSeconds := map[string][]float64{}
	pair := map[[2]string]*QualityApproverTaskType{}
	var seconds []float64
	var rejected, withReason, quick int
	for _, r := range rows {
		q.Made++
		if types[r.TaskType] == nil {
			types[r.TaskType] = &QualityTaskType{TaskType: r.TaskType}
		}
		types[r.TaskType].Count++
		switch r.Status {
		case storage.ProposalRejected:
			q.Rejected++
			rejected++
			if r.Reason != "" {
				withReason++
			}
		case storage.ProposalExpired:
			q.Expired++
		case storage.ProposalInvalid:
			q.Invalid++
		case storage.ProposalStale:
			q.Stale++
		}
		approved := r.DecidedBy != nil && r.DecidedAt != nil && r.Status != storage.ProposalRejected
		if approved {
			q.Approved++
			u := perUser[*r.DecidedBy]
			if u == nil {
				u = &QualityApprover{User: *r.DecidedBy}
				perUser[*r.DecidedBy] = u
			}
			u.Approved++
			key := [2]string{r.DecidedBy.String(), r.TaskType}
			if pair[key] == nil {
				pair[key] = &QualityApproverTaskType{User: *r.DecidedBy, TaskType: r.TaskType}
			}
			t, pt := types[r.TaskType], pair[key]
			t.Approved++
			pt.Approved++
			if !r.ApprovedWhole {
				q.Edited++
				u.Edited++
				t.Edited++
				pt.Edited++
			}
			if r.UndoneAt != nil {
				q.Reversed++
				t.Reversed++
				pt.Reversed++
			}
			if r.ApprovedWhole && (r.FirstViewedAt == nil || r.DecidedAt.Sub(*r.FirstViewedAt) < QuickApproval) {
				u.Quick++
				pt.Quick++
				quick++
			}
		}
		if r.DecidedAt != nil {
			d := r.DecidedAt.Sub(r.ProposedAt).Seconds()
			seconds = append(seconds, d)
			typeSeconds[r.TaskType] = append(typeSeconds[r.TaskType], d)
		}
	}
	if q.Approved > 0 {
		q.QuickShare = float64(quick) / float64(q.Approved)
		q.EditedShare = float64(q.Edited) / float64(q.Approved)
		q.ReversedShare = float64(q.Reversed) / float64(q.Approved)
	}
	if rejected > 0 {
		q.RejectedWithReasonShare = float64(withReason) / float64(rejected)
	}
	q.MedianDecisionSeconds = median(seconds)
	for _, u := range perUser {
		q.PerApprover = append(q.PerApprover, *u)
	}
	slices.SortFunc(q.PerApprover, func(a, b QualityApprover) int { return -(a.Approved - b.Approved) })
	for name, t := range types {
		t.Share = float64(t.Count) / float64(q.Made)
		t.MedianDecisionSeconds = median(typeSeconds[name])
		q.TaskTypes = append(q.TaskTypes, *t)
	}
	for _, pt := range pair {
		q.PerApproverType = append(q.PerApproverType, *pt)
	}
	slices.SortFunc(q.PerApproverType, func(a, b QualityApproverTaskType) int {
		if a.Approved != b.Approved {
			return b.Approved - a.Approved
		}
		if c := strings.Compare(a.TaskType, b.TaskType); c != 0 {
			return c
		}
		return strings.Compare(a.User.String(), b.User.String())
	})
	slices.SortFunc(q.TaskTypes, func(a, b QualityTaskType) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return len(a.TaskType) - len(b.TaskType)
	})
	return q
}

// median is the middle of the values, the mean of the two middle ones when there is an even number; 0 for none.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	slices.Sort(xs)
	if mid := len(xs) / 2; len(xs)%2 == 1 {
		return xs[mid]
	} else {
		return (xs[mid-1] + xs[mid]) / 2
	}
}
