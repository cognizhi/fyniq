//go:build local

package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/google/uuid"
)

// Voiding an asset registered by mistake (FR-FA-01, FR-GL-12; PRD-001 rev 0.151, p1-fixed-assets task 7.3, design D9),
// in Standard mode: a S$3,600 laptop over 36 months has posted October and November, S$100 each, by 5 December. Its
// void reverses both on their own dates, takes it off the register (listed with status=voided) and frees its bill line,
// which registers again. A second void is refused (ASSET-21); so is a void while a disposal of the asset stands, or while
// a month to reverse is in a closed period (ASSET-20).
// req: FR-FA-01
// req: FR-GL-12
// req: ledger/fixed-assets An asset registered by mistake is voided (rev 0.151)
func TestVoidingAFixedAsset(t *testing.T) {
	e := newEnv(t)
	s := inStandardMode(e)
	accountant := s.as(authz.Accountant)
	ctx := context.Background()
	register := func(bill string) response {
		t.Helper()
		r := s.call(http.MethodPost, "/v1/fixed-assets", accountant, map[string]any{"source": map[string]any{"document_id": bill, "line": 1}, "life_months": 36})
		if r.Status != http.StatusCreated {
			t.Fatalf("registerFixedAsset = %d: %s", r.Status, r.Raw)
		}
		return r
	}
	bill := s.laptopBill("3600.00", "SGD", "")
	id := register(bill).str("id")
	s.clock.Set(time.Date(2026, 12, 5, 4, 0, 0, 0, time.UTC))
	if _, err := journal.DepreciateDueAll(ctx, s.store, s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	g := s.call(http.MethodGet, "/v1/fixed-assets/"+id, accountant, nil)
	if g.str("accumulated_depreciation", "amount") != "200.00" {
		t.Fatalf("October and November: %s", g.Raw)
	}
	void := func(id, tag string, headers ...string) response {
		return s.call(http.MethodPost, "/v1/fixed-assets/"+id+"/void", accountant, map[string]any{"reason": "Registered from the wrong line"},
			append([]string{"If-Match", tag}, headers...)...)
	}

	// A preview writes nothing.
	if p := void(id, g.Header.Get("ETag"), "Prefer", "return=preview"); p.Status != http.StatusOK || p.Header.Get("Preference-Applied") != "return=preview" ||
		p.str("status") != "voided" {
		t.Fatalf("preview = %d: %s", p.Status, p.Raw)
	}
	if s.call(http.MethodGet, "/v1/fixed-assets/"+id, accountant, nil).str("status") != "in_use" {
		t.Fatalf("a preview voided the asset")
	}

	// Voided: both months reversed on their own dates.
	v := void(id, g.Header.Get("ETag"))
	if v.Status != http.StatusOK || v.str("status") != "voided" || v.str("accumulated_depreciation", "amount") != "0.00" {
		t.Fatalf("voidFixedAsset = %d: %s", v.Status, v.Raw)
	}
	var months []string
	for _, m := range v.Body["depreciation"].([]any) {
		x := m.(map[string]any)
		months = append(months, x["date"].(string)+" "+x["status"].(string))
	}
	if !slices.Equal(months, []string{"2026-10-31 reversed", "2026-11-30 reversed"}) {
		t.Errorf("the voided asset's months: %v", months)
	}
	s.do(func(ctx context.Context, u storage.UnitOfWork) error {
		entries, err := u.Ledger().OfSource(ctx, uuid.MustParse(id))
		if err != nil {
			return err
		}
		posted := map[uuid.UUID]time.Time{}
		for _, x := range entries {
			if x.ReversesEntryID == nil {
				posted[x.ID] = x.PostingDate
			}
		}
		reversed := 0
		for _, x := range entries {
			if x.ReversesEntryID != nil {
				reversed++
				if !x.PostingDate.Equal(posted[*x.ReversesEntryID]) || !strings.Contains(x.Memo, "Registered from the wrong line") {
					t.Errorf("a reversal on %s (%q), of a month posted on %s", x.PostingDate.Format(time.DateOnly), x.Memo, posted[*x.ReversesEntryID].Format(time.DateOnly))
				}
			}
		}
		if len(posted) != 2 || reversed != 2 {
			t.Errorf("%d months posted, %d reversed", len(posted), reversed)
		}
		return nil
	})

	// Off the register, on the voided list; its line registers again.
	if l := s.call(http.MethodGet, "/v1/fixed-assets", accountant, nil); strings.Contains(l.Raw, id) {
		t.Errorf("the register lists the voided asset: %s", l.Raw)
	}
	if l := s.call(http.MethodGet, "/v1/fixed-assets?status=voided", accountant, nil); !strings.Contains(l.Raw, id) {
		t.Errorf("the voided list: %s", l.Raw)
	}
	if l := s.call(http.MethodGet, "/v1/fixed-assets/unregistered", accountant, nil); !strings.Contains(l.Raw, bill) {
		t.Errorf("the line freed is not in the register: %s", l.Raw)
	}
	again := register(bill)
	if g := s.call(http.MethodGet, "/v1/fixed-assets/"+again.str("id"), accountant, nil); again.str("number") != "FA-0002" ||
		g.str("accumulated_depreciation", "amount") != "200.00" { // its months due post as it is registered
		t.Errorf("registered again: %s", g.Raw)
	}
	s.problemIs(void(id, v.Header.Get("ETag")), "voidFixedAsset", 409, asset.CodeVoided)
	s.problemIs(s.call(http.MethodPost, "/v1/fixed-assets/"+id+"/change", accountant, map[string]any{"name": "Renamed"}, "If-Match", v.Header.Get("ETag")),
		"changeFixedAsset", 409, asset.CodeVoided)

	// While a disposal of it stands, the disposal is voided first.
	s.disposalBooks()
	w := s.call(http.MethodPost, "/v1/fixed-assets/"+again.str("id")+"/disposals", accountant, map[string]any{"date": "2026-12-05", "proceeds": "none"},
		"If-Match", s.call(http.MethodGet, "/v1/fixed-assets/"+again.str("id"), accountant, nil).Header.Get("ETag"))
	if w.Status != http.StatusCreated {
		t.Fatalf("writing it off: %d %s", w.Status, w.Raw)
	}
	stands := void(again.str("id"), s.call(http.MethodGet, "/v1/fixed-assets/"+again.str("id"), accountant, nil).Header.Get("ETag"))
	s.problemIs(stands, "voidFixedAsset", 409, asset.CodeAssetVoid)
	if !strings.Contains(stands.Raw, "DSP-0001") {
		t.Errorf("the disposal that stands: %s", stands.Raw)
	}

	// A month to reverse in a closed period: October closed, the void waits for it to reopen.
	third := register(s.laptopBill("1200.00", "SGD", ""))
	s.do(func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		for _, p := range periods {
			if p.StartsOn.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
				_, err = u.Periods().Close(ctx, p.ID)
				return err
			}
		}
		t.Fatal("no October 2026")
		return nil
	})
	closed := void(third.str("id"), s.call(http.MethodGet, "/v1/fixed-assets/"+third.str("id"), accountant, nil).Header.Get("ETag"))
	s.problemIs(closed, "voidFixedAsset", 409, asset.CodeAssetVoid)
	if !strings.Contains(closed.Raw, "October 2026") || s.call(http.MethodGet, "/v1/fixed-assets/"+third.str("id"), accountant, nil).str("status") != "in_use" {
		t.Errorf("a month in a closed period: %s", closed.Raw)
	}

	// Every entry replays as it posted (ENG-06).
	s.do(func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	})
}

// In Founder mode a void needs a second person (FM-1; rev 0.151): asked for by one founder, it voids on the other's
// approval, and not before.
// req: FM-1
// req: FR-FA-01
func TestAFounderVoidsAnAssetWithTheOthersApproval(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	id, tag := e.registered("1600.00")
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/void", ahkow, map[string]any{"reason": "Not an asset"}, "If-Match", tag),
		"voidFixedAsset", 409, CodeAssetVoidApproval)
	asked := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/void-approval", ahkow, map[string]any{"reason": "Not an asset"}, "If-Match", tag)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectFixedAsset || asked.str("operation") != approval.OpFixedAssetVoid {
		t.Fatalf("requestFixedAssetVoid = %d: %s", asked.Status, asked.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/fixed-assets/"+id, ahkow, nil); g.str("status") != "in_use" {
		t.Fatalf("voided before the approval: %s", g.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/fixed-assets/"+id, ahkow, nil); g.str("status") != "voided" {
		t.Errorf("after the approval: %s", g.Raw)
	}
}

// In Founder mode a disposal's void is asked for by one founder and runs on the other's approval (FM-1; FR-FA-03): the
// write-off is reversed and the asset is in use again, as it stood before.
// req: FM-1
// req: FR-FA-03
func TestAFounderVoidsADisposalWithTheOthersApproval(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	id, tag := e.writtenOff()
	if g := e.call(http.MethodGet, "/v1/asset-disposals/"+id, ahkow, nil); g.str("status") != "posted" {
		t.Fatalf("the write-off: %s", g.Raw)
	}
	asked := e.call(http.MethodPost, "/v1/asset-disposals/"+id+"/void-approval", ahkow, map[string]any{"date": "2026-10-15", "reason": "Not written off"},
		"If-Match", tag)
	if asked.Status != http.StatusAccepted || asked.str("operation") != approval.OpAssetDisposalVoid {
		t.Fatalf("requestAssetDisposalVoid = %d: %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, "/v1/asset-disposals/"+id, ahkow, nil)
	if g.str("status") != "reversed" {
		t.Errorf("the disposal after the approval: %s", g.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/fixed-assets/"+g.str("asset_id"), ahkow, nil); a.str("status") != "in_use" || a.str("cost", "amount") != "1500.00" {
		t.Errorf("the asset after the void: %s", a.Raw)
	}
}
