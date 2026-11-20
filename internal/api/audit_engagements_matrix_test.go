//go:build local

package api

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

var (
	matrixAuditMu  sync.Mutex
	matrixAuditSeq int
	matrixAuditEng = map[*env]string{} // the matrix's one active engagement
)

// matrixAuditor makes the matrix's one member an Auditor too, once, so that an engagement has an auditor to name.
func matrixAuditor(e *env) (user string) {
	matrixBoardMember(e)
	l := e.call(http.MethodGet, "/v1/members", e.as(authz.Administrator), nil)
	m := l.Body["items"].([]any)[0].(map[string]any)
	if !contains(l.Raw, `"auditor"`) {
		if r := e.call(http.MethodPost, "/v1/members/"+m["id"].(string)+"/roles", e.as(authz.Administrator), map[string]any{"role": "auditor"}); r.Status != 201 {
			e.t.Fatalf("making the matrix member an Auditor: %d %s", r.Status, r.Raw)
		}
	}
	return m["user_id"].(string)
}

func matrixOpenBody(e *env, name string) map[string]any {
	return map[string]any{"name": name, "auditor_user_id": matrixAuditor(e), "period_from": "2026-01-01", "period_to": "2026-12-31", "opens_on": "2026-10-01", "closes_on": "2026-12-31"}
}

// matrixNewEngagement opens an engagement as the matrix's Owner, and confirms it when active is true.
func matrixNewEngagement(e *env, active bool) string {
	matrixAuditMu.Lock()
	matrixAuditSeq++
	n := matrixAuditSeq
	matrixAuditMu.Unlock()
	owner := asMatrixMember(e, e.as(authz.Owner))
	r := e.call(http.MethodPost, "/v1/audit-engagements", owner, matrixOpenBody(e, fmt.Sprintf("Matrix engagement %d", n)))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's engagement = %d: %s", r.Status, r.Raw)
	}
	id := r.str("id")
	if active {
		if c := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/confirm", owner, nil); c.Status != http.StatusOK {
			e.t.Fatalf("confirming the matrix's engagement = %d: %s", c.Status, c.Raw)
		}
	}
	return id
}

func matrixActiveEngagement(e *env) string {
	matrixAuditMu.Lock()
	id := matrixAuditEng[e]
	matrixAuditMu.Unlock()
	if id == "" {
		id = matrixNewEngagement(e, true)
		matrixAuditMu.Lock()
		matrixAuditEng[e] = id
		matrixAuditMu.Unlock()
	}
	return id
}

// matrixNewRequest asks a request of the matrix's engagement as its Auditor, naming the matrix's member (an Owner) to answer it.
func matrixNewRequest(e *env, eng string) string {
	r := e.call(http.MethodPost, "/v1/audit-engagements/"+eng+"/requests", asMatrixMember(e, e.as(authz.Auditor)),
		map[string]any{"title": "Bank confirmation", "assignee_user_id": matrixAuditor(e)})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's request = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

func matrixItems(e *env, path string) int {
	items, _ := e.call(http.MethodGet, path, asMatrixMember(e, e.as(authz.Owner)), nil).Body["items"].([]any)
	return len(items)
}

func matrixRequestStatus(e *env, eng, id string) string {
	for _, x := range e.call(http.MethodGet, "/v1/audit-engagements/"+eng+"/requests", asMatrixMember(e, e.as(authz.Owner)), nil).Body["items"].([]any) {
		if m := x.(map[string]any); m["id"] == id {
			return m["status"].(string)
		}
	}
	return ""
}

// matrixAuditEngagements are the auditor's engagement rows of the authorisation matrix (FR-UX-42).
var matrixAuditEngagements = map[string]func(e *env, tok string) (response, func() bool){
	"listAuditEngagements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit-engagements", asMatrixMember(e, tok), nil), nil
	},
	"openAuditEngagement": func(e *env, tok string) (response, func() bool) {
		before := matrixItems(e, "/v1/audit-engagements")
		r := e.call(http.MethodPost, "/v1/audit-engagements", asMatrixMember(e, tok), matrixOpenBody(e, "Matrix open "+uuid.NewString()))
		return r, func() bool { return matrixItems(e, "/v1/audit-engagements") == before }
	},
	"confirmAuditEngagement": func(e *env, tok string) (response, func() bool) {
		id := matrixNewEngagement(e, false)
		r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/confirm", asMatrixMember(e, tok), nil)
		return r, func() bool {
			for _, x := range e.call(http.MethodGet, "/v1/audit-engagements", asMatrixMember(e, e.as(authz.Owner)), nil).Body["items"].([]any) {
				if m := x.(map[string]any); m["id"] == id && m["status"] != "pending" {
					return false
				}
			}
			return true
		}
	},
	"closeAuditEngagement": func(e *env, tok string) (response, func() bool) {
		id := matrixNewEngagement(e, true)
		r := e.call(http.MethodPost, "/v1/audit-engagements/"+id+"/close", asMatrixMember(e, tok), nil)
		return r, func() bool {
			for _, x := range e.call(http.MethodGet, "/v1/audit-engagements", asMatrixMember(e, e.as(authz.Owner)), nil).Body["items"].([]any) {
				if m := x.(map[string]any); m["id"] == id && m["status"] != "active" {
					return false
				}
			}
			return true
		}
	},
	"listAuditRequests": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit-engagements/"+matrixActiveEngagement(e)+"/requests", asMatrixMember(e, tok), nil), nil
	},
	"askAuditRequest": func(e *env, tok string) (response, func() bool) {
		eng := matrixActiveEngagement(e)
		before := matrixItems(e, "/v1/audit-engagements/"+eng+"/requests")
		r := e.call(http.MethodPost, "/v1/audit-engagements/"+eng+"/requests", asMatrixMember(e, tok), map[string]any{"title": "Matrix request", "assignee_user_id": matrixAuditor(e)})
		return r, func() bool { return matrixItems(e, "/v1/audit-engagements/"+eng+"/requests") == before }
	},
	"answerAuditRequest": func(e *env, tok string) (response, func() bool) {
		eng := matrixActiveEngagement(e)
		id := matrixNewRequest(e, eng)
		r := e.call(http.MethodPost, "/v1/audit-requests/"+id+"/answer", asMatrixMember(e, tok), map[string]any{"answer": "Attached"})
		return r, func() bool { return matrixRequestStatus(e, eng, id) == "open" }
	},
	"closeAuditRequest": func(e *env, tok string) (response, func() bool) {
		eng := matrixActiveEngagement(e)
		id := matrixNewRequest(e, eng)
		if a := e.call(http.MethodPost, "/v1/audit-requests/"+id+"/answer", asMatrixMember(e, e.as(authz.Owner)), map[string]any{"answer": "Attached"}); a.Status != http.StatusOK {
			e.t.Fatalf("answering the matrix's request = %d: %s", a.Status, a.Raw)
		}
		r := e.call(http.MethodPost, "/v1/audit-requests/"+id+"/close", asMatrixMember(e, tok), nil)
		return r, func() bool { return matrixRequestStatus(e, eng, id) == "answered" }
	},
	"listAuditSamples": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit-engagements/"+matrixActiveEngagement(e)+"/samples", asMatrixMember(e, tok), nil), nil
	},
	"drawAuditSample": func(e *env, tok string) (response, func() bool) {
		eng := matrixActiveEngagement(e)
		before := matrixItems(e, "/v1/audit-engagements/"+eng+"/samples")
		r := e.call(http.MethodPost, "/v1/audit-engagements/"+eng+"/samples", asMatrixMember(e, tok), map[string]any{"rule": map[string]any{"from": "2026-01-01", "to": "2026-12-31", "count": 5}})
		return r, func() bool { return matrixItems(e, "/v1/audit-engagements/"+eng+"/samples") == before }
	},
}
