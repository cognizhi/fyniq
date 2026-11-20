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

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixPractice is the authorisation matrix's row for opening a practice organisation: previews, so nothing is made.
var matrixPractice = map[string]func(e *env, tok string) (response, func() bool){
	"listMyOrganisations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/session/organisations", tok, nil), nil
	},
	"switchMyOrganisation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/organisation", tok, map[string]any{"organisation_id": e.books.Org.String()}), nil
	},
	"createPracticeOrganisation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/organisation/practice", tok, map[string]any{}, "Prefer", "return=preview"), nil
	},
}

// A practice organisation (FR-UX-36; the owner's decision of 21 Nov 2026) is opened only by an Owner of an organisation that keeps books,
// stepped up, one at a time: an Accountant, an Administrator and an Owner not stepped up are refused; the Owner's gets a labelled
// organisation of its own whose books are empty and which holds nothing of the source's; a second is refused with its own code, and from
// the practice organisation too; another Owner opens theirs; the opening is in both organisations' audit trails.
// req: FR-UX-36
// req: SEC-34
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestAnOwnerOpensOnePracticeOrganisationAtATime(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)
	for _, role := range []authz.Role{authz.Accountant, authz.Administrator, authz.Controller, authz.Approver} {
		if r := e.call(http.MethodPost, "/v1/organisation/practice", f.as(f.ahkow, role), map[string]any{}); r.Status != http.StatusForbidden {
			t.Errorf("a %s opens a practice organisation: %d %s", role, r.Status, r.Raw)
		}
	}
	unstepped := e.principal(authn.Principal{Subject: "ahkow", UserID: uuid.MustParse(f.ahkow), Client: "web", Class: authn.ClassUser, Organisation: e.books.Org,
		Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-time.Hour)})
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/practice", unstepped, map[string]any{}), "createPracticeOrganisation", 403, CodeStepUpRequired)

	made := e.call(http.MethodPost, "/v1/organisation/practice", ahkow, map[string]any{"name": "Try the bills"})
	if made.Status != http.StatusCreated || made.str("name") != "Practice: Try the bills" || made.Body["practice"] != true || made.str("base_currency") != "SGD" {
		t.Fatalf("opening one: %d %s", made.Status, made.Raw)
	}
	practice := uuid.MustParse(made.str("id"))
	if practice == e.books.Org {
		t.Fatal("the practice organisation is the real one")
	}
	// One at a time: a second is refused with its own code, whatever the key.
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/practice", ahkow, map[string]any{}), "createPracticeOrganisation", http.StatusConflict, storage.CodePracticeHeld)
	// From the practice organisation itself, none.
	inside := e.principal(authn.Principal{Subject: "ahkow", UserID: uuid.MustParse(f.ahkow), Client: "web", Class: authn.ClassUser, Organisation: practice,
		Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now()})
	if r := e.call(http.MethodPost, "/v1/organisation/practice", inside, map[string]any{}); r.Status != http.StatusForbidden && r.Status != http.StatusConflict {
		t.Errorf("a practice organisation opens another: %d %s", r.Status, r.Raw)
	}
	// Its books are empty and it says it is a practice one.
	org := e.call(http.MethodGet, "/v1/organisation", inside, nil)
	if org.Status != 200 || org.Body["practice"] != true || !strings.HasPrefix(org.str("name"), "Practice: ") {
		t.Errorf("the practice organisation: %d %s", org.Status, org.Raw)
	}
	if accs := e.call(http.MethodGet, "/v1/accounts", inside, nil); accs.Status != 200 || len(accs.Body["items"].([]any)) != 0 {
		t.Errorf("a practice organisation starts with the source's chart: %d %s", accs.Status, accs.Raw)
	}
	if real := e.call(http.MethodGet, "/v1/organisation", ahkow, nil); real.Body["practice"] == true {
		t.Errorf("the real organisation says it is a practice one: %s", real.Raw)
	}
	// Another Owner opens theirs.
	if r := e.call(http.MethodPost, "/v1/organisation/practice", weiling, map[string]any{}); r.Status != http.StatusCreated || r.str("name") != "Practice: "+org2name(e) {
		t.Errorf("another Owner's: %d %s", r.Status, r.Raw)
	}
	// Both organisations' trails have it, from the same transaction.
	trail := workerStore(t)
	has := func(org uuid.UUID, action string) bool {
		found := false
		if err := trail.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
			events, err := u.Audit().Events(ctx, 0, 10000)
			for _, ev := range events {
				var b struct{ Action string }
				_ = json.Unmarshal([]byte(ev.Body), &b)
				found = found || b.Action == action
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return found
	}
	if !has(e.books.Org, "practice_organisation.opened") || !has(practice, "organisation.created") || !has(practice, "role_assignment.created") {
		t.Error("the opening is missing from a trail")
	}
}

func org2name(e *env) string {
	var name string
	_ = e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		name = o.Name
		return err
	})
	return name
}
