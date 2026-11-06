//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/succession"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// makeSuperAdmin makes user one of the installation's Super admins, as the setup or an accepted invitation would.
func makeSuperAdmin(t *testing.T, user string) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The shared database has no installation trail to audit it in, so the trigger that would audit it is off.
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO fyniq.super_admin (user_id) VALUES ($1) ON CONFLICT DO NOTHING", user); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// Owner succession (FR-ADM-09; PRD-001 rev 0.181, the owner's decisions of 6 Nov 2026). Ann, Ben and Cat are founders
// and Owners; Ann is a Super admin; Dan, an Accountant, joined later. Ann starts a succession of Ben, permanently
// unavailable, naming Dan to become an Owner: refused for herself, for Dan (no Owner), with Ben as his own successor or
// without step-up; Ben is told at once. A second one for Ben waits behind it. Ben objects, which ends it; Ann starts it
// again and withdraws it; then once more. Before its 7 days a Super admin cannot complete it, nor without a file;
// Ben attaches none to his own; Cat, an Owner but no Super admin, cannot complete it. Ann attaches the board
// resolution, and after 7 days completes it: Dan is an Owner, Ben no longer a member, and every Owner was told.
// req: FR-ADM-09
// req: access/users-and-sign-in Owner succession (rev 0.181)
// asvs: V8.2.2, V8.3.1
func TestAnOwnerIsSucceededAfterSevenDaysNotice(t *testing.T) {
	f := newTrio(t)
	e := f.e
	members := e.call(http.MethodGet, "/v1/members", f.as(f.ann), nil)
	memberID := func(user string) string {
		for _, x := range members.Body["items"].([]any) {
			if m := x.(map[string]any); m["user_id"] == user {
				return m["id"].(string)
			}
		}
		t.Fatalf("no member for %s: %s", user, members.Raw)
		return ""
	}
	ben := memberID(f.ben)
	dan := e.joinedIn(e.books.Org, "Dan", "accountant")
	members = e.call(http.MethodGet, "/v1/members", f.as(f.ann), nil)
	danMember := memberID(dan.String())
	makeSuperAdmin(t, f.ann)
	// Ann's own sign-in, so the installation knows her as its Super admin.
	ann := func() string {
		return e.principal(authn.Principal{Subject: "Ann", UserID: uuid.MustParse(f.ann), Client: "web", Class: authn.ClassUser,
			Organisation: e.books.Org, Roles: []authz.Role{authz.Owner, authz.Controller, authz.Administrator}, StepUpAt: e.clock.Now()})
	}
	start := func(who, member, successor string) response {
		body := map[string]any{"reason": "Ben left on 31 October; the board resolved on his removal", "evidence_kind": "board_resolution"}
		if successor != "" {
			body["successor_member_id"] = successor
		}
		return e.call(http.MethodPost, "/v1/members/"+member+"/succession", who, body)
	}
	e.problemIs(start(ann(), memberID(f.ann), ""), "startOwnerSuccession", 409, succession.CodeNotOwner)
	e.problemIs(start(ann(), danMember, ""), "startOwnerSuccession", 409, succession.CodeNotOwner)
	e.problemIs(start(ann(), ben, ben), "startOwnerSuccession", 422, succession.CodeSuccessor)
	stale := e.principal(authn.Principal{Subject: "Ann", UserID: uuid.MustParse(f.ann), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-time.Hour)})
	e.problemIs(start(stale, ben, ""), "startOwnerSuccession", 403, CodeStepUpRequired)
	e.problemIs(start(e.as(authz.Controller), ben, ""), "startOwnerSuccession", 403, CodeForbidden)

	s := start(ann(), ben, danMember)
	if s.Status != http.StatusCreated || s.str("status") != "notice" || s.str("successor", "user_id") != dan.String() ||
		s.str("due_at") != e.clock.Now().Add(succession.Wait).UTC().Format(time.RFC3339) || s.Body["you_may_withdraw"] != true || s.Body["files"] != 0.0 {
		t.Fatalf("starting it: %d %s", s.Status, s.Raw)
	}
	e.problemIs(start(f.as(f.cat), ben, ""), "startOwnerSuccession", 409, storage.CodeSuccession)
	fanOutAll(t, e)
	if n := e.call(http.MethodGet, "/v1/notifications", f.as(f.ben), nil); !strings.Contains(n.Raw, `"type":"security.owners"`) {
		t.Errorf("Ben was not told: %s", n.Raw)
	}
	path := func(r response, op string) string { return "/v1/owner-successions/" + r.str("id") + "/" + op }
	act := func(who string, r response, op string) response {
		return e.call(http.MethodPost, path(r, op), who, nil, "If-Match", r.Header.Get("ETag"))
	}

	// Ben objects: it ends.
	e.problemIs(act(f.as(f.cat), s, "object"), "objectToOwnerSuccession", 403, succession.CodeNotYours)
	if o := act(f.as(f.ben), s, "object"); o.Status != 200 || o.str("status") != "objected" || o.str("ended_by", "id") != f.ben {
		t.Fatalf("Ben objecting: %d %s", o.Status, o.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path(s, "withdraw"), ann(), nil, "If-Match", `"2"`), "withdrawOwnerSuccession", 409, succession.CodeEnded)

	// Started again, then withdrawn by Ann alone.
	s = start(ann(), ben, danMember)
	e.problemIs(act(f.as(f.ben), s, "withdraw"), "withdrawOwnerSuccession", 403, succession.CodeNotYours)
	if w := act(ann(), s, "withdraw"); w.Status != 200 || w.str("status") != "withdrawn" {
		t.Fatalf("Ann withdrawing: %d %s", w.Status, w.Raw)
	}

	// Once more, completed after 7 days with the board resolution.
	s = start(ann(), ben, danMember)
	e.problemIs(act(ann(), s, "complete"), "completeOwnerSuccession", 409, succession.CodeNotDue)
	resolution := []byte("%PDF-1.7 the board's resolution on Ben's removal")
	e.problemIs(e.upload(path(s, "evidence"), f.as(f.ben), map[string]string{"evidence_type": "board resolution"}, "resolution.pdf", resolution,
		"If-Match", s.Header.Get("ETag")), "addOwnerSuccessionEvidence", 403, succession.CodeNotYours)
	e.clock.Advance(succession.Wait)
	e.problemIs(act(ann(), s, "complete"), "completeOwnerSuccession", 409, succession.CodeNoFile)
	if up := e.upload(path(s, "evidence"), ann(), map[string]string{"evidence_type": "board resolution"}, "resolution.pdf", resolution,
		"If-Match", s.Header.Get("ETag")); up.Status != http.StatusCreated {
		t.Fatalf("attaching the resolution: %d %s", up.Status, up.Raw)
	}
	if l := e.call(http.MethodGet, path(s, "evidence"), f.as(f.cat), nil); len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the evidence: %s", l.Raw)
	}
	e.problemIs(act(f.as(f.cat), s, "complete"), "completeOwnerSuccession", 403, CodeNotSuperAdmin)
	l := e.call(http.MethodGet, "/v1/owner-successions", ann(), nil)
	if items := l.Body["items"].([]any); len(items) != 3 || items[0].(map[string]any)["you_may_complete"] != true || items[0].(map[string]any)["files"] != 1.0 {
		t.Fatalf("the successions: %s", l.Raw)
	}
	c := act(ann(), s, "complete")
	if c.Status != 200 || c.str("status") != "completed" || c.str("ended_by", "id") != f.ann {
		t.Fatalf("completing it: %d %s", c.Status, c.Raw)
	}
	after := e.call(http.MethodGet, "/v1/members", ann(), nil).Raw
	if strings.Contains(after, `"user_id":"`+f.ben+`"`) || !strings.Contains(after, `"display_name":"Dan","roles":[{"role":"accountant"`) ||
		!strings.Contains(after, `{"role":"owner"`) {
		t.Fatalf("the members after it: %s", after)
	}
	fanOutAll(t, e)
	if n := e.call(http.MethodGet, "/v1/notifications", f.as(f.cat), nil); !strings.Contains(n.Raw, "The succession completed") ||
		!strings.Contains(n.Raw, "Dan became an Owner") {
		t.Errorf("Cat was not told: %s", n.Raw)
	}
}

// succeedable is a succession waiting in the organisation in Standard mode: Bo, its Owner, started it for a new Owner
// who joined for it; it returns the succession and Bo's and the outgoing Owner's users.
func (e *env) succeedable() (storage.OwnerSuccession, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	s := inStandardMode(e)
	bo := e.standardBo()
	out := s.joinedIn(s.books.Org, "Outgoing "+uuid.NewString()[:8], "owner")
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: bo.String()}})
	var got storage.OwnerSuccession
	if err := s.store.Do(ctx, s.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Members().Membership(ctx, out)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(map[string]string{"outgoing_membership_id": m.ID.String()})
		d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOwnerSuccession, Payload: body})
		if err != nil {
			return err
		}
		got, err = u.OwnerSuccessions().Start(ctx, storage.NewOwnerSuccession{DocumentID: d.ID, OutgoingMembershipID: m.ID, OutgoingUserID: out,
			Reason: "Matrix test", EvidenceKind: succession.BoardResolution, StartedBy: bo, StartedAt: s.clock.Now()})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return got, bo, out
}

// standardBo is Bo's user: the Owner of the organisation in Standard mode.
func (e *env) standardBo() uuid.UUID {
	e.t.Helper()
	s := inStandardMode(e)
	for _, x := range s.call(http.MethodGet, "/v1/members?page_size=500", s.as(authz.Administrator), nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["display_name"] == "Bo" {
			return uuid.MustParse(m["user_id"].(string))
		}
	}
	e.t.Fatal("no Bo in the organisation in Standard mode")
	return uuid.Nil
}

// asIn is tok's person as user in the organisation in Standard mode, stepped up: a role's own token, with the user
// a row needs.
func (e *env) asIn(tok string, user uuid.UUID) string {
	s := inStandardMode(e)
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	if p.Organisation != e.books.Org || p.Class != authn.ClassUser {
		return tok
	}
	p.Organisation, p.UserID, p.StepUpAt = s.books.Org, user, s.clock.Now()
	return s.principal(p)
}

// matrixSuccession are the matrix's rows of Owner succession (QA-SEC-03): each in the organisation in Standard mode,
// as a preview, as the person the row needs (its starter, its outgoing Owner), so an allowed role is answered.
var matrixSuccession = map[string]func(e *env, tok string) (response, func() bool){
	"searchEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/evidence", tok, nil), nil
	},
	"listOwnerSuccessions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/owner-successions", tok, nil), nil
	},
	"startOwnerSuccession": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		bo := e.standardBo()
		next := s.joinedIn(s.books.Org, "Next "+uuid.NewString()[:8], "owner")
		var m storage.Membership
		if err := s.store.Do(context.Background(), s.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			m, err = u.Members().Membership(ctx, next)
			return err
		}); err != nil {
			e.t.Fatal(err)
		}
		r := s.call(http.MethodPost, "/v1/members/"+m.ID.String()+"/succession", e.asIn(tok, bo), map[string]any{"reason": "Matrix test",
			"evidence_kind": "board_resolution"}, "Prefer", "return=preview")
		return r, func() bool { return !strings.Contains(s.call(http.MethodGet, "/v1/owner-successions", s.as(authz.Owner), nil).Raw, next.String()) }
	},
	"addOwnerSuccessionEvidence": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		x, bo, _ := e.succeedable()
		r := s.upload("/v1/owner-successions/"+x.ID.String()+"/evidence", e.asIn(tok, bo), map[string]string{"evidence_type": "board resolution"},
			"resolution.pdf", []byte("%PDF-1.7 matrix"), "If-Match", etag(x.Version), "Prefer", "return=preview")
		return r, nil
	},
	"listOwnerSuccessionEvidence": func(e *env, tok string) (response, func() bool) {
		x, bo, _ := e.succeedable()
		return inStandardMode(e).call(http.MethodGet, "/v1/owner-successions/"+x.ID.String()+"/evidence", e.asIn(tok, bo), nil), nil
	},
	"objectToOwnerSuccession": func(e *env, tok string) (response, func() bool) {
		x, _, out := e.succeedable()
		s := inStandardMode(e)
		r := s.call(http.MethodPost, "/v1/owner-successions/"+x.ID.String()+"/object", e.asIn(tok, out), nil, "If-Match", etag(x.Version), "Prefer", "return=preview")
		return r, func() bool { return successionStatus(s, x.ID) == storage.SuccessionNotice }
	},
	"withdrawOwnerSuccession": func(e *env, tok string) (response, func() bool) {
		x, bo, _ := e.succeedable()
		s := inStandardMode(e)
		r := s.call(http.MethodPost, "/v1/owner-successions/"+x.ID.String()+"/withdraw", e.asIn(tok, bo), nil, "If-Match", etag(x.Version), "Prefer", "return=preview")
		return r, func() bool { return successionStatus(s, x.ID) == storage.SuccessionNotice }
	},
	"completeOwnerSuccession": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/owner-successions/"+uuid.NewString()+"/complete", tok, nil, "If-Match", `"1"`, "Prefer", "return=preview"), nil
	},
}

// successionStatus is a succession's status now.
func successionStatus(e *env, id uuid.UUID) string {
	var s storage.OwnerSuccession
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		s, err = u.OwnerSuccessions().Get(ctx, id)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return s.Status
}
