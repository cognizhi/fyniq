package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// renderKnowledge renders the organisation's settings, approval policies and team mode into pages and publishes what changed (PRD-001
// AI-50, AI-56; p1-knowledge-base design D6 and the owner's decision 3). It runs in the transaction of the change that moved one of
// them, so the assistant never describes an old setting: a setting's change, a policy's and the team mode's are each already an
// approval object, and the page is a render of exactly what was approved, so it publishes with it, as the snapshot's reason says,
// with no second approval of the same facts. A page whose newest version a person wrote is never overwritten: that is a finding
// for a person (AI-56).
func (a *API) renderKnowledge(ctx context.Context, u storage.UnitOfWork) error {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return err
	}
	in := knowledge.RenderInput{}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	in.TeamMode = mode
	for _, d := range settings.Catalogue {
		changes, err := u.Settings().Changes(ctx, d.Key)
		if err != nil {
			return err
		}
		v := settings.On(d, changes, today)
		in.Settings = append(in.Settings, knowledge.RenderSetting{Key: d.Key, Group: d.Group, Label: d.Label, Value: v.Value, Recommended: d.Recommended,
			Reason: d.Reason, Changed: v.Value != d.Recommended, Fixed: d.Fixed})
	}
	versions, err := u.ApprovalPolicies().List(ctx)
	if err != nil {
		return err
	}
	own := map[string]storage.ApprovalPolicy{}
	for _, p := range versions {
		if cur, ok := own[p.ObjectType]; !ok || p.Version > cur.Version {
			own[p.ObjectType] = p
		}
	}
	types := make([]string, 0, len(approval.Policies))
	for t := range approval.Policies {
		types = append(types, t)
	}
	for _, t := range types {
		rp := knowledge.RenderPolicy{ObjectType: t, Label: strings.ReplaceAll(t, "_", " ")}
		if o, ok := own[t]; ok {
			rp.Changed = true
			for _, s := range o.Steps {
				rp.Approvers = append(rp.Approvers, s.Quorum)
			}
		} else {
			for _, s := range approval.Policies[t].Steps {
				rp.Approvers = append(rp.Approvers, s.Quorum)
			}
		}
		in.Policies = append(in.Policies, rp)
	}
	k := u.OrganisationKnowledge()
	latest, err := k.Latest(ctx)
	if err != nil {
		return err
	}
	have := map[string]storage.KBVersion{}
	for _, l := range latest {
		have[l.Key] = l
	}
	now := a.deps.Clock.Now()
	publish := map[string]int{}
	for _, p := range knowledge.Render(in) {
		prev, exists := have[p.Key]
		switch {
		case exists && prev.Origin != storage.KBOriginRendered:
			if prev.Body != p.Body { // a person's version stands: the difference is for a person to resolve
				open, err := k.OpenFindings(ctx, p.Key)
				if err != nil {
					return err
				}
				if !slicesContainsKind(open, "human_edit_conflict") {
					if _, err := k.AddFinding(ctx, storage.KBFinding{Kind: "human_edit_conflict", PageKey: p.Key, PageVersion: prev.Number, CreatedAt: now,
						Detail: "the settings now render a different page; your version stands until you resolve this"}); err != nil {
						return err
					}
				}
			}
			continue
		case exists && prev.Body == p.Body && prev.Title == p.Title && prev.Summary == p.Summary:
			continue
		}
		srcs := make([]orgSource, 0, len(p.Sources))
		for _, s := range p.Sources {
			srcs = append(srcs, orgSource{Kind: s.Kind, Ref: s.Ref, Title: s.Title})
		}
		sj, err := json.Marshal(srcs)
		if err != nil {
			return err
		}
		v, err := k.AddVersion(ctx, storage.KBVersion{Key: p.Key, Type: p.Type, Title: p.Title, Summary: p.Summary, Aliases: p.Aliases, Body: p.Body, Sources: sj,
			Origin: storage.KBOriginRendered, AuthorKind: "system", CreatedAt: now})
		if err != nil {
			return err
		}
		publish[p.Key] = v.Number
	}
	if len(publish) == 0 {
		return nil
	}
	_, err = k.Publish(ctx, publish, nil, nil, "Rendered from settings", now)
	return err
}

func slicesContainsKind(fs []storage.KBFinding, kind string) bool {
	for _, f := range fs {
		if f.Kind == kind {
			return true
		}
	}
	return false
}
