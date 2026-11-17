package proposal_test

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/proposal"
)

var update = flag.Bool("update", false, "rewrite contracts/ai/tasks.lock.json")

const lockPath = "../../contracts/ai/tasks.lock.json"

// fixture is a valid task type's definition, changed by edit.
func fixture(edit func(map[string]any)) []byte {
	t := map[string]any{
		"id": "test.annotate_note", "version": 1, "name": "Annotate a note", "description": "Adds a note to a record. A fixture of the tests.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"note"},
			"properties": map[string]any{"note": map[string]any{"type": "string", "maxLength": 200, "description": "The note."}}},
		"command": "AnnotateNote", "tier": 1, "limits": map[string]any{"items": 5},
		"approvers": []any{"owner", "company_secretary"}, "expiry_hours": 72,
	}
	if edit != nil {
		edit(t)
	}
	b, _ := json.Marshal(t)
	return b
}

func load(id string, b []byte) (*proposal.Catalogue, error) {
	return proposal.Load(fstest.MapFS{"tasks/" + id + ".json": {Data: b}})
}

// A task type is an ID and version, a name, the description the model and the approver read, a strict, bounded input
// schema, the domain command it maps to, its tier, its limits, the roles that approve it and its expiry; anything else,
// or anything open, is refused as the catalogue loads. Tier 3 is reserved and never allowed today; tier 1 alone allows
// bulk approval.
// req: ai/task-catalogue Catalogue entry attributes
// req: ai/task-catalogue Risk tiers and approver roles
// req: AI-27
func TestTheCatalogueLoadsStrictly(t *testing.T) {
	c, err := load("test.annotate_note", fixture(nil))
	if err != nil {
		t.Fatal(err)
	}
	tt, ok := c.Get("test.annotate_note")
	if !ok || !tt.Bulk() || tt.Expiry().Hours() != 72 || tt.ModelName() != "propose_test_annotate_note" || len(tt.SHA256) != 64 {
		t.Errorf("the task type: %+v", tt)
	}
	// The tool the model is offered is as strict and bounded as the read tools' (AI-27).
	if m := tt.ModelTool(); m.Name != "propose_test_annotate_note" || aitool.CheckInput(m.Schema) != nil ||
		!strings.Contains(m.Description, "A person approves it") {
		t.Errorf("the model's tool: %+v %v", m, aitool.CheckInput(m.Schema))
	}
	if two, _ := load("test.annotate_note", fixture(func(m map[string]any) { m["tier"] = 2 })); two != nil {
		if tt, _ := two.Get("test.annotate_note"); tt.Bulk() {
			t.Error("a tier-2 task type allows bulk approval")
		}
	}
	for name, edit := range map[string]func(map[string]any){
		"a file not named by its ID": func(m map[string]any) { m["id"] = "test.other_note" },
		"a field the format lacks":   func(m map[string]any) { m["auto_approve"] = true },
		"tier 3":                     func(m map[string]any) { m["tier"] = 3 },
		"tier 0":                     func(m map[string]any) { m["tier"] = 0 },
		"no approvers":               func(m map[string]any) { m["approvers"] = []any{} },
		"the AI as an approver":      func(m map[string]any) { m["approvers"] = []any{"ai"} },
		"a role that is none":        func(m map[string]any) { m["approvers"] = []any{"wizard"} },
		"no items":                   func(m map[string]any) { m["limits"] = map[string]any{"items": 0} },
		"too many items":             func(m map[string]any) { m["limits"] = map[string]any{"items": 101} },
		"an amount with no currency": func(m map[string]any) { m["limits"] = map[string]any{"items": 1, "amount_per_item": "100.00"} },
		"an amount that is no number": func(m map[string]any) {
			m["limits"] = map[string]any{"items": 1, "amount_total": "1,000", "currency": "SGD"}
		},
		"a negative amount": func(m map[string]any) {
			m["limits"] = map[string]any{"items": 1, "amount_total": "-1", "currency": "SGD"}
		},
		"no expiry":                 func(m map[string]any) { m["expiry_hours"] = 0 },
		"an expiry past 72 hours":   func(m map[string]any) { m["expiry_hours"] = 96 },
		"a command that is no name": func(m map[string]any) { m["command"] = "annotate note" },
		"an open input": func(m map[string]any) {
			m["inputSchema"] = map[string]any{"type": "object", "properties": map[string]any{"note": map[string]any{"type": "string", "maxLength": 200, "description": "x"}}}
		},
		"an unbounded string": func(m map[string]any) {
			m["inputSchema"] = map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"note": map[string]any{"type": "string", "description": "x"}}}
		},
	} {
		if _, err := load("test.annotate_note", fixture(edit)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	long := "test.annotate_" + strings.Repeat("note_", 9) + "now" // propose_ and it pass a tool name's 64 characters
	if _, err := load(long, fixture(func(m map[string]any) { m["id"] = long })); err == nil || !strings.Contains(err.Error(), "at most 56") {
		t.Errorf("an ID too long for a tool's name: %v", err)
	}
}

// The never-list (§9.6): approving or rejecting anything, payments, payee bank details, users, roles, team mode,
// policies, the AI catalogue and settings, statutory parameters, reopening, voiding or deleting, filing, exporting, the
// audit trail, shares and investor rights, a director's balance, officers and registers, and an obligation marked met
// can never be proposed. A catalogue file that adds one is refused as it loads, and the proposal service
// refuses it again by name. Every task type of Appendix E passes.
// req: ai/task-catalogue Never in the catalogue
// req: CTL-AI-04
// asvs: V8.2.1
func TestNothingOnTheNeverListCanBeProposed(t *testing.T) {
	for id, command := range map[string]string{
		"ap.approve_bill":           "ApproveBill",
		"pay.release_batch":         "ReleasePaymentBatch",
		"ap.pay_bill":               "PayBill",
		"ct.change_bank_details":    "ChangeBankDetails",
		"payee.update_account":      "UpdatePayee",
		"adm.invite_users":          "InviteUsers",
		"adm.assign_role":           "AssignRole",
		"adm.switch_team_mode":      "SwitchTeamMode",
		"apr.change_policy":         "ChangePolicy",
		"ai.edit_catalogue":         "EditCatalogue",
		"ai.change_settings":        "ChangeSettings",
		"tax.activate_pack":         "ActivatePack",
		"gl.reopen_period":          "ReopenPeriod",
		"ar.void_invoice":           "VoidInvoice",
		"gl.delete_journal":         "DeleteJournal",
		"tax.file_return":           "FileReturn",
		"sec.lodge_annual_return":   "LodgeAnnualReturn",
		"org.export_data":           "ExportData",
		"aud.purge_events":          "PurgeAudit",
		"sec.draft_document_signed": "SignDocument",
		"eq.issue_shares":           "IssueShares",
		"eq.transfer_holding":       "ChangeHolder",
		"eq.update_instrument":      "UpdateInstrument",
		"eq.grant_investor_rights":  "GrantInvestorRights",
		"dir.settle_balance":        "RepayDirectorBalance",
		"dir.convert_loan":          "ConvertToEquity",
		"dir.waive_loan":            "WaiveLoan",
		"sec.appoint_officer":       "AppointOfficer",
		"sec.update_register":       "UpdateRegister",
		"sec.mark_obligation_met":   "MarkObligationMet",
	} {
		if proposal.Refuse(id, command) == "" {
			t.Errorf("%s (%s) is not refused", id, command)
		}
		if _, err := load(id, fixture(func(m map[string]any) { m["id"], m["command"] = id, command })); err == nil || !strings.Contains(err.Error(), "never-list") {
			t.Errorf("%s loaded into the catalogue: %v", id, err)
		}
	}
	for id, command := range map[string]string{ // Appendix E, rev 0.6 to 0.279
		"bank.categorise_lines": "CategoriseBankLines", "bank.match_payments": "MatchPayments", "bank.rule_upsert": "UpsertBankRule",
		"doc.attach": "AttachDocument", "ar.draft_invoice": "DraftInvoice", "ap.draft_bill": "DraftBill", "exp.draft_claim": "DraftExpenseClaim",
		"ar.draft_reminders": "DraftReminders", "plan.save_scenario": "SaveScenario", "rpt.draft_commentary": "DraftCommentary",
		"gl.post_journal": "PostJournal", "ar.post_invoice": "PostInvoice", "ap.post_bill": "PostBill", "ct.update_contact": "UpdateContact",
		"recur.create_template": "CreateRecurringTemplate", "plan.propose_budget": "ProposeBudget", "kb.propose_page": "ProposePage",
		"sec.create_task": "CreateComplianceTask", "sec.draft_document": "DraftCorporateDocument", "sec.draft_reminder": "DraftReminder",
	} {
		if why := proposal.Refuse(id, command); why != "" {
			t.Errorf("%s (%s), of Appendix E, is refused: %s", id, command, why)
		}
	}
}

// The catalogue the release ships loads, and a changed definition is a new version, pinned in contracts/ai/tasks.lock.json
// (CMP-AI-03): a change made without one, or a version going back, fails.
// req: ai/task-catalogue Catalogue as versioned, dual-controlled data
// req: platform/release-and-versioning AI catalogue, prompt and tool versions
// req: CTL-AI-04
func TestEveryChangeToATaskTypeIsANewVersion(t *testing.T) {
	type pin struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	}
	pinned := map[string]pin{}
	if b, err := os.ReadFile(lockPath); err == nil {
		if err := json.Unmarshal(b, &pinned); err != nil {
			t.Fatal(err)
		}
	} else if !*update {
		t.Fatal(err)
	}
	now := map[string]pin{}
	for _, tt := range proposal.Default().Types() {
		now[tt.ID] = pin{tt.Version, tt.SHA256}
		was, ok := pinned[tt.ID]
		switch {
		case ok && was.Version == tt.Version && was.SHA256 != tt.SHA256:
			t.Errorf("%s changed without a new version: raise its version from %d", tt.ID, tt.Version)
		case ok && tt.Version < was.Version:
			t.Errorf("%s went back from version %d to %d", tt.ID, was.Version, tt.Version)
		}
	}
	if t.Failed() || maps.Equal(pinned, now) {
		return
	}
	if !*update {
		t.Fatalf("contracts/ai/tasks.lock.json is not the catalogue as it is: run go test ./internal/proposal -run %s -update", t.Name())
	}
	b, _ := json.MarshalIndent(now, "", "  ")
	if err := os.WriteFile(lockPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
