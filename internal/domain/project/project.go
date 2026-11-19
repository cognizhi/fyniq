// Package project checks a project as entered (PRD-001 FR-PRJ-01): a value of the Project dimension with a customer,
// a billing currency, dates, a status, a billing method, a budget in hours and in money, and the people who log time on
// it and the managers who approve that time (FR-PRJ-07). Every problem comes back together (ENG-22), in plain words
// with its code (ENG-23). Naming a manager never gives a right: who may approve comes from the roles (design D4), and
// a manager who holds none is a warning, not a refusal.
package project

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The error codes (ENG-23).
const (
	CodeStatus   = "PRJ-01" // not a status of a project
	CodeBilling  = "PRJ-02" // not a billing method
	CodeDates    = "PRJ-03" // dates out of order, or not dates
	CodeBudget   = "PRJ-04" // a budget that is not a whole number of minutes or an amount of the project's currency
	CodeCustomer = "PRJ-05" // a customer that is not one of the organisation's customers, or one on an internal project
	CodeCurrency = "PRJ-06" // a currency Fyniq does not know
	CodePeople   = "PRJ-07" // a person who is not an active member, named twice, or a project with no manager
	CodeTask     = "PRJ-08" // a task name that is empty, too long or another live task's
	CodeOff      = "PRJ-09" // Projects and time are switched off (FR-ORG-06)
)

// MaxBudgetMinutes is the largest hours budget, as the database keeps it.
const MaxBudgetMinutes = 100_000_000

// Statuses and BillingMethods are the values a project takes, in the order a form lists them.
var (
	Statuses       = []string{storage.ProjectPlanned, storage.ProjectActive, storage.ProjectOnHold, storage.ProjectClosed}
	BillingMethods = []string{storage.BillingTimeAndMaterials, storage.BillingFixedFee, storage.BillingRetainer, storage.BillingInternal}
)

// Problem is what is wrong with a field, and how to fix it.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Draft is a project as entered. Dates are YYYY-MM-DD, budgets plain decimals; empty is not set.
type Draft struct {
	CustomerID    string
	Currency      string
	Status        string
	BillingMethod string
	StartsOn      string
	EndsOn        string
	BudgetMinutes *int
	BudgetAmount  string
	People        []Person
}

// Person is a member of the project as entered.
type Person struct {
	UserID  string
	Manager bool
}

// Know is what a check needs of the organisation: its customers, its active members and its base currency's code.
type Know struct {
	Customers map[uuid.UUID]bool // contact IDs marked as customers and active
	Members   map[uuid.UUID]bool // user IDs of the active members
}

// Checked is a project that passed: what the storage port takes, with the people.
type Checked struct {
	CustomerID    *uuid.UUID
	Currency      string
	Status        string
	BillingMethod string
	StartsOn      *time.Time
	EndsOn        *time.Time
	BudgetMinutes *int
	BudgetAmount  *storage.Decimal
	People        []storage.ProjectPerson
}

// Check reads a draft against what the organisation knows, with every problem at once. A currency is needed when the
// project is new (existing is nil); an existing project keeps its own.
func Check(d Draft, existing *storage.Project, k Know) (Checked, []Problem) {
	var probs []Problem
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, "body." + field, msg, fix}) }
	out := Checked{Currency: strings.ToUpper(strings.TrimSpace(d.Currency)), Status: d.Status, BillingMethod: d.BillingMethod, BudgetMinutes: d.BudgetMinutes}

	if existing != nil && existing.Configured {
		out.Currency = existing.Currency
		if d.Currency != "" && strings.ToUpper(strings.TrimSpace(d.Currency)) != existing.Currency {
			add(CodeCurrency, "currency", "a project keeps the currency it was made in, "+existing.Currency,
				"Leave the currency as it is, or add another project in "+d.Currency)
		}
	}
	cur, err := money.Lookup(out.Currency)
	if err != nil {
		add(CodeCurrency, "currency", fmt.Sprintf("%q is not a currency Fyniq knows", out.Currency), "Use an ISO 4217 code such as SGD or USD")
	}
	if !contains(Statuses, d.Status) {
		add(CodeStatus, "status", fmt.Sprintf("%q is not a status of a project", d.Status), "Choose planned, active, on_hold or closed")
	}
	if !contains(BillingMethods, d.BillingMethod) {
		add(CodeBilling, "billing_method", fmt.Sprintf("%q is not a billing method", d.BillingMethod),
			"Choose time_and_materials, fixed_fee, retainer or internal")
	}
	if d.CustomerID != "" {
		id, err := uuid.Parse(d.CustomerID)
		switch {
		case err != nil:
			add(CodeCustomer, "customer_id", "is not an ID", "Send the customer's ID from the contacts")
		case !k.Customers[id]:
			add(CodeCustomer, "customer_id", "is not one of the organisation's customers", "Choose a contact marked as a customer, or mark the contact first")
		default:
			out.CustomerID = &id
		}
	}
	if d.BillingMethod == storage.BillingInternal && d.CustomerID != "" {
		out.CustomerID = nil
		add(CodeCustomer, "customer_id", "an internal project has no customer", "Leave the customer out, or choose another billing method")
	}

	starts, serr := day(d.StartsOn)
	ends, eerr := day(d.EndsOn)
	if serr != nil {
		add(CodeDates, "starts_on", "is not a date", "Send the start as YYYY-MM-DD")
	}
	if eerr != nil {
		add(CodeDates, "ends_on", "is not a date", "Send the end as YYYY-MM-DD")
	}
	if serr == nil && eerr == nil && starts != nil && ends != nil && ends.Before(*starts) {
		add(CodeDates, "ends_on", "is before the start", "Choose an end on or after "+starts.Format(time.DateOnly))
	}
	out.StartsOn, out.EndsOn = starts, ends

	if d.BudgetMinutes != nil && (*d.BudgetMinutes < 0 || *d.BudgetMinutes > MaxBudgetMinutes) {
		add(CodeBudget, "budget_minutes", "is not a number of minutes between 0 and 100,000,000", "Send the hours budget in whole minutes, such as 24000 for 400 hours")
	}
	if d.BudgetAmount != "" && err == nil {
		m, merr := money.ParseField(d.BudgetAmount, cur, "budget_amount")
		switch {
		case merr != nil:
			add(CodeBudget, "budget_amount", merr.Error(), "Send a plain decimal in "+cur.Code+", with at most "+fmt.Sprint(cur.MinorUnits)+" decimals")
		case m.Sign() < 0:
			add(CodeBudget, "budget_amount", "is below zero", "A budget is zero or more")
		default:
			s := storage.Decimal(m.String())
			out.BudgetAmount = &s
		}
	}

	seen := map[uuid.UUID]bool{}
	managers := 0
	for i, p := range d.People {
		field := fmt.Sprintf("people[%d].user_id", i)
		id, err := uuid.Parse(p.UserID)
		switch {
		case err != nil:
			add(CodePeople, field, "is not an ID", "Send the member's user ID from the members list")
			continue
		case !k.Members[id]:
			add(CodePeople, field, "is not an active member of the organisation", "Invite the person to the organisation first, or choose another")
			continue
		case seen[id]:
			add(CodePeople, field, "is named twice", "Name each person once")
			continue
		}
		seen[id] = true
		if p.Manager {
			managers++
		}
		out.People = append(out.People, storage.ProjectPerson{UserID: id, Manager: p.Manager})
	}
	if managers == 0 && d.BillingMethod != storage.BillingInternal && d.Status != storage.ProjectClosed && contains(BillingMethods, d.BillingMethod) {
		add(CodePeople, "people", "a project needs a manager to approve its time", "Mark at least one member as a manager, or make the project internal")
	}
	return out, probs
}

// CheckTask checks a task's name against the project's other live tasks.
func CheckTask(name string, others []storage.ProjectTask, self uuid.UUID) (string, []Problem) {
	name = strings.TrimSpace(name)
	var probs []Problem
	if l := len([]rune(name)); l < 1 || l > 100 {
		probs = append(probs, Problem{CodeTask, "body.name", "is empty or longer than 100 characters", "Name the task in 1 to 100 characters"})
	}
	for _, o := range others {
		if o.Active && o.ID != self && strings.EqualFold(strings.TrimSpace(o.Name), name) {
			probs = append(probs, Problem{CodeTask, "body.name", "is another live task's name in this project", "Choose another name, or put the other task out of use"})
			break
		}
	}
	return name, probs
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func day(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
