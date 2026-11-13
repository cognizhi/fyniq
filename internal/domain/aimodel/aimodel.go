// Package aimodel keeps each AI connection's list of models current from its provider's own list (PRD-001 AI-80,
// AI-81, AI-84; p1-ai-providers design D3). A read is complete when every page was read to the provider's end within
// the page cap and each matched the adapter's schema; partial when the cap or a later page stopped it; failed when the
// first page did. Only a complete read marks a model no longer listed; a partial or failed one keeps the last complete
// list. A provider's own shutdown date passed takes a model off whatever the read. Models are never removed, so their
// history stays.
package aimodel

import (
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// CodeListUnavailable is a connection switched on without a complete read of its model list: PRD-001's
// AI_MODEL_LIST_UNAVAILABLE (AI-80).
const CodeListUnavailable = "AI-MODEL-01"

// The outcomes of a read (AI-84).
const (
	Complete = "complete"
	Partial  = "partial"
	Failed   = "failed"
)

// Why a list was read (AI-80).
const (
	ByCredential = "credential" // a key was approved
	ByTest       = "test"
	ByEnable     = "enable"
	ByDaily      = "daily" // 06:00 SGT
	ByNotFound   = "not_found"
	ByRefresh    = "refresh" // a person asked
)

// The states a listed model shows (AI-81).
const (
	New            = "new" // first listed in the last 7 days
	Available      = "available"
	Deprecated     = "deprecated" // the provider gives a shutdown date still ahead
	NoLongerListed = "no_longer_listed"
	NotCallable    = "not_callable"
)

// NewFor is how long a model shows as new.
const NewFor = 7 * 24 * time.Hour

// DefaultPageCap is how many pages a read follows before it stops, partial.
const DefaultPageCap = 50

// Model is a listed model as an organisation keeps it.
type Model struct {
	ID            string // the provider's
	Name          string
	AliasOf       string
	Created       *time.Time
	Shutdown      *time.Time
	ContextWindow int
	MaxOutput     int
	Reports       []string
	FirstListed   time.Time
	LastListed    time.Time
	Listed        bool // listed by the last complete read
	NotCallableAt *time.Time
}

// State is the state m shows at now (AI-81).
func State(m Model, now time.Time) string {
	switch {
	case m.NotCallableAt != nil:
		return NotCallable
	case !m.Listed, m.Shutdown != nil && !m.Shutdown.After(now):
		return NoLongerListed
	case m.Shutdown != nil:
		return Deprecated
	case now.Sub(m.FirstListed) < NewFor:
		return New
	}
	return Available
}

// Callable reports whether m may serve: listed, not past its shutdown, and not refused by the provider (AI-81).
func Callable(m Model, now time.Time) bool {
	s := State(m, now)
	return s != NoLongerListed && s != NotCallable
}

// Read reads a provider's list page by page to cap pages, and says whether it was complete (AI-84).
func Read(list func(after string) (ports.ModelPage, error), cap int) (models []ports.ListedModel, outcome string, pages int, err error) {
	after := ""
	for pages < cap {
		page, err := list(after)
		if err != nil {
			if pages == 0 {
				return nil, Failed, 0, err
			}
			return models, Partial, pages, err
		}
		pages++
		models = append(models, page.Models...)
		if page.Next == "" {
			return models, Complete, pages, nil
		}
		after = page.Next
	}
	return models, Partial, pages, nil
}

// Merge applies a read at now to the models kept: each listed model added, or refreshed with what the list reports
// now and callable again; after a complete read, each kept model it does not list marked no longer listed. It returns
// every model kept, and how many the read added and removed.
func Merge(kept []Model, listed []ports.ListedModel, outcome string, now time.Time) (out []Model, added, removed int) {
	out = slices.Clone(kept)
	index := map[string]int{}
	for i, m := range out {
		index[m.ID] = i
	}
	seen := map[string]bool{}
	for _, l := range listed {
		if l.ID == "" || seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		i, ok := index[l.ID]
		if !ok {
			out = append(out, Model{ID: l.ID, FirstListed: now})
			i = len(out) - 1
			index[l.ID] = i
			added++
		} else if !out[i].Listed {
			added++ // listed again
		}
		m := &out[i]
		m.Name, m.AliasOf, m.Created, m.Shutdown, m.ContextWindow, m.MaxOutput = l.Name, l.AliasOf, l.Created, l.Shutdown, l.ContextWindow, l.MaxOutput
		m.Reports = append([]string{}, l.Reports...)
		m.LastListed, m.Listed, m.NotCallableAt = now, true, nil
	}
	if outcome == Complete {
		for i := range out {
			if !seen[out[i].ID] && out[i].Listed {
				out[i].Listed = false
				removed++
			}
		}
	}
	return out, added, removed
}
