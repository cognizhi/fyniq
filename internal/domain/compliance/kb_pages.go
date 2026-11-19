package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// kbPages (CTL-AI-08, AI-52, AI-56; p1-knowledge-base task 9.3) is the nightly check of an organisation's published knowledge-base
// pages, read as the worker reads them: each page in the live snapshot still passes the structural lint (a summary, a source
// cited by every section and listed by the page, the notes it cites still there and, for a page made only from notes, every number
// in them), and each version was written by who its origin says (a compiled or rendered version by the compiler or the system, a
// human-edited one by a person), so a recompile never stands in a person's version. The check reads only the organisation's own
// tables: the platform's pages and the reader filter run in the API's role, which the worker does not hold, and are tested in CI.
func kbPages(ctx context.Context, u storage.UnitOfWork, _ time.Time) ([]Finding, string, error) {
	kb := u.OrganisationKnowledge()
	published, err := kb.Published(ctx)
	if err != nil {
		return nil, "", err
	}
	notes, err := kb.Notes(ctx)
	if err != nil {
		return nil, "", err
	}
	latest, err := kb.Latest(ctx)
	if err != nil {
		return nil, "", err
	}
	byKey := map[string][]storage.KBVersion{}
	for _, l := range latest {
		vs, err := kb.Versions(ctx, l.Key)
		if err != nil {
			return nil, "", err
		}
		byKey[l.Key] = vs
	}
	found, versions := kbPageFindings(published, notes, byKey)
	return found, fmt.Sprintf("%d published page(s) and %d version(s) of the organisation's pages", len(published), versions), nil
}

// kbPageFindings are the pages and versions that break the control, with how many versions were read.
func kbPageFindings(published map[string]int, notes []storage.KBNote, byKey map[string][]storage.KBVersion) ([]Finding, int) {
	var out []Finding
	text := map[string]string{}
	for _, n := range notes {
		text["note:"+n.Key] = n.Body
	}
	versions := 0
	for key, vs := range byKey {
		for _, v := range vs {
			versions++
			who := v.AuthorKind
			switch {
			case v.Origin == "human_edited" && who != "person":
				out = append(out, Finding{Kind: "kb_version", ID: fmt.Sprintf("%s v%d", key, v.Number), Detail: "A human-edited version not written by a person"})
			case (v.Origin == "compiled" || v.Origin == "rendered") && who == "person":
				out = append(out, Finding{Kind: "kb_version", ID: fmt.Sprintf("%s v%d", key, v.Number), Detail: "A " + v.Origin + " version written under a person's name"})
			}
		}
	}
	for key, n := range published {
		var v *storage.KBVersion
		for i := range byKey[key] {
			if byKey[key][i].Number == n {
				v = &byKey[key][i]
			}
		}
		id := fmt.Sprintf("%s v%d", key, n)
		if v == nil {
			out = append(out, Finding{Kind: "kb_page", ID: id, Detail: "The published version is not among the page's versions"})
			continue
		}
		var listed []struct{ Kind, Ref string }
		_ = json.Unmarshal(v.Sources, &listed)
		var refs []string
		onlyNotes := true
		for _, s := range listed {
			refs = append(refs, s.Kind+":"+s.Ref)
			onlyNotes = onlyNotes && s.Kind == "note"
		}
		resolve := func(ref string) (string, bool) {
			if t, ok := text[ref]; ok {
				return t, true
			}
			return "", !strings.HasPrefix(ref, "note:") // a setting, policy or platform page is checked when it is published
		}
		exists := func(k string) bool { return !strings.HasPrefix(k, "org.") || byKey[k] != nil }
		for _, f := range knowledge.LintOrgPage(knowledge.OrgPage{Key: key, Summary: v.Summary, Body: v.Body, Sources: refs}, resolve, exists) {
			if f.Rule == knowledge.OrgRuleNumber && !onlyNotes {
				continue // the numbers of a setting's or a platform page's text are checked when the page is published
			}
			out = append(out, Finding{Kind: "kb_page", ID: id, Detail: f.Rule + ": " + f.Detail})
		}
	}
	return out, versions
}
