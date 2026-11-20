package authz

import (
	"slices"
	"strings"
)

// The narrow scopes of an API client (FR-INT-02, AI-34; the scope names of AI-37 and the api-integrations skill:
// fyniq.<read|write>.<area>, never a wildcard). An integration client and a personal access token act for the member
// behind them and never beyond that member's roles; their scopes narrow those roles further, to whole areas of Appendix
// D's rows:
//
//   - fyniq.read.<area> views the area's rows;
//   - fyniq.write.<area> also prepares and posts on the area's writable rows (C and P), and views the area.
//
// No scope approves or releases (A and R): approvals stay with people in Fyniq (FR-INT-01, §6.2). The rows outside every
// area are never reached by a client, whatever its member may do: users and roles, approval policies and the AI catalogue,
// connected apps, the AI's providers and activity, vendor and claimant bank details, payment batches, payroll, the cap
// table and director accounts, evidence, the audit trail, the compliance and data-protection records, the tenant export
// and the workbook import. Payroll and equity have no scope here: AI-37 grants them only with settings that arrive later.
var (
	// ScopeReads are the rows each area's scopes view.
	ScopeReads = map[string][]Capability{
		"reports":    {Reports, ChecksHealth},
		"sales":      {Invoices, ContactsAndItems},
		"purchases":  {Bills, ContactsAndItems},
		"banking":    {BankTransactions, BankReconciliation},
		"accounting": {Journals, ChartOfAccounts, DimensionValues, PeriodClose, FixedAssets, AccountingBasis},
		"planning":   {Budgets},
		"compliance": {GSTReturns, StatutoryCalendar, StatutoryParameters},
	}
	// ScopeWrites are the rows a fyniq.write.<area> scope prepares and posts on: the documents an integration feeds in.
	ScopeWrites = map[string][]Capability{
		"sales":      {Invoices, ContactsAndItems},
		"purchases":  {Bills, ContactsAndItems},
		"banking":    {BankTransactions},
		"accounting": {Journals},
		// tasks (FR-INT-24, rev 0.610): the tasks an integration makes of its own; no financial record and no compliance task.
		"tasks": {IntegrationTasks},
	}
)

// ClientScope reports whether an API client may be granted scope: a read of a known area, or a write of an area that
// has writable rows.
func ClientScope(scope string) bool {
	if area, ok := strings.CutPrefix(scope, "fyniq.read."); ok {
		_, known := ScopeReads[area]
		return known
	}
	if area, ok := strings.CutPrefix(scope, "fyniq.write."); ok {
		_, known := ScopeWrites[area]
		return known
	}
	return false
}

// ClientScopes are every scope an API client may be granted, sorted.
func ClientScopes() []string {
	var out []string
	for area := range ScopeReads {
		out = append(out, "fyniq.read."+area)
	}
	for area := range ScopeWrites {
		out = append(out, "fyniq.write."+area)
	}
	slices.Sort(out)
	return out
}

// ScopeAllows reports whether scopes cover what p needs: a view of its row (and of the row of the record it is under),
// or a C or P on a writable row. The organisation itself (Membership) is readable with any scope. Nothing else is: a
// row outside every area, an approval and a release are never in scope.
func ScopeAllows(scopes []string, p Policy) bool {
	if len(scopes) == 0 {
		return false
	}
	if p.Parent != "" && !scopeCovers(scopes, p.Parent, View) {
		return false
	}
	if p.Capability == Membership {
		return p.Need == View
	}
	return scopeCovers(scopes, p.Capability, p.Need)
}

func scopeCovers(scopes []string, c Capability, need Right) bool {
	for _, s := range scopes {
		if area, ok := strings.CutPrefix(s, "fyniq.write."); ok {
			if slices.Contains(ScopeWrites[area], c) && (need == Create || need == Post || need == View) {
				return true
			}
			if need == View && slices.Contains(ScopeReads[area], c) {
				return true
			}
		}
		if area, ok := strings.CutPrefix(s, "fyniq.read."); ok && need == View && slices.Contains(ScopeReads[area], c) {
			return true
		}
	}
	return false
}
