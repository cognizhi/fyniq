package proposal

import "testing"

// Every task type of Appendix E that this change builds, by ID and command, is outside the never-list (§9.6): posting a bill or an invoice is not
// a payment or an approval (the human who approves the proposal approves the document), and no ID names a bank field, an approval or an
// auto-apply.
// req: ai/task-catalogue Never in the catalogue
// req: CTL-AI-04
func TestTheAppendixETaskTypesAreNotOnTheNeverList(t *testing.T) {
	for id, cmd := range map[string]string{"bank.categorise_lines": "CategoriseBankLines", "bank.match_payments": "MatchPayments", "bank.rule_upsert": "UpsertBankRule", "doc.attach": "AttachDocument",
		"ap.draft_bill": "DraftBill", "exp.draft_claim": "DraftClaim", "ar.draft_reminders": "DraftReminders", "plan.save_scenario": "SaveScenario", "rpt.draft_commentary": "DraftCommentary",
		"gl.post_journal": "PostJournal", "ar.post_invoice": "PostInvoice", "ap.post_bill": "PostBill", "ct.update_contact": "UpdateContact", "recur.create_template": "CreateRecurring",
		"plan.propose_budget": "CreateBudgetVersion", "kb.propose_page": "ProposePage"} {
		if why := Refuse(id, cmd); why != "" {
			t.Errorf("%s/%s refused: %s", id, cmd, why)
		}
	}
}
