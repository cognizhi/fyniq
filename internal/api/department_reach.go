package api

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An Approver narrowed to departments (FR-ADM-01, rev 0.253; the owner's decisions of 20 Nov 2026 [owner to confirm]) reaches the
// bills, invoices and expense claims of their departments only: a record is in a department when its lines carry that
// department's values, and one whose lines carry none is in no department. A role they hold besides the Approver that reaches the
// row unqualified (an Accountant, a Bookkeeper) is not narrowed by it. The scope only narrows: an Approver role given with no
// scope behaves as it did.

// departmentReach is what keeps a narrowed Approver to their departments' records on one row.
type departmentReach struct {
	scope       authn.DepartmentScope
	departments []uuid.UUID // every value of the department dimension
}

// narrowedTo reports whether p's Approver role narrows them on a row: it is narrowed, and no other role of theirs holds the
// right unqualified there (the Employee's "C (own)" does not count: it reaches one's own claims).
func narrowedTo(p authn.Principal, row authz.Capability, need authz.Right) bool {
	if !p.Approver.Narrowed {
		return false
	}
	others := slices.DeleteFunc(slices.Clone(p.Roles), func(r authz.Role) bool { return r == authz.Approver })
	return !authz.Decide(authz.Policy{Operation: "narrowed", Capability: row, Need: need, Unqualified: true}, others, p.AI()).Allowed
}

// reachOf is the reach that narrows p on a row for need, or nil when nothing does.
func reachOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, row authz.Capability, need authz.Right) (*departmentReach, error) {
	if !narrowedTo(p, row, need) {
		return nil, nil
	}
	all, err := departmentValues(ctx, u)
	if err != nil {
		return nil, err
	}
	return &departmentReach{scope: p.Approver, departments: all}, nil
}

// holds reports whether a record whose lines carry these dimension values is in the departments: its department values are at
// least one and all theirs. A nil reach holds every record.
func (r *departmentReach) holds(values []uuid.UUID) bool {
	if r == nil {
		return true
	}
	var mine []uuid.UUID
	for _, v := range values {
		if slices.Contains(r.departments, v) {
			mine = append(mine, v)
		}
	}
	return r.scope.Holds(mine)
}

// refValues are the dimension values of lines, as IDs.
func refValues(lines ...[]DimensionValueRef) []uuid.UUID {
	var out []uuid.UUID
	for _, vs := range lines {
		for _, v := range vs {
			if id, err := uuid.Parse(string(v)); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// narrowedMayRead are the reads of the sales, purchases and expense-claims rows a narrowed Approver keeps, each of which keeps to
// their departments' records: the lists and records themselves, and what a claim's review needs. Every other read of those rows
// is the company's, and their route to a request is the Inbox.
var narrowedMayRead = map[string]bool{
	"listBills": true, "getBill": true, "listInvoices": true, "getInvoice": true, "listExpenseClaims": true, "getExpenseClaim": true,
	"listClaimCategories": true, "getClaimGSTCodes": true,
}

// narrowedRefused reports whether policy is a read of the sales, purchases or expense-claims rows that a narrowed Approver does not
// keep (FR-ADM-01): the company's totals and registers.
func narrowedRefused(p authn.Principal, policy authz.Policy) bool {
	if policy.Parent == authz.Bills || policy.Parent == authz.Invoices { // a bill's or an invoice's evidence is on the record: not theirs to browse or change
		return narrowedTo(p, policy.Parent, authz.View)
	}
	if policy.Need != authz.View || policy.Public || narrowedMayRead[policy.Operation] {
		return false
	}
	switch policy.Capability {
	case authz.Bills, authz.Invoices, authz.ExpenseClaims:
		return narrowedTo(p, policy.Capability, authz.View)
	}
	return false
}
