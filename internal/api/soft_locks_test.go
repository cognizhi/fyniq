//go:build local

package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Soft locks (FR-GL-09): the Controller soft-locks October, and from then on a bill dated in it is refused for everyone, a Bookkeeper and the Accountant without a pass included; the
// Accountant makes a pass with a reason (and nobody else can: not an Owner or a Controller, not an integration or the AI); the bill posts under it and the exceptions report names it;
// releasing the lock needs the approving right and a reason; a module lock refuses that module's documents and leaves the others; the hard close is not changed.
// req: FR-GL-09
// asvs: V8.2.2, V4.2.1, V16.3.3
func TestAttackSoftLocksRefuseEveryPostingButAnAccountantsPassAndListIt(t *testing.T) {
	f := newTrio(t)
	e := f.e
	ctl := f.as(f.ann)                                      // Owner, Controller and Administrator: may lock and release
	acc := f.as(f.ben, authz.Accountant)                    // the Accountant: may lock, makes the pass
	other := f.as(f.ann, authz.Accountant)                  // another Accountant, who made no pass
	book := f.as(f.cat, authz.Bookkeeper, authz.Accountant) // a Bookkeeper who also holds the Accountant role is still only a person with no pass
	period := func() (id, tag, status string) {
		for _, it := range e.call(http.MethodGet, "/v1/periods?page_size=100", ctl, nil).Body["items"].([]any) {
			if p := it.(map[string]any); p["starts_on"] == "2026-10-01" {
				return p["id"].(string), fmt.Sprintf(`"%d-%s"`, int(p["version"].(float64)), p["status"].(string)), p["status"].(string)
			}
		}
		t.Fatal("no October period")
		return
	}
	pid, tag, _ := period()
	key := func() []string { return []string{"Idempotency-Key", uuid.NewString()} }
	postBill := func(who, net string) response {
		t.Helper()
		d := e.call(http.MethodPost, "/v1/bills", who, e.billDraft(net, "0.00"))
		if d.Status != http.StatusCreated {
			t.Fatalf("createBill %s = %d: %s", net, d.Status, d.Raw)
		}
		return e.call(http.MethodPost, "/v1/bills/"+d.str("id")+"/post", who, nil, "If-Match", d.Header.Get("ETag"))
	}

	// Locking: a person's, with If-Match; a preview locks nothing; a Bookkeeper, the AI and an integration cannot.
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", ctl, map[string]any{}, append(key(), "If-Match", `"0-open"`)...), "softLockPeriod", 412, CodeStale)
	if pv := e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", ctl, map[string]any{}, "If-Match", tag, "Prefer", "return=preview"); pv.Status != 200 || pv.Header.Get("Preference-Applied") == "" {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if _, _, st := period(); st != "open" {
		t.Fatalf("a preview locked the period: %s", st)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", e.as(authz.Bookkeeper), map[string]any{}, append(key(), "If-Match", tag)...), "softLockPeriod", 403, CodeForbidden)
	ai := e.principal(authn.Principal{Client: "agent", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner, authz.Controller}})
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", ai, map[string]any{}, append(key(), "If-Match", tag)...), "softLockPeriod", 403, CodeForbidden)
	l := e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", ctl, map[string]any{"reason": "The October accounts are with the auditors."}, append(key(), "If-Match", tag)...)
	if l.Status != 200 || l.str("status") != "soft_locked" {
		t.Fatalf("soft-locking: %d %s", l.Status, l.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock", ctl, map[string]any{}, append(key(), "If-Match", l.Header.Get("ETag"))...), "softLockPeriod", 409, storage.CodePeriodState)

	// Nobody posts into it without a pass.
	e.problemIs(postBill(acc, "100.00"), "postBill", 409, storage.CodeSoftLocked)
	e.problemIs(postBill(book, "101.00"), "postBill", 409, storage.CodeSoftLocked)
	e.problemIs(postBill(other, "102.00"), "postBill", 409, storage.CodeSoftLocked)

	// The pass: only the Accountant role, with a reason and a second factor, and not for a closed period.
	pass := func(who string, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock-passes", who, body, key()...)
	}
	good := map[string]any{"reason": "A late supplier bill dated 14 Oct that the auditor asked to see.", "hours": 4}
	e.problemIs(pass(ctl, good), "makeSoftLockPass", 403, CodePassNeedsAccountant) // an Owner and Controller hold the right to lock, never the Accountant's pass
	e.problemIs(pass(acc, map[string]any{"reason": "short"}), "makeSoftLockPass", 422, CodeInvalid)
	e.problemIs(pass(acc, map[string]any{"reason": "          ", "hours": 4}), "makeSoftLockPass", 422, CodeLockReason)
	e.problemIs(pass(acc, map[string]any{"reason": good["reason"], "hours": 9}), "makeSoftLockPass", 422, CodeInvalid)
	weak := e.principal(authn.Principal{Subject: "x", UserID: uuid.MustParse(f.ben), Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant}, StepUpAt: e.clock.Now().Add(-time.Hour)})
	e.problemIs(pass(weak, good), "makeSoftLockPass", 403, CodeStepUpRequired)
	if pv := e.call(http.MethodPost, "/v1/periods/"+pid+"/soft-lock-passes", acc, good, "Prefer", "return=preview"); pv.Header.Get("Preference-Applied") == "" {
		t.Fatalf("the preview of a pass: %d %s", pv.Status, pv.Raw)
	}
	e.problemIs(postBill(acc, "103.00"), "postBill", 409, storage.CodeSoftLocked) // the preview made no pass
	p := pass(acc, good)
	if p.Status != 201 || p.str("period_id") != pid || p.str("expires_at") == "" {
		t.Fatalf("making the pass: %d %s", p.Status, p.Raw)
	}

	// The pass is the Accountant's alone: another Accountant and the Bookkeeper are still refused, and the bill posts for the Accountant, who is named on the report.
	e.problemIs(postBill(other, "104.00"), "postBill", 409, storage.CodeSoftLocked)
	e.problemIs(postBill(book, "105.00"), "postBill", 409, storage.CodeSoftLocked)
	if ok := postBill(acc, "106.00"); ok.Status != http.StatusOK {
		t.Fatalf("posting under the pass: %d %s", ok.Status, ok.Raw)
	}
	rep := e.call(http.MethodGet, "/v1/soft-lock-exceptions", ctl, nil)
	items, _ := rep.Body["items"].([]any)
	if rep.Status != 200 || len(items) != 1 {
		t.Fatalf("the exceptions report: %d %s", rep.Status, rep.Raw)
	}
	x := items[0].(map[string]any)
	if x["user_id"] != f.ben || x["scope"] != "period" || x["source_type"] != "bill" || x["posting_date"] != "2026-10-14" || x["reason"] != good["reason"] || x["period_id"] != pid {
		t.Fatalf("the exception: %v", x)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/soft-lock-exceptions", e.as(authz.Employee), nil), "listSoftLockExceptions", 403, CodeForbidden)

	// Releasing: the approving right, a second factor and a reason. The Accountant (C) cannot.
	_, tag, _ = period()
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/unlock", acc, map[string]any{"reason": "Releasing it for them, as asked."}, append(key(), "If-Match", tag)...), "unlockPeriod", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+pid+"/unlock", ctl, map[string]any{}, append(key(), "If-Match", tag)...), "unlockPeriod", 422, CodeLockReason)
	u := e.call(http.MethodPost, "/v1/periods/"+pid+"/unlock", ctl, map[string]any{"reason": "The auditors have what they need."}, append(key(), "If-Match", tag)...)
	if u.Status != 200 || u.str("status") != "open" {
		t.Fatalf("releasing: %d %s", u.Status, u.Raw)
	}
	if ok := postBill(book, "107.00"); ok.Status != http.StatusOK {
		t.Fatalf("posting after the release: %d %s", ok.Status, ok.Raw)
	}
	if n := len(e.call(http.MethodGet, "/v1/soft-lock-exceptions", ctl, nil).Body["items"].([]any)); n != 1 {
		t.Fatalf("a posting in an open period is no exception: %d", n)
	}

	// A module lock: ap refuses a bill and not an invoice; ar the other way round; releasing needs the approving right and a reason.
	mod := func(who, m, verb string, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/periods/"+pid+"/modules/"+m+"/"+verb, who, body, key()...)
	}
	e.problemIs(mod(e.as(authz.Bookkeeper), "ap", "lock", map[string]any{}), "lockPeriodModule", 403, CodeForbidden)
	e.problemIs(mod(ctl, "sales", "lock", map[string]any{}), "lockPeriodModule", 422, CodeInvalid)
	if r := mod(acc, "ap", "lock", map[string]any{}); r.Status != 200 || len(r.Body["locked_modules"].([]any)) != 1 {
		t.Fatalf("locking ap: %d %s", r.Status, r.Raw)
	}
	e.problemIs(postBill(book, "108.00"), "postBill", 409, storage.CodeSoftLocked)
	id, itag := e.createInvoice("1", "109.00")
	if r := e.call(http.MethodPost, "/v1/invoices/"+id+"/issue", e.as(authz.Accountant), nil, "If-Match", itag); r.Status != http.StatusOK && r.Status != http.StatusConflict {
		t.Fatalf("issuing an invoice while only ap is locked: %d %s", r.Status, r.Raw)
	} else if r.Status == http.StatusConflict && r.problemCode() == storage.CodeSoftLocked {
		t.Fatalf("an invoice was stopped by ap's lock: %s", r.Raw)
	}
	e.problemIs(mod(acc, "ap", "release", map[string]any{"reason": "Releasing it for them, as asked."}), "releasePeriodModule", 403, CodeForbidden)
	e.problemIs(mod(ctl, "ap", "release", map[string]any{"reason": "no"}), "releasePeriodModule", 422, CodeLockReason)
	if r := mod(ctl, "ap", "release", map[string]any{"reason": "Purchases are final for October."}); r.Status != 200 || r.Body["locked_modules"] != nil {
		t.Fatalf("releasing ap: %d %s", r.Status, r.Raw)
	}
	if ok := postBill(book, "110.00"); ok.Status != http.StatusOK {
		t.Fatalf("posting after releasing ap: %d %s", ok.Status, ok.Raw)
	}
}

// matrixSoftLocks are the authorisation matrix's rows of the soft-lock operations.
var matrixSoftLocks = map[string]func(e *env, tok string) (response, func() bool){
	"softLockPeriod": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+uuid.NewString()+"/soft-lock", steppedUp(e, tok), map[string]any{}, "If-Match", `"1-open"`, "Idempotency-Key", uuid.NewString()), nil
	},
	"unlockPeriod": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+uuid.NewString()+"/unlock", steppedUp(e, tok), map[string]any{"reason": "Matrix row for the release."}, "If-Match", `"1-soft_locked"`, "Idempotency-Key", uuid.NewString()), nil
	},
	"lockPeriodModule": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+uuid.NewString()+"/modules/ap/lock", steppedUp(e, tok), map[string]any{}, "Idempotency-Key", uuid.NewString()), nil
	},
	"releasePeriodModule": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+uuid.NewString()+"/modules/ap/release", steppedUp(e, tok), map[string]any{"reason": "Matrix row for the release."}, "Idempotency-Key", uuid.NewString()), nil
	},
	"makeSoftLockPass": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/periods/"+uuid.NewString()+"/soft-lock-passes", steppedUp(e, tok), map[string]any{"reason": "Matrix row for the pass.", "hours": 1}, "Idempotency-Key", uuid.NewString()), nil
	},
	"listSoftLockExceptions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/soft-lock-exceptions", tok, nil), nil
	},
}

func init() {
	// The matrix's period does not exist, so a role the policy allows is answered by the record, past the policy.
	pastPolicy["softLockPeriod"] = CodeNotFound
	pastPolicy["unlockPeriod"] = CodeNotFound
	pastPolicy["lockPeriodModule"] = CodeNotFound
	pastPolicy["releasePeriodModule"] = CodeNotFound
	pastPolicy["makeSoftLockPass"] = CodeNotFound
}
