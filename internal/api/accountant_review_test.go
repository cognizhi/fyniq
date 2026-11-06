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
	"github.com/cognizhi/fyniq/internal/domain/accountantreview"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// closeMonth closes a period on two people's acts, as the close test does: checks as one, the attestation as the other.
func (f founders) closeMonth(id string) response {
	f.e.t.Helper()
	e, ahkow, weiling := f.e, f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	tag := func() string {
		return e.call(http.MethodGet, "/v1/periods/"+id+"/close", ahkow, nil).Header.Get("ETag")
	}
	checklistDone(e.t, e.store, e.books.Org, uuid.MustParse(id))
	c := e.call(http.MethodPost, "/v1/periods/"+id+"/close-checks", ahkow, nil, "If-Match", tag())
	r := e.call(http.MethodPost, "/v1/periods/"+id+"/close", weiling, map[string]any{"checks_id": c.str("checks", "id"), "statement": "Reviewed."}, "If-Match", tag())
	if r.Status != http.StatusOK {
		e.t.Fatalf("closing %s: %d %s", id, r.Status, r.Raw)
	}
	return r
}

// The accountant's quarterly review of a closed month (FM-7, G4-109; PRD-001 rev 0.176, the owner's decisions of 6 Nov
// 2026). Grace, the Accountant, opens the review of October once it is closed, never of a month still open nor twice of
// the same close; she raises two queries and links one adjusting journal; she cannot sign off until an Owner and the
// Controller have answered each, once, which she cannot do herself. Signed off, it changes no more. October then
// reopens on another founder's approval: the review is superseded and kept, and October closed again is reviewed afresh.
// req: FM-7
// req: G4-109
// req: AUD-04
// req: access/team-modes Review record per period
// req: access/team-modes One closed month per review
// req: access/team-modes Adjusting journals
// req: access/team-modes Reopening a reviewed period
// req: access/team-modes G4-109 Quarterly review with queries and an adjusting journal
// req: access/roles-and-segregation Permission matrix: Accountant review
func TestTheAccountantReviewsAClosedMonth(t *testing.T) {
	f := newFounders(t)
	e := f.e
	grace := e.personAs(e.otherUser(), authz.Accountant)
	ahkow, weiling := f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	periodTag := func(id string) string {
		return e.call(http.MethodGet, "/v1/periods/"+id+"/close", ahkow, nil).Header.Get("ETag")
	}
	open := func(who, id string) response {
		return e.call(http.MethodPost, "/v1/periods/"+id+"/accountant-review", who, nil, "If-Match", periodTag(id))
	}

	e.postedJournal("100.00")
	closed := f.closeMonth(oct)
	l := e.call(http.MethodGet, "/v1/accountant-reviews", grace, nil)
	if l.Body["you_may_open"] != true || !strings.Contains(l.Raw, `"to_review":[{"id":"`+oct+`"`) {
		t.Fatalf("before any review: %s", l.Raw)
	}
	e.problemIs(open(grace, nov), "openAccountantReview", 409, accountantreview.CodeNotClosed)
	e.problemIs(open(weiling, oct), "openAccountantReview", 403, CodeForbidden)
	r := open(grace, oct)
	if r.Status != http.StatusCreated || r.str("status") != "in_review" || r.str("period", "name") != "October 2026" ||
		r.str("close", "snapshot_sha256") != closed.str("close", "snapshot_sha256") || r.Body["you_may_ask"] != true || r.Body["you_may_sign_off"] != true {
		t.Fatalf("openAccountantReview = %d: %s", r.Status, r.Raw)
	}
	review, tag := r.str("id"), r.Header.Get("ETag")
	e.problemIs(open(grace, oct), "openAccountantReview", 409, accountantreview.CodeReviewed)
	path := "/v1/accountant-reviews/" + review

	// Two queries and one adjusting journal (G4-109).
	ask := func(q string) {
		t.Helper()
		x := e.call(http.MethodPost, path+"/queries", grace, map[string]any{"question": q}, "If-Match", tag)
		if x.Status != http.StatusOK {
			t.Fatalf("asking %q: %d %s", q, x.Status, x.Raw)
		}
		tag = x.Header.Get("ETag")
	}
	ask("Why is the October rent on 6010 twice?")
	ask("Is the deposit to the landlord refundable?")
	e.problemIs(e.call(http.MethodPost, path+"/queries", grace, map[string]any{"question": "Stale"}, "If-Match", `"1"`), "askAccountantReviewQuery", 412, CodeStale)
	journal, _ := e.createDraft("250.00")
	e.problemIs(e.call(http.MethodPost, path+"/journals", grace, map[string]any{"journal_id": e.books.Bank.ID.String()}, "If-Match", tag), "linkAccountantReviewJournal",
		422, accountantreview.CodeNotJournal)
	x := e.call(http.MethodPost, path+"/journals", grace, map[string]any{"journal_id": journal}, "If-Match", tag)
	if js, _ := x.Body["journals"].([]any); x.Status != http.StatusOK || len(js) != 1 || js[0].(map[string]any)["status"] != "draft" {
		t.Fatalf("linking the journal: %d %s", x.Status, x.Raw)
	}
	tag = x.Header.Get("ETag")
	sign := func(who string) response {
		return e.call(http.MethodPost, path+"/sign-off", who, map[string]any{"statement": "Reviewed the October close"}, "If-Match", tag)
	}
	e.problemIs(sign(grace), "signOffAccountantReview", 409, accountantreview.CodeOpenQueries)

	// Management answers each query, once; the Accountant never answers her own.
	g := e.call(http.MethodGet, path, weiling, nil)
	qs := g.Body["queries"].([]any)
	if g.Body["unanswered"] != float64(2) || g.Body["you_may_answer"] != true || g.Body["you_may_ask"] != false {
		t.Fatalf("the review as Wei Ling sees it: %s", g.Raw)
	}
	answerQ := func(who string, i int, text string) response {
		return e.call(http.MethodPost, path+"/queries/"+qs[i].(map[string]any)["id"].(string)+"/answer", who, map[string]any{"answer": text}, "If-Match", tag)
	}
	e.problemIs(answerQ(grace, 0, "My own answer"), "answerAccountantReviewQuery", 403, CodeForbidden)
	a1 := answerQ(weiling, 0, "The October rent was paid early; JNL moves it to prepayments")
	if a1.Status != http.StatusOK {
		t.Fatalf("Wei Ling answering: %d %s", a1.Status, a1.Raw)
	}
	tag = a1.Header.Get("ETag")
	e.problemIs(answerQ(ahkow, 0, "Again"), "answerAccountantReviewQuery", 409, accountantreview.CodeAnswered)
	a2 := answerQ(ahkow, 1, "Yes, at the lease's end")
	if a2.Status != http.StatusOK || a2.Body["unanswered"] != float64(0) {
		t.Fatalf("Ah Kow answering: %d %s", a2.Status, a2.Raw)
	}
	tag = a2.Header.Get("ETag")

	// Signed off, stepped up, it changes no more.
	e.problemIs(sign(e.as(authz.Accountant)), "signOffAccountantReview", 403, CodeStepUpRequired)
	s := sign(grace)
	if s.Status != http.StatusOK || s.str("status") != "signed_off" || s.str("statement") != "Reviewed the October close" || s.Body["you_may_ask"] != false {
		t.Fatalf("signOffAccountantReview = %d: %s", s.Status, s.Raw)
	}
	tag = s.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, path+"/queries", grace, map[string]any{"question": "One more"}, "If-Match", tag), "askAccountantReviewQuery", 409,
		accountantreview.CodeNotInReview)

	// October reopens on Wei Ling's approval: the review is superseded and kept; closed again, it is reviewed afresh.
	asked := e.call(http.MethodPost, "/v1/periods/"+oct+"/reopen", ahkow, map[string]any{"reason": "The accountant's adjustment"}, "If-Match", periodTag(oct))
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("reopening October: %d %s", d.Status, d.Raw)
	}
	if g := e.call(http.MethodGet, path, grace, nil); g.str("status") != "superseded" || g.Body["superseded_at"] == nil || g.str("reopen_id") == "" ||
		g.str("statement") != "Reviewed the October close" {
		t.Fatalf("after the reopening: %s", g.Raw)
	}
	f.closeMonth(oct)
	again := open(grace, oct)
	if again.Status != http.StatusCreated || again.str("id") == review {
		t.Fatalf("reviewing October afresh: %d %s", again.Status, again.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/accountant-reviews", e.as(authz.Auditor), nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("both reviews are not kept: %s", l.Raw)
	}
}

// The database keeps the accountant's review to its rules, whatever the application asks (FY264, FY265, FY266; FM-7):
// a review of a month still open, of an older close or twice of one close; a query after the sign-off, a second answer,
// a sign-off while a query waits; a link to a document that is not a journal; a reopening that leaves the review live;
// and any change or deletion by the tables' owner are each refused.
// req: FM-7
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackAnAccountantReviewKeepsItsRules(t *testing.T) {
	f := newFounders(t)
	e := f.e
	grace := e.otherUser()
	weiling := uuid.MustParse(f.weiling)
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
	f.closeMonth(e.books.Oct.ID.String())
	var close storage.PeriodClose
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		close, err = u.Closes().Latest(ctx, e.books.Oct.ID)
		return err
	})
	attack("a review of a month still open", storage.CodeAccountantReview, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().Open(ctx, e.books.Nov.ID, close.ID, grace, now)
		return err
	})
	var r storage.AccountantReview
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if r, err = u.AccountantReviews().Open(ctx, e.books.Oct.ID, close.ID, grace, now); err != nil {
			return err
		}
		r, err = u.AccountantReviews().Ask(ctx, r.ID, r.Version, grace, now, "Why?")
		return err
	})
	attack("a second review of one close", storage.CodeInvalid, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().Open(ctx, e.books.Oct.ID, close.ID, grace, now)
		return err
	})
	attack("a sign-off while a query waits", storage.CodeAccountantReview, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().SignOff(ctx, r.ID, r.Version, grace, now, "Done")
		return err
	})
	attack("a link to a document that is not a journal", storage.CodeAccountantReview, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().Link(ctx, r.ID, r.Version, close.ID, grace, now)
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = u.AccountantReviews().Answer(ctx, r.ID, r.Version, r.Queries[0].ID, weiling, now, "Because")
		return err
	})
	attack("a second answer", storage.CodeAccountantReview, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().Answer(ctx, r.ID, r.Version, r.Queries[0].ID, weiling, now, "Again")
		return err
	})
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = u.AccountantReviews().SignOff(ctx, r.ID, r.Version, grace, now, "Done")
		return err
	})
	attack("a query after the sign-off", storage.CodeVersion, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountantReviews().Ask(ctx, r.ID, r.Version, grace, now, "More?")
		return err
	})

	// Never changed or deleted: the API role holds no grant to, and the tables' owner is refused by the guards.
	for _, c := range []struct{ role, what, stmt, state string }{
		{"fyniq_api", "the API changing what a review covers", "UPDATE fyniq.accountant_review SET period_id = period_id, version = version + 1", "42501"},
		{"fyniq_api", "the API deleting a query", "DELETE FROM fyniq.accountant_review_query", "42501"},
		{"fyniq_api", "the API reopening a review", "UPDATE fyniq.accountant_review SET status = 'in_review', signed_by = NULL, signed_at = NULL, statement = NULL, version = version + 1", "FY264"},
		{"fyniq", "the owner deleting a review", "DELETE FROM fyniq.accountant_review", "FY264"},
		{"fyniq", "the owner changing a question", "UPDATE fyniq.accountant_review_query SET question = 'x'", "FY265"},
		{"fyniq", "the owner unlinking a journal", "TRUNCATE fyniq.accountant_review_journal", "FY266"},
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

// matrixMonth closes the next spare month from January 2020 on two actors' acts, through the close service, and
// returns it: the matrix's other rows post into October and November, which stay open.
func matrixMonth(e *env) storage.Period {
	e.t.Helper()
	start := time.Date(2020, time.Month(1+e.spareMonths), 1, 0, 0, 0, 0, time.UTC)
	e.spareMonths++
	var p storage.Period
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		p, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: start, EndsOn: start.AddDate(0, 1, -1)})
		return err
	})
	checklistDone(e.t, e.store, e.books.Org, p.ID)
	as := func(who string) context.Context {
		return storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	svc := closing.Service{Store: e.store}
	c, err := svc.RunChecks(as("matrix-preparer"), e.books.Org, p.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := svc.Close(as("matrix-attester"), e.books.Org, p.ID, c.ID, "Reviewed."); err != nil {
		e.t.Fatalf("closing the matrix's %s: %v", start.Format("Jan 2006"), err)
	}
	return p
}

// matrixReviewOf is a review in review of a fresh spare month, opened by another person, with an unanswered query when
// asked is true.
func matrixReviewOf(e *env, asked bool) uuid.UUID {
	e.t.Helper()
	p := matrixMonth(e)
	by := e.otherUser()
	var r storage.AccountantReview
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err := u.Closes().Latest(ctx, p.ID)
		if err != nil {
			return err
		}
		if r, err = u.AccountantReviews().Open(ctx, p.ID, c.ID, by, e.clock.Now()); err != nil || !asked {
			return err
		}
		_, err = u.AccountantReviews().Ask(ctx, r.ID, r.Version, by, e.clock.Now(), "Matrix")
		return err
	})
	return r.ID
}

// matrixReview is the matrix's review in review, made again once a call ends it.
func matrixReview(e *env) (string, string) {
	e.t.Helper()
	g := e.call(http.MethodGet, "/v1/accountant-reviews/"+e.review.String(), e.as(authz.Accountant), nil)
	if e.review == uuid.Nil || g.str("status") != "in_review" {
		e.review = matrixReviewOf(e, false)
		g = e.call(http.MethodGet, "/v1/accountant-reviews/"+e.review.String(), e.as(authz.Accountant), nil)
	}
	return e.review.String(), g.Header.Get("ETag")
}

// matrixAccountantReview has one request per operation on the accountant's reviews.
var matrixAccountantReview = map[string]func(e *env, tok string) (response, func() bool){
	"listAccountantReviews": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/accountant-reviews", tok, nil), nil
	},
	"getAccountantReview": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixReview(e)
		return e.call(http.MethodGet, "/v1/accountant-reviews/"+id, tok, nil), nil
	},
	"openAccountantReview": func(e *env, tok string) (response, func() bool) {
		p := matrixMonth(e)
		tag := e.call(http.MethodGet, "/v1/periods/"+p.ID.String()+"/close", e.as(authz.Controller), nil).Header.Get("ETag")
		count := func() int {
			return len(e.call(http.MethodGet, "/v1/accountant-reviews?page_size=500", e.as(authz.Auditor), nil).Body["items"].([]any))
		}
		was := count()
		r := e.call(http.MethodPost, "/v1/periods/"+p.ID.String()+"/accountant-review", steppedUp(e, tok), nil, "If-Match", tag)
		return r, func() bool { return count() == was }
	},
	"askAccountantReviewQuery": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixReview(e)
		r := e.call(http.MethodPost, "/v1/accountant-reviews/"+id+"/queries", steppedUp(e, tok), map[string]any{"question": "Matrix"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/accountant-reviews/"+id, e.as(authz.Auditor), nil).Header.Get("ETag") == tag
		}
	},
	"answerAccountantReviewQuery": func(e *env, tok string) (response, func() bool) {
		id := matrixReviewOf(e, true).String()
		g := e.call(http.MethodGet, "/v1/accountant-reviews/"+id, e.as(authz.Auditor), nil)
		q := g.Body["queries"].([]any)[0].(map[string]any)["id"].(string)
		r := e.call(http.MethodPost, "/v1/accountant-reviews/"+id+"/queries/"+q+"/answer", steppedUp(e, tok), map[string]any{"answer": "Matrix"},
			"If-Match", g.Header.Get("ETag"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/accountant-reviews/"+id, e.as(authz.Auditor), nil).Body["unanswered"] == float64(1)
		}
	},
	"linkAccountantReviewJournal": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixReview(e)
		journal, _ := e.createDraft("1.00")
		r := e.call(http.MethodPost, "/v1/accountant-reviews/"+id+"/journals", steppedUp(e, tok), map[string]any{"journal_id": journal}, "If-Match", tag)
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/accountant-reviews/"+id, e.as(authz.Auditor), nil).Raw, journal)
		}
	},
	"signOffAccountantReview": func(e *env, tok string) (response, func() bool) {
		g := e.call(http.MethodGet, "/v1/accountant-reviews/"+e.signable.String(), e.as(authz.Auditor), nil)
		if e.signable == uuid.Nil || g.str("status") != "in_review" {
			e.signable = matrixReviewOf(e, false)
			g = e.call(http.MethodGet, "/v1/accountant-reviews/"+e.signable.String(), e.as(authz.Auditor), nil)
		}
		id := e.signable.String()
		r := e.call(http.MethodPost, "/v1/accountant-reviews/"+id+"/sign-off", steppedUp(e, tok), map[string]any{"statement": "Matrix"}, "If-Match", g.Header.Get("ETag"))
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/accountant-reviews/"+id, e.as(authz.Auditor), nil).str("status") == "in_review"
		}
	},
}
