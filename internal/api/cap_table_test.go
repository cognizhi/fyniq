//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// capLines are a cap table's lines as "holder class shares ownership voting", in order.
func capLines(t *testing.T, table any) []string {
	t.Helper()
	m, ok := table.(map[string]any)
	if !ok {
		t.Fatalf("no cap table: %v", table)
	}
	var out []string
	for _, x := range m["lines"].([]any) {
		l := x.(map[string]any)
		out = append(out, strings.Join([]string{l["holder_name"].(string), l["account_code"].(string), l["issued"].(string), capText(l["ownership"]),
			capText(l["voting"])}, " "))
	}
	return out
}

func capText(v any) string {
	s, _ := v.(string)
	return s
}

// The cap table at any date (FR-EQ-20), G.3's round recorded as shares issued before the books start: 1,000,000 founder
// shares on 2 Jan 2025, and 200,000 and 52,083 on 12 Jun 2025. On 11 Jun the founder holds every share; on 12 Jun they
// hold 79.8669…% (79.87), the investor 15.9733…% and the third subscriber 4.1597…%, each line listing the share issue it
// comes from, with one vote a share assumed while 3010 has no recorded class. compare shows both days side by side. The
// export is a CSV, a workbook or a PDF, each audited; the Board reads only its own holding, an Employee neither, and no
// operation writes the table.
// req: FR-EQ-20
// req: RND-DISPLAY
// req: equity/cap-table-and-investors Cap table at any date
// asvs: V8.2.1
func TestCapTable(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	founder := e.directorContact("Ah Kow")
	ct := e.call(http.MethodGet, "/v1/contacts/"+founder, weiling, nil)
	link := e.call(http.MethodPut, "/v1/contacts/"+founder+"/member", kow, map[string]any{"member_user_id": c.ahkow}, "If-Match", ct.Header.Get("ETag"))
	if r := e.call(http.MethodPost, "/v1/approvals/"+link.str("id")+"/approve", weiling, hashOf(link), "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("linking Ah Kow: %d %s", r.Status, r.Raw)
	}
	investor := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	safe := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Hatch Capital Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	ordinary := e.shareCapitalIn(e.books.Org)
	for _, x := range []struct{ holder, shares, price, on string }{
		{founder, "1000000", "0.01", "2025-01-02"}, {investor, "200000", "3.00", "2025-06-12"}, {safe, "52083", "2.40", "2025-06-12"},
	} {
		if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": x.holder, "class_account_id": ordinary, "shares": x.shares,
			"price": x.price, "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": x.on}); r.Status != http.StatusCreated {
			t.Fatalf("issuing %s: %d %s", x.shares, r.Status, r.Raw)
		}
	}

	r := e.call(http.MethodGet, "/v1/cap-table?at=2025-06-11&compare=2025-06-12", weiling, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("the cap table: %d %s", r.Status, r.Raw)
	}
	if got := capLines(t, r.Body); strings.Join(got, "|") != "Ah Kow 3010 1000000 100.00 100.00" || r.str("issued") != "1000000" {
		t.Errorf("on 11 Jun: %v %s", got, r.str("issued"))
	}
	want := "Ah Kow 3010 1000000 79.87 79.87|Hatch Capital Pte. Ltd. 3010 52083 4.16 4.16|Seedling Ventures Pte. Ltd. 3010 200000 15.97 15.97"
	if got := capLines(t, r.Body["compared"]); strings.Join(got, "|") != want {
		t.Errorf("on 12 Jun:\n%v\nwant %s", got, want)
	}
	compared := r.Body["compared"].(map[string]any)
	if compared["issued"] != "1252083" || compared["votes"] != "1252083" || compared["fully_diluted"] != "1252083" {
		t.Errorf("the totals on 12 Jun: %v", compared)
	}
	line := compared["lines"].([]any)[2].(map[string]any)
	moves := line["movements"].([]any)
	if mv := moves[0].(map[string]any); len(moves) != 1 || mv["kind"] != "issue" || mv["shares"] != "200000" || mv["effective_on"] != "2025-06-12" ||
		capText(mv["share_issue_id"]) == "" || mv["transaction_id"] != nil || line["votes_assumed"] != true || line["class_name"] != "Ordinary shares" {
		t.Errorf("the investor's line and its drill-down: %v", line)
	}
	// Left out, the day is the organisation's today, as Ah Kow, who holds the founder's shares, reads it.
	today := e.call(http.MethodGet, "/v1/cap-table", kow, nil)
	if today.str("at") != "2026-10-15" || strings.Join(capLines(t, today.Body), "|") != want || today.Body["holders"].([]any)[0].(map[string]any)["mine"] != true {
		t.Errorf("today, as Ah Kow: %s", today.Raw)
	}
	if bad := e.call(http.MethodGet, "/v1/cap-table?at=12-06-2025", weiling, nil); bad.Status != http.StatusUnprocessableEntity {
		t.Errorf("a day that is not a date: %d %s", bad.Status, bad.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/cap-table?at=2025-06-12", e.as(authz.Auditor), nil); a.Status != http.StatusOK {
		t.Errorf("the Auditor: %d %s", a.Status, a.Raw)
	}

	// The export: a row per holding and its total, a workbook, and a PDF; each audited.
	csv := e.call(http.MethodGet, "/v1/cap-table/export?at=2025-06-12&format=csv", weiling, nil)
	rows, _ := base64.StdEncoding.DecodeString(csv.str("content_base64"))
	if csv.Status != http.StatusOK || csv.str("file_name") != "cap-table-2025-06-12.csv" || !bytes.Contains(rows, []byte(`"Ah Kow","Ordinary shares","3010",1000000,79.87,1000000,79.87,1000000,79.87`)) ||
		!bytes.Contains(rows, []byte(`"Total","","",1252083,100.00,1252083,100.00,1252083,100.00`)) || csv.Body["rows"].(float64) != 3 {
		t.Errorf("the cap table as CSV: %d %s\n%s", csv.Status, csv.Raw, rows)
	}
	if x := e.call(http.MethodGet, "/v1/cap-table/export?at=2025-06-12", weiling, nil); x.Status != http.StatusOK || x.str("media_type") != mediaXLSX {
		t.Errorf("the cap table as a workbook: %d %.300s", x.Status, x.Raw)
	}
	pdf := e.call(http.MethodGet, "/v1/cap-table/export?at=2025-06-12&format=pdf", weiling, nil)
	printed, _ := base64.StdEncoding.DecodeString(pdf.str("content_base64"))
	if pdf.Status != http.StatusOK || pdf.str("file_name") != "cap-table-2025-06-12.pdf" || !bytes.HasPrefix(printed, []byte("%PDF-")) {
		t.Errorf("the cap table as a PDF: %d %.300s", pdf.Status, pdf.Raw)
	}
	if trail := auditTrail(t, e); strings.Count(trail, `"report":"cap_table"`) != 3 || !strings.Contains(trail, csv.str("sha256")) {
		t.Error("the exports were not audited")
	}

	// The Board reads its own holding, never the table; a Board member who is no holder holds nothing; an Employee neither.
	board := c.as(c.ahkow, authz.BoardViewer)
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table", board, nil), "getCapTable", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/export", board, nil), "exportCapTable", 403, CodeForbidden)
	mine := e.call(http.MethodGet, "/v1/me/holding?at=2025-06-12", board, nil)
	if mine.Status != http.StatusOK || mine.Body["linked"] != true || mine.str("holder", "ownership") != "79.87" || mine.str("issued") != "1252083" ||
		len(mine.Body["lines"].([]any)) != 1 || strings.Contains(mine.Raw, "Seedling") {
		t.Errorf("the Board's own holding: %d %s", mine.Status, mine.Raw)
	}
	none := e.call(http.MethodGet, "/v1/me/holding", c.as(c.mei, authz.BoardViewer), nil)
	if none.Status != http.StatusOK || none.Body["linked"] != false || none.Body["holder"] != nil || len(none.Body["lines"].([]any)) != 0 {
		t.Errorf("a Board member who holds nothing: %d %s", none.Status, none.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/me/holding", e.as(authz.Employee), nil), "getMyHolding", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table", e.as(authz.Employee), nil), "getCapTable", 403, CodeForbidden)
}

// matrixCapTable are the authorisation matrix's rows for the cap table (FR-EQ-20): reads only, so none changes anything.
var matrixCapTable = map[string]func(e *env, tok string) (response, func() bool){
	"getCapTable": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/cap-table", tok, nil), nil
	},
	"exportCapTable": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/cap-table/export?format=csv", tok, nil), nil
	},
	"getMyHolding": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/me/holding", tok, nil), nil
	},
}
