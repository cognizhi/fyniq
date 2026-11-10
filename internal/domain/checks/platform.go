package checks

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the payees' and the claims' items (design D7).
const (
	viewVendorBank = "Vendor bank details"
	viewClaims     = "Expense claims"
)

// The audit object types whose change reruns the payments' checks (design D5).
var (
	onBankDetails = []string{"document", "allocation", "journal_entry", "contact"}
	onAdvances    = []string{"document", "allocation", "organisation_setting"}
)

// changedWithin is how recent a change of a vendor's bank details is flagged while payments are pending (C.2 #11).
const changedWithin = 30 * 24 * time.Hour

// ScheduledJobs are the jobs C.2 #12 watches, by name, with what each does: the recurring documents and their releases,
// depreciation, the revaluation and the reminders (FR-AUT-01).
var ScheduledJobs = map[string]string{
	"journals.recurring":    "Recurring journals' drafts",
	"bills.recurring":       "Recurring bills' drafts",
	"invoices.recurring":    "Recurring invoices",
	"journals.auto-reverse": "Automatic reversals",
	"bills.releases":        "Bills' spread costs",
	"invoices.releases":     "Invoices' spread revenue",
	"bank.releases":         "Bank lines' spreads",
	"assets.depreciation":   "Depreciation",
	"fx.revalue":            "Month-end revaluation",
	"fx.reversals":          "Revaluation reversals",
	"notify.bills_due":      "Bills-due reminders",
	"notify.deadlines":      "Statutory deadline reminders",
	"notify.cash_buffer":    "Cash buffer alerts",
	"audit.erase":           "Security events' details erased after their retention",
}

// The checks of the payments out: p1-checks slice 4 (task 4.3).
var paymentChecks = []Definition{
	{Key: "purchases.bank-details-changed", Ref: "C.2 #11", Area: "Purchases", Worst: Warning, Title: "No supplier's new bank account waits to be paid unconfirmed",
		Explain: "A supplier whose bank details changed in the last 30 days, with bills still to pay, is how invoice fraud " +
			"pays the wrong account. Before paying, confirm the new account with the supplier on a number you already had, then " +
			"mark it reviewed.",
		Rules: "FR-CT-03, SOD-1", Owner: "controller", View: viewVendorBank, Triggers: onBankDetails, Run: bankDetailsChanged},
	{Key: "claims.advances-unsettled", Ref: "C.2 #39", Area: "Claims", Worst: Warning, Title: "Every cash advance is settled by the claim deadline",
		Explain: "A cash advance is settled by the claims that name it and a refund of what is left. One still open more than " +
			"the claim deadline after it was paid is for review: ask for the claims, or the refund.",
		Rules: "FR-AP-17, FR-AP-15", Owner: "accountant", View: viewClaims, Triggers: onAdvances, Run: advancesUnsettled},
}

// The checks of the platform: p1-checks slice 4 (task 4.4).
var platformChecks = []Definition{
	{Key: "scheduler.jobs-on-time", Ref: "C.2 #12", Area: "Scheduler", Worst: Error, Title: "Every scheduled job ran on time",
		Explain: "Fyniq makes the recurring documents, posts depreciation, the spreads and the month-end revaluation, and sends " +
			"the bills-due reminders by itself. A job that has not run within its time, or whose last run failed, leaves the " +
			"books short of those entries.",
		Rules: "FR-AUT-01, ADR-T7", Owner: "controller", View: viewClose, Run: jobsOnTime},
	{Key: "parameters.packs-current", Ref: "C.2 #16", Area: "Parameters", Worst: Warning, Title: "The statutory parameters in force are current",
		Explain: "GST, CPF and the other statutory values come from versioned packs, each with a date by which it is checked " +
			"against its official source. A pack in force past that date, or one with a later version loaded and not yet " +
			"activated, may give figures that are out of date.",
		Rules: "ENG-30, ENG-33", Owner: "controller", View: viewParameters, Triggers: []string{"statutory_pack_activation"}, Run: packsCurrent},
	{Key: "evidence.hashes-verified", Ref: "C.2 #31", Area: "Evidence", Worst: Error, Title: "Every evidence file still matches its fingerprint",
		Explain: "Each night every file attached to a posted record is read back from the locked store and its fingerprint " +
			"(SHA-256) compared with the one recorded when it was attached. A missing or changed file means evidence was " +
			"tampered with or lost.",
		Rules: "FR-DOC-07, CTL-AUD-06", Owner: "owner", View: viewEvidence, Triggers: []string{"evidence"}, Run: evidenceIntact},
	{Key: "email.sending-healthy", Ref: "C.2 #37", Area: "Email", Worst: Warning, Title: "Email from Fyniq reaches its recipients",
		Explain: "Invoices, reminders and remittance advice are sent from the installation's domains. A domain whose SPF, " +
			"DKIM or DMARC records are wrong, or too many hard bounces or complaints over 30 days, sends mail to spam or gets " +
			"it blocked: 2% hard bounces or 0.1% complaints with 50 or more sent, any complaint with fewer (rev 0.166).",
		Rules: "FR-INT-21", Owner: "administrator", View: viewIntegrations, Triggers: []string{"organisation", "email_message"}, Run: sendingHealthy},
	{Key: "integrations.connections-healthy", Ref: "C.2 #38", Area: "Integrations", Worst: Warning,
		Title: "Every integration connects, and none of its keys expires soon",
		Explain: "An enabled connector whose last connection test failed is not bringing in what it should. A key or " +
			"certificate whose provider gave it an expiry date stops working on that day: replace it before then (rev 0.166).",
		Rules: "FR-INT-20, FR-INT-26", Owner: "administrator", View: viewIntegrations, Triggers: []string{"organisation_connector"}, Run: connectionsHealthy},
}

// expiresWithin is how soon a connector's secret expiring is flagged (C.2 #38).
const expiresWithin = 14

// C.2 #38: the organisation's enabled connectors whose last connection test failed, and the current secrets of its
// installed connectors that expire within 14 days or have expired.
func connectionsHealthy(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	installed, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return Result{}, err
	}
	secrets, err := u.Connectors().OrganisationSecrets(ctx)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, c := range installed {
		if c.Installation || c.State == storage.ConnectorUninstalled {
			continue
		}
		if c.State == storage.ConnectorEnabled && c.Test != nil && !c.Test.Passed {
			items = append(items, Item{Kind: "connector", ID: c.ID.String(), Label: c.Connector,
				Detail: "Its last connection test failed on " + day(c.Test.At) + ": " + c.Test.Reason, Version: c.Test.At.UTC().Format(time.RFC3339)})
		}
		for _, s := range secrets {
			if s.ConnectorID != c.ID || s.ValidUntil != nil || s.DestroyedAt != nil || s.ExpiresOn == nil ||
				s.ExpiresOn.After(e.Today.AddDate(0, 0, expiresWithin)) {
				continue
			}
			what := "expires on " + day(*s.ExpiresOn)
			if s.ExpiresOn.Before(e.Today) {
				what = "expired on " + day(*s.ExpiresOn)
			}
			items = append(items, Item{Kind: "connector_secret", ID: c.ID.String() + "/" + s.Name, Label: c.Connector + " " + s.Name,
				Detail: fmt.Sprintf("Its %s %s", s.Name, what), Version: s.ID.String()})
		}
	}
	if len(items) == 0 {
		return Fine("Every enabled integration passed its last test, and none of its keys expires within 14 days"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d integration problem(s)", len(items)),
		"Test the connection again and fix what it says; replace a key before it expires, giving its new expiry date"), nil
}

// The thresholds of C.2 #37 (rev 0.166, the owner's decision of 2 Nov 2026): over 30 days, with at least minSent messages
// sent, hard bounces at 2% or more or complaints at 0.1% or more; with fewer, any complaint (Google's and Yahoo's sender
// guidance, and providers' bounce practice).
const (
	emailWindow       = 30 * 24 * time.Hour
	minSent           = 50
	bouncesPerMille   = 20 // 2%
	complaintsPer1000 = 1  // 0.1%
)

// The Appendix D row whose view opens the email's items (design D7).
const viewIntegrations = "Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)"

// C.2 #37: the sending domains as the night's check left them (FR-INT-21, design D6), and the organisation's hard
// bounces and complaints over 30 days. Between nights the domains' items are the last night's.
func sendingHealthy(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	var items []Item
	known := false
	if ev, ok := e.Latest[email.ActionDomainsChecked]; ok {
		known = true
		failing, _ := ev.Changes["failing"][1].([]any)
		for _, f := range failing {
			if text, ok := f.(string); ok {
				domain, wrong, _ := strings.Cut(text, ": ")
				items = append(items, Item{Kind: "email_domain", ID: domain, Label: domain, Detail: "Its records are not right: " + wrong,
					Version: Fingerprint(wrong)})
			}
		}
	} else if prior, ok := e.Prior["email.sending-healthy"]; ok && prior.Status != string(NotAssessed) {
		known = true
		was, err := ItemsOf(prior)
		if err != nil {
			return Result{}, err
		}
		for _, it := range was {
			if it.Kind == "email_domain" {
				items = append(items, it)
			}
		}
	}
	c, err := u.EmailMessages().Counts(ctx, e.Now.Add(-emailWindow))
	if err != nil {
		return Result{}, err
	}
	items = append(items, emailRates(c)...)
	if len(items) > 0 {
		return Found(Warning, items, fmt.Sprintf("%d problem(s) with the email Fyniq sends", len(items)),
			"Ask whoever runs the installation to fix a domain's records; check the addresses that bounced, and stop mail to those who complain"), nil
	}
	if !known {
		return Missing("the nightly check of the sending domains (email.domains), which has not run since the checks began",
			"the platform's schedule: it runs at 04:30 Singapore time"), nil
	}
	return Fine(fmt.Sprintf("The sending domains' records are right, and %d message(s) sent in 30 days are within the limits", c.Sent)), nil
}

// The Appendix D rows whose view opens the parameters' and the evidence's items (design D7).
const (
	viewParameters = "Statutory parameters"
	viewEvidence   = "Evidence: add, supersede (rev 0.3)"
)

// C.2 #31: the latest outcome of the nightly re-verification of the evidence files' hashes (FR-DOC-07, design D6): it does
// not re-hash the files itself.
func evidenceIntact(_ context.Context, _ storage.UnitOfWork, e *Env) (Result, error) {
	ev, ok := e.Latest[evidence.ActionVerified]
	if !ok {
		if _, ran := e.Prior["evidence.hashes-verified"]; ran {
			return Result{Status: Unchanged}, nil
		}
		return Missing("the nightly re-verification of the evidence files (evidence.verify), which has not run since the checks began",
			"the platform's schedule: it runs at 03:30 Singapore time"), nil
	}
	status, _ := ev.Changes["status"][1].(string)
	problem, _ := ev.Changes["problem"][1].(string)
	when := strings.TrimSuffix(ev.OccurredAt, "Z")
	if status == "passed" {
		checked, _ := ev.Changes["checked"][1].(float64)
		return Fine(fmt.Sprintf("The %d evidence files' fingerprints verified at %s", int(checked), when)), nil
	}
	var items []Item
	files, _ := ev.Changes["files"][1].([]any)
	for _, f := range files {
		if id, ok := f.(string); ok {
			items = append(items, Item{Kind: "evidence", ID: id, Label: "Evidence file " + id,
				Detail: "Its locked copy is missing, or no longer matches the fingerprint recorded when it was attached"})
		}
	}
	if problem != "" {
		items = append(items, Item{Kind: "evidence", Label: "Evidence store", Detail: problem})
	}
	count, _ := ev.Changes["mismatched"][1].(float64)
	return Result{Status: Error, Count: max(int(count), len(items)), Items: items, Detail: "The evidence files failed their re-verification at " + when,
		Fix: "Treat it as an incident: follow the evidence runbook and tell the Owner"}, nil
}

// C.2 #16: of each pack the organisation activated, the version in force today past its review date, and the later
// versions of the pack loaded and not activated (ENG-30, ENG-33).
func packsCurrent(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	versions, err := u.Packs().Versions(ctx)
	if err != nil {
		return Result{}, err
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return Result{}, err
	}
	active := map[string]bool{}
	for _, a := range acts {
		active[a.VersionID] = true
	}
	inForce := func(v storage.PackVersion) bool {
		return !v.EffectiveFrom.After(e.Today) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(e.Today))
	}
	current := map[string]storage.PackVersion{} // by pack: the activated version in force today
	for _, v := range versions {
		if !active[v.ID] || !inForce(v) {
			continue
		}
		if c, ok := current[v.Pack]; !ok || v.EffectiveFrom.After(c.EffectiveFrom) || (v.EffectiveFrom.Equal(c.EffectiveFrom) && v.LoadedAt.After(c.LoadedAt)) {
			current[v.Pack] = v
		}
	}
	var items []Item
	for _, pack := range slices.Sorted(maps.Keys(current)) {
		c := current[pack]
		if c.ReviewDate.Before(e.Today) {
			items = append(items, Item{Kind: "statutory_pack", ID: c.ID, Label: c.ID,
				Detail:  "Its review date, " + day(c.ReviewDate) + ", has passed: a newer version is to be published, or this one confirmed",
				Version: "review"})
		}
		for _, v := range versions {
			if v.Pack == pack && !active[v.ID] && v.ID != c.ID && !v.EffectiveFrom.Before(c.EffectiveFrom) {
				items = append(items, Item{Kind: "statutory_pack", ID: v.ID, Label: v.ID,
					Detail:  fmt.Sprintf("A later version of %s, in force from %s, is loaded and not activated", pack, day(v.EffectiveFrom)),
					Version: "not-activated"})
			}
		}
	}
	if len(current) == 0 {
		return Fine("No statutory pack is activated yet"), nil
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d statutory packs in force is current", len(current))), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d statutory pack(s) to bring up to date", len(items)),
		"Activate the later version on Settings › Statutory parameters, or ask the platform team about one past its review date"), nil
}

// C.2 #12: the watched jobs whose heartbeat is missing or older than they may go without one. A failed run beats no
// heartbeat, so it shows here once its time has passed.
func jobsOnTime(_ context.Context, _ storage.UnitOfWork, e *Env) (Result, error) {
	if e.beats == nil {
		return Missing("the scheduler's heartbeats", "the platform's schedule"), nil
	}
	var items []Item
	for _, j := range e.jobs {
		label := cmp.Or(ScheduledJobs[j.Name], j.Name)
		last, ok := e.beats[j.Name]
		switch {
		case !ok:
			items = append(items, Item{Kind: "job", ID: j.Name, Label: label, Detail: "It has not run"})
		case e.Now.Sub(last) > j.Stale:
			items = append(items, Item{Kind: "job", ID: j.Name, Label: label,
				Detail: fmt.Sprintf("It last ran %s ago, on %s; it may go %s without running", e.Now.Sub(last).Round(time.Minute),
					last.UTC().Format("2 Jan 2006 15:04 UTC"), j.Stale)})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d scheduled jobs ran on time", len(e.jobs))), nil
	}
	return Found(Error, items, fmt.Sprintf("%d scheduled job(s) late or failing", len(items)),
		"Tell whoever runs the installation: the scheduler's runbook says how to find why a job stopped"), nil
}

// C.2 #11: vendors whose bank details were approved in the last 30 days, with posted bills still open.
func bankDetailsChanged(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	changes, err := documents(ctx, u, []storage.DocumentType{storage.DocumentPayeeBankChange}, storage.DocumentPosted, "", "")
	if err != nil {
		return Result{}, err
	}
	// When each change was approved: its request's decision, as the application's clock says (FM-1).
	approved, err := u.Approvals().List(ctx, "approved")
	if err != nil {
		return Result{}, err
	}
	decided := map[string]time.Time{}
	for _, a := range approved {
		if a.ObjectType == approval.ObjectPayeeBankChange && a.DecidedAt != nil {
			decided[a.ObjectKey] = *a.DecidedAt
		}
	}
	type change struct {
		doc      storage.Document
		s        payee.Stored
		on       time.Time
		replaced bool
	}
	recent := map[uuid.UUID][]change{}
	for _, doc := range changes {
		on, ok := decided[doc.ID.String()]
		if !ok && doc.PostedAt != nil {
			on, ok = *doc.PostedAt, true
		}
		if !ok || e.Now.Sub(on) > changedWithin {
			continue
		}
		s, err := payee.Read(doc)
		if err != nil {
			return Result{}, err
		}
		p, err := s.Payee()
		if err != nil || p.ContactID == uuid.Nil { // a person's own account is FR-AP-22's, not this check's
			continue
		}
		accounts, err := u.PayeeBankAccounts().OfContact(ctx, p.ContactID)
		if err != nil {
			return Result{}, err
		}
		replaced := slices.ContainsFunc(accounts, func(a storage.PayeeBankAccount) bool {
			return a.Currency == s.Currency && a.State == storage.AccountRetired
		})
		recent[p.ContactID] = append(recent[p.ContactID], change{doc, s, on, replaced})
	}
	if len(recent) == 0 {
		return Fine("No supplier's bank details changed in the last 30 days"), nil
	}
	ap, err := controlAccount(ctx, u, ledger.RoleAccountsPayable)
	if err != nil {
		return Result{}, err
	}
	if ap == nil {
		return Missing("the accounts payable account (2010)", "Accounting › Chart of accounts"), nil
	}
	l, err := payables.Open(ctx, u, e.Base, ap.ID, e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, s := range l.Suppliers {
		cs, ok := recent[s.ID]
		if !ok {
			continue
		}
		var bills []string
		for _, it := range s.Items {
			if it.Kind == payables.KindBill && it.Open.Sign() > 0 {
				bills = append(bills, fmt.Sprintf("%s (%s %s)", cmp.Or(it.Number, "a bill"), it.Open, it.Open.Currency().Code))
			}
		}
		if len(bills) == 0 {
			continue
		}
		var what, ids []string
		for _, c := range cs {
			verb := "added"
			if c.replaced {
				verb = "changed"
			}
			what = append(what, fmt.Sprintf("%s account %s ending %s on %s", c.s.Currency, verb, c.s.NumberLast4, day(c.on)))
			ids = append(ids, c.doc.ID.String())
		}
		items = append(items, Item{Kind: "contact", ID: s.ID.String(), Label: s.Name,
			Detail:  fmt.Sprintf("%s; still to pay: %s", strings.Join(what, ", "), strings.Join(bills, ", ")),
			Version: Fingerprint(strings.Join(ids, ","))})
	}
	if len(items) == 0 {
		return Fine("No supplier whose bank details changed in the last 30 days has a bill still to pay"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d supplier(s) with new bank details and bills still to pay", len(items)),
		"Confirm each new account with the supplier on a number you already had before paying, then mark it reviewed"), nil
}

// C.2 #39: posted cash advances still open more than the claim deadline after they were paid.
func advancesUnsettled(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	docs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentCashAdvance}, storage.DocumentPosted, "", "")
	if err != nil {
		return Result{}, err
	}
	balances, err := claim.AdvanceBalances(ctx, u, docs, e.Base, time.Time{})
	if err != nil {
		return Result{}, err
	}
	names, err := memberNames(ctx, u, e.Now)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, doc := range docs {
		b, ok := balances[doc.ID]
		if !ok {
			continue
		}
		a, err := claim.ReadAdvance(doc)
		if err != nil {
			return Result{}, err
		}
		ws, err := claim.AdvanceWarnings(ctx, u, a, b, e.Today)
		if err != nil {
			return Result{}, err
		}
		if len(ws) > 0 {
			it := docItem("cash_advance", doc, cmp.Or(names[a.PersonID], "a former member"), ws[0].Message)
			it.Version = Fingerprint(b.Open.String())
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return Fine("No cash advance is open past the claim deadline"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d cash advance(s) open past the claim deadline", len(items)),
		"Ask for the claims that settle each, or for what is left to be refunded"), nil
}

// memberNames are the active members' names by user ID.
func memberNames(ctx context.Context, u storage.UnitOfWork, now time.Time) (map[uuid.UUID]string, error) {
	ms, err := u.Members().List(ctx, now)
	out := make(map[uuid.UUID]string, len(ms))
	for _, m := range ms {
		out[m.UserID] = m.DisplayName
	}
	return out, err
}

// emailRates are the items the 30 days' counts give (rev 0.166): hard bounces at 2% or more and complaints at 0.1% or
// more with at least minSent sent; with fewer, any complaint.
func emailRates(c storage.EmailCounts) []Item {
	var out []Item
	if c.Sent >= minSent && c.HardBounces*1000 >= c.Sent*bouncesPerMille {
		out = append(out, Item{Kind: "email_rate", ID: "hard_bounces", Label: "Hard bounces",
			Detail: fmt.Sprintf("%d of %d messages sent in 30 days bounced hard: 2%% or more", c.HardBounces, c.Sent), Version: fmt.Sprint(c.HardBounces)})
	}
	if c.Complaints > 0 && (c.Sent < minSent || c.Complaints*1000 >= c.Sent*complaintsPer1000) {
		out = append(out, Item{Kind: "email_rate", ID: "complaints", Label: "Complaints",
			Detail: fmt.Sprintf("%d of %d messages sent in 30 days drew a complaint", c.Complaints, c.Sent), Version: fmt.Sprint(c.Complaints)})
	}
	return out
}
