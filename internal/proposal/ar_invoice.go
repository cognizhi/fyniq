package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectbilling"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ar.draft_invoice (PRD-001 Appendix E, FR-PRJ-10; p1-projects, AI draft): a draft invoice of a time-and-materials project's approved,
// billable, unbilled time and posted billable costs, made through the same function the API's draftProjectInvoice uses
// (projectbilling.InvoiceDraft), so the AI's draft and a person's are the same. Version 1 drafts a project's work; an invoice from a
// quote, an order or a description is a later version. It is tier 1: a draft posts nothing and moves no money, and nothing is issued, so
// the invoice follows the ordinary approval policies when a person issues it (AI-30). The billing rates on its lines are the project's
// billing rates, never a person's cost rate (G4-050).

// invoiceDraft is ar.draft_invoice's payload.
type invoiceDraft struct {
	Project        string `json:"project"`
	RevenueAccount string `json:"revenue_account"`
	GSTCode        string `json:"gst_code"`
	Date           string `json:"date"`
}

// invoiceWork is what the dry run and the execution both read: the draft as it would be made, and the project it bills.
type invoiceWork struct {
	draft   invoice.Draft
	project storage.Project
	account storage.Account
	billing int64 // a fingerprint of what the draft bills, the base version the executor checks (AI-P5)
}

func projectUnbilledKey(id uuid.UUID) string { return "project_unbilled:" + id.String() }

func refusal(msg, hint string) error {
	return &storage.Error{Code: storage.CodeInvalid, Message: msg, Hint: hint}
}

// fingerprint is the draft's lines and the work it bills as a person reads them.
func fingerprint(d invoice.Draft) int64 {
	var b strings.Builder
	for _, l := range d.Lines {
		fmt.Fprintf(&b, "%s|%s|%s;", l.Description, l.UnitPrice.String(), l.GSTCode)
	}
	if d.ProjectBilling != nil {
		for _, t := range d.ProjectBilling.TimeEntries {
			b.WriteString(t.String() + ";")
		}
		for _, x := range d.ProjectBilling.Expenses {
			fmt.Fprintf(&b, "%s/%d;", x.DocumentID, x.Line)
		}
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(b.String()))
	return int64(h.Sum64() >> 1)
}

// seesProject is whether the person the proposal runs for may read the project: any right on "Timesheets and project time" without a
// qualifier, or being on it (an Approver's or an Employee's own projects).
func seesProject(ctx context.Context, u storage.UnitOfWork, as Actor, id uuid.UUID) (bool, error) {
	if authz.Decide(authz.Policy{Operation: "view", Capability: authz.Timesheets, Need: authz.View, Unqualified: true}, as.Roles, false).Allowed {
		return true, nil
	}
	mine, err := u.Projects().OfUser(ctx, as.User)
	if err != nil {
		return false, err
	}
	return slices.Contains(mine, id), nil
}

// work reads the payload against the records as the person it runs for may read them and makes the draft, refusing with the field and the fix.
func (d invoiceDraft) work(ctx context.Context, u storage.UnitOfWork, as Actor) (invoiceWork, error) {
	var w invoiceWork
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return w, err
	}
	def, _ := settings.Lookup("features.projects")
	if settings.On(def, changes, as.Today).Value != "true" {
		return w, refusal("Projects and time are switched off for this organisation", "An Owner or Administrator turns them on under Settings, Features")
	}
	// The project is named by its code, as the person asking wrote it: the platform finds it, and a code that is no project of the person's
	// is refused whichever way it was wrong, so a guess finds nothing (the approver sees the customer and the amounts in the dry run).
	projects, err := u.Projects().List(ctx)
	if err != nil {
		return w, err
	}
	notFound := refusal("payload.project is not the code of a project you may read", "Use the project's code as the person asking wrote it, such as ACME-01")
	i := slices.IndexFunc(projects, func(p storage.Project) bool {
		return p.Configured && strings.EqualFold(p.Code, strings.TrimSpace(d.Project))
	})
	if i < 0 {
		return w, notFound
	}
	w.project = projects[i]
	if seen, err := seesProject(ctx, u, as, w.project.ID); err != nil {
		return w, err
	} else if !seen {
		return w, notFound
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return w, err
	}
	i = slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == d.RevenueAccount })
	if i < 0 || accounts[i].Type != storage.Revenue || accounts[i].Group || !accounts[i].Active {
		return w, refusal("payload.revenue_account is not the code of an active revenue account", "Use the code of a revenue account the person asking can read")
	}
	w.account = accounts[i]
	day := time.Date(as.Today.Year(), as.Today.Month(), as.Today.Day(), 0, 0, 0, 0, time.UTC)
	if d.Date != "" {
		if day, err = time.Parse(time.DateOnly, d.Date); err != nil {
			return w, refusal("payload.date is not a date", "Send the date as YYYY-MM-DD, or leave it out for today")
		}
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return w, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return w, err
	}
	members, err := u.Members().List(ctx, as.Today)
	if err != nil {
		return w, err
	}
	names := make(map[uuid.UUID]string, len(members))
	for _, m := range members {
		names[m.UserID] = m.DisplayName
	}
	w.draft, err = projectbilling.InvoiceDraft(ctx, u, w.project, base, projectbilling.DraftOptions{AccountID: w.account.ID, GSTCode: d.GSTCode, Day: day, Names: names})
	var pe *projectbilling.Error
	switch {
	case errors.As(err, &pe):
		return w, refusal(pe.Message, pe.Fix)
	case err != nil:
		return w, err
	}
	w.billing = fingerprint(w.draft)
	return w, nil
}

// DraftInvoice is ar.draft_invoice's command (Appendix E): the draft invoice of a project's unbilled work, kept as the approving person
// with the AI as originator. The dry run shows the lines and the total and posts nothing; executed, it makes the draft only while what it
// bills is what the dry run showed (AI-P5), and a draft marks nothing billed: the marks are made when the invoice is posted.
type DraftInvoice struct{}

// Policy implements Command: the right to draft a project's invoice, which is to create invoices and to see project reports.
func (DraftInvoice) Policy() authz.Policy { return authz.DraftProjectInvoice }

// Measure implements Command: one draft, no amount (a draft moves no money).
func (DraftInvoice) Measure(payload json.RawMessage) (Measure, error) {
	var d invoiceDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command.
func (DraftInvoice) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var d invoiceDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Impact{}, err
	}
	w, err := d.work(ctx, u, as)
	if err != nil {
		return Impact{}, err
	}
	customer, err := u.Contacts().Get(ctx, w.draft.CustomerID)
	if err != nil {
		return Impact{}, err
	}
	total := w.draft.Currency
	sum := money.Zero(total)
	lines := make([]map[string]any, 0, len(w.draft.Lines))
	for _, l := range w.draft.Lines {
		amount := l.UnitPrice.String()
		lines = append(lines, map[string]any{"description": l.Description, "amount": amount, "gst_code": l.GSTCode, "account": w.account.Code + " " + w.account.Name})
		line, err := money.Parse(amount, total)
		if err != nil {
			return Impact{}, err
		}
		if sum, err = sum.Add(line); err != nil {
			return Impact{}, err
		}
	}
	entries, costs := 0, 0
	if w.draft.ProjectBilling != nil {
		entries, costs = len(w.draft.ProjectBilling.TimeEntries), len(w.draft.ProjectBilling.Expenses)
	}
	changes, err := json.Marshal(map[string]any{"invoices": []map[string]any{{"change": "draft", "customer": customer.LegalName, "project": w.project.Code, "currency": total.Code,
		"date": w.draft.Date.Format(time.DateOnly), "lines": lines, "total": sum.String(), "time_entries": entries, "costs": costs}},
		"ledger": "A draft posts nothing: the invoice is issued, and its approval policy applies, by a person"})
	if err != nil {
		return Impact{}, err
	}
	return Impact{Summary: fmt.Sprintf("Drafts an invoice to %s for %s %s from %d time entries and %d costs of project %s; nothing is issued or posted",
		customer.LegalName, total.Code, sum.String(), entries, costs, w.project.Code), Changes: changes, Base: map[string]int64{projectUnbilledKey(w.project.ID): w.billing}}, nil
}

// Execute implements Command.
func (DraftInvoice) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var d invoiceDraft
	if err := json.Unmarshal(payload, &d); err != nil {
		return Execution{}, err
	}
	w, err := d.work(ctx, u, as)
	if err != nil {
		return Execution{}, err
	}
	want, ok := base[projectUnbilledKey(w.project.ID)]
	if !ok {
		return Execution{}, fmt.Errorf("the dry run did not read %s", projectUnbilledKey(w.project.ID))
	}
	if w.billing != want {
		return Execution{}, ErrStale // time was approved, billed or returned, or a billing rate or markup changed, since the dry run
	}
	doc, err := journal.CreateInvoiceIn(ctx, u, w.draft)
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{"invoice:" + doc.ID.String()}, Entries: []string{}}, nil
}
