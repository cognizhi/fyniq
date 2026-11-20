package notify

import "github.com/cognizhi/fyniq/internal/ports/storage"

// The notification types of delegation and of reminders and escalations for pending approvals (FR-APR-06, FR-APR-08; p2-mobile-approvals-automation). They are added to the
// catalogue here, beside the types of earlier changes, and are approval types: in the centre always, emailed at once unless a person chooses otherwise.
const (
	// ApprovalDelegatedType tells a delegator and their delegate that a delegation of approval authority was made or ended.
	ApprovalDelegatedType = "approval.delegated"
	// ApprovalReminderType reminds those who may sign a pending approval's current step that it still waits.
	ApprovalReminderType = "approval.reminder"
	// ApprovalEscalatedType tells the Owners and the requester that an approval has waited past its escalation time.
	ApprovalEscalatedType = "approval.escalated"
)

func init() {
	Catalogue = append(Catalogue,
		Type{Key: ApprovalDelegatedType, Label: "Approval authority was delegated to you, or by you", Category: Approvals, Severity: "info", Email: storage.EmailImmediate},
		Type{Key: ApprovalReminderType, Label: "An approval still waits for you", Category: Approvals, Severity: "info", Actionable: true, Email: storage.EmailImmediate},
		Type{Key: ApprovalEscalatedType, Label: "An approval has waited a long time", Category: Approvals, Severity: "warning", Actionable: true, Email: storage.EmailImmediate})
}
