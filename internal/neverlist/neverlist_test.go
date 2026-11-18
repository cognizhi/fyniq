package neverlist

import "testing"

// The never-list is words of a task type's ID and of its command, and of a tool's verb (§9.6): a change that approves, pays,
// exports, edits a setting, files or touches the audit trail is refused, and a plain read is not.
// req: AI-40
// req: AI-19
func TestTheNeverListRefusesByWords(t *testing.T) {
	for _, id := range []string{"approval.approve", "payment.release", "payee.change", "user.invite", "setting.update", "pack.activate", "period.reopen",
		"journal.void", "gst.file", "organisation.export", "audit.purge", "share.allot", "director.repay", "register.update"} {
		if Refuse(id, "") == "" && Refuse("x", id) == "" {
			t.Errorf("%s was not refused", id)
		}
	}
	for _, id := range []string{"get", "list", "explain", "search", "query", "sec.create_task", "bank.categorise_lines"} {
		if what := Refuse(id, ""); what != "" {
			t.Errorf("%s was refused: %s", id, what)
		}
	}
	if Refuse("CreateComplianceTask", "") != "" || Refuse("ApproveJournal", "") == "" {
		t.Error("camel-case words are not split")
	}
}
