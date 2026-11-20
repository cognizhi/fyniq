//go:build local

package api

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// matrixReportRows are a report's rows that need no account of the chart: the matrix's organisation has none loaded.
func matrixReportRows() []map[string]any {
	return []map[string]any{{"id": "hd", "label": "Heading", "kind": "heading"}, {"id": "two", "label": "Two", "kind": "calc", "formula": "1 + 1", "format": "ratio"}}
}

var (
	matrixReportMu  sync.Mutex
	matrixReportSeq int
	matrixReportShr = map[*env]string{} // the shared report each call that needs one reads
	matrixReportSch = map[*env]string{} // its schedule
)

// matrixReportOwner is a token for a member who may save and schedule reports, the matrix's accountant.
func matrixReportOwner(e *env) string {
	return asMatrixMember(e, e.as(authz.Accountant))
}

// matrixSharedReport is a shared report of the matrix's organisation, and its current ETag.
func matrixSharedReport(e *env) (id, etag string) {
	matrixReportMu.Lock()
	id = matrixReportShr[e]
	matrixReportMu.Unlock()
	if id == "" {
		r := e.call(http.MethodPost, "/v1/report-definitions", matrixReportOwner(e), reportBody("Matrix report", "shared", matrixReportRows()))
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's report = %d: %s", r.Status, r.Raw)
		}
		id = r.str("id")
		matrixReportMu.Lock()
		matrixReportShr[e] = id
		matrixReportMu.Unlock()
	}
	return id, e.call(http.MethodGet, "/v1/report-definitions/"+id, matrixReportOwner(e), nil).Header.Get("ETag")
}

func matrixFreshReport(e *env) (id, etag string) {
	matrixReportMu.Lock()
	matrixReportSeq++
	n := matrixReportSeq
	matrixReportMu.Unlock()
	r := e.call(http.MethodPost, "/v1/report-definitions", matrixReportOwner(e), reportBody(fmt.Sprintf("Matrix fresh %d", n), "shared", matrixReportRows()))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("a fresh matrix report = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// matrixReportReader is the matrix's one member, an Owner as well (matrixApprover), so a recipient who may read the whole ledger's
// reports: the organisation keeps one member, so the founder bootstrap the other rows rely on holds.
func matrixReportReader(e *env) uuid.UUID {
	return uuid.MustParse(matrixApprover(e))
}

func matrixScheduleBody(e *env, def string, active bool) map[string]any {
	return map[string]any{"definition_id": def, "cadence": "weekly", "weekday": 1, "period_rule": "last_closed_month", "channel": "email",
		"recipients": []string{matrixReportReader(e).String()}, "active": active}
}

// matrixSchedule is a schedule of the matrix's shared report and its ETag.
func matrixSchedule(e *env) (id, etag string) {
	matrixReportMu.Lock()
	id = matrixReportSch[e]
	matrixReportMu.Unlock()
	def, _ := matrixSharedReport(e)
	if id == "" {
		r := e.call(http.MethodPost, "/v1/report-schedules", matrixReportOwner(e), matrixScheduleBody(e, def, true))
		if r.Status != http.StatusCreated {
			e.t.Fatalf("the matrix's schedule = %d: %s", r.Status, r.Raw)
		}
		id = r.str("id")
		matrixReportMu.Lock()
		matrixReportSch[e] = id
		matrixReportMu.Unlock()
	}
	l := e.call(http.MethodGet, "/v1/report-schedules?definition_id="+def, matrixReportOwner(e), nil)
	for _, x := range l.Body["items"].([]any) {
		if s := x.(map[string]any); s["id"] == id {
			return id, fmt.Sprintf(`"%d"`, int(s["version"].(float64)))
		}
	}
	e.t.Fatalf("the matrix's schedule %s is not listed", id)
	return "", ""
}

// matrixReportBuilder are the report builder's rows of the authorisation matrix (FR-RPT-04, FR-RPT-07).
var matrixReportBuilder = map[string]func(e *env, tok string) (response, func() bool){
	"listReportDefinitions": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/report-definitions", asMatrixMember(e, tok), nil), nil
	},
	"createReportDefinition": func(e *env, tok string) (response, func() bool) {
		matrixReportMu.Lock()
		matrixReportSeq++
		name := fmt.Sprintf("Matrix created %d", matrixReportSeq)
		matrixReportMu.Unlock()
		r := e.call(http.MethodPost, "/v1/report-definitions", asMatrixMember(e, tok), reportBody(name, "shared", matrixReportRows()))
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/report-definitions", matrixReportOwner(e), nil)
			for _, x := range l.Body["items"].([]any) {
				if x.(map[string]any)["name"] == name {
					return false
				}
			}
			return true
		}
	},
	"getReportDefinition": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixSharedReport(e)
		return e.call(http.MethodGet, "/v1/report-definitions/"+id, asMatrixMember(e, tok), nil), nil
	},
	"replaceReportDefinition": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixSharedReport(e)
		r := e.call(http.MethodPut, "/v1/report-definitions/"+id, asMatrixMember(e, tok), reportBody("Matrix report", "shared", matrixReportRows()), "If-Match", etag)
		return r, func() bool { _, now := matrixSharedReport(e); return now == etag }
	},
	"archiveReportDefinition": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixFreshReport(e)
		r := e.call(http.MethodPost, "/v1/report-definitions/"+id+"/archive", asMatrixMember(e, tok), nil, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/report-definitions/"+id, matrixReportOwner(e), nil).Body["archived"] == false
		}
	},
	"runReportDefinition": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixSharedReport(e)
		return e.call(http.MethodGet, "/v1/report-definitions/"+id+"/run?to=2026-10", asMatrixMember(e, tok), nil), nil
	},
	"runReportDraft": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/report-builder/run", asMatrixMember(e, tok), map[string]any{"definition": reportBody("x", "private", matrixReportRows())["definition"], "to": "2026-10"}), nil
	},
	"listReportSchedules": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixSharedReport(e)
		return e.call(http.MethodGet, "/v1/report-schedules?definition_id="+id, asMatrixMember(e, tok), nil), nil
	},
	"createReportSchedule": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixSharedReport(e)
		before := e.call(http.MethodGet, "/v1/report-schedules?definition_id="+id, matrixReportOwner(e), nil)
		n := len(before.Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/report-schedules", asMatrixMember(e, tok), matrixScheduleBody(e, id, true))
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/report-schedules?definition_id="+id, matrixReportOwner(e), nil).Body["items"].([]any)) == n
		}
	},
	"replaceReportSchedule": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixSchedule(e)
		def, _ := matrixSharedReport(e)
		body := matrixScheduleBody(e, def, true)
		delete(body, "definition_id")
		r := e.call(http.MethodPut, "/v1/report-schedules/"+id, asMatrixMember(e, tok), body, "If-Match", etag)
		return r, func() bool { _, now := matrixSchedule(e); return now == etag }
	},
	"listReportScheduleRuns": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixSchedule(e)
		return e.call(http.MethodGet, "/v1/report-schedules/"+id+"/runs", asMatrixMember(e, tok), nil), nil
	},
}

var (
	matrixPackPub = map[*env]string{} // a published pack of the matrix's organisation
	matrixPackUpd = map[*env]string{} // an update made from it, and sent
)

// matrixPublishedPack is a published board pack of the matrix's organisation: made as its Owner-and-Accountant member.
func matrixPublishedPack(e *env) string {
	matrixReportMu.Lock()
	id := matrixPackPub[e]
	matrixReportMu.Unlock()
	if id != "" {
		return id
	}
	owner := asMatrixMember(e, e.as(authz.Owner))
	matrixApprover(e)
	r := e.call(http.MethodPost, "/v1/board-packs", owner, map[string]any{"month": "2026-10"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's pack = %d: %s", r.Status, r.Raw)
	}
	id = r.str("id")
	if p := e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", owner, nil, "If-Match", `"1"`); p.Status != http.StatusOK {
		e.t.Fatalf("publishing the matrix's pack = %d: %s", p.Status, p.Raw)
	}
	matrixReportMu.Lock()
	matrixPackPub[e] = id
	matrixReportMu.Unlock()
	return id
}

// matrixDraftPack is the draft of November's pack, made when there is none, and its ETag.
func matrixDraftPack(e *env) (id, etag string) {
	owner := asMatrixMember(e, e.as(authz.Owner))
	matrixPublishedPack(e)
	for _, x := range e.call(http.MethodGet, "/v1/board-packs", owner, nil).Body["items"].([]any) {
		if p := x.(map[string]any); p["month"] == "2026-11" && p["status"] == "draft" {
			return p["id"].(string), fmt.Sprintf(`"%d"`, int(p["edit"].(float64)))
		}
	}
	r := e.call(http.MethodPost, "/v1/board-packs", owner, map[string]any{"month": "2026-11"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("a draft = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// matrixNoDraft publishes November's draft, if there is one, so that the next is made fresh.
func matrixNoDraft(e *env) {
	owner := asMatrixMember(e, e.as(authz.Owner))
	matrixPublishedPack(e)
	for _, x := range e.call(http.MethodGet, "/v1/board-packs", owner, nil).Body["items"].([]any) {
		if p := x.(map[string]any); p["month"] == "2026-11" && p["status"] == "draft" {
			r := e.call(http.MethodPost, "/v1/board-packs/"+p["id"].(string)+"/publish", owner, nil, "If-Match", fmt.Sprintf(`"%d"`, int(p["edit"].(float64))))
			if r.Status != http.StatusOK {
				e.t.Fatalf("publishing the draft = %d: %s", r.Status, r.Raw)
			}
		}
	}
}

// matrixDraftUpdate is a draft update of the published pack and its ETag.
func matrixDraftUpdate(e *env) (id, etag string) {
	pack := matrixPublishedPack(e)
	r := e.call(http.MethodPost, "/v1/investor-updates", asMatrixMember(e, e.as(authz.Owner)), map[string]any{"pack_id": pack, "kind": "monthly", "title": "Matrix update"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's update = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// matrixSentUpdate is an update that was sent, to the matrix's member.
func matrixSentUpdate(e *env) string {
	matrixReportMu.Lock()
	id := matrixPackUpd[e]
	matrixReportMu.Unlock()
	if id != "" {
		return id
	}
	id, etag := matrixDraftUpdate(e)
	r := e.call(http.MethodPost, "/v1/investor-updates/"+id+"/send", asMatrixMember(e, e.as(authz.Owner)), map[string]any{"recipients": []string{matrixApprover(e)}}, "If-Match", etag)
	if r.Status != http.StatusOK {
		e.t.Fatalf("sending the matrix's update = %d: %s", r.Status, r.Raw)
	}
	matrixReportMu.Lock()
	matrixPackUpd[e] = id
	matrixReportMu.Unlock()
	return id
}

// matrixBoardPacks are the board pack's and the investor update's rows of the authorisation matrix (FR-RPT-06, FR-EQ-23).
var matrixBoardPacks = map[string]func(e *env, tok string) (response, func() bool){
	"getDeferralRollForward": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/reports/deferral-roll-forward?from=2026-10&to=2026-10", asMatrixMember(e, tok), nil), nil
	},
	"listDashboardTiles": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/dashboard-tiles", asMatrixMember(e, tok), nil), nil
	},
	"getMyDashboard": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/dashboards/mine", asMatrixMember(e, tok), nil), nil
	},
	"getRoleDashboard": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/dashboards/roles/controller", asMatrixMember(e, tok), nil), nil
	},
	"saveMyDashboard": func(e *env, tok string) (response, func() bool) {
		// The matrix's one member is every role's person: read the layout's version as the Owner, whatever the role asked.
		cur := e.call(http.MethodGet, "/v1/dashboards/mine", asMatrixMember(e, e.as(authz.Owner)), nil)
		etag := cur.Header.Get("ETag")
		r := e.call(http.MethodPut, "/v1/dashboards/mine", asMatrixMember(e, tok), map[string]any{"tiles": []map[string]any{{"tile": "cash", "x": 0, "y": 0, "w": 4, "h": 1}}}, "If-Match", etag)
		return r, nil
	},
	"saveRoleDashboard": func(e *env, tok string) (response, func() bool) {
		cur := e.call(http.MethodGet, "/v1/dashboards/roles/auditor", asMatrixMember(e, e.as(authz.Owner)), nil)
		etag := cur.Header.Get("ETag")
		r := e.call(http.MethodPut, "/v1/dashboards/roles/auditor", asMatrixMember(e, tok), map[string]any{"tiles": []map[string]any{{"tile": "cash", "x": 0, "y": 0, "w": 4, "h": 1}}}, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/dashboards/roles/auditor", asMatrixMember(e, e.as(authz.Owner)), nil).Header.Get("ETag") == etag
		}
	},
	"listKpiAlerts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/kpi-alerts", asMatrixMember(e, tok), nil), nil
	},
	"listBoardPacks": func(e *env, tok string) (response, func() bool) {
		matrixPublishedPack(e)
		return e.call(http.MethodGet, "/v1/board-packs", asMatrixMember(e, tok), nil), nil
	},
	"getBoardPack": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/board-packs/"+matrixPublishedPack(e), asMatrixMember(e, tok), nil), nil
	},
	"createBoardPackDraft": func(e *env, tok string) (response, func() bool) {
		matrixNoDraft(e)
		owner := asMatrixMember(e, e.as(authz.Owner))
		n := len(e.call(http.MethodGet, "/v1/board-packs", owner, nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/board-packs", asMatrixMember(e, tok), map[string]any{"month": "2026-11"})
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/board-packs", owner, nil).Body["items"].([]any)) == n
		}
	},
	"editBoardPackDraft": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixDraftPack(e)
		owner := asMatrixMember(e, e.as(authz.Owner))
		r := e.call(http.MethodPut, "/v1/board-packs/"+id, asMatrixMember(e, tok), map[string]any{"highlights": "h", "commentary": "c", "asks": "a"}, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/board-packs/"+id, owner, nil).Header.Get("ETag") == etag
		}
	},
	"publishBoardPack": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixDraftPack(e)
		owner := asMatrixMember(e, e.as(authz.Owner))
		r := e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", asMatrixMember(e, tok), nil, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/board-packs/"+id, owner, nil).Body["status"] == "draft"
		}
	},
	"listInvestorUpdates": func(e *env, tok string) (response, func() bool) {
		matrixSentUpdate(e)
		return e.call(http.MethodGet, "/v1/investor-updates", asMatrixMember(e, tok), nil), nil
	},
	"getInvestorUpdate": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/investor-updates/"+matrixSentUpdate(e), asMatrixMember(e, tok), nil), nil
	},
	"createInvestorUpdate": func(e *env, tok string) (response, func() bool) {
		pack := matrixPublishedPack(e)
		owner := asMatrixMember(e, e.as(authz.Owner))
		n := len(e.call(http.MethodGet, "/v1/investor-updates", owner, nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/investor-updates", asMatrixMember(e, tok), map[string]any{"pack_id": pack, "kind": "monthly", "title": "Matrix created"})
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/investor-updates", owner, nil).Body["items"].([]any)) == n
		}
	},
	"editInvestorUpdate": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixDraftUpdate(e)
		r := e.call(http.MethodPut, "/v1/investor-updates/"+id, asMatrixMember(e, tok), map[string]any{"title": "Matrix edited", "highlights": "h", "asks": "a"}, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/investor-updates/"+id, asMatrixMember(e, e.as(authz.Owner)), nil).Body["title"] == "Matrix update"
		}
	},
	"sendInvestorUpdate": func(e *env, tok string) (response, func() bool) {
		id, etag := matrixDraftUpdate(e)
		r := e.call(http.MethodPost, "/v1/investor-updates/"+id+"/send", asMatrixMember(e, tok), map[string]any{"recipients": []string{matrixApprover(e)}}, "If-Match", etag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/investor-updates/"+id, asMatrixMember(e, e.as(authz.Owner)), nil).Body["status"] == "draft"
		}
	},
}
