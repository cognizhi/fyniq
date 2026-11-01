package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the checks (FR-CTL-01, FR-CTL-02, FR-CTL-05)

// The checks' codes (ENG-23).
const (
	CodeUnknownCheck = "CHECK-01" // no such check, or its module is off
	CodeCheckOwner   = "CHECK-02" // the person named cannot own the check
	CodeNotToReview  = "CHECK-04" // the item is not one the check lists to review
)

// CheckHealth is books health (FR-CTL-02): from the worst status any check the caller sees found.
type CheckHealth struct {
	Score       string `json:"score" enum:"ok,review,problems" doc:"problems when any check found ✖ or could not run; review when any found ⚠ or ⏰; ok otherwise. Checks not assessed do not change it." example:"review"`
	Problems    int    `json:"problems" doc:"Checks that found ✖, or whose run failed." example:"0"`
	Warnings    int    `json:"warnings" doc:"Checks that found ⚠." example:"1"`
	Due         int    `json:"due" doc:"Checks that found ⏰." example:"0"`
	OK          int    `json:"ok" doc:"Checks that found nothing." example:"8"`
	NotAssessed int    `json:"not_assessed" doc:"Checks not assessed for want of an input, or not run yet: counted beside the score." example:"1"`
}

// CheckItem is one item a check found.
type CheckItem struct {
	Kind   string `json:"kind" doc:"What it is, which decides where it opens: period, account, opening_balances, role, audit_chain, and the documents later checks name." example:"period"`
	ID     string `json:"id,omitempty" doc:"Its ID, where it has one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	Label  string `json:"label" doc:"Its name." example:"October 2026"`
	Detail string `json:"detail,omitempty" doc:"What is wrong with it." example:"6900 Clearing: give each a cash-flow line of the standard chart in the chart of accounts"`
	// Severity and Version (rev 0.164) let a reader mark an item to review as reviewed: until it changes.
	Severity string `json:"severity,omitempty" enum:"error,warning" doc:"The item's own severity, for a check that finds both: error (✖, it cannot post) or warning (⚠, to review); left out for the check's own." example:"warning"`
	Version  string `json:"version,omitempty" doc:"The item's version as a review names it: a review holds until the item changes." example:"3"`
}

// CheckOwnerOut is who owns a check: a person named for it, or the catalogue's role.
type CheckOwnerOut struct {
	Role    RoleName `json:"role" doc:"The role the catalogue names as its owner."`
	Person  *Person  `json:"person,omitempty" doc:"The person named instead of the role, when one is."`
	Version int      `json:"version" doc:"The owner's version: If-Match takes it as the ETag (\"0\" before anyone was named)." example:"0"`
}

// CheckOut is one check and its latest result.
type CheckOut struct {
	Key     string `json:"key" doc:"The check, stable." example:"books.balance-sheet-balances"`
	Ref     string `json:"ref" doc:"Its row of PRD-001 Appendix C (Expert)." example:"C.1 #2"`
	Area    string `json:"area" doc:"The area it belongs to." example:"Books"`
	Worst   string `json:"worst" enum:"error,warning,due" doc:"The worst it can find: error (✖) blocks the close, warning (⚠) asks for review, due (⏰) is a deadline." example:"error"`
	Title   string `json:"title" doc:"What it checks." example:"The balance sheet balances every month"`
	Explain string `json:"explain" doc:"Why it matters, in plain words." example:"Each month's assets equal its liabilities and equity."`
	Rules   string `json:"rules" doc:"The rule IDs it checks (Expert)." example:"INV-10, BS_Chk"`
	Nightly bool   `json:"nightly" doc:"true for the nightly ledger verification group (FR-CTL-05)." example:"false"`
	Status  string `json:"status" enum:"ok,warning,error,due,not_assessed,failed" doc:"What its latest run found; not_assessed before its first run, or for want of an input; failed when its run failed, which counts as ✖." example:"ok"`
	Count   int    `json:"count" doc:"How many items it found." example:"0"`
	// Items are the first 50, shown only to a reader who may open them (design D7).
	Items       []CheckItem   `json:"items" nullable:"false" doc:"The first 50 items it found, each with what is wrong. Empty when the caller cannot open them (items_hidden)."`
	ItemsHidden bool          `json:"items_hidden" doc:"true when it found items the caller cannot open: their area's right is another role's." example:"false"`
	ItemsArea   string        `json:"items_area,omitempty" doc:"The Appendix D row whose view opens its items." example:"Reports, dashboards"`
	Detail      string        `json:"detail,omitempty" doc:"What it found, in words." example:"The balance sheet balances in every open month"`
	Fix         string        `json:"fix,omitempty" doc:"How to fix what it found." example:"Report it: the ledger's own checks should have refused this"`
	Needs       string        `json:"needs,omitempty" doc:"For a check not assessed, the input it needs." example:"an open month that has started"`
	Setting     string        `json:"setting,omitempty" doc:"Where that input is set." example:"Accounting › Periods"`
	RunAt       string        `json:"run_at,omitempty" format:"date-time" doc:"When it last ran." example:"2026-11-01T20:15:04Z"`
	Trigger     string        `json:"trigger,omitempty" enum:"change,nightly,request" doc:"What ran it last: a change it reacts to, the nightly run, or Run now." example:"nightly"`
	ChangedAt   string        `json:"changed_at,omitempty" format:"date-time" doc:"When its status last changed." example:"2026-10-31T09:12:00Z"`
	Revision    int64         `json:"ledger_revision" doc:"The ledger revision its last run read at." example:"42"`
	Owner       CheckOwnerOut `json:"owner" doc:"Who owns it."`
}

// CheckRunOut is a recorded run.
type CheckRunOut struct {
	Trigger    string `json:"trigger" enum:"change,nightly,request" doc:"What ran it." example:"request"`
	StartedAt  string `json:"started_at" format:"date-time" doc:"When it started." example:"2026-11-01T02:00:00Z"`
	FinishedAt string `json:"finished_at" format:"date-time" doc:"When it finished." example:"2026-11-01T02:00:03Z"`
	Revision   int64  `json:"ledger_revision" doc:"The ledger revision it read at." example:"42"`
	Checks     int    `json:"checks" doc:"How many checks it ran." example:"10"`
}

// ChecksOut are the checks the caller sees, with books health.
type ChecksOut struct {
	Health     CheckHealth  `json:"health" doc:"Books health from these checks (FR-CTL-02)."`
	Checks     []CheckOut   `json:"checks" nullable:"false" doc:"Each check the caller sees, by area in the catalogue's order; within an area, those not assessed first."`
	LastRun    *CheckRunOut `json:"last_run,omitempty" doc:"The latest run of any kind."`
	LastNight  *CheckRunOut `json:"last_nightly_run,omitempty" doc:"The latest nightly run, whose results the Compliance Center reads (FR-CTL-05)."`
	Requested  bool         `json:"run_requested" doc:"true while a Run now waits for the worker." example:"false"`
	OwnerNames bool         `json:"may_name_owners" doc:"true when the caller may name a person as a check's owner (Appendix D: C)." example:"false"`
	MayReview  bool         `json:"may_review" doc:"true when the caller may mark an item to review as reviewed (Appendix D: A; rev 0.164)." example:"true"`
}

type checksOutput struct{ Body ChecksOut }

type checkPath struct {
	Key string `path:"key" pattern:"^[a-z][a-z0-9-]*(\\.[a-z0-9-]+)+$" maxLength:"100" doc:"The check." example:"books.balance-sheet-balances"`
}

type checkOutput struct {
	ETag string `header:"ETag" doc:"The owner's version, for If-Match on setCheckOwner."`
	Body CheckOut
}

// CheckRunRequest is the answer to Run now.
type CheckRunRequest struct {
	RequestedAt string `json:"requested_at" format:"date-time" doc:"When it was asked for: the run that follows finishes after it." example:"2026-11-01T09:00:00Z"`
	Waiting     bool   `json:"already_waiting" doc:"true when an earlier Run now was still waiting, so this one joined it." example:"false"`
}

type runCatalogueInput struct {
	KeyHeader
	PreferHeader
}

type runCatalogueOutput struct {
	Status  int
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked for (FR-INT-14)."`
	Body    CheckRunRequest
}

// CheckOwnerIn names a check's owner.
type CheckOwnerIn struct {
	Person *string `json:"person_user_id" format:"uuid" required:"true" nullable:"true" doc:"The member's user ID; null gives the check back to the catalogue's role." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type setCheckOwnerInput struct {
	Key string `path:"key" pattern:"^[a-z][a-z0-9-]*(\\.[a-z0-9-]+)+$" maxLength:"100" doc:"The check." example:"setup.system-accounts-exist"`
	IfMatchHeader
	PreferHeader
	Body CheckOwnerIn
}

// seer is who reads the checks: which checks and whose items they see (design D7).
type seer struct {
	p     authn.Principal
	all   bool            // an unqualified view: every check
	areas map[string]bool // a qualified view's areas: sales, purchases, payroll
}

func seerOf(p authn.Principal) seer {
	s := seer{p: p, areas: map[string]bool{}}
	for _, r := range p.Roles {
		cell := authz.Grants[authz.ChecksHealth][authz.Column(r)]
		if cell.Rights == "" {
			continue
		}
		if cell.Qualifier == "" {
			s.all = true
		} else {
			s.areas[cell.Qualifier] = true
		}
	}
	if p.AI() { // the AI acting for a person sees no more than its own column allows (Appendix D)
		if c := authz.Grants[authz.ChecksHealth]["AI"]; c.Rights == "" {
			s.all, s.areas = false, map[string]bool{}
		}
	}
	return s
}

// sees reports whether the reader sees a check at all.
func (s seer) sees(d checks.Definition) bool {
	return s.all || (d.Qualifier != "" && s.areas[d.Qualifier])
}

// opens reports whether the reader may open a check's items: a full view of the items' own row, unqualified unless the
// check is of the reader's area, as a sales view opens sales items but never the whole books' (design D7).
func (s seer) opens(d checks.Definition) bool {
	policy := authz.Policy{Operation: "openCheckItems", Capability: authz.Capability(d.View), Need: authz.View, Unqualified: true}
	if d.Qualifier != "" {
		policy.Qualified = []string{d.Qualifier}
	}
	got := authz.Decide(policy, s.p.Roles, s.p.AI())
	return got.Allowed && got.Right != authz.Masked
}

func runOut(r storage.CheckRun) *CheckRunOut {
	return &CheckRunOut{Trigger: r.Trigger, StartedAt: r.StartedAt.UTC().Format(time.RFC3339), FinishedAt: r.FinishedAt.UTC().Format(time.RFC3339),
		Revision: r.LedgerRevision, Checks: len(r.Keys)}
}

// checkOut is a definition with its state and owner, as the reader sees it.
func checkOut(d checks.Definition, state *storage.CheckState, owner *storage.CheckOwner, s seer, reviewed map[string]bool) (CheckOut, error) {
	out := CheckOut{Key: d.Key, Ref: d.Ref, Area: d.Area, Worst: string(d.Worst), Title: d.Title, Explain: d.Explain, Rules: d.Rules,
		Nightly: slices.Contains(nightlyGroup, d.Ref), Status: string(checks.NotAssessed), Items: []CheckItem{}, ItemsArea: d.View,
		Owner: CheckOwnerOut{Role: RoleName(d.Owner)}}
	if state == nil {
		out.Needs, out.Detail = "its first run", "Not assessed: it has not run yet"
	} else {
		out.Status, out.Count, out.Detail, out.Fix, out.Needs, out.Setting = state.Status, state.Count, state.Detail, state.Fix, state.Needs, state.Setting
		out.RunAt, out.Trigger, out.Revision = state.RunAt.UTC().Format(time.RFC3339), state.Trigger, state.LedgerRevision
		out.ChangedAt = state.ChangedAt.UTC().Format(time.RFC3339)
		items, err := checks.ItemsOf(*state)
		if err != nil {
			return out, err
		}
		// Items marked reviewed since the run leave at once; the next run, which the review asked for, agrees (rev 0.164).
		status, count, kept, dropped := checks.WithoutReviewed(d.Key, checks.Status(state.Status), state.Count, items, func(k string) bool { return reviewed[k] })
		if dropped > 0 {
			out.Status, out.Count, items = string(status), count, kept
			if status == checks.OK {
				out.Detail, out.Fix = fmt.Sprintf("Nothing left to review: %d item(s) marked reviewed", dropped), ""
			}
		}
		if s.opens(d) {
			for _, it := range items {
				out.Items = append(out.Items, CheckItem(it))
			}
		} else {
			out.ItemsHidden = state.Count > 0
		}
	}
	if owner != nil {
		out.Owner.Version = owner.Version
		if owner.Person != nil {
			out.Owner.Person = &Person{Kind: "user", ID: owner.Person.String()}
		}
	}
	return out, nil
}

// nightlyGroup are the checks of FR-CTL-05's nightly ledger verification on the Checks page: the balances and the audit
// trail now, the sub-ledgers with their slice (design D6).
var nightlyGroup = []string{"C.2 #1", "C.2 #2", "C.2 #3"}

// visibleChecks are the catalogue's checks whose modules are on, of those the reader lists when s is not nil (design D7).
func (a *API) visibleChecks(ctx context.Context, u storage.UnitOfWork, s *seer) ([]checks.Definition, error) {
	features, err := featuresOn(ctx, a, u)
	if err != nil {
		return nil, err
	}
	on := map[string]bool{"features.multi_currency": features.MultiCurrency, "features.payroll": features.Payroll,
		"features.projects": features.Projects, "features.portals": features.Portals, "features.ai_assistant": features.AIAssistant}
	var out []checks.Definition
	for _, d := range checks.Catalogue() {
		if (s == nil || s.sees(d)) && (d.Module == "" || on[d.Module]) {
			out = append(out, d)
		}
	}
	return out, nil
}

func ownerETag(version int) string { return `"` + strconv.Itoa(version) + `"` }

// listChecks reads the checks the reader sees, with books health.
func (a *API) listChecks(ctx context.Context, u storage.UnitOfWork, s seer) (ChecksOut, error) {
	out := ChecksOut{Checks: []CheckOut{}}
	defs, err := a.visibleChecks(ctx, u, &s)
	if err != nil {
		return out, err
	}
	states, err := u.Checks().States(ctx)
	if err != nil {
		return out, err
	}
	owners, err := u.Checks().Owners(ctx)
	if err != nil {
		return out, err
	}
	byState := map[string]*storage.CheckState{}
	for i := range states {
		byState[states[i].Key] = &states[i]
	}
	byOwner := map[string]*storage.CheckOwner{}
	for i := range owners {
		byOwner[owners[i].Key] = &owners[i]
	}
	reviewed, err := reviewedSet(ctx, u)
	if err != nil {
		return out, err
	}
	var people []*Person
	var statuses []checks.Status
	for _, d := range defs {
		c, err := checkOut(d, byState[d.Key], byOwner[d.Key], s, reviewed)
		if err != nil {
			return out, err
		}
		statuses = append(statuses, checks.Status(c.Status))
		out.Checks = append(out.Checks, c)
	}
	// Within an area, the checks not assessed first, so none hides behind the score (design, risks); otherwise the
	// catalogue's order.
	areas := map[string]int{}
	for _, d := range defs {
		if _, ok := areas[d.Area]; !ok {
			areas[d.Area] = len(areas)
		}
	}
	slices.SortStableFunc(out.Checks, func(x, y CheckOut) int {
		return cmp.Or(cmp.Compare(areas[x.Area], areas[y.Area]),
			cmp.Compare(boolRank(x.Status != string(checks.NotAssessed)), boolRank(y.Status != string(checks.NotAssessed))))
	})
	for i := range out.Checks {
		people = append(people, out.Checks[i].Owner.Person)
	}
	if err := a.name(ctx, u, people...); err != nil {
		return out, err
	}
	h := checks.HealthOf(statuses)
	out.Health = CheckHealth{Score: h.Score, Problems: h.Problems, Warnings: h.Warnings, Due: h.Due, OK: h.OK, NotAssessed: h.NotAssessed}
	if r, err := u.Checks().LatestRun(ctx, ""); err == nil {
		out.LastRun = runOut(r)
	} else if !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	if r, err := u.Checks().LatestRun(ctx, storage.CheckNightly); err == nil {
		out.LastNight = runOut(r)
	} else if !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	if out.Requested, err = u.Outbox().Waiting(ctx, checks.RequestKind); err != nil {
		return out, err
	}
	out.OwnerNames = authz.Decide(authz.SetCheckOwner, s.p.Roles, s.p.AI()).Allowed
	out.MayReview = authz.Decide(authz.ReviewCheckItem, s.p.Roles, s.p.AI()).Allowed
	return out, nil
}

// reviewedSet are the review keys of the items marked reviewed.
func reviewedSet(ctx context.Context, u storage.UnitOfWork) (map[string]bool, error) {
	reviews, err := u.Checks().Reviews(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(reviews))
	for _, r := range reviews {
		out[checks.ReviewKey(r.Key, r.ItemKind, r.ItemID, r.ItemVersion)] = true
	}
	return out, nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// checkOf is the catalogue's check of key, whose module is on, or CHECK-01. Opened by its key, any check shows its status
// and words to every reader; only the list keeps a reader whose view is of an area to that area's checks (design D7).
func (a *API) checkOf(ctx context.Context, u storage.UnitOfWork, key string) (checks.Definition, error) {
	defs, err := a.visibleChecks(ctx, u, nil)
	if err != nil {
		return checks.Definition{}, err
	}
	if i := slices.IndexFunc(defs, func(d checks.Definition) bool { return d.Key == key }); i >= 0 {
		return defs[i], nil
	}
	return checks.Definition{}, problem(CodeUnknownCheck, fmt.Sprintf("There is no check %q here.", key),
		"Pick a check from listChecks; a module's checks show once the module is on")
}

// readCheck is one check as the reader sees it.
func (a *API) readCheck(ctx context.Context, u storage.UnitOfWork, s seer, key string) (CheckOut, error) {
	d, err := a.checkOf(ctx, u, key)
	if err != nil {
		return CheckOut{}, err
	}
	states, err := u.Checks().States(ctx)
	if err != nil {
		return CheckOut{}, err
	}
	owners, err := u.Checks().Owners(ctx)
	if err != nil {
		return CheckOut{}, err
	}
	var state *storage.CheckState
	if i := slices.IndexFunc(states, func(x storage.CheckState) bool { return x.Key == key }); i >= 0 {
		state = &states[i]
	}
	var owner *storage.CheckOwner
	if i := slices.IndexFunc(owners, func(x storage.CheckOwner) bool { return x.Key == key }); i >= 0 {
		owner = &owners[i]
	}
	reviewed, err := reviewedSet(ctx, u)
	if err != nil {
		return CheckOut{}, err
	}
	out, err := checkOut(d, state, owner, s, reviewed)
	if err != nil {
		return out, err
	}
	return out, a.name(ctx, u, out.Owner.Person)
}

// CheckReviewIn marks an item of a check reviewed.
type CheckReviewIn struct {
	Kind string `json:"kind" maxLength:"40" doc:"The item's kind, as the check lists it." example:"bill"`
	ID   string `json:"id" maxLength:"100" doc:"The item's ID, as the check lists it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d90"`
	Note string `json:"note,omitempty" maxLength:"500" doc:"What the review found, for the audit trail." example:"Checked with the supplier: two separate deliveries."`
}

type reviewCheckItemInput struct {
	Key string `path:"key" pattern:"^[a-z][a-z0-9-]*(\\.[a-z0-9-]+)+$" maxLength:"100" doc:"The check." example:"purchases.bills-to-review"`
	KeyHeader
	PreferHeader
	Body CheckReviewIn
}

type reviewCheckItemOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CheckOut
}

// notToReview is CHECK-04.
func notToReview(why string) error {
	return problem(CodeNotToReview, "That item is not one this check lists to review: "+why+".",
		"Pick an item to review (⚠) from the check as listChecks shows it", FieldProblem{Code: CodeNotToReview, Field: "body.id", Message: why,
			Fix: "Pick an item the check lists to review"})
}

// ownerRefused is CHECK-02.
func ownerRefused(why string) error {
	return problem(CodeCheckOwner, "That person cannot own this check: "+why+".",
		"Name an active member whose roles open the check's items, or send null to give it back to its role",
		FieldProblem{Code: CodeCheckOwner, Field: "body.person_user_id", Message: why, Fix: "Name another member"})
}

func registerChecks(a *API) {
	register(a, authz.ListChecks, exempt(huma.Operation{
		OperationID: "listChecks", Method: http.MethodGet, Path: "/v1/checks", Tags: []string{"Checks"},
		Summary: "List the checks and books health",
		Description: "Every check of the checks engine the caller sees (FR-CTL-01), by area, each with its Appendix C row, what it checks " +
			"and why, its latest result (ok, ⚠ warning, ✖ error, ⏰ due, not assessed with the input it needs, or failed, which counts as " +
			"✖), the items it found, its owner and when it ran; and books health from them (FR-CTL-02). A check's items show only to a " +
			"reader who may view their own row (Appendix D); a reader whose view is of an area (sales, purchases, payroll) sees that " +
			"area's checks only. A module's checks show once the module is on (FR-ORG-06). Read-only; no side effects.",
	}, "cursor", "The catalogue's checks, a hundred at most, are read whole: books health is worked out from all of them (FR-CTL-02)."),
		nil, func(ctx context.Context, _ *struct{}) (*checksOutput, error) {
			p, _ := authn.From(ctx)
			var out ChecksOut
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out, err = a.listChecks(ctx, u, seerOf(p))
				return err
			})
			if err != nil {
				return nil, err
			}
			return &checksOutput{Body: out}, nil
		})

	register(a, authz.GetCheck, exempt(huma.Operation{
		OperationID: "getCheck", Method: http.MethodGet, Path: "/v1/checks/{key}", Tags: []string{"Checks"},
		Summary:     "Get a check",
		Description: "One check as listChecks shows it, with its owner's ETag for setCheckOwner; its items only when the caller may view their own row. A check that does not exist, or whose module is off, is CHECK-01. Read-only; no side effects.",
	}, "cursor", "One check, not a list: its items are the first 50 it found, beside the count of all it found."), []string{CodeUnknownCheck}, func(ctx context.Context, in *checkPath) (*checkOutput, error) {
		p, _ := authn.From(ctx)
		var out CheckOut
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.readCheck(ctx, u, seerOf(p), in.Key)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &checkOutput{ETag: ownerETag(out.Owner.Version), Body: out}, nil
	})

	register(a, authz.RunChecks, huma.Operation{
		OperationID: "runChecks", Method: http.MethodPost, Path: "/v1/checks/runs", Tags: []string{"Checks"}, DefaultStatus: http.StatusAccepted,
		Summary: "Run every check now",
		Description: "Asks the worker to run every check of the organisation now (Run now; FR-CTL-01, design D5). The answer is 202: " +
			"listChecks' last_run finishes after requested_at once it has run, and run_requested is true until then. A Run now asked " +
			"while one waits joins it. At most one a minute for the organisation: 429 with Retry-After after that. Side effects: the " +
			"background run, which records each check's result, audits each change of status and records the run. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks for nothing.",
	}, []string{CodeRateLimited, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, func(ctx context.Context, in *runCatalogueInput) (*runCatalogueOutput, error) {
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (CheckRunRequest, error) {
			now := a.deps.Clock.Now()
			out := CheckRunRequest{RequestedAt: now.UTC().Format(time.RFC3339)}
			waiting, err := u.Outbox().Waiting(ctx, checks.RequestKind)
			if err != nil || waiting {
				out.Waiting = waiting
				return out, err
			}
			start := now.UTC().Truncate(rateWindow)
			used, err := u.RateLimits().Count(ctx, "checks run now", storage.RateWrite, start)
			if err != nil {
				return out, err
			}
			if used > 1 {
				reset := int(start.Add(rateWindow).Sub(now).Seconds()) + 1
				return out, problem(CodeRateLimited, "The checks were run on request in this minute already.",
					fmt.Sprintf("Wait %d seconds, then run them again.", reset)).withHeader("Retry-After", strconv.Itoa(reset))
			}
			item, err := checks.RequestItem(uploader(ctx))
			if err != nil {
				return out, err
			}
			if a.deps.Outbox != nil {
				_, err = a.deps.Outbox.Enqueue(ctx, u, p.Organisation, item)
			} else { // no dispatcher here: the tick sends it
				_, err = u.Outbox().Enqueue(ctx, item)
			}
			return out, err
		})
		if err != nil {
			return nil, err
		}
		return &runCatalogueOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReviewCheckItem, exempt(huma.Operation{
		OperationID: "reviewCheckItem", Method: http.MethodPost, Path: "/v1/checks/{key}/reviews", Tags: []string{"Checks"}, DefaultStatus: http.StatusCreated,
		Summary: "Mark an item to review as reviewed",
		Description: "Marks an item a check lists to review (⚠) as reviewed, with an optional note (FR-CTL-01, ENG-21; the owner's decision " +
			"of 1 Nov 2026, rev 0.164): it leaves the check at once, and comes back if the item changes, as the review names its version. " +
			"An item that cannot post (✖) is fixed, never reviewed; an item the check does not list to review, or that the caller's roles " +
			"do not open, is CHECK-04. Marking it again at the same version records nothing more. Side effects: the review and its " +
			"audit event, and the checks it concerns run again. Idempotent with Idempotency-Key; Prefer: return=preview checks it and " +
			"saves nothing.",
	}, "If-Match", "A review adds to the check and changes nothing of it: it names the item's version itself, and marking an item twice records nothing more."),
		[]string{CodeUnknownCheck, CodeNotToReview, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
			CodeIdempotencyOld, storage.CodeCheckReview, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *reviewCheckItemInput) (*reviewCheckItemOutput, error) {
			p, _ := authn.From(ctx)
			s := seerOf(p)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CheckOut, error) {
				d, err := a.checkOf(ctx, u, in.Key)
				if err != nil {
					return CheckOut{}, err
				}
				if !s.opens(d) {
					return CheckOut{}, notToReview("your roles do not open its items")
				}
				// The item as the latest run found it, reviewed already or not: marking it again records nothing more.
				states, err := u.Checks().States(ctx)
				if err != nil {
					return CheckOut{}, err
				}
				j := slices.IndexFunc(states, func(x storage.CheckState) bool { return x.Key == in.Key })
				if j < 0 {
					return CheckOut{}, notToReview("the check has not run yet")
				}
				items, err := checks.ItemsOf(states[j])
				if err != nil {
					return CheckOut{}, err
				}
				i := slices.IndexFunc(items, func(it checks.Item) bool { return it.Kind == in.Body.Kind && it.ID == in.Body.ID })
				if i < 0 {
					// A run since the review left it out: marking it again records nothing more.
					reviews, err := u.Checks().Reviews(ctx)
					if err != nil {
						return CheckOut{}, err
					}
					if slices.ContainsFunc(reviews, func(r storage.CheckReview) bool {
						return r.Key == in.Key && r.ItemKind == in.Body.Kind && r.ItemID == in.Body.ID
					}) {
						return a.readCheck(ctx, u, s, in.Key)
					}
				}
				switch {
				case i < 0:
					return CheckOut{}, notToReview("the check does not list it now")
				case items[i].Severity == string(checks.Error) || (items[i].Severity == "" && states[j].Status != string(checks.Warning)):
					return CheckOut{}, notToReview("it is not one to review: an item that cannot post is fixed, not reviewed")
				}
				if _, _, err := u.Checks().Review(ctx, storage.NewCheckReview{Key: in.Key, ItemKind: in.Body.Kind, ItemID: in.Body.ID,
					ItemVersion: items[i].Version, Note: strings.TrimSpace(in.Body.Note), ReviewedBy: uploader(ctx), ReviewedAt: a.deps.Clock.Now()}); err != nil {
					return CheckOut{}, err
				}
				return a.readCheck(ctx, u, s, in.Key)
			})
			if err != nil {
				return nil, err
			}
			return &reviewCheckItemOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.SetCheckOwner, huma.Operation{
		OperationID: "setCheckOwner", Method: http.MethodPut, Path: "/v1/checks/{key}/owner", Tags: []string{"Checks"},
		Summary: "Name a check's owner",
		Description: "Names a person as the owner of a check instead of the role the catalogue names, or gives it back to the role with " +
			"null (FR-CTL-01, design D8). The person must be an active member whose roles open the check's items; otherwise CHECK-02. " +
			"Side effects: the owner at its next version and its audit event, with the person before and after. Needs If-Match with " +
			"the owner's ETag from getCheck. Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeUnknownCheck, CodeCheckOwner, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeVersion, storage.CodeCheck, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *setCheckOwnerInput) (*checkOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			var person *uuid.UUID
			if in.Body.Person != nil {
				id, err := uuid.Parse(*in.Body.Person)
				if err != nil {
					return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.person_user_id", Message: "is not a user ID", Fix: "Send a member's user ID, or null"}})
				}
				person = &id
			}
			s := seerOf(p)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CheckOut, error) {
				d, err := a.checkOf(ctx, u, in.Key)
				if err != nil {
					return CheckOut{}, err
				}
				before, err := a.readCheck(ctx, u, s, in.Key)
				if err != nil {
					return CheckOut{}, err
				}
				if err := checkIfMatch(in.IfMatch, ownerETag(before.Owner.Version)); err != nil {
					return CheckOut{}, err
				}
				if person != nil {
					members, err := u.Members().List(ctx, a.deps.Clock.Now())
					if err != nil {
						return CheckOut{}, err
					}
					i := slices.IndexFunc(members, func(m storage.Member) bool { return m.UserID == *person })
					if i < 0 {
						return CheckOut{}, ownerRefused("they are not an active member of the organisation")
					}
					var roles []authz.Role
					for _, r := range members[i].Roles {
						roles = append(roles, authz.Role(r.Role))
					}
					them := seerOf(authn.Principal{Roles: roles, Class: authn.ClassUser})
					if !them.sees(d) || !them.opens(d) {
						return CheckOut{}, ownerRefused("their roles do not open the check's items")
					}
				}
				if _, err := u.Checks().SetOwner(ctx, in.Key, person, uploader(ctx), a.deps.Clock.Now(), before.Owner.Version); err != nil {
					return CheckOut{}, err
				}
				return a.readCheck(ctx, u, s, in.Key)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &checkOutput{Body: w.Body}, nil
			}
			return &checkOutput{ETag: ownerETag(w.Body.Owner.Version), Body: w.Body}, nil
		})
}
