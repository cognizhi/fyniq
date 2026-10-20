package authz

import "slices"

// TransactionRows are the Appendix D rows of financial transactions. Posting, approving or releasing on one of them
// (P, A or R) is the second side of SOD-6, "post or approve transactions" (§6.3); vendor bank details count, as
// Appendix D's note on the Owner and SOD-6 counts them.
var TransactionRows = []Capability{BankTransactions, BankReconciliation, Invoices, Bills, VendorBankDetails, PaymentBatches,
	ExpenseClaims, DirectorAccounts, CapTable, Journals, PayRuns, FixedAssets, PeriodClose, GSTReturns}

// Conflict is a segregation-of-duties conflict that one person's roles hold together (§6.3).
type Conflict struct {
	SoD   string // the conflict's ID, such as "SOD-6"
	Roles []Role // the roles that hold its two sides, in the order of Principals
}

// SoD6 is the one conflict a combination of roles makes by itself: administering users or roles and posting or
// approving transactions (privilege abuse). The other conflicts of §6.3 are about one item, such as one payment
// batch or one journal, and are checked when the action happens, from the item's history.
const SoD6 = "SOD-6"

// Administers reports whether role administers users or roles: C or A on "Users, roles, team mode". The Owner's A
// is not counted: Appendix D lets the Owner hold both sides of SOD-6 because every Owner action on users, roles,
// policies and bank details is dual-controlled, so no single Owner grants a right and uses it unreviewed.
func Administers(r Role) bool {
	if r == Owner {
		return false
	}
	return holdsAny(r, []Capability{UsersAndRoles}, "CA")
}

// Transacts reports whether role posts, approves or releases transactions: P, A or R on a row of TransactionRows,
// whatever the cell's qualifier (a limit or a department narrows the right; it is still the right).
func Transacts(r Role) bool { return holdsAny(r, TransactionRows, "PAR") }

func holdsAny(r Role, rows []Capability, letters string) bool {
	col := Column(r)
	if col == "" || r == AI {
		return false
	}
	for _, row := range rows {
		for _, l := range Grants[row][col].Rights {
			if slices.Contains([]rune(letters), l) {
				return true
			}
		}
	}
	return false
}

// RoleConflicts returns the conflicts that roles, held by one person, make together: SOD-6 when one of them
// administers users or roles and another posts, approves or releases transactions (rev 0.42, provisional). Founder
// mode allows it with compensating controls; Standard and Enterprise mode block it unless it is overridden with a
// reason and a second approver (§6.4).
func RoleConflicts(roles []Role) []Conflict {
	var admin, trans []Role
	for _, r := range roles {
		if Administers(r) {
			admin = append(admin, r)
		}
		if Transacts(r) {
			trans = append(trans, r)
		}
	}
	if len(admin) == 0 || len(trans) == 0 {
		return nil
	}
	var both []Role
	for _, r := range Principals() {
		if slices.Contains(admin, r) || slices.Contains(trans, r) {
			both = append(both, r)
		}
	}
	return []Conflict{{SoD: SoD6, Roles: both}}
}
