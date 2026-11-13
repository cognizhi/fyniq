package aimodel_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/ports"
)

var now = time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC)

func pages(ps ...[]string) func(after string) (ports.ModelPage, error) {
	return func(after string) (ports.ModelPage, error) {
		i := 0
		if after != "" {
			for i = range ps {
				if ps[i][0] == after {
					i++
					break
				}
			}
		}
		page := ports.ModelPage{}
		for _, id := range ps[i] {
			page.Models = append(page.Models, ports.ListedModel{ID: id})
		}
		if i < len(ps)-1 {
			page.Next = ps[i][0]
		}
		return page, nil
	}
}

// A list is read page by page to the provider's end: complete; a page cap or a later page's failure stops it partial,
// and a first page's failure makes it failed (AI-80, AI-84).
// req: AI-80
// req: AI-84
func TestReadsAreCompleteOrNot(t *testing.T) {
	list := pages([]string{"a", "b"}, []string{"c"}, []string{"d"})
	if ms, outcome, n, err := aimodel.Read(list, 50); err != nil || outcome != aimodel.Complete || n != 3 || len(ms) != 4 {
		t.Errorf("a whole list: %v %s %d %d", err, outcome, n, len(ms))
	}
	if ms, outcome, n, _ := aimodel.Read(list, 2); outcome != aimodel.Partial || n != 2 || len(ms) != 3 {
		t.Errorf("past the cap: %s %d %d", outcome, n, len(ms))
	}
	calls := 0
	broken := func(after string) (ports.ModelPage, error) {
		calls++
		if calls == 2 {
			return ports.ModelPage{}, errors.New("timeout")
		}
		return list(after)
	}
	if _, outcome, n, err := aimodel.Read(broken, 50); outcome != aimodel.Partial || n != 1 || err == nil {
		t.Errorf("a later page failing: %s %d %v", outcome, n, err)
	}
	if _, outcome, _, err := aimodel.Read(func(string) (ports.ModelPage, error) { return ports.ModelPage{}, errors.New("401") }, 50); outcome != aimodel.Failed || err == nil {
		t.Errorf("the first page failing: %s", outcome)
	}
}

// The list keeps itself current (AI-81, AI-84): a new model shows new for 7 days; a shutdown date ahead shows it
// deprecated and one passed takes it off; only a complete read marks a model no longer listed; a partial read keeps the
// last complete list; listed again, a model is back and callable.
// req: AI-81
// req: AI-84
func TestTheListKeepsItselfCurrent(t *testing.T) {
	soon, gone := now.Add(30*24*time.Hour), now.Add(-time.Hour)
	kept, added, removed := aimodel.Merge(nil, []ports.ListedModel{{ID: "m1"}, {ID: "m2", Shutdown: &soon}, {ID: "m3"}, {ID: "m3"}}, aimodel.Complete, now)
	if len(kept) != 3 || added != 3 || removed != 0 {
		t.Fatalf("the first read: %d %d %d", len(kept), added, removed)
	}
	if aimodel.State(kept[0], now) != aimodel.New || aimodel.State(kept[0], now.Add(8*24*time.Hour)) != aimodel.Available ||
		aimodel.State(kept[1], now) != aimodel.Deprecated {
		t.Errorf("states: %s %s", aimodel.State(kept[0], now), aimodel.State(kept[1], now))
	}
	later := now.Add(24 * time.Hour)
	kept, _, removed = aimodel.Merge(kept, []ports.ListedModel{{ID: "m1"}}, aimodel.Partial, later)
	if removed != 0 || !kept[2].Listed {
		t.Errorf("a partial read removed a model")
	}
	kept, _, removed = aimodel.Merge(kept, []ports.ListedModel{{ID: "m1"}, {ID: "m2", Shutdown: &gone}}, aimodel.Complete, later)
	if removed != 1 || kept[2].Listed || aimodel.State(kept[2], later) != aimodel.NoLongerListed || aimodel.Callable(kept[2], later) {
		t.Errorf("a complete read without m3: %d %+v", removed, kept[2])
	}
	if aimodel.State(kept[1], later) != aimodel.NoLongerListed || aimodel.Callable(kept[1], later) {
		t.Errorf("a shutdown date passed: %s", aimodel.State(kept[1], later))
	}
	refused := later
	kept[0].NotCallableAt = &refused
	if aimodel.State(kept[0], later) != aimodel.NotCallable {
		t.Errorf("a refused model: %s", aimodel.State(kept[0], later))
	}
	kept, added, _ = aimodel.Merge(kept, []ports.ListedModel{{ID: "m1"}, {ID: "m3"}}, aimodel.Complete, later)
	if added != 1 || !kept[2].Listed || kept[0].NotCallableAt != nil || kept[2].FirstListed != now {
		t.Errorf("listed again: %d %+v %+v", added, kept[0], kept[2])
	}
}
