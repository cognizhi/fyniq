//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixEntityGroups are the authorisation matrix's rows of the group operations.
var matrixEntityGroups = map[string]func(e *env, tok string) (response, func() bool){
	"getEntityGroup": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/group", tok, nil), nil
	},
	"inviteGroupEntity": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/entities", steppedUp(e, tok), map[string]any{"label": "Matrix entity"}), nil
	},
	"withdrawGroupInvitation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/entities/"+uuid.NewString()+"/withdraw", steppedUp(e, tok), map[string]any{"reason": "Matrix"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"acceptGroupInvitation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/accept", steppedUp(e, tok), map[string]any{"code": "AAAAA-BBBBB-CCCCC-DDDDD-EEEEE-F"}, "Idempotency-Key", uuid.NewString()), nil
	},
	"endGroupLink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/group/end", steppedUp(e, tok), map[string]any{"organisation_id": uuid.NewString(), "reason": "Matrix"}, "Idempotency-Key", uuid.NewString()), nil
	},
}

func init() {
	// The matrix's organisation has no group: an allowed role is answered by the link's state, past the policy.
	pastPolicy["withdrawGroupInvitation"] = CodeNotFound
	pastPolicy["acceptGroupInvitation"] = storage.CodeGroupCode
	pastPolicy["endGroupLink"] = storage.CodeGroupLink
}

// An entity is an organisation and a group is organisations linked by the consent of both Owners (FR-ENT-04): the parent's Owner, stepped up, invites with a one-time code; a role that is no
// Owner is refused; the entity's Owner accepts it in the entity's own organisation, and a wrong code is refused with one answer; each organisation then sees only its own side; either Owner ends
// it with a reason; a withdrawn invitation's code stops working.
// req: FR-ENT-04
// asvs: V8.2.1, V8.3.1, V4.2.1, V2.3.1
func TestAnOwnerInvitesAnEntityItsOwnerAcceptsAndEitherEndsIt(t *testing.T) {
	parent, entity := newFounders(t), newFounders(t)
	pe, ee := parent.e, entity.e
	owner, entityOwner := parent.as(parent.ahkow), entity.as(entity.ahkow)
	for _, role := range []authz.Role{authz.Accountant, authz.Administrator, authz.Controller, authz.Approver} {
		if r := pe.call(http.MethodPost, "/v1/group/entities", parent.as(parent.ahkow, role), map[string]any{"label": "x"}); r.Status != http.StatusForbidden {
			t.Errorf("a %s invites an entity: %d %s", role, r.Status, r.Raw)
		}
	}
	unstepped := pe.principal(authn.Principal{Subject: "ahkow", UserID: uuid.MustParse(parent.ahkow), Client: "web", Class: authn.ClassUser, Organisation: pe.books.Org,
		Roles: []authz.Role{authz.Owner}, StepUpAt: pe.clock.Now().Add(-time.Hour)})
	pe.problemIs(pe.call(http.MethodPost, "/v1/group/entities", unstepped, map[string]any{"label": "x"}), "inviteGroupEntity", 403, CodeStepUpRequired)

	made := pe.call(http.MethodPost, "/v1/group/entities", owner, map[string]any{"label": "Cognizhi Sdn. Bhd."})
	if made.Status != http.StatusCreated || made.str("state") != "invited" || made.str("code") == "" || made.str("expires_at") == "" {
		t.Fatalf("inviting: %d %s", made.Status, made.Raw)
	}
	code := made.str("code")
	view := pe.call(http.MethodGet, "/v1/group", owner, nil)
	if view.Status != 200 || view.str("role") != "parent" || len(view.Body["entities"].([]any)) != 1 {
		t.Fatalf("the parent's view: %d %s", view.Status, view.Raw)
	}
	if view.Raw != "" && containsCode(view.Raw, code) {
		t.Error("the code is shown again")
	}
	// The entity's side: a wrong code, a role that is no Owner, then the Owner.
	ee.problemIs(ee.call(http.MethodPost, "/v1/group/accept", entityOwner, map[string]any{"code": "AAAAA-BBBBB-CCCCC-DDDDD-EEEEE-F"}), "acceptGroupInvitation", 422, storage.CodeGroupCode)
	if r := ee.call(http.MethodPost, "/v1/group/accept", entity.as(entity.ahkow, authz.Accountant), map[string]any{"code": code}); r.Status != http.StatusForbidden {
		t.Errorf("an Accountant accepts: %d %s", r.Status, r.Raw)
	}
	acc := ee.call(http.MethodPost, "/v1/group/accept", entityOwner, map[string]any{"code": code}, "Idempotency-Key", uuid.NewString())
	if acc.Status != http.StatusCreated || acc.str("state") != "active" || acc.str("parent_organisation_id") != pe.books.Org.String() {
		t.Fatalf("accepting: %d %s", acc.Status, acc.Raw)
	}
	scopes, _ := acc.Body["scopes"].([]any)
	if len(scopes) != 2 {
		t.Errorf("the scopes: %v", scopes)
	}
	ee.problemIs(ee.call(http.MethodPost, "/v1/group/accept", entityOwner, map[string]any{"code": code}), "acceptGroupInvitation", 422, storage.CodeGroupCode)
	// Each side sees its own.
	pv := pe.call(http.MethodGet, "/v1/group", owner, nil)
	row := pv.Body["entities"].([]any)[0].(map[string]any)
	if row["state"] != "active" || row["entity_organisation_id"] != ee.books.Org.String() || row["entity_currency"] != "SGD" {
		t.Errorf("the parent's row: %v", row)
	}
	ev := ee.call(http.MethodGet, "/v1/group", entityOwner, nil)
	if ev.str("role") != "entity" || len(ev.Body["entities"].([]any)) != 0 || len(ev.Body["memberships"].([]any)) != 1 {
		t.Errorf("the entity's view: %s", ev.Raw)
	}
	// An entity that is a parent's entity invites nobody.
	if r := ee.call(http.MethodPost, "/v1/group/entities", entityOwner, map[string]any{"label": "Grandchild"}); r.Status != http.StatusForbidden || r.problemCode() != storage.CodeGroup {
		t.Errorf("an entity invites: %d %s", r.Status, r.Raw)
	}
	// Ending needs a reason and a live link; the entity's Owner ends it; the parent's view says ended.
	ee.problemIs(ee.call(http.MethodPost, "/v1/group/end", entityOwner, map[string]any{"organisation_id": uuid.NewString(), "reason": "x"}), "endGroupLink", 404, storage.CodeGroupLink)
	if r := ee.call(http.MethodPost, "/v1/group/end", entityOwner, map[string]any{"organisation_id": pe.books.Org.String(), "reason": "  "}); r.Status != 422 {
		t.Errorf("an end without a reason: %d %s", r.Status, r.Raw)
	}
	ended := ee.call(http.MethodPost, "/v1/group/end", entityOwner, map[string]any{"organisation_id": pe.books.Org.String(), "reason": "We left the group"})
	if ended.Status != 200 || ended.str("role") != "entity" {
		t.Fatalf("ending: %d %s", ended.Status, ended.Raw)
	}
	pv = pe.call(http.MethodGet, "/v1/group", owner, nil)
	row = pv.Body["entities"].([]any)[0].(map[string]any)
	if row["state"] != "ended" || row["ended_reason"] != "We left the group" || pv.str("role") != "none" {
		t.Errorf("the parent after the entity left: %s", pv.Raw)
	}
	// A withdrawn invitation's code stops working.
	again := pe.call(http.MethodPost, "/v1/group/entities", owner, map[string]any{"label": "Second"})
	if again.Status != http.StatusCreated {
		t.Fatalf("a second invitation: %d %s", again.Status, again.Raw)
	}
	if w := pe.call(http.MethodPost, "/v1/group/entities/"+again.str("id")+"/withdraw", owner, map[string]any{"reason": "Typed the wrong entity"}); w.Status != 200 || w.str("state") != "ended" {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	ee.problemIs(ee.call(http.MethodPost, "/v1/group/accept", entityOwner, map[string]any{"code": again.str("code")}), "acceptGroupInvitation", 422, storage.CodeGroupCode)
	pe.problemIs(pe.call(http.MethodPost, "/v1/group/entities/"+again.str("id")+"/withdraw", owner, map[string]any{"reason": "again"}), "withdrawGroupInvitation", 404, CodeNotFound)
}

func containsCode(raw, code string) bool {
	return len(code) > 0 && strings.Contains(raw, code)
}
