package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// What the knowledge-base administration screen reads (PRD-001 AI-55, AI-56; p1-knowledge-base task 8.1): the index of the
// platform's and the organisation's pages, a page's versions, the snapshots and the lint findings, and the one write that is
// the screen's own, resolving a finding. Like the notes and publication operations these are not offered to the assistant and
// are on the never-list for MCP and agent clients (§9.6).

// KBPageSummary is a page in the index.
type KBPageSummary struct {
	Key       string `json:"key" example:"org.expense-policy"`
	Source    string `json:"source" enum:"organisation,platform" doc:"organisation for the organisation's own page, platform for Fyniq's." example:"organisation"`
	Type      string `json:"type" example:"policy"`
	Title     string `json:"title" example:"Expense policy"`
	Summary   string `json:"summary" example:"How claims are made here."`
	DataClass string `json:"data_class" example:"general"`
	State     string `json:"state" enum:"published,draft,unpublished,platform" doc:"published when the live snapshot lists it; draft when a newer version waits for a person; unpublished when it has no published version yet; platform for Fyniq's own pages." example:"published"`
	Published int    `json:"published_version" doc:"The live snapshot's version; 0 when none." example:"1"`
	Newest    int    `json:"newest_version" doc:"Its newest version; 0 for a platform page." example:"2"`
	Origin    string `json:"origin" doc:"How its newest version was made: rendered, compiled, human_edited, overlay or rollback; empty for a platform page." example:"compiled"`
	Findings  int    `json:"open_findings" doc:"Lint findings open on it." example:"0"`
	Overlaid  bool   `json:"overlaid,omitempty" doc:"A platform page this organisation has an overlay of." example:"false"`
	BaseMoved bool   `json:"base_changed,omitempty" doc:"An overlay made before Fyniq changed the platform page it replaces: the assistant says it may be out of date until a person reviews it." example:"false"`
}

// KBPages is the index.
type KBPages struct {
	Items []KBPageSummary `json:"items" nullable:"false"`
}

// KBPageVersions are a page's versions, newest first.
type KBPageVersions struct {
	Key   string          `json:"key" example:"org.expense-policy"`
	Items []KBPageVersion `json:"items" nullable:"false"`
}

// KBSnapshots are the snapshots, newest first.
type KBSnapshots struct {
	Items []KBSnapshot `json:"items" nullable:"false"`
}

// KBFindingView is what the lint, or the weekly pass, found.
type KBFindingView struct {
	ID         string `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a30"`
	Kind       string `json:"kind" example:"human_edit_conflict"`
	Page       string `json:"page" example:"org.expense-policy"`
	Version    int    `json:"version" doc:"The version it is about." example:"2"`
	Detail     string `json:"detail" example:"The note changed after a person edited the page: the recompile waits beside it."`
	Status     string `json:"status" enum:"open,resolved" example:"open"`
	CreatedAt  string `json:"created_at" format:"date-time" example:"2026-11-19T08:00:00Z"`
	ResolvedBy string `json:"resolved_by,omitempty" example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	ResolvedAt string `json:"resolved_at,omitempty" format:"date-time" example:"2026-11-19T09:00:00Z"`
	Resolution string `json:"resolution,omitempty" example:"Kept the person's version."`
}

// KBFindings are the findings, newest first.
type KBFindings struct {
	Items []KBFindingView `json:"items" nullable:"false"`
}

type (
	kbIndexOut     struct{ Body KBPages }
	kbVersionsOut  struct{ Body KBPageVersions }
	kbSnapshotsOut struct{ Body KBSnapshots }
	kbFindingsOut  struct{ Body KBFindings }
	kbFindingsIn   struct {
		Status string `query:"status" enum:"open,resolved" required:"false" doc:"Only the open or only the resolved findings; left out, all." example:"open"`
	}
	// KBFindingPath is a finding's path parameter (exported: huma reads only an exported embedded struct).
	KBFindingPath struct {
		ID string `path:"finding_id" format:"uuid" doc:"The finding's ID." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a30"`
	}
	kbResolveIn struct {
		KBFindingPath
		KeyHeader
		PreferHeader
		Body struct {
			Resolution string `json:"resolution" minLength:"1" maxLength:"1000" doc:"What was done, in a sentence." example:"Kept the person's version."`
		}
	}
	kbResolveOut struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     KBFindingView
	}
)

func kbFindingView(f storage.KBFinding) KBFindingView {
	out := KBFindingView{ID: f.ID.String(), Kind: f.Kind, Page: f.PageKey, Version: f.PageVersion, Detail: f.Detail, Status: f.Status,
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339), Resolution: f.Resolution}
	if f.ResolvedBy != nil {
		out.ResolvedBy = "user:" + f.ResolvedBy.String()
	}
	if f.ResolvedAt != nil {
		out.ResolvedAt = f.ResolvedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func registerKnowledgeAdmin(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	reads := append([]string{CodeInvalid}, locks...)
	list := func(op huma.Operation, what string) huma.Operation {
		return exempt(op, "cursor", what+" are a few dozen, read whole: the page sorts and filters them itself.")
	}
	register(a, authz.ListKBPages, list(huma.Operation{
		OperationID: "listKBPages", Method: http.MethodGet, Path: "/v1/knowledge-base/pages", Tags: []string{"Knowledge base"},
		Summary: "List the knowledge base's pages",
		Description: "The index the assistant reads, as an administrator sees it (AI-55): Fyniq's own pages and the organisation's, each with its key, " +
			"title, summary, data class, state, published and newest version, how its newest version was made and its open findings. A platform " +
			"page the organisation has overlaid says so. Read-only; no side effects. On the never-list for MCP and agent clients (§9.6).",
	}, "The pages"), reads, func(ctx context.Context, _ *struct{}) (*kbIndexOut, error) {
		p, _ := authn.From(ctx)
		out := &kbIndexOut{Body: KBPages{Items: []KBPageSummary{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			k := u.OrganisationKnowledge()
			latest, err := k.Latest(ctx)
			if err != nil {
				return err
			}
			published, err := k.Published(ctx)
			if err != nil {
				return err
			}
			findings, err := k.Findings(ctx, "open", 500)
			if err != nil {
				return err
			}
			open := map[string]int{}
			for _, f := range findings {
				open[f.PageKey]++
			}
			overlaid := map[string]bool{}
			for _, v := range latest {
				state := "unpublished"
				switch {
				case published[v.Key] == v.Number:
					state = "published"
				case published[v.Key] > 0:
					state = "draft"
				}
				moved := false
				if v.BaseKey != "" {
					overlaid[v.BaseKey] = true
					base, ok, err := u.KnowledgeBase().PlatformPage(ctx, v.BaseKey, adminReader(p))
					if err != nil {
						return err
					}
					moved = ok && base.Digest != v.BaseDigest
				}
				out.Body.Items = append(out.Body.Items, KBPageSummary{Key: v.Key, Source: "organisation", Type: v.Type, Title: v.Title, Summary: v.Summary, DataClass: v.DataClass,
					State: state, Published: published[v.Key], Newest: v.Number, Origin: v.Origin, Findings: open[v.Key], BaseMoved: moved})
			}
			idx, err := u.KnowledgeBase().PlatformIndex(ctx, adminReader(p))
			if err != nil {
				return err
			}
			for _, e := range idx {
				out.Body.Items = append(out.Body.Items, KBPageSummary{Key: e.Key, Source: "platform", Type: e.Type, Title: e.Title, Summary: e.Summary, DataClass: e.DataClass,
					State: "platform", Overlaid: overlaid[e.Key], Findings: open[e.Key]})
			}
			return nil
		})
		sort.SliceStable(out.Body.Items, func(i, j int) bool {
			x, y := out.Body.Items[i], out.Body.Items[j]
			if x.Source != y.Source {
				return x.Source > y.Source // the organisation's own first
			}
			return x.Key < y.Key
		})
		return out, err
	})

	register(a, authz.ListKBPageVersions, list(huma.Operation{
		OperationID: "listKBPageVersions", Method: http.MethodGet, Path: "/v1/knowledge-base/pages/{key}/versions", Tags: []string{"Knowledge base"},
		Summary: "List a knowledge-base page's versions",
		Description: "Every version of an organisation page, or of the organisation's overlay of a platform page, newest first, with its text, sources, " +
			"origin, author and whether it is published, a draft or superseded (AI-55, AI-56), so any two can be compared. Read-only; no side effects. " +
			"On the never-list for MCP and agent clients (§9.6).",
	}, "A page's versions"), append([]string{CodeNotFound}, reads...), func(ctx context.Context, in *struct{ KBPagePath }) (*kbVersionsOut, error) {
		p, _ := authn.From(ctx)
		out := &kbVersionsOut{Body: KBPageVersions{Key: in.Key, Items: []KBPageVersion{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			k := u.OrganisationKnowledge()
			vs, err := k.Versions(ctx, in.Key)
			if err != nil {
				return err
			}
			if len(vs) == 0 {
				return problem(CodeNotFound, "The organisation has no page "+in.Key+".", "Choose a page from the index")
			}
			published, err := k.Published(ctx)
			if err != nil {
				return err
			}
			for _, v := range vs {
				out.Body.Items = append(out.Body.Items, kbVersionView(v, published, vs[0].Number))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.ListKBSnapshots, list(huma.Operation{
		OperationID: "listKBSnapshots", Method: http.MethodGet, Path: "/v1/knowledge-base/snapshots", Tags: []string{"Knowledge base"},
		Summary: "List the knowledge base's snapshots",
		Description: "The numbered snapshots of what the organisation has published (AI-56), newest first, each with who made it, when, why and the approval " +
			"request that signed it (none when an Owner published alone in Founder mode, which waits in the weekly review). Read-only; no side effects. " +
			"On the never-list for MCP and agent clients (§9.6).",
	}, "The snapshots"), reads, func(ctx context.Context, _ *struct{}) (*kbSnapshotsOut, error) {
		p, _ := authn.From(ctx)
		out := &kbSnapshotsOut{Body: KBSnapshots{Items: []KBSnapshot{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.OrganisationKnowledge().Snapshots(ctx, 200)
			for _, s := range all {
				out.Body.Items = append(out.Body.Items, kbSnapshotView(s))
			}
			return err
		})
		return out, err
	})

	register(a, authz.ListKBFindings, list(huma.Operation{
		OperationID: "listKBFindings", Method: http.MethodGet, Path: "/v1/knowledge-base/findings", Tags: []string{"Knowledge base"},
		Summary: "List the knowledge base's lint findings",
		Description: "What the lint and the compiler found on the organisation's pages (AI-52, AI-56): a stale or missing source, a number in no cited source, " +
			"a contradiction with a setting, or a recompile waiting beside a person's edit. An open finding on a page stops its publication until a person " +
			"resolves it. Newest first, up to 200. Read-only; no side effects. On the never-list for MCP and agent clients (§9.6).",
	}, "The findings"), reads, func(ctx context.Context, in *kbFindingsIn) (*kbFindingsOut, error) {
		p, _ := authn.From(ctx)
		out := &kbFindingsOut{Body: KBFindings{Items: []KBFindingView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.OrganisationKnowledge().Findings(ctx, in.Status, 200)
			for _, f := range all {
				out.Body.Items = append(out.Body.Items, kbFindingView(f))
			}
			return err
		})
		return out, err
	})

	register(a, authz.ResolveKBFinding, huma.Operation{
		OperationID: "resolveKBFinding", Method: http.MethodPost, Path: "/v1/knowledge-base/findings/{finding_id}/resolve", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusOK,
		Summary: "Resolve a knowledge-base finding",
		Description: "Closes an open finding with what was done about it (AI-56): a person fixed the page or its source, kept their own version, or accepted the " +
			"finding. A finding closes once. Side effects: the audit event kb_finding.resolved. Idempotent with Idempotency-Key; Prefer: return=preview saves " +
			"nothing. On the never-list for MCP and agent clients (§9.6).",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeKnowledge}, locks...),
		func(ctx context.Context, in *kbResolveIn) (*kbResolveOut, error) {
			p, _ := authn.From(ctx)
			if p.Class != authn.ClassUser || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "A person signed in resolves a finding.", "Sign in to Fyniq")
			}
			id, err := uuid.Parse(in.ID)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "finding_id", Message: "is not an ID", Fix: "Use the ID the findings list gives"}})
			}
			resolution := strings.TrimSpace(in.Body.Resolution)
			if resolution == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.resolution", Message: "is empty", Fix: "Say what was done about it"}})
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (KBFindingView, error) {
				k := u.OrganisationKnowledge()
				all, err := k.Findings(ctx, "", 1000)
				if err != nil {
					return KBFindingView{}, err
				}
				var found *storage.KBFinding
				for i := range all {
					if all[i].ID == id {
						found = &all[i]
					}
				}
				if found == nil {
					return KBFindingView{}, ports.ErrNotFound
				}
				if err := k.ResolveFinding(ctx, id, p.UserID, resolution, now); err != nil {
					return KBFindingView{}, err
				}
				found.Status, found.Resolution, found.ResolvedBy, found.ResolvedAt = "resolved", resolution, &p.UserID, &now
				return kbFindingView(*found), nil
			})
			if err != nil {
				return nil, err
			}
			return &kbResolveOut{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
