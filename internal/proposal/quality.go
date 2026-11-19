package proposal

import (
	"slices"
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
	TaskType string  `json:"task_type"`
	Count    int     `json:"count"`
	Share    float64 `json:"share" doc:"Of the month's proposals, from 0 to 1."`
}

// QualityApprover is what one person approved in the month.
type QualityApprover struct {
	User     uuid.UUID `json:"user"`
	Approved int       `json:"approved"`
	Quick    int       `json:"quick"`
}

// Quality is a month's approval-quality metrics.
type Quality struct {
	Made     int `json:"made"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
	Expired  int `json:"expired"`
	Invalid  int `json:"invalid"`
	Stale    int `json:"stale"`
	// QuickShare is the share of approvals made within QuickApproval of the decider's first look at the review with no item changed, or with no look
	// recorded at all, from 0 to 1; 0 when nothing was approved.
	QuickShare float64 `json:"quick_share"`
	// MedianDecisionSeconds is the median time from a proposal to the decision, for those decided; 0 when none was.
	MedianDecisionSeconds float64 `json:"median_decision_seconds"`
	// RejectedWithReasonShare is the share of rejections that gave a reason; 0 when nothing was rejected.
	RejectedWithReasonShare float64           `json:"rejected_with_reason_share"`
	PerApprover             []QualityApprover `json:"per_approver"`
	TaskTypes               []QualityTaskType `json:"task_types"`
}

// MeasureQuality computes the month's metrics from the proposals' rows. An approval is a decision with a decider that was not a rejection, an expiry or a
// supersession; it is Quick when the decider opened the review less than QuickApproval before deciding, or never opened it, and approved the whole payload.
func MeasureQuality(rows []storage.AIQualityRow) Quality {
	q := Quality{PerApprover: []QualityApprover{}, TaskTypes: []QualityTaskType{}}
	perUser := map[uuid.UUID]*QualityApprover{}
	types := map[string]int{}
	var seconds []float64
	var rejected, withReason, quick int
	for _, r := range rows {
		q.Made++
		types[r.TaskType]++
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
			if r.ApprovedWhole && (r.FirstViewedAt == nil || r.DecidedAt.Sub(*r.FirstViewedAt) < QuickApproval) {
				u.Quick++
				quick++
			}
		}
		if r.DecidedAt != nil {
			seconds = append(seconds, r.DecidedAt.Sub(r.ProposedAt).Seconds())
		}
	}
	if q.Approved > 0 {
		q.QuickShare = float64(quick) / float64(q.Approved)
	}
	if rejected > 0 {
		q.RejectedWithReasonShare = float64(withReason) / float64(rejected)
	}
	if len(seconds) > 0 {
		slices.Sort(seconds)
		mid := len(seconds) / 2
		if len(seconds)%2 == 1 {
			q.MedianDecisionSeconds = seconds[mid]
		} else {
			q.MedianDecisionSeconds = (seconds[mid-1] + seconds[mid]) / 2
		}
	}
	for _, u := range perUser {
		q.PerApprover = append(q.PerApprover, *u)
	}
	slices.SortFunc(q.PerApprover, func(a, b QualityApprover) int { return -(a.Approved - b.Approved) })
	for t, n := range types {
		q.TaskTypes = append(q.TaskTypes, QualityTaskType{TaskType: t, Count: n, Share: float64(n) / float64(q.Made)})
	}
	slices.SortFunc(q.TaskTypes, func(a, b QualityTaskType) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return len(a.TaskType) - len(b.TaskType)
	})
	return q
}
