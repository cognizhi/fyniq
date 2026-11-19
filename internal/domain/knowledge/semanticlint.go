package knowledge

import (
	"fmt"
	"sort"
)

// The weekly semantic lint's deterministic half (PRD-001 AI-52, AI-56; p1-knowledge-base design D7, task 6.3): what a person should look at
// across an organisation's whole knowledge base, found without a model. A model's contradiction and cross-reference pass is the other
// half (`[live-verify]`); every finding here is raised as a lint finding for the Owner and the Administrator, one open finding of a kind per
// page, and none of them publishes or changes anything.
const (
	SemStale       = "stale"                   // a note changed since its page was compiled from it
	SemNoPage      = "source_without_page"     // a note with no page at all
	SemSuperseded  = "superseded"              // an overlay of a platform page that Fyniq has changed or removed since
	SemMissingLink = "missing_cross_reference" // a [[link]] in a published page that matches no page
	SemOrphan      = "orphan"                  // a published page no other page links to and that links to none
)

// orphanFloor is the number of published pages below which no page is called an orphan: a handful of pages need no web of links.
const orphanFloor = 4

// SemNote is a note and what its page is.
type SemNote struct {
	Key     string
	Version int
	// PageVersion is the newest version of the note's page, and PageNoteVersion the version of the note it was compiled from; both zero
	// when the note has no page.
	PageVersion     int
	PageNoteVersion int
}

// SemLive is a published organisation page.
type SemLive struct {
	Key  string
	Body string
	// BaseKey, BaseDigest are the platform page an overlay replaces and its digest then; PlatformDigest is its digest now, and
	// PlatformGone is true when the release no longer has the page.
	BaseKey        string
	BaseDigest     string
	PlatformDigest string
	PlatformGone   bool
}

// SemFinding is one thing for a person to look at.
type SemFinding struct {
	Kind    string
	PageKey string
	Version int
	Detail  string
}

// PageKeyOfNote is the key of the page made from a note.
func PageKeyOfNote(note string) string { return "org." + note }

// SemanticLint is the deterministic findings across an organisation's notes and published pages, in a stable order. exists says whether
// a [[link]] names a page (the organisation's, published or not, or the platform's).
func SemanticLint(notes []SemNote, live []SemLive, exists func(key string) bool) []SemFinding {
	var out []SemFinding
	for _, n := range notes {
		key := PageKeyOfNote(n.Key)
		switch {
		case n.PageVersion == 0:
			out = append(out, SemFinding{Kind: SemNoPage, PageKey: key, Detail: fmt.Sprintf("the note %s has no page yet: recompile, or write the page", n.Key)})
		case n.PageNoteVersion > 0 && n.PageNoteVersion < n.Version:
			out = append(out, SemFinding{Kind: SemStale, PageKey: key, Version: n.PageVersion,
				Detail: fmt.Sprintf("the note %s is at version %d and its page was compiled from version %d: recompile, or edit the page to match", n.Key, n.Version, n.PageNoteVersion)})
		}
	}
	inbound := map[string]int{}
	outbound := map[string]int{}
	for _, p := range live {
		for _, l := range Links(p.Body) {
			if l == p.Key {
				continue
			}
			outbound[p.Key]++
			inbound[l]++
			if !exists(l) {
				out = append(out, SemFinding{Kind: SemMissingLink, PageKey: p.Key, Detail: "the page links to [[" + l + "]], which matches no page"})
			}
		}
		if p.BaseKey != "" {
			switch {
			case p.PlatformGone:
				out = append(out, SemFinding{Kind: SemSuperseded, PageKey: p.Key, Detail: "Fyniq no longer has the page " + p.BaseKey + " this overlay replaces: decide whether to keep it"})
			case p.PlatformDigest != p.BaseDigest:
				out = append(out, SemFinding{Kind: SemSuperseded, PageKey: p.Key, Detail: "Fyniq changed the page " + p.BaseKey + " after this overlay was made: compare the two and update the overlay"})
			}
		}
	}
	if len(live) >= orphanFloor {
		for _, p := range live {
			if inbound[p.Key] == 0 && outbound[p.Key] == 0 {
				out = append(out, SemFinding{Kind: SemOrphan, PageKey: p.Key, Detail: "no page links to this page and it links to none: link it from the page it belongs with, or retire it"})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PageKey != out[j].PageKey {
			return out[i].PageKey < out[j].PageKey
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// BlocksPublication says whether an open finding of this kind stops its page's publication. The weekly pass's advisory kinds (a stale page,
// a note with no page, a superseded overlay, a link to no page, an orphan) never do: the new version that answers one is the page's
// publication, and a person closes the finding when it is. A conflict with a person's edit, a contradiction, a number in no source, a bad
// citation and a broken front matter do.
func BlocksPublication(kind string) bool {
	switch kind {
	case SemStale, SemNoPage, SemSuperseded, SemMissingLink, SemOrphan:
		return false
	}
	return true
}
