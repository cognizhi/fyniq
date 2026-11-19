package project_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/project"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func know(customer, member uuid.UUID) project.Know {
	return project.Know{Customers: map[uuid.UUID]bool{customer: true}, Members: map[uuid.UUID]bool{member: true}}
}

func codes(ps []project.Problem) map[string]string {
	out := map[string]string{}
	for _, p := range ps {
		out[p.Field] = p.Code
	}
	return out
}

// A project that passes keeps what was entered, in exact decimals and whole minutes; every problem of one that does not
// comes back together, in plain words with its code (ENG-22, ENG-23).
// req: FR-PRJ-01
// req: ENG-22
// req: ENG-23
func TestAProjectIsCheckedWithEveryProblemAtOnce(t *testing.T) {
	cust, who := uuid.New(), uuid.New()
	minutes := 24000
	good := project.Draft{CustomerID: cust.String(), Currency: "sgd", Status: "active", BillingMethod: "time_and_materials", StartsOn: "2026-11-01",
		EndsOn: "2027-02-28", BudgetMinutes: &minutes, BudgetAmount: "60000.00", People: []project.Person{{UserID: who.String(), Manager: true}}}
	c, probs := project.Check(good, nil, know(cust, who))
	if len(probs) != 0 || c.Currency != "SGD" || *c.BudgetAmount != "60000.00" || *c.BudgetMinutes != 24000 || len(c.People) != 1 || !c.People[0].Manager {
		t.Fatalf("a good project: %+v %v", c, probs)
	}

	bad := project.Draft{CustomerID: uuid.NewString(), Currency: "XXX", Status: "done", BillingMethod: "barter", StartsOn: "2027-03-01", EndsOn: "2027-02-01",
		BudgetAmount: "12.345", People: []project.Person{{UserID: uuid.NewString()}, {UserID: who.String()}, {UserID: who.String()}}}
	_, probs = project.Check(bad, nil, know(cust, who))
	got := codes(probs)
	for field, code := range map[string]string{"body.currency": project.CodeCurrency, "body.status": project.CodeStatus, "body.billing_method": project.CodeBilling,
		"body.customer_id": project.CodeCustomer, "body.ends_on": project.CodeDates, "body.people[0].user_id": project.CodePeople, "body.people[2].user_id": project.CodePeople} {
		if got[field] != code {
			t.Errorf("%s: %q, want %s (%v)", field, got[field], code, probs)
		}
	}
	for _, p := range probs {
		if p.Message == "" || p.Fix == "" {
			t.Errorf("%s says nothing of what is wrong or how to fix it: %+v", p.Field, p)
		}
	}

	// A budget finer than the currency's cents, an hours budget past the limit, and a billed project with no manager.
	d := good
	d.BudgetAmount, d.People = "12.345", nil
	huge := project.MaxBudgetMinutes + 1
	d.BudgetMinutes = &huge
	got = codes(func() []project.Problem { _, p := project.Check(d, nil, know(cust, who)); return p }())
	if got["body.budget_amount"] != project.CodeBudget || got["body.budget_minutes"] != project.CodeBudget || got["body.people"] != project.CodePeople {
		t.Errorf("budgets and managers: %v", got)
	}

	// An internal project has no customer and needs no manager; a closed one needs none either.
	d = project.Draft{Currency: "SGD", Status: "active", BillingMethod: "internal"}
	if _, probs := project.Check(d, nil, know(cust, who)); len(probs) != 0 {
		t.Errorf("an internal project: %v", probs)
	}
	d.CustomerID = cust.String()
	if _, probs := project.Check(d, nil, know(cust, who)); codes(probs)["body.customer_id"] != project.CodeCustomer {
		t.Errorf("an internal project with a customer: %v", probs)
	}

	// A project keeps its currency.
	existing := &storage.Project{Configured: true, Currency: "SGD"}
	d = good
	d.Currency = "USD"
	if _, probs := project.Check(d, existing, know(cust, who)); codes(probs)["body.currency"] != project.CodeCurrency || !strings.Contains(probs[0].Message, "keeps the currency") {
		t.Errorf("another currency: %v", probs)
	}
}

// req: FR-PRJ-05
func TestATaskNameIsUniqueAmongTheLiveOnes(t *testing.T) {
	live, out := uuid.New(), uuid.New()
	others := []storage.ProjectTask{{ID: live, Name: "Data migration", Active: true}, {ID: out, Name: "Support", Active: false}}
	if n, probs := project.CheckTask("  Support ", others, uuid.Nil); n != "Support" || len(probs) != 0 {
		t.Errorf("the name of a task out of use is free: %q %v", n, probs)
	}
	if _, probs := project.CheckTask("data MIGRATION", others, uuid.Nil); len(probs) != 1 || probs[0].Code != project.CodeTask {
		t.Errorf("a live task's name: %v", probs)
	}
	if _, probs := project.CheckTask("Data migration", others, live); len(probs) != 0 {
		t.Errorf("a task keeps its own name: %v", probs)
	}
	if _, probs := project.CheckTask(" ", others, uuid.Nil); len(probs) != 1 {
		t.Errorf("an empty name: %v", probs)
	}
}
