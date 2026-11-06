package api

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/breakglass"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ break-glass (FM-8, PRD-001 rev 0.175)
//
// In Founder mode an Owner starts a break-glass, with a reason and stepped up, for 24 hours or until an Owner ends it,
// capped by the break-glass payment cap as it stood then. While it is active, that Owner releases a payment batch still
// waiting for approval without a second person: the break-glass approves it instead (FY242). It never repays a director,
// never pays the Owner using it, pays only accounts in force for 30 days, and stays within its cap. Every Owner and the
// Accountant are emailed at its start, each release and its end; every page shows its banner while it is active. It is
// logged for review as it starts; its review is due 7 days after it ends, by another Owner once it has ended or by the
// Accountant from the due date, and a new one waits until the last is reviewed (the owner's decisions of 6 Nov 2026).

// ObjectBreakGlass is what a break-glass's review log entry names.
const ObjectBreakGlass = "break_glass"

// BreakGlass is a break-glass as the API shows it.
type BreakGlass struct {
	ID           string              `json:"id" format:"uuid" doc:"The break-glass's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Status       string              `json:"status" enum:"active,ended" doc:"active for 24 hours from its start unless an Owner ends it sooner; ended after." example:"active"`
	StartedBy    Person              `json:"started_by" doc:"The Owner who started it: the only one who releases under it."`
	Reason       string              `json:"reason" doc:"Why they started it." example:"Wei Jie is on a flight until Friday; salaries are due tomorrow"`
	StartedAt    time.Time           `json:"started_at" doc:"When it started." example:"2026-11-06T01:00:00Z"`
	ExpiresAt    time.Time           `json:"expires_at" doc:"When it expires: 24 hours after it started (FM-8)." example:"2026-11-07T01:00:00Z"`
	EndedAt      *time.Time          `json:"ended_at,omitempty" doc:"When an Owner ended it sooner; absent unless one did." example:"2026-11-06T09:00:00Z"`
	EndedBy      *Person             `json:"ended_by,omitempty" doc:"The Owner who ended it sooner."`
	Cap          Money               `json:"cap" doc:"What it may release in all: the break-glass payment cap when it started."`
	Released     Money               `json:"released" doc:"What it released."`
	Left         Money               `json:"left" doc:"What it may still release."`
	Releases     []BreakGlassRelease `json:"releases" nullable:"false" doc:"The batches it released, oldest first."`
	ReviewDueAt  time.Time           `json:"review_due_at" doc:"When its review is due: 7 days after it ends. Until then only another Owner reviews it; from then the Accountant may too." example:"2026-11-14T01:00:00Z"`
	Overdue      bool                `json:"overdue" doc:"true while it is unreviewed after its review's due date: Checks shows it as cannot post until fixed (C.2 #53), which refuses the close (FM-4)." example:"false"`
	Review       *BreakGlassReviewed `json:"review,omitempty" doc:"Its review, once given."`
	YouMayEnd    bool                `json:"you_may_end" doc:"true when you, an Owner, may end it now." example:"false"`
	YouMayReview bool                `json:"you_may_review" doc:"true when you may review it now." example:"false"`
	Version      int                 `json:"version" doc:"Its version: send it as If-Match to end it." example:"1"`
}

// BreakGlassRelease is a batch a break-glass released.
type BreakGlassRelease struct {
	BatchID    string    `json:"batch_id" format:"uuid" doc:"The batch." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c10"`
	Number     string    `json:"number" doc:"Its number." example:"PB-0004"`
	Total      Money     `json:"total" doc:"What it paid."`
	ReleasedAt time.Time `json:"released_at" doc:"When it was released." example:"2026-11-06T02:00:00Z"`
}

// BreakGlassReviewed is a break-glass's review.
type BreakGlassReviewed struct {
	ReviewedBy Person    `json:"reviewed_by" doc:"Who reviewed it: another Owner, or the Accountant."`
	ReviewedAt time.Time `json:"reviewed_at" doc:"When." example:"2026-11-09T02:00:00Z"`
	Note       string    `json:"note,omitempty" doc:"What they noted." example:"Checked both payments against the bills; fine"`
}

// BreakGlassBlock is why a break-glass cannot start now.
type BreakGlassBlock struct {
	Code    string `json:"code" doc:"The rule." example:"BREAKGLASS-02"`
	Message string `json:"message" doc:"What stops it, in plain words." example:"An earlier break-glass waits for its review."`
	Fix     string `json:"fix" doc:"What to do." example:"Ask another Owner to review it"`
}

// BreakGlassPage is the organisation's break-glasses, newest first, with the one active now and whether one may start.
type BreakGlassPage struct {
	Items       []BreakGlass     `json:"items" doc:"Every break-glass, newest first."`
	NextCursor  string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
	Active      *BreakGlass      `json:"active,omitempty" doc:"The break-glass active now; absent when none is."`
	Cap         Money            `json:"cap" doc:"The break-glass payment cap now: what the next break-glass may release (setting approvals.break_glass_cap)."`
	YouMayStart bool             `json:"you_may_start" doc:"true when you, an Owner, may start one now." example:"false"`
	Blocked     *BreakGlassBlock `json:"blocked,omitempty" doc:"Why none may start now, whoever asks; absent when one may."`
}

// BreakGlassStart starts a break-glass.
type BreakGlassStart struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why: who is unreachable, and what must be paid. Every Owner and the Accountant read it." example:"Wei Jie is on a flight until Friday; salaries are due tomorrow"`
}

// BreakGlassReview reviews a break-glass.
type BreakGlassReview struct {
	Note string `json:"note,omitempty" maxLength:"500" doc:"What you noted." example:"Checked both payments against the bills; fine"`
}

// BreakGlassPath names a break-glass.
type BreakGlassPath struct {
	BreakGlassID string `path:"break_glass_id" format:"uuid" doc:"The break-glass's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

type breakGlassListInput struct{ PageQuery }

type breakGlassStartInput struct {
	KeyHeader
	PreferHeader
	Body BreakGlassStart
}

type breakGlassEndInput struct {
	BreakGlassPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type breakGlassReviewInput struct {
	BreakGlassPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BreakGlassReview
}

type breakGlassListOutput struct{ Body BreakGlassPage }

type breakGlassOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The break-glass's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BreakGlass
}

// BreakGlassCodes are the rules of break-glass.
func BreakGlassCodes() []string {
	return []string{breakglass.CodeFounderOnly, breakglass.CodeWaiting, breakglass.CodeNotActive, breakglass.CodeNotWaiting, breakglass.CodeDirector,
		breakglass.CodeOwn, breakglass.CodeNewAccount, breakglass.CodeCap, breakglass.CodeEnded, breakglass.CodeTooEarly, breakglass.CodeReviewer,
		storage.CodeBreakGlass}
}

// glasses are the organisation's break-glasses with their review log entries, as one unit of work reads them.
type glasses struct {
	all     []storage.BreakGlass
	entries map[uuid.UUID]storage.ReviewLogEntry // by break-glass
}

func readGlasses(ctx context.Context, u storage.UnitOfWork) (glasses, error) {
	all, err := u.BreakGlasses().List(ctx)
	if err != nil {
		return glasses{}, err
	}
	es, err := u.ReviewLog().List(ctx)
	if err != nil {
		return glasses{}, err
	}
	out := glasses{all: all, entries: map[uuid.UUID]storage.ReviewLogEntry{}}
	for _, e := range es {
		if e.Kind != storage.ReviewBreakGlass {
			continue
		}
		if id, err := uuid.Parse(e.ObjectKey); err == nil {
			out.entries[id] = e
		}
	}
	return out, nil
}

// active is the break-glass active at now, if any.
func (g glasses) active(now time.Time) *storage.BreakGlass {
	for i := range g.all {
		if breakglass.Active(g.all[i], now) {
			return &g.all[i]
		}
	}
	return nil
}

// block is why no break-glass may start at now in mode: Founder mode only, and never while one waits for its review.
func (g glasses) block(mode string, now time.Time) *BreakGlassBlock {
	if mode != team.Founder {
		return &BreakGlassBlock{Code: breakglass.CodeFounderOnly, Message: "Break-glass is for Founder mode only.",
			Fix: "In Standard and Enterprise modes another person approves each payment batch"}
	}
	for _, x := range g.all {
		if e, ok := g.entries[x.ID]; !ok || e.ReviewedBy == nil {
			if breakglass.Active(x, now) {
				return &BreakGlassBlock{Code: breakglass.CodeWaiting, Message: "A break-glass is active already: it cannot be extended or renewed.",
					Fix: "Use it until it expires, or end it"}
			}
			return &BreakGlassBlock{Code: breakglass.CodeWaiting, Message: "An earlier break-glass waits for its review, so another does not start yet.",
				Fix: "Ask another Owner to review it, or the Accountant once its review is due"}
		}
	}
	return nil
}

// isOwner reports whether p holds the Owner's right on users and roles: who starts and ends a break-glass.
func isOwner(p authn.Principal) bool {
	return !p.AI() && p.UserID != uuid.Nil && authz.Decide(authz.StartBreakGlass, p.Roles, false).Allowed
}

// breakGlassOf is g as p sees it at now.
func (a *API) breakGlassOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, gs glasses, g storage.BreakGlass,
	names map[uuid.UUID]string, now time.Time) (BreakGlass, error) {
	cur, err := money.Lookup(g.Currency)
	if err != nil {
		return BreakGlass{}, err
	}
	capped, err := money.ParseStored(string(g.Cap), cur)
	if err != nil {
		return BreakGlass{}, err
	}
	used, err := breakglass.Used(g)
	if err != nil {
		return BreakGlass{}, err
	}
	left, err := breakglass.Left(g)
	if err != nil {
		return BreakGlass{}, err
	}
	out := BreakGlass{ID: g.ID.String(), Status: "ended", StartedBy: Person{Kind: "user", ID: g.StartedBy.String(), Name: names[g.StartedBy]},
		Reason: g.Reason, StartedAt: g.StartedAt.UTC(), ExpiresAt: g.ExpiresAt.UTC(), Cap: amountOf(capped), Released: amountOf(used),
		Left: amountOf(left), Releases: []BreakGlassRelease{}, ReviewDueAt: breakglass.Due(g).UTC(), Version: g.Version}
	if breakglass.Active(g, now) {
		out.Status = "active"
		out.YouMayEnd = isOwner(p)
	}
	if g.EndedAt != nil {
		at := g.EndedAt.UTC()
		out.EndedAt = &at
		out.EndedBy = &Person{Kind: "user", ID: g.EndedBy.String(), Name: names[*g.EndedBy]}
	}
	for _, x := range g.Uses {
		doc, err := u.Documents().Get(ctx, x.BatchID)
		if err != nil {
			return BreakGlass{}, err
		}
		out.Releases = append(out.Releases, BreakGlassRelease{BatchID: x.BatchID.String(), Number: doc.Number, Total: moneyOf(x.Total, x.Currency),
			ReleasedAt: x.UsedAt.UTC()})
	}
	e, logged := gs.entries[g.ID]
	switch {
	case logged && e.ReviewedBy != nil:
		out.Review = &BreakGlassReviewed{ReviewedBy: Person{Kind: "user", ID: e.ReviewedBy.String(), Name: names[*e.ReviewedBy]},
			ReviewedAt: e.ReviewedAt.UTC(), Note: e.Note}
	default:
		out.Overdue = !now.Before(breakglass.Due(g))
		if p.UserID != uuid.Nil && !p.AI() {
			out.YouMayReview, _ = breakglass.Reviewer(g, p.UserID, p.Roles, now)
		}
	}
	return out, nil
}

// breakGlassCap is the break-glass payment cap in force today.
func breakGlassCap(ctx context.Context, a *API, u storage.UnitOfWork) (money.Money, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return money.Money{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return money.Money{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return money.Money{}, err
	}
	d, _ := settings.Lookup("approvals.break_glass_cap")
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return money.Money{}, err
	}
	return money.Parse(settings.On(d, changes, today).Value, base)
}

// notifyBreakGlass tells every Owner and the Accountant, and the Owner who started g, what happened to it (FM-8).
func (a *API) notifyBreakGlass(ctx context.Context, u storage.UnitOfWork, g storage.BreakGlass, title string, now time.Time) error {
	ms, err := u.Members().List(ctx, now)
	if err != nil {
		return err
	}
	to := []uuid.UUID{g.StartedBy}
	for _, m := range ms {
		if m.Status != "active" || slices.Contains(to, m.UserID) {
			continue
		}
		if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool {
			return authz.Role(r.Role) == authz.Owner || authz.Role(r.Role) == authz.Accountant
		}) {
			to = append(to, m.UserID)
		}
	}
	return a.notify(ctx, u, notify.Event{Type: notify.BreakGlassType, RecordType: ObjectBreakGlass, RecordID: &g.ID, Title: title,
		Link: "/break-glass", Audience: notify.Audience{Users: to}, At: now})
}

// singapore is a moment as people in Singapore read it.
func singapore(t time.Time) string { return t.In(appclock.Singapore).Format("2 Jan 2006 at 15:04") }

// glassView reads g again and shows it.
func (a *API) glassView(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, now time.Time) (BreakGlass, error) {
	gs, err := readGlasses(ctx, u)
	if err != nil {
		return BreakGlass{}, err
	}
	i := slices.IndexFunc(gs.all, func(x storage.BreakGlass) bool { return x.ID == id })
	if i < 0 {
		return BreakGlass{}, problem(CodeNotFound, "No break-glass has that ID.", "List the break-glasses to find its ID")
	}
	names, err := memberNames(ctx, u, now)
	if err != nil {
		return BreakGlass{}, err
	}
	return a.breakGlassOf(ctx, u, p, gs, gs.all[i], names, now)
}

// breakGlassPayments are the payments of b as break-glass reads them: who each pays, and since when its account is in
// force.
func breakGlassPayments(ctx context.Context, u storage.UnitOfWork, b paymentbatch.Batch, names map[uuid.UUID]string) ([]breakglass.Payment, error) {
	var out []breakglass.Payment
	for _, pay := range b.Payments {
		x := breakglass.Payment{Director: pay.Director != uuid.Nil}
		if pay.Payee.IsPerson() {
			x.Payee, x.Member = names[pay.Payee.PersonID], pay.Payee.PersonID
		} else {
			c, err := u.Contacts().Get(ctx, pay.Payee.ContactID)
			if err != nil {
				return nil, err
			}
			x.Payee = c.LegalName
			if c.MemberUserID != nil {
				x.Member = *c.MemberUserID
			}
		}
		if pay.Account != nil {
			r, err := changeApproval(ctx, u, pay.Account.ChangeID)
			if err != nil {
				return nil, err
			}
			if r != nil && r.DecidedAt != nil {
				at := *r.DecidedAt
				x.InForce = &at
			}
		}
		out = append(out, x)
	}
	return out, nil
}

// releaseUnderBreakGlass releases a batch waiting for approval as p, under p's active break-glass, in u (FM-8): the
// batch's checks and break-glass's limits, a request waiting for its approval voided, its number, the use that approves
// it, and the release as any release writes it.
func (a *API) releaseUnderBreakGlass(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, ifMatch string) (PaymentBatch, error) {
	doc, err := batchDoc(ctx, u, id, true)
	if err != nil {
		return PaymentBatch{}, err
	}
	if err := checkIfMatch(ifMatch, batchETag(doc)); err != nil {
		return PaymentBatch{}, err
	}
	if doc.Status != storage.DocumentDraft {
		return PaymentBatch{}, problem(breakglass.CodeNotWaiting, "The batch is not waiting for approval: break-glass approves only a batch that is.",
			"Release an approved batch as usual")
	}
	v, err := a.batchView(ctx, u)
	if err != nil {
		return PaymentBatch{}, err
	}
	b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	if len(b.Errors) > 0 {
		return PaymentBatch{}, batchProblems(b.Errors)
	}
	gs, err := readGlasses(ctx, u)
	if err != nil {
		return PaymentBatch{}, err
	}
	g := gs.active(v.now)
	if g == nil || g.StartedBy != p.UserID {
		return PaymentBatch{}, problem(breakglass.CodeNotActive, "You have no active break-glass.",
			"Start break-glass on Team mode, or ask another person to approve the batch")
	}
	names, err := memberNames(ctx, u, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	pays, err := breakGlassPayments(ctx, u, b, names)
	if err != nil {
		return PaymentBatch{}, err
	}
	refused, err := breakglass.Refusals(*g, p.UserID, b.Total, pays, v.now)
	if err != nil {
		return PaymentBatch{}, err
	}
	if len(refused) > 0 {
		var fps []FieldProblem
		for _, x := range refused {
			field := "batch"
			if x.Payee != "" {
				field = "payments[" + x.Payee + "]"
			}
			fps = append(fps, FieldProblem{Code: x.Code, Field: field, Message: x.Message, Fix: x.Fix})
		}
		return PaymentBatch{}, problem(refused[0].Code, fmt.Sprintf("The batch is not released under break-glass: %d problem(s), each listed in errors.", len(refused)),
			refused[0].Fix, fps...)
	}

	// A request waiting for the batch's approval is voided: the break-glass approved it instead.
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return PaymentBatch{}, err
	}
	for _, r := range pending {
		if r.ObjectType == approval.ObjectPaymentBatch && r.ObjectKey == id.String() {
			if _, err := u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Void), "Released under break-glass "+g.ID.String(), v.now); err != nil {
				return PaymentBatch{}, err
			}
		}
	}
	if _, err := u.Sequences().Number(ctx, id, storage.DocumentPaymentBatch); err != nil {
		return PaymentBatch{}, err
	}
	if _, err := u.BreakGlasses().Use(ctx, storage.BreakGlassUse{BreakGlassID: g.ID, BatchID: id, BatchVersion: doc.Version, UsedBy: p.UserID,
		UsedAt: v.now, Currency: paymentbatch.SGD.Code, Total: storage.Decimal(b.Total.String())}); err != nil {
		return PaymentBatch{}, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return PaymentBatch{}, err
	}
	if doc, err = u.Documents().Get(ctx, id); err != nil {
		return PaymentBatch{}, err
	}
	if _, err := a.writeRelease(ctx, u, p, doc, b, v, uuid.Nil, g.ID); err != nil {
		return PaymentBatch{}, err
	}
	if err := a.notifyBreakGlass(ctx, u, *g, fmt.Sprintf("%s released %s, %s SGD, under break-glass", cmp.Or(names[p.UserID], "An Owner"), doc.Number,
		b.Total.String()), v.now); err != nil {
		return PaymentBatch{}, err
	}
	return a.batchOf(ctx, u, v, doc, false)
}

func registerBreakGlass(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListBreakGlass, huma.Operation{
		OperationID: "listBreakGlass", Method: http.MethodGet, Path: "/v1/break-glass", Tags: []string{"Members"},
		Summary: "List the break-glasses",
		Description: "Every break-glass of the organisation, newest first (FM-8; PRD-001 rev 0.175): who started it and why, its 24 hours, its " +
			"cap and what it released, its end, and its review, due 7 days after it ends. Also the one active now, the cap the next one " +
			"would start with, and whether one may start: in Founder mode only, and never while an earlier one waits for its review. For " +
			"whoever views payment batches; never the AI. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *breakGlassListInput) (*breakGlassListOutput, error) {
		p, _ := authn.From(ctx)
		now := a.deps.Clock.Now()
		var page BreakGlassPage
		var all []BreakGlass
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			gs, err := readGlasses(ctx, u)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, now)
			if err != nil {
				return err
			}
			for i := len(gs.all) - 1; i >= 0; i-- {
				v, err := a.breakGlassOf(ctx, u, p, gs, gs.all[i], names, now)
				if err != nil {
					return err
				}
				all = append(all, v)
				if v.Status == "active" {
					page.Active = &v
				}
			}
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return err
			}
			page.Blocked = gs.block(mode, now)
			page.YouMayStart = page.Blocked == nil && isOwner(p)
			capped, err := breakGlassCap(ctx, a, u)
			page.Cap = amountOf(capped)
			return err
		})
		if err != nil {
			return nil, err
		}
		if all == nil {
			all = []BreakGlass{}
		}
		page.Items, page.NextCursor, err = pageOf(all, func(v BreakGlass) string { return v.ID }, in.PageQuery)
		return &breakGlassListOutput{Body: page}, err
	})

	register(a, authz.GetBreakGlass, huma.Operation{
		OperationID: "getBreakGlass", Method: http.MethodGet, Path: "/v1/break-glass/{break_glass_id}", Tags: []string{"Members"},
		Summary: "Get a break-glass",
		Description: "A break-glass with what it released and its review (FM-8), and its ETag. For whoever views payment batches; never the " +
			"AI. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *BreakGlassPath) (*breakGlassOutput, error) {
		id, err := parseID("break_glass_id", in.BreakGlassID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out BreakGlass
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			out, err = a.glassView(ctx, u, p, id, a.deps.Clock.Now())
			return err
		})
		if err != nil {
			return nil, err
		}
		return &breakGlassOutput{Status: http.StatusOK, ETag: etag(out.Version), Body: out}, nil
	})

	register(a, authz.StartBreakGlass, huma.Operation{
		OperationID: "startBreakGlass", Method: http.MethodPost, Path: "/v1/break-glass", Tags: []string{"Members"},
		Summary: "Start break-glass", DefaultStatus: http.StatusCreated,
		Description: "Starts a break-glass for 24 hours (FM-8; PRD-001 rev 0.175): an Owner, stepped up in the last 5 minutes, with a " +
			"reason, in Founder mode only, when the other founder is unreachable. While it is active they release a payment batch still " +
			"waiting for approval without a second person (releasePaymentBatchUnderBreakGlass), within the break-glass payment cap as it " +
			"stands now. Nothing else changes: every action FM-1 holds still waits for a second person. Refused while an earlier break-glass " +
			"waits for its review, so it is never extended or renewed (BREAKGLASS-02), and outside Founder mode (BREAKGLASS-01); the " +
			"database refuses both too. Side effects: the break-glass, its review log entry (FM-3), its banner on every page while it is " +
			"active, and an email at once to every Owner and the Accountant. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and starts nothing.",
	}, append([]string{CodeSessionRequired, CodeStepUpRequired, storage.CodeBreakGlass, breakglass.CodeFounderOnly, breakglass.CodeWaiting}, writes...),
		func(ctx context.Context, in *breakGlassStartInput) (*breakGlassOutput, error) {
			reason := strings.TrimSpace(in.Body.Reason)
			if reason == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say who is unreachable and what must be paid"}})
			}
			p, err := a.steppedUpPerson(ctx, "Starting break-glass")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BreakGlass, error) {
				now := a.deps.Clock.Now()
				gs, err := readGlasses(ctx, u)
				if err != nil {
					return BreakGlass{}, err
				}
				mode, _, _, err := teamModeOf(ctx, u)
				if err != nil {
					return BreakGlass{}, err
				}
				if b := gs.block(mode, now); b != nil {
					return BreakGlass{}, problem(b.Code, b.Message, b.Fix)
				}
				capped, err := breakGlassCap(ctx, a, u)
				if err != nil {
					return BreakGlass{}, err
				}
				g, err := u.BreakGlasses().Start(ctx, storage.NewBreakGlass{StartedBy: p.UserID, Reason: reason, StartedAt: now,
					Currency: capped.Currency().Code, Cap: storage.Decimal(capped.String())})
				if err != nil {
					return BreakGlass{}, err
				}
				names, err := memberNames(ctx, u, now)
				if err != nil {
					return BreakGlass{}, err
				}
				who := cmp.Or(names[p.UserID], "An Owner")
				actor := p.UserID
				if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewBreakGlass, ObjectType: ObjectBreakGlass, ObjectKey: g.ID.String(),
					Summary: fmt.Sprintf("%s started break-glass until %s Singapore time, up to %s %s: %s", who, singapore(g.ExpiresAt), capped, capped.Currency().Code, reason),
					Actor:   &actor, LoggedAt: g.StartedAt}); err != nil {
					return BreakGlass{}, err
				}
				if err := a.notifyBreakGlass(ctx, u, g, fmt.Sprintf("%s started break-glass until %s Singapore time: %s", who, singapore(g.ExpiresAt), reason), now); err != nil {
					return BreakGlass{}, err
				}
				return a.glassView(ctx, u, p, g.ID, now)
			})
			if err != nil {
				return nil, err
			}
			out := &breakGlassOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = etag(w.Body.Version), replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.EndBreakGlass, huma.Operation{
		OperationID: "endBreakGlass", Method: http.MethodPost, Path: "/v1/break-glass/{break_glass_id}/end", Tags: []string{"Members"},
		Summary: "End break-glass now",
		Description: "Ends an active break-glass before its 24 hours (FM-8): an Owner signed in to Fyniq, such as the founder who used it or the " +
			"other one, back. It releases nothing more, and its review may be given from now; it is due 7 days later. BREAKGLASS-09 when it " +
			"has ended already. Side effects: the end, audited, and an email at once to every Owner and the Accountant. Needs If-Match with " +
			"its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and ends nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, storage.CodeBreakGlass, breakglass.CodeEnded}, writes...),
		func(ctx context.Context, in *breakGlassEndInput) (*breakGlassOutput, error) {
			id, err := parseID("break_glass_id", in.BreakGlassID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Ending break-glass needs an Owner signed in to Fyniq.", "Sign in to Fyniq in the browser and do it there.")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BreakGlass, error) {
				now := a.deps.Clock.Now()
				g, err := u.BreakGlasses().Get(ctx, id)
				if err != nil {
					return BreakGlass{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(g.Version)); err != nil {
					return BreakGlass{}, err
				}
				if !breakglass.Active(g, now) {
					return BreakGlass{}, problem(breakglass.CodeEnded, "The break-glass has ended already.", "Nothing to end: review it instead")
				}
				if g, err = u.BreakGlasses().End(ctx, id, g.Version, p.UserID, now); err != nil {
					return BreakGlass{}, err
				}
				names, err := memberNames(ctx, u, now)
				if err != nil {
					return BreakGlass{}, err
				}
				if err := a.notifyBreakGlass(ctx, u, g, fmt.Sprintf("%s ended break-glass; its review is due by %s Singapore time",
					cmp.Or(names[p.UserID], "An Owner"), singapore(breakglass.Due(g))), now); err != nil {
					return BreakGlass{}, err
				}
				return a.glassView(ctx, u, p, id, now)
			})
			if err != nil {
				return nil, err
			}
			out := &breakGlassOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = etag(w.Body.Version), replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.ReviewBreakGlass, huma.Operation{
		OperationID: "reviewBreakGlass", Method: http.MethodPost, Path: "/v1/break-glass/{break_glass_id}/review", Tags: []string{"Members"},
		Summary: "Review a break-glass",
		Description: "Records your review of a break-glass and what it released (FM-8, FM-3; PRD-001 rev 0.175), with an optional note, once: " +
			"after it has ended, by another Owner at any time, or by the Accountant from its review's due date, 7 days after it ended, the " +
			"other founder being still away; never by the Owner who used it (BREAKGLASS-10, BREAKGLASS-11; the database refuses a review " +
			"before it ends or by its Owner). Its review clears its line on Checks (C.2 #53) and lets another break-glass start. Needs a " +
			"person signed in to Fyniq; never the AI. Side effects: the review on its review log entry, audited. Needs If-Match with " +
			"its ETag: the review is of the break-glass as read. Idempotent with Idempotency-Key; Prefer: return=preview checks it " +
			"and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, approval.CodeReviewed, storage.CodeApproval, breakglass.CodeTooEarly,
		breakglass.CodeReviewer}, writes...),
		func(ctx context.Context, in *breakGlassReviewInput) (*breakGlassOutput, error) {
			id, err := parseID("break_glass_id", in.BreakGlassID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Reviewing a break-glass needs a person with a Fyniq user, signed in to Fyniq.",
					"Sign in to Fyniq in the browser and do it there.")
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			note := strings.TrimSpace(in.Body.Note)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BreakGlass, error) {
				now := a.deps.Clock.Now()
				gs, err := readGlasses(ctx, u)
				if err != nil {
					return BreakGlass{}, err
				}
				i := slices.IndexFunc(gs.all, func(x storage.BreakGlass) bool { return x.ID == id })
				e, logged := gs.entries[id]
				if i < 0 || !logged {
					return BreakGlass{}, problem(CodeNotFound, "No break-glass has that ID.", "List the break-glasses to find its ID")
				}
				if err := checkIfMatch(in.IfMatch, etag(gs.all[i].Version)); err != nil {
					return BreakGlass{}, err
				}
				if e.ReviewedBy != nil {
					return BreakGlass{}, problem(approval.CodeReviewed, "The break-glass was reviewed already.", "Nothing to do")
				}
				if ok, why := breakglass.Reviewer(gs.all[i], p.UserID, p.Roles, now); !ok {
					return BreakGlass{}, problem(why.Code, why.Message, why.Fix)
				}
				if err := u.ReviewLog().Review(ctx, e.ID, p.UserID, now, note); err != nil {
					return BreakGlass{}, err
				}
				return a.glassView(ctx, u, p, id, now)
			})
			if err != nil {
				return nil, err
			}
			out := &breakGlassOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = etag(w.Body.Version), replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.ReleasePaymentBatchUnderBreakGlass, huma.Operation{
		OperationID: "releasePaymentBatchUnderBreakGlass", Method: http.MethodPost, Path: "/v1/payment-batches/{batch_id}/break-glass-release",
		Tags: []string{"Purchases"}, Summary: "Release a payment batch under break-glass",
		Description: "Releases a batch still waiting for approval without a second person, under your active break-glass (FM-8; PRD-001 rev " +
			"0.175): the Owner who started it, holding the Payment releaser's right, stepped up in the last 5 minutes. The break-glass " +
			"approves the batch instead of a second person; it is numbered and released as releasePaymentBatch releases one, and a request " +
			"waiting for its approval is voided. Every check of the batch runs again. Refused when it repays a director or shareholder " +
			"(BREAKGLASS-05), pays you, as a claimant or a vendor you sign in as (BREAKGLASS-06, FM-9), pays a bank account that came into " +
			"force in the last 30 days (BREAKGLASS-07), or would take the break-glass's releases above its cap (BREAKGLASS-08), each " +
			"problem listed at once; and when you have no active break-glass (BREAKGLASS-03) or the batch is not waiting for approval " +
			"(BREAKGLASS-04). The database refuses a use outside the window, by anyone else or above the cap. Side effects, in one " +
			"transaction: the use, the batch's number and approval, the release with its payments and payment list as releasePaymentBatch " +
			"writes them, and an email at once to every Owner and the Accountant. Needs If-Match with the batch's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview runs every check and releases nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, payee.CodeNoKeys, storage.CodeBatch,
		storage.CodeBatchHeld, storage.CodeBreakGlass, storage.CodeVersion, breakglass.CodeNotActive, breakglass.CodeNotWaiting, breakglass.CodeDirector,
		breakglass.CodeOwn, breakglass.CodeNewAccount, breakglass.CodeCap},
		append(append(append(locks, batchCheckCodes()...), BatchCodes...), postingCodes()...)...),
		func(ctx context.Context, in *batchCommandInput) (*batchReleaseOutput, error) {
			id, err := parseID("batch_id", in.BatchID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := a.steppedUpPerson(ctx, "Releasing a payment batch under break-glass")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PaymentBatch, error) {
				return a.releaseUnderBreakGlass(ctx, u, p, id, in.IfMatch)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &batchReleaseOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &batchReleaseOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentPosted)), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
