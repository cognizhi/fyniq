//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// mileageCategory gives e's organisation, once, the mileage category on Rent, and returns its ID.
func (e *env) mileageCategory() string {
	e.t.Helper()
	e.claimSetup()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		cats, err := u.ClaimCategories().List(ctx)
		if err != nil {
			return err
		}
		for _, c := range cats {
			if c.Mileage && c.Active {
				id = c.ID.String()
				return nil
			}
		}
		c, err := u.ClaimCategories().Create(ctx, storage.NewClaimCategory{Name: "Mileage", AccountID: e.books.Rent.ID, Mileage: true, Currency: "SGD",
			Active: true, At: e.clock.Now()})
		id = c.ID.String()
		return err
	})
	return id
}

// mileageRate records a vehicle's rate per km from a day, as an approved change to the setting records it.
func (e *env) mileageRate(vehicle, rate string, from time.Time) {
	e.t.Helper()
	key := claim.Vehicles[vehicle]
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		changes, err := u.Settings().Changes(ctx, key)
		if err != nil {
			return err
		}
		_, err = u.Settings().Record(ctx, key, "", rate, from, len(changes)+1)
		return err
	})
}

// trip is a mileage line: a trip by car or motorcycle on a day, of km as driven.
func trip(category, date, vehicle, km string) map[string]any {
	return map[string]any{"kind": "mileage", "date": date, "category_id": category, "description": "Client visit", "from": "Office", "to": "Acme HQ",
		"return": true, "vehicle": vehicle, "distance": km}
}

// FR-AP-16 and RND-MILEAGE (p1-expense-claims task 4.1; UC-EXP-02): a trip in the claimant's own car is claimed at the
// organisation's rate per km for a car in force on its date, the distance rounded to 0.1 km and the amount to the cent,
// with no GST; the recommended S$0.60 applies until the organisation changes it (rev 0.151). A rate changed mid-month applies by trip
// date. Each month's trips are the member's mileage log; their saved places are their own.
// req: FR-AP-16
// req: RND-MILEAGE
// req: purchases/expense-claims Mileage claim for a client visit
// req: ledger/posting-rules Mileage claim
func TestMileageIsClaimedAtTheRate(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	mileage := e.mileageCategory()
	ahkow := c.as(c.ahkow, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Employee)

	// No rate changed: a car is at the recommended S$0.60 a km (rev 0.151), so 18.4 km is S$11.04.
	r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-24", "lines": []any{trip(mileage, "2026-10-14", "car", "18.4")}})
	if r.Status != 201 {
		t.Fatalf("drafting: %d %s", r.Status, r.Raw)
	}
	path := "/v1/expense-claims/" + r.str("id")
	g := e.call(http.MethodGet, path, ahkow, nil)
	if l, _ := g.Body["lines"].([]any); len(l) != 1 || l[0].(map[string]any)["rate"].(map[string]any)["amount"] != "0.60" ||
		l[0].(map[string]any)["amount"].(map[string]any)["amount"] != "11.04" {
		t.Fatalf("a trip at the recommended rate: %s", g.Raw)
	}

	// The organisation changes it to S$0.70 from 20 October: 18.4 km on the 14th stays S$11.04; 10.04 km on the 22nd
	// rounds to 10.0 km, S$7.00.
	e.mileageRate("car", "0.70", porttest.Day(10, 20))
	draft := map[string]any{"date": "2026-10-24", "lines": []any{trip(mileage, "2026-10-14", "car", "18.4"), trip(mileage, "2026-10-22", "car", "10.04")}}
	fixed := e.call(http.MethodPut, path, ahkow, draft, "If-Match", g.Header.Get("ETag"))
	lines, _ := fixed.Body["lines"].([]any)
	if fixed.Status != 200 || fixed.str("total", "amount") != "18.04" || len(lines) != 2 {
		t.Fatalf("two trips: %d %s", fixed.Status, fixed.Raw)
	}
	first, second := lines[0].(map[string]any), lines[1].(map[string]any)
	if first["claimed_km"] != "18.4" || first["amount"].(map[string]any)["amount"] != "11.04" || first["rate"].(map[string]any)["amount"] != "0.60" ||
		second["claimed_km"] != "10" || second["amount"].(map[string]any)["amount"] != "7.00" || second["rate"].(map[string]any)["amount"] != "0.70" {
		t.Errorf("the trips: %s", fixed.Raw)
	}
	bike := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-24", "lines": []any{trip(mileage, "2026-10-14", "motorcycle", "5")}})
	if g := e.call(http.MethodGet, "/v1/expense-claims/"+bike.str("id"), ahkow, nil); g.str("total", "amount") != "1.25" { // the recommended S$0.25 a km
		t.Errorf("a motorcycle at the recommended rate: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"lines": []any{trip(mileage, "2026-10-14", "bicycle", "5")}}),
		"createExpenseClaim", 422, CodeInvalid)

	// Approved, it posts the trips with no GST: Dr Rent 11.04 and 7.00, Cr 2150 18.04.
	asked := e.call(http.MethodPost, path+"/submit", ahkow, nil, "If-Match", fixed.Header.Get("ETag"))
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); d.Status != 200 {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	done := e.call(http.MethodGet, path, ahkow, nil)
	var legs []string
	for _, x := range done.Body["ledger_impact"].([]any) {
		legs = append(legs, x.(map[string]any)["base_amount"].(map[string]any)["amount"].(string))
	}
	if done.str("status") != "posted" || strings.Join(legs, " ") != "11.04 7.00 -18.04" {
		t.Errorf("the entry: %v %s", legs, done.Raw)
	}

	// October's mileage log: the two trips approved and the motorcycle's still a draft at S$1.25, S$19.29; another
	// Employee does not see it, finance does.
	log := e.call(http.MethodGet, "/v1/expense-claims/mileage-log?month=2026-10", ahkow, nil)
	if log.Status != 200 || len(log.Body["trips"].([]any)) != 3 || log.str("total", "amount") != "19.29" || log.str("claimant", "name") != "Ah Kow" {
		t.Errorf("his log: %d %s", log.Status, log.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/expense-claims/mileage-log?month=2026-10&claimant_id="+c.ahkow, mei, nil), "getMileageLog", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/expense-claims/mileage-log?month=2026-10&claimant_id="+c.ahkow, weiling, nil); len(l.Body["trips"].([]any)) != 3 {
		t.Errorf("finance reads it: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/expense-claims/mileage-log?month=2026-11", ahkow, nil); len(l.Body["trips"].([]any)) != 0 {
		t.Errorf("November: %s", l.Raw)
	}

	// His saved places are his own.
	office := e.call(http.MethodPost, "/v1/claim-places", ahkow, map[string]any{"name": "Office", "address": "1 Raffles Place"})
	if office.Status != 201 || office.str("name") != "Office" {
		t.Fatalf("saving a place: %d %s", office.Status, office.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/claim-places", ahkow, map[string]any{"name": " office ", "address": "Elsewhere"}), "createClaimPlace", 422, CodeInvalid)
	if l := e.call(http.MethodGet, "/v1/claim-places", mei, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another member's places: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/claim-places/"+office.str("id"), mei, map[string]any{"name": "Mine", "address": "Mine"}, "If-Match", office.Header.Get("ETag")),
		"replaceClaimPlace", 404, CodeNotFound)
	moved := e.call(http.MethodPut, "/v1/claim-places/"+office.str("id"), ahkow, map[string]any{"name": "Office", "address": "8 Marina View", "active": false},
		"If-Match", office.Header.Get("ETag"))
	if moved.Status != 200 || moved.Body["active"] != false || moved.Body["version"] != float64(2) {
		t.Errorf("changing it: %d %s", moved.Status, moved.Raw)
	}
}

// matrixMileage are the authorisation matrix's rows for saved places and the mileage log, as the matrix's member.
var matrixMileage = map[string]func(e *env, tok string) (response, func() bool){
	"listClaimPlaces": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/claim-places", steppedUp(e, tok), nil), nil
	},
	"createClaimPlace": func(e *env, tok string) (response, func() bool) {
		name := "Place " + uuid.NewString()[:8]
		r := e.call(http.MethodPost, "/v1/claim-places", steppedUp(e, tok), map[string]any{"name": name, "address": "1 Raffles Place"})
		return r, func() bool {
			return !strings.Contains(e.call(http.MethodGet, "/v1/claim-places", steppedUp(e, e.as(authz.Employee)), nil).Raw, name)
		}
	},
	"replaceClaimPlace": func(e *env, tok string) (response, func() bool) {
		var place storage.ClaimPlace
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			place, err = u.ClaimPlaces().Create(ctx, storage.NewClaimPlace{UserID: uuid.MustParse(matrixApprover(e)), Name: "Place " + uuid.NewString()[:8],
				Address: "1 Raffles Place", Active: true, At: e.clock.Now()})
			return err
		})
		r := e.call(http.MethodPut, "/v1/claim-places/"+place.ID.String(), steppedUp(e, tok), map[string]any{"name": place.Name, "address": "8 Marina View"},
			"If-Match", etag(place.Version))
		return r, func() bool {
			var now storage.ClaimPlace
			e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
				now, err = u.ClaimPlaces().Get(ctx, place.ID)
				return err
			})
			return now.Version == 1
		}
	},
	"getMileageLog": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/expense-claims/mileage-log?month=2026-10", steppedUp(e, tok), nil), nil
	},
}
