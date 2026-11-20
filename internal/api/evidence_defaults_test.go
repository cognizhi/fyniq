//go:build local

package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
)

// ruleRow is a row of the evidence rules table as the API answers it.
type ruleRow struct {
	Key         string
	Area        string
	Level       string
	Setting     string
	Built       bool
	Required    []struct{ Label string }
	Recommended []struct{ Label string }
}

func (r ruleRow) labels(items []struct{ Label string }) []string {
	out := make([]string, len(items))
	for i, x := range items {
		out[i] = x.Label
	}
	return out
}

// rulesTable reads the rules as tok sees them.
func (e *env) rulesTable(tok string) map[string]ruleRow {
	e.t.Helper()
	r := e.call(http.MethodGet, "/v1/evidence-rules", tok, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("getEvidenceRules = %d: %s", r.Status, r.Raw)
	}
	var body struct{ Rules []ruleRow }
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		e.t.Fatal(err)
	}
	out := map[string]ruleRow{}
	for _, row := range body.Rules {
		out[row.Key] = row
	}
	return out
}

// The five areas of §8.16's table, as the API answers them: every activity the specification names, with what it
// requires and recommends in the table's words, and the level it starts at. Each activity whose module exists is built
// (enforced now), the others ship as rules that start to apply with their modules. Bills, manual journals and statutory
// filings (CPF payments among them) hold their level in an organisation setting (FR-ORG-08), which the answer follows;
// the others' modules fix theirs. Any member may read the table, and the enforcement of the three configurable
// activities is TestEvidenceRulesHoldBillsAndJournals's.
// req: FR-DOC-06
// req: documents/attachments-and-evidence Default evidence for purchases, payee changes and payments
// req: documents/attachments-and-evidence Default evidence for journals, reconciliations and close
// req: documents/attachments-and-evidence Default evidence for filings, statutory payments and grant claims
// req: documents/attachments-and-evidence Default evidence for equity and investor records
// req: documents/attachments-and-evidence Default evidence for governance changes and control reviews
func TestEvidenceRulesAreTheDefaults(t *testing.T) {
	e := newEnv(t)
	// The tests' books relax the bills' and journals' levels (relaxEvidence); a new organisation has them as shipped.
	e.setEvidence("evidence.bill", "optional", "required", 2)
	e.setEvidence("evidence.manual_journal", "optional", "required", 2)
	rules := e.rulesTable(e.as(authz.Auditor))
	type want struct {
		key, area, level string
		built            bool
		required, rec    []string
	}
	for _, w := range []want{
		// purchases, payee changes and payments
		{"bill", "purchases", "required", true, []string{"Receipt or invoice"}, []string{"Business purpose note"}},
		{"expense_claim", "purchases", "required", true, []string{"Receipt or invoice"}, []string{"Business purpose note"}},
		{"founder_cost", "purchases", "required", true, []string{"Receipt or invoice"}, []string{"Business purpose note"}},
		{"payee_bank_change", "purchases", "required", true,
			[]string{"Bank letter, or a screenshot of the vendor's confirmation through a known channel"}, []string{"Call-back note (who, when, number used)"}},
		{"payment_release", "purchases", "recommended", false, nil, []string{"Bank confirmation screenshot or file"}},
		// journals, reconciliations and close
		{"manual_journal", "journals", "required", true, []string{"Supporting calculation or document"}, []string{"Explanation"}},
		{"bank_reconciliation", "journals", "required", true, []string{"Bank statement for the period (FM-5)"}, nil},
		{"period_close", "journals", "required", true, []string{"Signed checklist (generated)", "Reconciliations",
			"For each bank, provider-clearing, control, deferral and director account with a balance, its support: a statement, or a subledger or schedule (INV-07)",
			"At the year-end close, the director-account confirmation (FR-EQ-12)"}, []string{"Flux explanations (FR-GL-14)"}},
		// filings, statutory payments and grant claims
		{"statutory_filing", "filings", "required", true, []string{"Submission acknowledgement"}, []string{"Working papers"}},
		{"cpf_payment", "filings", "required", true, []string{"Payment acknowledgement"}, []string{"CPF submission file"}},
		{"grant_claim", "filings", "required", false, []string{"Letter of offer or award", "Paid invoices and proof of payment for each claimed cost"},
			[]string{"Evidence of each deliverable", "The claim working paper"}},
		// equity and investor records
		{"share_issue", "equity", "required", true, []string{"Shareholders' and board resolutions", "Written agreement for non-cash consideration", "ACRA acknowledgement"},
			[]string{"Valuation or price basis"}},
		{"share_transfer", "equity", "required", true, []string{"Signed transfer form", "ACRA acknowledgement"}, []string{"Stamp duty certificate"}},
		{"instrument", "equity", "required", true, []string{"Signed instrument or grant letter"}, []string{"Board resolution"}},
		{"founder_settlement", "equity", "required", true, []string{"Founder's balance confirmation", "Deed or resolution"}, nil},
		{"investor_record", "equity", "recommended", false, nil, []string{"Source document (such as an updated agreement)"}},
		// governance changes and control reviews
		{"governance_change", "governance", "required", false, []string{"Reason (text)"}, []string{"Supporting document"}},
		{"control_evidence", "governance", "required", false, []string{"Review record or screenshot"}, nil},
		{"board_pack", "governance", "required", true, []string{"The published pack's PDF (generated)"}, nil},
	} {
		got, ok := rules[w.key]
		if !ok {
			t.Errorf("the table has no %s row", w.key)
			continue
		}
		if got.Area != w.area || got.Level != w.level || got.Built != w.built ||
			!slices.Equal(got.labels(got.Required), append([]string{}, w.required...)) || !slices.Equal(got.labels(got.Recommended), append([]string{}, w.rec...)) {
			t.Errorf("%s: area %s level %s built %t required %q recommended %q; want %+v", w.key, got.Area, got.Level, got.Built,
				got.labels(got.Required), got.labels(got.Recommended), w)
		}
	}
	if len(rules) != len(evidence.Catalogue) {
		t.Errorf("%d rows answered for %d in the table", len(rules), len(evidence.Catalogue))
	}
	// The configurable ones follow their organisation setting; the others' level is fixed by their modules.
	for key, setting := range map[string]string{"bill": "evidence.bill", "manual_journal": "evidence.manual_journal",
		"statutory_filing": "evidence.statutory_filing", "cpf_payment": "evidence.statutory_filing"} {
		if rules[key].Setting != setting {
			t.Errorf("%s holds its level in %q, want %q", key, rules[key].Setting, setting)
		}
	}
	if rules["share_issue"].Setting != "" || rules["payee_bank_change"].Setting != "" {
		t.Errorf("a rule its module fixes must not be a setting: %+v %+v", rules["share_issue"], rules["payee_bank_change"])
	}
	e.setEvidence("evidence.bill", "required", "recommended", 3)
	e.setEvidence("evidence.statutory_filing", "required", "optional", 2)
	changed := e.rulesTable(e.as(authz.Auditor))
	if changed["bill"].Level != "recommended" || changed["statutory_filing"].Level != "optional" || changed["cpf_payment"].Level != "optional" ||
		changed["manual_journal"].Level != "required" {
		t.Errorf("the levels after two settings changed: bill %s, filing %s, cpf %s, journal %s", changed["bill"].Level,
			changed["statutory_filing"].Level, changed["cpf_payment"].Level, changed["manual_journal"].Level)
	}
}
