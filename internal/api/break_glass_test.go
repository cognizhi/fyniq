//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/breakglass"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// settledVendor is a vendor whose SGD account came into force 31 days ago, which break-glass may pay (FM-8).
func (e *env) settledVendor(name string) string {
	e.t.Helper()
	now := e.clock.Now()
	e.clock.Set(now.Add(-31 * 24 * time.Hour))
	defer e.clock.Set(now)
	v := e.vendor(name)
	e.vendorAccount(v, "0123456789")
	return v
}

// draftBatch prepares a batch of items as an Accountant, paid from acc, and returns its ID and ETag.
func (e *env) draftBatch(acc storage.MoneyAccount, items ...string) (string, string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/payment-batches", e.as(authz.Accountant), batchBody(acc, items...))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createPaymentBatch = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// releaseUnder releases a batch under break-glass as tok.
func (e *env) releaseUnder(id, tag, tok string) response {
	return e.call(http.MethodPost, "/v1/payment-batches/"+id+"/break-glass-release", tok, nil, "If-Match", tag)
}

// A founder alone releases what must be paid under break-glass, and nothing more (FM-8; PRD-001 rev 0.175, the owner's
// decisions of 6 Nov 2026; G4-062, G4-063). Wei Ling, an Owner holding the Payment releaser's right, starts a
// break-glass with a reason while Ah Kow is unreachable: it lasts 24 hours, at the cap in force then, and every page
// shows a banner nobody can dismiss. She releases a batch Ah Kow asked approval for, which voids his request; a second
// break-glass, a batch paying an account changed this month, one paying her own claim and one above the cap are refused,
// as is Ah Kow releasing under her break-glass. She cannot review her own break-glass; Ah Kow cannot until it ends,
// which he does on his return. A new one waits for the review. The Accountant may review it only from its due date, 7
// days after it ended, when it is overdue; the review clears it, and another break-glass may start.
// req: FM-8
// req: FM-9
// req: FM-3
// req: G4-062
// req: G4-063
// req: FR-NTF-03
// req: access/team-modes Starting break-glass
// req: access/team-modes Allowed actions
// req: access/team-modes Payees with a recent bank change
// req: access/team-modes Total per break-glass
// req: access/team-modes Break-glass cannot extend itself
// req: access/team-modes Banner and notifications
// req: access/team-modes Post-review
// req: access/team-modes A new break-glass waits for the last one's review
// req: access/team-modes Releasing a batch under break-glass
// req: access/team-modes G4-062 Break-glass with the other founder unreachable
// req: access/team-modes G4-063 Break-glass to settle the founder's own balance
// asvs: V2.3.5, V8.2.2
func TestAFounderAloneReleasesUnderBreakGlass(t *testing.T) {
	f := newFounders(t)
	e := f.e
	weiLing := f.as(f.weiling, authz.Owner, authz.PaymentReleaser)
	ahKow := f.as(f.ahkow)
	acc := e.batchAccount()
	settled := e.settledVendor("Settled Supplies Pte Ltd")
	batch, tag := e.draftBatch(acc, "bill:"+e.billOf(settled, "100.00", "9.00"))

	l := e.call(http.MethodGet, "/v1/break-glass", weiLing, nil)
	if l.Status != http.StatusOK || l.Body["you_may_start"] != true || l.str("cap", "amount") != "20000.00" || l.Body["active"] != nil {
		t.Fatalf("before any break-glass: %s", l.Raw)
	}
	// The founders lowered the cap to S$150.00 (a change under dual control, whose approval has its own tests).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "approvals.break_glass_cap", "20000.00", "150.00", porttest.Day(10, 1), 1)
		return err
	})
	e.problemIs(e.releaseUnder(batch, tag, weiLing), "releasePaymentBatchUnderBreakGlass", 409, breakglass.CodeNotActive)
	e.problemIs(e.call(http.MethodPost, "/v1/break-glass", weiLing, map[string]any{"reason": " "}), "startBreakGlass", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, "/v1/break-glass", e.as(authz.Owner), map[string]any{"reason": "Unreachable"}), "startBreakGlass", 403,
		CodeStepUpRequired)

	s := e.call(http.MethodPost, "/v1/break-glass", weiLing, map[string]any{"reason": "Ah Kow is on a flight until Friday; the rent is due"})
	if s.Status != http.StatusCreated || s.str("status") != "active" || s.str("started_by", "id") != f.weiling || s.str("cap", "amount") != "150.00" {
		t.Fatalf("startBreakGlass = %d: %s", s.Status, s.Raw)
	}
	glass, glassTag := s.str("id"), s.Header.Get("ETag")
	started, _ := time.Parse(time.RFC3339Nano, s.str("started_at"))
	expires, _ := time.Parse(time.RFC3339Nano, s.str("expires_at"))
	if expires.Sub(started) != 24*time.Hour {
		t.Errorf("it lasts %s", expires.Sub(started))
	}
	e.problemIs(e.call(http.MethodPost, "/v1/break-glass", ahKow, map[string]any{"reason": "Again"}), "startBreakGlass", 409, breakglass.CodeWaiting)

	// The banner: on every member's pages, critical, never dismissed. Every Owner and the Accountant are told by email.
	bs := e.call(http.MethodGet, "/v1/banners", e.as(authz.Employee), nil)
	first, _ := bs.Body["items"].([]any)
	if len(first) == 0 || first[0].(map[string]any)["id"] != glass || first[0].(map[string]any)["severity"] != "critical" ||
		first[0].(map[string]any)["dismissible"] != false || !strings.Contains(first[0].(map[string]any)["message"].(string), "Break-glass is on") {
		t.Fatalf("the banner: %s", bs.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/banners/"+glass+"/dismiss", ahKow, nil), "dismissBanner", 409, CodeBannerStays)
	fanOutAll(t, e)
	if n := e.call(http.MethodGet, "/v1/notifications", ahKow, nil); !strings.Contains(n.Raw, `"type":"security.break_glass"`) {
		t.Errorf("Ah Kow, the other Owner, was not told: %s", n.Raw)
	}

	// Ah Kow asked for the batch's approval before he left; Wei Ling releases it under break-glass, which voids his request.
	asked := e.askBatch(batch, f.as(f.ahkow, authz.Accountant), tag, false)
	r := e.releaseUnder(batch, tag, weiLing)
	if r.Status != http.StatusOK || r.str("status") != "released" || r.str("release", "break_glass_id") != glass || r.Body["release"].(map[string]any)["approval_id"] != nil ||
		!strings.HasPrefix(r.str("number"), "PB-") {
		t.Fatalf("releasePaymentBatchUnderBreakGlass = %d: %s", r.Status, r.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/approvals/"+asked.str("id"), ahKow, nil); a.str("status") != string(approval.Void) {
		t.Errorf("Ah Kow's request: %s", a.Raw)
	}
	g := e.call(http.MethodGet, "/v1/break-glass/"+glass, ahKow, nil)
	if g.str("released", "amount") != "109.00" || g.str("left", "amount") != "41.00" || len(g.Body["releases"].([]any)) != 1 {
		t.Errorf("what it released: %s", g.Raw)
	}

	// What break-glass refuses: an account changed this month, her own claim, more than the cap, and Ah Kow under her break-glass.
	fresh := e.vendor("Fresh Account Pte Ltd")
	e.vendorAccount(fresh, "1112223334")
	b2, t2 := e.draftBatch(acc, "bill:"+e.billOf(fresh, "10.00", "0.90"))
	e.problemIs(e.releaseUnder(b2, t2, weiLing), "releasePaymentBatchUnderBreakGlass", 422, breakglass.CodeNewAccount)
	mine := f.as(f.weiling, authz.Employee)
	id, _ := e.myBankChange(mine, myAccount("5550001234"))
	e.myScreenshot(id, mine)
	e.approveAsAnother(approval.ObjectClaimantBankChange, id, approval.OpClaimantBankChange, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := payee.Approve(ctx, u, uuid.MustParse(id), e.clock.Now())
		return err
	})
	b3, t3 := e.draftBatch(acc, "claim:"+e.postedClaim(uuid.MustParse(f.weiling)))
	e.problemIs(e.releaseUnder(b3, t3, weiLing), "releasePaymentBatchUnderBreakGlass", 422, breakglass.CodeOwn)
	b4, t4 := e.draftBatch(acc, "bill:"+e.billOf(settled, "40.00", "3.60"))
	e.problemIs(e.releaseUnder(b4, t4, weiLing), "releasePaymentBatchUnderBreakGlass", 422, breakglass.CodeCap)
	b5, t5 := e.draftBatch(acc, "bill:"+e.billOf(settled, "1.00", "0.09"))
	e.problemIs(e.releaseUnder(b5, t5, f.as(f.ahkow, authz.PaymentReleaser)), "releasePaymentBatchUnderBreakGlass", 409, breakglass.CodeNotActive)
	if x := e.call(http.MethodGet, "/v1/payment-batches/"+b5, ahKow, nil); x.str("status") != "draft" {
		t.Errorf("a refused release changed the batch: %s", x.Raw)
	}

	// Its review: never by Wei Ling, and only once it has ended. Ah Kow, back, ends it; a new one waits for its review.
	review := func(tok string) response {
		tag := e.call(http.MethodGet, "/v1/break-glass/"+glass, ahKow, nil).Header.Get("ETag")
		return e.call(http.MethodPost, "/v1/break-glass/"+glass+"/review", tok, map[string]any{"note": "Checked the payment against the bill"}, "If-Match", tag)
	}
	e.problemIs(review(weiLing), "reviewBreakGlass", 403, breakglass.CodeReviewer)
	e.problemIs(review(ahKow), "reviewBreakGlass", 409, breakglass.CodeTooEarly)
	end := e.call(http.MethodPost, "/v1/break-glass/"+glass+"/end", ahKow, nil, "If-Match", glassTag)
	if end.Status != http.StatusOK || end.str("status") != "ended" || end.str("ended_by", "id") != f.ahkow {
		t.Fatalf("endBreakGlass = %d: %s", end.Status, end.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/break-glass/"+glass+"/end", ahKow, nil, "If-Match", end.Header.Get("ETag")), "endBreakGlass", 409,
		breakglass.CodeEnded)
	e.problemIs(e.releaseUnder(b5, t5, weiLing), "releasePaymentBatchUnderBreakGlass", 409, breakglass.CodeNotActive)
	e.problemIs(e.call(http.MethodPost, "/v1/break-glass", weiLing, map[string]any{"reason": "Again"}), "startBreakGlass", 409, breakglass.CodeWaiting)

	// The review log lists it with its due date; the generic review sends it to its own.
	log := e.call(http.MethodGet, "/v1/review-log", ahKow, nil)
	var entry map[string]any
	for _, x := range log.Body["items"].([]any) {
		if m := x.(map[string]any); m["kind"] == "break_glass" {
			entry = m
		}
	}
	if entry == nil || entry["object_key"] != glass || entry["due_at"] == nil || entry["overdue"] != false || entry["you_may_review"] != false {
		t.Fatalf("the review log: %s", log.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/review-log/reviews", ahKow, map[string]any{"entry_ids": []string{entry["id"].(string)}}), "reviewLogEntries", 403,
		breakglass.CodeReviewer)

	// The Accountant reviews only from its due date, when it is overdue.
	accountant := e.personAs(e.otherUser(), authz.Accountant)
	e.problemIs(review(accountant), "reviewBreakGlass", 403, breakglass.CodeReviewer)
	e.clock.Set(started.Add(breakglass.ReviewDue + time.Minute))
	if g := e.call(http.MethodGet, "/v1/break-glass/"+glass, ahKow, nil); g.Body["overdue"] != true || g.Body["you_may_review"] != true {
		t.Errorf("due: %s", g.Raw)
	}
	done := review(accountant)
	if done.Status != http.StatusOK || done.Body["review"] == nil || done.Body["overdue"] != false {
		t.Fatalf("the Accountant's review = %d: %s", done.Status, done.Raw)
	}
	e.problemIs(review(ahKow), "reviewBreakGlass", 409, approval.CodeReviewed)
	weiLing = f.as(f.weiling, authz.Owner) // stepped up again, a week later
	if again := e.call(http.MethodPost, "/v1/break-glass", weiLing, map[string]any{"reason": "Ah Kow is abroad again"}); again.Status != http.StatusCreated {
		t.Errorf("a new break-glass once the last is reviewed = %d: %s", again.Status, again.Raw)
	}
}

// clearGlass ends every active break-glass and has another Owner review each not reviewed yet, so an Owner may start one.
func clearGlass(e *env) {
	e.t.Helper()
	owner := steppedUp(e, e.as(authz.Owner))
	reviewer := e.personAs(e.otherUser(), authz.Owner)
	for _, x := range e.call(http.MethodGet, "/v1/break-glass?page_size=500", owner, nil).Body["items"].([]any) {
		g := x.(map[string]any)
		if g["review"] != nil {
			continue
		}
		id := g["id"].(string)
		if g["status"] == "active" {
			if r := e.call(http.MethodPost, "/v1/break-glass/"+id+"/end", owner, nil, "If-Match", etag(int(g["version"].(float64)))); r.Status != http.StatusOK {
				e.t.Fatalf("ending the matrix's break-glass = %d: %s", r.Status, r.Raw)
			}
		}
		tag := e.call(http.MethodGet, "/v1/break-glass/"+id, owner, nil).Header.Get("ETag")
		if r := e.call(http.MethodPost, "/v1/break-glass/"+id+"/review", reviewer, map[string]any{}, "If-Match", tag); r.Status != http.StatusOK {
			e.t.Fatalf("reviewing the matrix's break-glass = %d: %s", r.Status, r.Raw)
		}
	}
}

// matrixGlass is an active break-glass the matrix's member started as an Owner, stepped up, once every earlier one is
// ended and reviewed: the member is who every role's stepped-up call acts as. It returns its ID and ETag.
func matrixGlass(e *env) (string, string) {
	e.t.Helper()
	clearGlass(e)
	s := e.call(http.MethodPost, "/v1/break-glass", steppedUp(e, e.as(authz.Owner)), map[string]any{"reason": "Matrix"})
	if s.Status != http.StatusCreated {
		e.t.Fatalf("starting the matrix's break-glass = %d: %s", s.Status, s.Raw)
	}
	return s.str("id"), s.Header.Get("ETag")
}

// glassCount is how many break-glasses the organisation has.
func glassCount(e *env) int {
	return len(e.call(http.MethodGet, "/v1/break-glass?page_size=500", e.as(authz.Owner), nil).Body["items"].([]any))
}

// matrixBreakGlass has one request per operation on break-glass.
var matrixBreakGlass = map[string]func(e *env, tok string) (response, func() bool){
	"listBreakGlass": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/break-glass", tok, nil), nil
	},
	"getBreakGlass": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixGlass(e)
		return e.call(http.MethodGet, "/v1/break-glass/"+id, tok, nil), nil
	},
	"startBreakGlass": func(e *env, tok string) (response, func() bool) {
		clearGlass(e)
		was := glassCount(e)
		r := e.call(http.MethodPost, "/v1/break-glass", steppedUp(e, tok), map[string]any{"reason": "Matrix"})
		return r, func() bool { return glassCount(e) == was }
	},
	"endBreakGlass": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixGlass(e)
		r := e.call(http.MethodPost, "/v1/break-glass/"+id+"/end", steppedUp(e, tok), nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/break-glass/"+id, e.as(authz.Owner), nil).str("status") == "active"
		}
	},
	// Reviewing is decided past its policy, by the break-glass's own rules: an unknown one is refused past the policy (404).
	"reviewBreakGlass": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/break-glass/"+uuid.NewString()+"/review", steppedUp(e, tok), map[string]any{}, "If-Match", "*"), nil
	},
	"releasePaymentBatchUnderBreakGlass": func(e *env, tok string) (response, func() bool) {
		matrixGlass(e)
		id, tag := e.draftBatch(e.batchAccount(), "bill:"+e.billOf(e.settledVendor("Matrix glass payee "+uuid.NewString()[:8]), "10.00", "0.90"))
		r := e.releaseUnder(id, tag, steppedUp(e, tok))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil).str("status") == "draft"
		}
	},
}

// The database keeps break-glass within its limits, whatever the application asks (FY261, FY262, FY263, FY239, FY151;
// FM-8): a break-glass by a member who is not an Owner, one not logged for review, a second while the first waits for
// its review; a use by another person, before it started or after it expired, above its cap, or of a batch no longer
// waiting; a release under it by another person; an end after it expired or a second end; a review before it ended or
// by its Owner; and any change or deletion, even by the tables' owner, are each refused.
// req: FM-8
// req: QA-ENG-05
// req: access/team-modes Break-glass cannot extend itself
// asvs: V2.3.5, V8.2.2
func TestAttackBreakGlassKeepsItsLimits(t *testing.T) {
	f := newFounders(t)
	e := f.e
	weiLing, ahKow := uuid.MustParse(f.weiling), uuid.MustParse(f.ahkow)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker"}})
	attack := func(name, code string, fn func(ctx context.Context, u storage.UnitOfWork) error) {
		t.Helper()
		err := e.store.Do(ctx, e.books.Org, fn)
		var se *storage.Error
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("%s: want %s, got %v", name, code, err)
		}
	}
	now := e.clock.Now()
	var g storage.BreakGlass
	start := func(by uuid.UUID, logged bool) func(ctx context.Context, u storage.UnitOfWork) error {
		return func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if g, err = u.BreakGlasses().Start(ctx, storage.NewBreakGlass{StartedBy: by, Reason: "Attack", StartedAt: now, Currency: "SGD", Cap: "100.00"}); err != nil || !logged {
				return err
			}
			_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewBreakGlass, ObjectType: ObjectBreakGlass, ObjectKey: g.ID.String(),
				Summary: "Attack", Actor: &by, LoggedAt: now})
			return err
		}
	}
	attack("a member who is not an Owner", storage.CodeBreakGlass, start(e.otherUser(), true))
	attack("a break-glass not logged for review", storage.CodeBreakGlass, start(weiLing, false))
	e.do(start(weiLing, true))
	glass := g
	attack("a second while the first waits for its review", storage.CodeBreakGlass, start(ahKow, true))

	acc := e.batchAccount()
	settled := e.settledVendor("Attacked Glass Pte Ltd")
	small, _ := e.draftBatch(acc, "bill:"+e.billOf(settled, "10.00", "0.90"))
	big, _ := e.draftBatch(acc, "bill:"+e.billOf(settled, "100.00", "9.00"))
	use := func(by uuid.UUID, at time.Time, batch, total string) func(ctx context.Context, u storage.UnitOfWork) error {
		return func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, uuid.MustParse(batch))
			if err != nil {
				return err
			}
			_, err = u.BreakGlasses().Use(ctx, storage.BreakGlassUse{BreakGlassID: glass.ID, BatchID: doc.ID, BatchVersion: doc.Version, UsedBy: by, UsedAt: at,
				Currency: "SGD", Total: storage.Decimal(total)})
			return err
		}
	}
	attack("a use by the other Owner", storage.CodeBreakGlass, use(ahKow, now, small, "10.90"))
	attack("a use before it started", storage.CodeBreakGlass, use(weiLing, now.Add(-time.Second), small, "10.90"))
	attack("a use once it expired", storage.CodeBreakGlass, use(weiLing, now.Add(24*time.Hour), small, "10.90"))
	attack("a use above its cap", storage.CodeBreakGlass, use(weiLing, now, big, "109.00"))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if err := use(weiLing, now, small, "10.90")(ctx, u); err != nil {
			return err
		}
		if _, err := u.Sequences().Number(ctx, uuid.MustParse(small), storage.DocumentPaymentBatch); err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, uuid.MustParse(small), storage.DocumentPosted) // FY242 takes the use as its approval
	})
	attack("a use of a batch no longer waiting", storage.CodeBreakGlass, use(weiLing, now, small, "10.90"))
	attack("a release under it by another person", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentBatches().Release(ctx, storage.PaymentBatchRelease{BatchID: uuid.MustParse(small), BatchVersion: 1, BreakGlassID: glass.ID,
			ReleasedBy: ahKow, ReleasedAt: now, Writer: "payment-list@1", FileName: "PB-0001.csv", MediaType: "text/csv", Sealed: make([]byte, 64),
			SHA256: make([]byte, 32), Payments: 1, Currency: "SGD", Total: "10.90"})
		return err
	})

	// It ends once, inside its window; its review comes after the end, from someone else.
	attack("an end after it expired", storage.CodeInvalid, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BreakGlasses().End(ctx, glass.ID, glass.Version, weiLing, now.Add(25*time.Hour))
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BreakGlasses().End(ctx, glass.ID, glass.Version, ahKow, now.Add(time.Hour))
		return err
	})
	attack("a second end", storage.CodeVersion, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.BreakGlasses().End(ctx, glass.ID, glass.Version+1, ahKow, now.Add(2*time.Hour))
		return err
	})
	var entry uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.ReviewLog().List(ctx)
		for _, x := range es {
			if x.Kind == storage.ReviewBreakGlass && x.ObjectKey == glass.ID.String() {
				entry = x.ID
			}
		}
		return err
	})
	attack("a review before it ended", storage.CodeApproval, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.ReviewLog().Review(ctx, entry, ahKow, now.Add(30*time.Minute), "")
	})
	attack("a review by its Owner", storage.CodeApproval, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.ReviewLog().Review(ctx, entry, weiLing, now.Add(2*time.Hour), "")
	})

	// Never changed or deleted: the API role holds no grant to, and the tables' owner is refused by the guards.
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing a reason", "UPDATE fyniq.break_glass SET reason = 'x', version = version + 1", "42501"},
		{"fyniq_api", "the API deleting a use", "DELETE FROM fyniq.break_glass_use", "42501"},
		{"fyniq", "the owner extending one", "UPDATE fyniq.break_glass SET expires_at = expires_at + interval '1 day', version = version + 1", "FY262"},
		{"fyniq", "the owner deleting one", "DELETE FROM fyniq.break_glass", "FY262"},
		{"fyniq", "the owner raising a cap", "UPDATE fyniq.break_glass SET cap = cap * 10, version = version + 1", "FY262"},
		{"fyniq", "the owner changing a use", "UPDATE fyniq.break_glass_use SET total = total - 1", "FY263"},
		{"fyniq", "the owner deleting a use", "DELETE FROM fyniq.break_glass_use", "FY263"},
		{"fyniq", "the owner emptying them", "TRUNCATE fyniq.break_glass_use", "FY263"},
	} {
		conn, err := pgx.Connect(context.Background(), db.URL(c.role))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := conn.Begin(context.Background())
		if err == nil {
			if _, err = tx.Exec(context.Background(), "SELECT set_config('app.org_id', $1, true)", e.books.Org.String()); err == nil {
				_, err = tx.Exec(context.Background(), c.stmt)
			}
			_ = tx.Rollback(context.Background())
		}
		conn.Close(context.Background())
		if err == nil || !strings.Contains(err.Error(), c.state) {
			t.Errorf("%s: want %s, got %v", c.what, c.state, err)
		}
	}
}
