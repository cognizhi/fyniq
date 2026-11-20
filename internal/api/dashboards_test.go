//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// A dashboard is tiles a role or a person arranged (FR-VIZ-09, FR-KPI-05): the person's own wins over their role's, which wins over the default;
// a tile is a KPI of the catalogue placed once inside the grid; clearing a layout brings the role's back; a role's layout is an Owner's or
// Administrator's to set; and the layout holds no figure and no right, so a viewer who may not read the KPIs gets no tiles.
// req: FR-VIZ-09
// req: FR-KPI-05
// asvs: V4.2.1
func TestADashboardIsTheTilesAPersonOrARoleArranged(t *testing.T) {
	e := newEnv(t)
	me := uuid.New()
	controller := e.person(me, authz.Controller)
	owner := e.person(uuid.New(), authz.Owner)

	// The catalogue is the KPIs'; with nothing arranged, the default shows.
	tiles := e.call(http.MethodGet, "/v1/dashboard-tiles?page_size=500", controller, nil)
	if tiles.Status != http.StatusOK || len(tiles.Body["items"].([]any)) < 30 {
		t.Fatalf("the tiles = %d: %.200s", tiles.Status, tiles.Raw)
	}
	d := e.call(http.MethodGet, "/v1/dashboards/mine", controller, nil)
	if d.Status != http.StatusOK || d.str("source") != "default" || d.Header.Get("ETag") != `"0"` || len(d.Body["tiles"].([]any)) != 6 {
		t.Fatalf("the default = %d: %s", d.Status, d.Raw)
	}

	// A role's layout is the Owner's to set (over "0"), not a Controller's.
	layout := func(keys ...string) map[string]any {
		var ts []map[string]any
		for i, k := range keys {
			ts = append(ts, map[string]any{"tile": k, "x": (i % 3) * 4, "y": i / 3, "w": 4, "h": 1})
		}
		return map[string]any{"tiles": ts}
	}
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/roles/controller", controller, layout("cash"), "If-Match", `"0"`), "saveRoleDashboard", 403, CodeDashboardRole)
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/roles/nonsense", owner, layout("cash"), "If-Match", `"0"`), "saveRoleDashboard", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/roles/controller", owner, layout("cash")), "saveRoleDashboard", 428, CodeIfMatchRequired)
	set := e.call(http.MethodPut, "/v1/dashboards/roles/controller", owner, layout("cash", "runway"), "If-Match", `"0"`)
	if set.Status != http.StatusOK || set.Header.Get("ETag") != `"1"` {
		t.Fatalf("the role's layout = %d: %s", set.Status, set.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/roles/controller", owner, layout("cash"), "If-Match", `"0"`), "saveRoleDashboard", 412, CodeStale)
	d = e.call(http.MethodGet, "/v1/dashboards/mine", controller, nil)
	if d.str("source") != "role" || d.str("role") != "controller" || len(d.Body["tiles"].([]any)) != 2 {
		t.Fatalf("the role's layout shows to a Controller: %s", d.Raw)
	}

	// A person's own layout wins; a layout that breaks a rule is refused with its code.
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/mine", controller, layout("cash", "cash"), "If-Match", `"0"`), "saveMyDashboard", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/mine", controller, map[string]any{"tiles": []map[string]any{{"tile": "cash", "x": 10, "y": 0, "w": 4, "h": 1}}}, "If-Match", `"0"`), "saveMyDashboard", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/mine", controller, layout("not_a_kpi"), "If-Match", `"0"`), "saveMyDashboard", 422, "API-INPUT-02")
	mine := e.call(http.MethodPut, "/v1/dashboards/mine", controller, layout("net_burn", "revenue", "dso"), "If-Match", `"0"`)
	if mine.Status != http.StatusOK || mine.Header.Get("ETag") != `"1"` || mine.str("source") != "user" {
		t.Fatalf("their own = %d: %s", mine.Status, mine.Raw)
	}
	if d = e.call(http.MethodGet, "/v1/dashboards/mine", controller, nil); d.str("source") != "user" || len(d.Body["tiles"].([]any)) != 3 {
		t.Fatalf("their own wins: %s", d.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/dashboards/mine", controller, layout("cash"), "If-Match", `"0"`), "saveMyDashboard", 412, CodeStale)
	// Clearing it brings the role's back, as the next version.
	cleared := e.call(http.MethodPut, "/v1/dashboards/mine", controller, map[string]any{"tiles": []any{}}, "If-Match", `"1"`)
	if cleared.Status != http.StatusOK || cleared.Header.Get("ETag") != `"2"` {
		t.Fatalf("clearing = %d: %s", cleared.Status, cleared.Raw)
	}
	if d = e.call(http.MethodGet, "/v1/dashboards/mine", controller, nil); d.str("source") != "role" {
		t.Fatalf("after clearing: %s", d.Raw)
	}
	// Someone else's layout is theirs: another Controller sees the role's, not mine.
	if d = e.call(http.MethodGet, "/v1/dashboards/mine", e.person(uuid.New(), authz.Controller), nil); d.str("source") != "role" || d.Header.Get("ETag") != `"0"` {
		t.Errorf("another Controller: %s", d.Raw)
	}
	// A viewer who may not read the KPIs gets no tiles, whatever a layout names; a Board viewer reads them.
	if d = e.call(http.MethodGet, "/v1/dashboards/mine", e.person(uuid.New(), authz.ARSpecialist), nil); d.Status != http.StatusForbidden {
		t.Errorf("an AR specialist's view of the reports reads no KPIs: %d", d.Status)
	}
	if d = e.call(http.MethodGet, "/v1/dashboards/mine", e.person(uuid.New(), authz.BoardViewer), nil); d.Status != http.StatusOK || len(d.Body["tiles"].([]any)) == 0 {
		t.Errorf("a Board viewer reads the KPIs: %s", d.Raw)
	}
}
