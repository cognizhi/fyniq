//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func figuresOf(r response, pack string) string {
	b, _ := json.Marshal(r.Body["figures"])
	_ = pack
	return string(b)
}

// The board pack through the API (FR-RPT-06, FR-RPT-08; p2-reporting-planning), on kpiBooks: a month's pack is a draft whose figures read as
// they stand (preview), and publishing freezes them with their ledger revision, so a posting made afterwards does not change the pack a board
// read; the texts are a person's own; a published pack never changes and a correction is the next version; a reader limited to the board pack
// sees published packs only.
// req: FR-RPT-06
// req: FR-RPT-08
// req: QA-SEC-03
// asvs: V4.2.1
func TestABoardPackIsFrozenWhenPublishedAndReadAfterSignIn(t *testing.T) {
	e := newEnv(t)
	acc := kpiBooks(e)
	controller := e.person(uuid.New(), authz.Controller)
	accountant := e.person(uuid.New(), authz.Accountant)
	board := e.person(uuid.New(), authz.BoardViewer)
	bookkeeper := e.person(uuid.New(), authz.Bookkeeper)

	// Who builds and who publishes; a month the books have no period for.
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs", bookkeeper, map[string]any{"month": "2026-11"}), "createBoardPackDraft", 403, CodePackRole)
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs", controller, map[string]any{"month": "2031-01"}), "createBoardPackDraft", 422, "API-INPUT-02")
	made := e.call(http.MethodPost, "/v1/board-packs", accountant, map[string]any{"month": "2026-11"}, "Idempotency-Key", "bp-1")
	if made.Status != http.StatusCreated || made.str("status") != "draft" || made.Header.Get("ETag") != `"1"` || made.str("month") != "2026-11" {
		t.Fatalf("making the draft = %d: %s", made.Status, made.Raw)
	}
	id := made.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs", accountant, map[string]any{"month": "2026-11"}), "createBoardPackDraft", 409, storage.CodeBoardPackDraft)

	// The texts are a person's own, over the draft's ETag.
	texts := map[string]any{"highlights": "Revenue passed S$5,000.", "commentary": "Hosting rose with the new region.", "asks": "An introduction to two banks."}
	e.problemIs(e.call(http.MethodPut, "/v1/board-packs/"+id, accountant, texts), "editBoardPackDraft", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPut, "/v1/board-packs/"+id, bookkeeper, texts, "If-Match", `"1"`), "editBoardPackDraft", 403, CodePackRole)
	edited := e.call(http.MethodPut, "/v1/board-packs/"+id, accountant, texts, "If-Match", `"1"`)
	if edited.Status != http.StatusOK || edited.Header.Get("ETag") != `"2"` || edited.str("commentary") != "Hosting rose with the new region." {
		t.Fatalf("editing = %d: %s", edited.Status, edited.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/board-packs/"+id, accountant, texts, "If-Match", `"1"`), "editBoardPackDraft", 412, CodeStale)

	// A draft shows its figures as they read now, marked preview; the board viewer is not found it.
	draft := e.call(http.MethodGet, "/v1/board-packs/"+id, controller, nil)
	if draft.Status != http.StatusOK || draft.Body["preview"] != true || draft.Body["figures"] == nil {
		t.Fatalf("the draft = %d: %.400s", draft.Status, draft.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/board-packs/"+id, board, nil), "getBoardPack", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/board-packs", board, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("a board viewer lists a draft: %s", l.Raw)
	}

	// Only an Owner or Controller publishes; the figures are those at the revision, November's by hand (kpiBooks): revenue 6,000.00.
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", accountant, nil, "If-Match", `"2"`), "publishBoardPack", 403, CodePackRole)
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", controller, nil, "If-Match", `"1"`), "publishBoardPack", 412, CodeStale)
	pub := e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", controller, nil, "If-Match", `"2"`, "Idempotency-Key", "bp-pub")
	if pub.Status != http.StatusOK || pub.str("status") != "published" || pub.Body["ledger_revision"].(float64) < 1 || pub.Body["preview"] == true {
		t.Fatalf("publishing = %d: %.400s", pub.Status, pub.Raw)
	}
	frozen := figuresOf(pub, id)
	for _, want := range []string{`"pl"`, `"bs"`, `"cf"`, `"kpis"`, `"period_status"`, `6000.00`} {
		if !contains(frozen, want) {
			t.Errorf("the published figures lack %s", want)
		}
	}
	// A posting made afterwards does not change the pack; a draft read now would show it.
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(time.November, 28), PostingIdentity: "late:" + uuid.NewString() + ":v1:test@1", RuleVersion: "test@1",
			Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1}, Lines: []storage.NewLine{
				{AccountID: acc["1100"].ID, Currency: "SGD", Amount: "700.00", FXRate: "1", FXRateSource: "base", BaseAmount: "700.00"},
				{AccountID: acc["4010"].ID, Currency: "SGD", Amount: "-700.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-700.00"}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	again := e.call(http.MethodGet, "/v1/board-packs/"+id, board, nil)
	if again.Status != http.StatusOK || figuresOf(again, id) != frozen || again.Body["preview"] == true {
		t.Fatalf("the board viewer reads the published pack = %d, frozen %v", again.Status, figuresOf(again, id) == frozen)
	}
	// A published pack never changes; a correction is the next version, which supersedes it.
	e.problemIs(e.call(http.MethodPut, "/v1/board-packs/"+id, accountant, texts, "If-Match", `"3"`), "editBoardPackDraft", 409, CodePackNotDraft)
	e.problemIs(e.call(http.MethodPost, "/v1/board-packs/"+id+"/publish", controller, nil, "If-Match", `"3"`), "publishBoardPack", 409, CodePackNotDraft)
	v2 := e.call(http.MethodPost, "/v1/board-packs", controller, map[string]any{"month": "2026-11"})
	if v2.Status != http.StatusCreated || v2.Body["version"].(float64) != 2 || v2.str("supersedes") != id {
		t.Fatalf("the correction = %d: %s", v2.Status, v2.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/board-packs", board, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("a board viewer lists the published pack only: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/board-packs", controller, nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("a controller lists both versions: %s", l.Raw)
	}
}

// An investor update is built from a published pack, sent once to members who may read the pack, and each recipient's first open is recorded;
// a Board viewer it was not sent to is not found it, and the update never changes after it is sent (FR-EQ-23, FR-EQ-24).
// req: FR-EQ-23
// req: FR-EQ-24
// asvs: V4.2.1
func TestAnInvestorUpdateGoesOnceToThoseItIsSentTo(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	controller := e.person(uuid.New(), authz.Controller)
	pack := e.call(http.MethodPost, "/v1/board-packs", controller, map[string]any{"month": "2026-11"}).str("id")
	// From a draft pack, no update.
	e.problemIs(e.call(http.MethodPost, "/v1/investor-updates", controller, map[string]any{"pack_id": pack, "kind": "monthly", "title": "November"}), "createInvestorUpdate", 422, CodePackNotReady)
	if r := e.call(http.MethodPost, "/v1/board-packs/"+pack+"/publish", controller, nil, "If-Match", `"1"`); r.Status != http.StatusOK {
		t.Fatalf("publishing = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/investor-updates", e.person(uuid.New(), authz.Accountant), map[string]any{"pack_id": pack, "kind": "monthly", "title": "November"}), "createInvestorUpdate", 403, CodePackRole)
	made := e.call(http.MethodPost, "/v1/investor-updates", controller, map[string]any{"pack_id": pack, "kind": "monthly", "title": "November 2026 investor update"})
	if made.Status != http.StatusCreated || made.str("status") != "draft" || made.Header.Get("ETag") != `"1"` {
		t.Fatalf("making the update = %d: %s", made.Status, made.Raw)
	}
	id := made.str("id")
	edit := map[string]any{"title": "November 2026 investor update", "highlights": "Revenue up 50%.", "asks": "Introductions to two banks."}
	if r := e.call(http.MethodPut, "/v1/investor-updates/"+id, controller, edit, "If-Match", `"1"`); r.Status != http.StatusOK || r.Header.Get("ETag") != `"2"` {
		t.Fatalf("editing = %d: %s", r.Status, r.Raw)
	}

	investor := e.joined("Ivy", "board_viewer")
	outsider := e.joined("Otto", "board_viewer")
	// Recipients must be active members who may read the pack: a stranger is no member.
	e.problemIs(e.call(http.MethodPost, "/v1/investor-updates/"+id+"/send", controller, map[string]any{"recipients": []string{uuid.NewString()}}, "If-Match", `"2"`), "sendInvestorUpdate", 422, "API-INPUT-02")
	sent := e.call(http.MethodPost, "/v1/investor-updates/"+id+"/send", controller, map[string]any{"recipients": []string{investor.String()}}, "If-Match", `"2"`, "Idempotency-Key", "iu-send")
	if sent.Status != http.StatusOK || sent.str("status") != "sent" || len(sent.Body["recipients"].([]any)) != 1 {
		t.Fatalf("sending = %d: %s", sent.Status, sent.Raw)
	}
	// A sent update never changes and goes once.
	e.problemIs(e.call(http.MethodPut, "/v1/investor-updates/"+id, controller, edit, "If-Match", `"3"`), "editInvestorUpdate", 409, CodePackSent)
	e.problemIs(e.call(http.MethodPost, "/v1/investor-updates/"+id+"/send", controller, map[string]any{"recipients": []string{investor.String()}}, "If-Match", `"3"`), "sendInvestorUpdate", 409, CodePackSent)

	// The recipient sees it as unread, opens it with its pack, and then it is read; the one it was not sent to is not found it.
	ivy, otto := e.person(investor, authz.BoardViewer), e.person(outsider, authz.BoardViewer)
	list := e.call(http.MethodGet, "/v1/investor-updates", ivy, nil)
	if items := list.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["read"] != false {
		t.Fatalf("the recipient's list: %s", list.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/investor-updates", otto, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another board viewer lists the update: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/investor-updates/"+id, otto, nil), "getInvestorUpdate", 404, CodeNotFound)
	opened := e.call(http.MethodGet, "/v1/investor-updates/"+id, ivy, nil)
	if opened.Status != http.StatusOK || opened.Body["pack"] == nil || opened.Body["read"] == nil {
		t.Fatalf("opening = %d: %.300s", opened.Status, opened.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/investor-updates", ivy, nil); l.Body["items"].([]any)[0].(map[string]any)["read"] != true {
		t.Errorf("the update is not read after its recipient opened it: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/investor-updates", controller, nil); len(l.Body["items"].([]any)) != 1 ||
		len(l.Body["items"].([]any)[0].(map[string]any)["read_by"].([]any)) != 1 {
		t.Errorf("the sender sees who has read it: %s", l.Raw)
	}
}
