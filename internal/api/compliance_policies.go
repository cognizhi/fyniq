package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ The policy library (CMP-07; p1-compliance-center D8, D9)

// CodeUnknownPolicy: the library has no such policy, or no such version of it.
const CodeUnknownPolicy = "COMPLIANCE-03"

// AcknowledgeWithin is how long a member has to acknowledge a published version (CMP-07): the domain's rule.
const AcknowledgeWithin = controls.AcknowledgeWithin

// PolicyVersionView is a published version of a policy.
type PolicyVersionView struct {
	Version      int    `json:"version" doc:"Its number, from 1." example:"2"`
	Title        string `json:"title" doc:"The policy's title." example:"Data protection policy"`
	SHA256       string `json:"sha256" doc:"The SHA-256 of its text, so a copy can be checked." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	From         string `json:"from" doc:"What it was published from: Fyniq's starting draft (fyniq-policy-draft@1), or a revision of the organisation's draft." example:"revision 3"`
	PublishedBy  string `json:"published_by" doc:"Who published it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	PublishedAt  string `json:"published_at" format:"date-time" doc:"When." example:"2027-01-05T10:00:00Z"`
	Acknowledged int    `json:"acknowledged" doc:"How many members acknowledged it." example:"3"`
}

// MyPolicyAcknowledgement is the caller's acknowledgement of a policy's current version, or when it falls due.
type MyPolicyAcknowledgement struct {
	Version        int     `json:"version" doc:"The current version, which the caller acknowledges." example:"2"`
	DueOn          string  `json:"due_on" format:"date" doc:"30 days after the later of its publication and the caller joining." example:"2027-02-04"`
	AcknowledgedAt *string `json:"acknowledged_at,omitempty" format:"date-time" doc:"When the caller acknowledged it; none while it waits." example:"2027-01-06T09:00:00Z"`
	Overdue        bool    `json:"overdue" doc:"true once its due day has passed without an acknowledgement." example:"false"`
}

// PolicyDraftView is the draft a policy's next version is published from.
type PolicyDraftView struct {
	Source      string `json:"source" enum:"fyniq,organisation" doc:"fyniq: Fyniq's starting draft, not edited yet. organisation: a revision of the organisation's own." example:"organisation"`
	Revision    int    `json:"revision" doc:"The organisation's revision; 0 for Fyniq's draft. The draft's ETag carries it for If-Match." example:"3"`
	SHA256      string `json:"sha256" doc:"The SHA-256 of its text." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Body        string `json:"body,omitempty" doc:"Its text, as Markdown; only for those who may edit the library." example:"# Data protection policy\n\n## Purpose\n..."`
	Unpublished bool   `json:"unpublished" doc:"true while it differs from the current version: publishing it makes the next version." example:"true"`
}

// PolicyView is a policy of the library.
type PolicyView struct {
	Key      string                   `json:"key" enum:"information-security,acceptable-use,access-control,incident-response,backup,change-management,ai-use,data-protection,notice-members,notice-contacts" doc:"The policy's key." example:"data-protection"`
	Kind     string                   `json:"kind" enum:"policy,notice" doc:"policy: one of CMP-07's eight policies, which every member acknowledges. notice: a privacy notice (CMP-PD-03): the members' notice, which every member acknowledges, or the contacts' notice, whose summary goes in what Fyniq sends to contacts." example:"policy"`
	Title    string                   `json:"title" doc:"Its title." example:"Data protection policy"`
	Summary  string                   `json:"summary,omitempty" doc:"A notice's summary: the first paragraph of its version in force. The contacts' summary ends every email Fyniq sends to a contact." example:"We keep the names, email addresses and phone numbers of the people we deal with at our customers and suppliers..."`
	Current  *PolicyVersionView       `json:"current,omitempty" doc:"The version in force; none before the first is published."`
	Members  int                      `json:"members" doc:"How many members the current version applies to." example:"4"`
	Mine     *MyPolicyAcknowledgement `json:"mine,omitempty" doc:"The caller's acknowledgement of the current version; none before one is published, and none for the contacts' notice, which members do not acknowledge."`
	Draft    PolicyDraftView          `json:"draft" doc:"The draft its next version is published from."`
	Body     string                   `json:"body,omitempty" doc:"With getPolicy: the current version's text, as Markdown." example:"# Data protection policy\n\n## Purpose\n..."`
	Versions []PolicyVersionView      `json:"versions,omitempty" doc:"With getPolicy: every published version, oldest first."`
	Sources  []PolicySourceView       `json:"sources,omitempty" doc:"With getPolicy: the official sources Fyniq's starting draft was written from."`
}

// PolicySourceView is an official source a starting draft was written from.
type PolicySourceView struct {
	Title     string `json:"title" doc:"The source." example:"PDPC, Guide on Managing and Notifying Data Breaches under the PDPA"`
	URL       string `json:"url" format:"uri" doc:"Where it is." example:"https://www.pdpc.gov.sg/"`
	Retrieved string `json:"retrieved" format:"date" doc:"The day it was read." example:"2026-11-09"`
}

// Policies is the library.
type Policies struct {
	Items []PolicyView `json:"items" nullable:"false" doc:"The policies of CMP-07, the privacy notices of CMP-PD-03, or both, as kind asks, in the library's order."`
}

type (
	// PolicyPath is a policy's path parameter (exported: huma reads only an exported embedded struct).
	PolicyPath struct {
		Key string `path:"key" enum:"information-security,acceptable-use,access-control,incident-response,backup,change-management,ai-use,data-protection,notice-members,notice-contacts" doc:"The policy's key." example:"data-protection"`
	}
	// PolicyVersionPath is a published version's path parameters.
	PolicyVersionPath struct {
		Key     string `path:"key" enum:"information-security,acceptable-use,access-control,incident-response,backup,change-management,ai-use,data-protection,notice-members,notice-contacts" doc:"The policy's key." example:"data-protection"`
		Version int    `path:"version" minimum:"1" doc:"The version." example:"2"`
	}
	policiesOutput struct{ Body Policies }
	policiesInput  struct {
		Kind string `query:"kind" enum:"policy,notice,all" default:"policy" doc:"policy (the default): the eight policies; notice: the two privacy notices; all: both." example:"notice"`
	}
	policyOutput   struct {
		ETag string `header:"ETag" doc:"The draft's revision, for If-Match on savePolicyDraft and publishPolicyVersion."`
		Body PolicyView
	}
	policyDraftInput struct {
		PolicyPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body struct {
			Body string `json:"body" minLength:"1" maxLength:"100000" doc:"The draft's text, as Markdown." example:"# Data protection policy\n\n## Purpose\n..."`
		}
	}
	policyPublishInput struct {
		PolicyPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	policyAckInput struct {
		PolicyVersionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	policyWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The draft's revision, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PolicyView
	}
)

func draftETag(revision int) string { return etag("r" + strconv.Itoa(revision)) }

func policyAnswer(w written[PolicyView]) *policyWriteOutput {
	if w.Previewed {
		return &policyWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &policyWriteOutput{Status: w.Status, ETag: draftETag(w.Body.Draft.Revision), Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// policyViews are the library's policies for the caller: each with its current version, its draft (the text only for those
// who may edit), the caller's acknowledgement, and, with full, the current text, every version and the draft's sources.
func (a *API) policyViews(ctx context.Context, u storage.UnitOfWork, full bool, kind string) ([]PolicyView, error) {
	p, _ := authn.From(ctx)
	drafts, err := compliance.LibraryDrafts()
	if err != nil {
		return nil, err
	}
	lib, err := u.Compliance().Policies(ctx)
	if err != nil {
		return nil, err
	}
	now := a.deps.Clock.Now()
	members, err := u.Members().List(ctx, now)
	if err != nil {
		return nil, err
	}
	me := uploader(ctx)
	var joined time.Time
	for _, m := range members {
		if "user:"+m.UserID.String() == me {
			joined = m.Since
		}
	}
	editor := authz.Decide(authz.SavePolicyDraft, p.Roles, p.AI()).Allowed
	out := make([]PolicyView, 0, len(drafts))
	for _, d := range drafts {
		k := "policy"
		if compliance.IsNotice(d.Key) {
			k = "notice"
		}
		if kind != "all" && kind != k {
			continue
		}
		v := PolicyView{Key: d.Key, Kind: k, Title: d.Title, Members: len(members),
			Draft: PolicyDraftView{Source: "fyniq", SHA256: d.SHA256, Body: d.Body}}
		if i := slices.IndexFunc(lib.Drafts, func(x storage.PolicyDraft) bool { return x.Key == d.Key }); i >= 0 {
			x := lib.Drafts[i]
			v.Draft = PolicyDraftView{Source: "organisation", Revision: x.Revision, SHA256: hex.EncodeToString(x.SHA256), Body: x.Body}
		}
		var current *storage.PublishedPolicy
		for i, pv := range lib.Versions {
			if pv.Key != d.Key {
				continue
			}
			acks := 0
			for _, ack := range lib.Acknowledgements {
				if ack.Key == pv.Key && ack.Version == pv.Version {
					acks++
				}
			}
			view := PolicyVersionView{Version: pv.Version, Title: pv.Title, SHA256: hex.EncodeToString(pv.SHA256), From: pv.From,
				PublishedBy: pv.PublishedBy, PublishedAt: pv.PublishedAt.UTC().Format(time.RFC3339), Acknowledged: acks}
			if full {
				v.Versions = append(v.Versions, view)
			}
			current, v.Current = &lib.Versions[i], &view
		}
		v.Draft.Unpublished = current == nil || hex.EncodeToString(current.SHA256) != v.Draft.SHA256
		if current != nil && v.Kind == "notice" {
			v.Summary = organisation.NoticeSummary(current.Body)
		}
		if current != nil && compliance.Acknowledged(d.Key) {
			due := controls.AcknowledgeBy(current.PublishedAt, joined)
			mine := &MyPolicyAcknowledgement{Version: current.Version, DueOn: controls.Day(due).Format(time.DateOnly)}
			for _, ack := range lib.Acknowledgements {
				if ack.Key == current.Key && ack.Version == current.Version && ack.By == me {
					at := ack.At.UTC().Format(time.RFC3339)
					mine.AcknowledgedAt = &at
				}
			}
			mine.Overdue = mine.AcknowledgedAt == nil && now.After(due)
			v.Mine = mine
		}
		if current != nil && full {
			v.Body = current.Body
		}
		if !editor {
			v.Draft.Body = ""
		}
		if full {
			for _, s := range d.Sources {
				v.Sources = append(v.Sources, PolicySourceView{Title: s.Title, URL: s.URL, Retrieved: s.Retrieved})
			}
		} else {
			v.Draft.Body = ""
		}
		out = append(out, v)
	}
	return out, nil
}

func (a *API) onePolicy(ctx context.Context, u storage.UnitOfWork, key string) (PolicyView, error) {
	all, err := a.policyViews(ctx, u, true, "all")
	if err != nil {
		return PolicyView{}, err
	}
	for _, v := range all {
		if v.Key == key {
			return v, nil
		}
	}
	return PolicyView{}, problem(CodeUnknownPolicy, fmt.Sprintf("The library has no policy %q.", key), "Use a key from listPolicies.")
}

func registerPolicies(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodePolicy}, locks...)
	register(a, authz.ListPolicies, exempt(huma.Operation{
		OperationID: "listPolicies", Method: http.MethodGet, Path: "/v1/compliance/policies", Tags: []string{"Compliance"},
		Summary: "List the policy library",
		Description: "The eight policies of the library (CMP-07), or with kind the two privacy notices (CMP-PD-03): each with its version " +
			"in force, how many members acknowledged it, the caller's own acknowledgement and when it falls due, a notice's summary, and " +
			"the draft its next version would be published from. Read-only; no side effects.",
	}, "cursor", "The library holds CMP-07's eight policies and CMP-PD-03's two notices: the list is always whole and never grows."),
		append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *policiesInput) (*policiesOutput, error) {
			p, _ := authn.From(ctx)
			out := &policiesOutput{Body: Policies{Items: []PolicyView{}}}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out.Body.Items, err = a.policyViews(ctx, u, false, cmpOr(in.Kind, "policy"))
				return err
			})
			return out, err
		})

	register(a, authz.GetPolicy, exempt(huma.Operation{
		OperationID: "getPolicy", Method: http.MethodGet, Path: "/v1/compliance/policies/{key}", Tags: []string{"Compliance"},
		Summary:     "Get a policy",
		Description: "One policy as listPolicies shows it, with the text of its version in force, every published version, and the official sources Fyniq's starting draft was written from. Those who may edit the library also get the draft's text. Read-only; no side effects.",
	}, "cursor", "One policy, not a list: its versions are a handful, published by hand."), append([]string{CodeUnknownPolicy}, locks...),
		func(ctx context.Context, in *PolicyPath) (*policyOutput, error) {
			p, _ := authn.From(ctx)
			var v PolicyView
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				v, err = a.onePolicy(ctx, u, in.Key)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &policyOutput{ETag: draftETag(v.Draft.Revision), Body: v}, nil
		})

	register(a, authz.SavePolicyDraft, huma.Operation{
		OperationID: "savePolicyDraft", Method: http.MethodPut, Path: "/v1/compliance/policies/{key}/draft", Tags: []string{"Compliance"},
		Summary:     "Save a policy's draft",
		Description: "Saves the next revision of the organisation's draft of the policy (CMP-07), from Fyniq's starting draft or the last revision. Nothing is in force until it is published. Side effects: the revision, the audit event policy.draft_saved. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeUnknownPolicy, CodeStale, CodeIfMatchRequired}, writes...),
		func(ctx context.Context, in *policyDraftInput) (*policyWriteOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PolicyView, error) {
				before, err := a.onePolicy(ctx, u, in.Key)
				if err != nil {
					return PolicyView{}, err
				}
				if err := checkIfMatch(in.IfMatch, draftETag(before.Draft.Revision)); err != nil {
					return PolicyView{}, err
				}
				if strings.TrimSpace(in.Body.Body) == "" {
					return PolicyView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.body", Message: "is empty", Fix: "Write the policy's text"}})
				}
				if _, err := u.Compliance().SavePolicyDraft(ctx, storage.PolicyDraft{Key: in.Key, Revision: before.Draft.Revision + 1, Body: in.Body.Body,
					SavedBy: uploader(ctx), SavedAt: now}); err != nil {
					return PolicyView{}, err
				}
				return a.onePolicy(ctx, u, in.Key)
			})
			if err != nil {
				return nil, err
			}
			return policyAnswer(w), nil
		})

	register(a, authz.PublishPolicyVersion, huma.Operation{
		OperationID: "publishPolicyVersion", Method: http.MethodPost, Path: "/v1/compliance/policies/{key}/versions", Tags: []string{"Compliance"},
		Summary:     "Publish a policy's next version",
		Description: "Publishes the policy's draft (Fyniq's starting draft, or the organisation's latest revision) as its next version (CMP-07). A published version never changes, and every member is asked to acknowledge it within 30 days. A draft no different from the version in force is refused. Side effects: the version, the audit event policy.published. Needs If-Match with the draft's ETag, so what is published is what was read. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeUnknownPolicy, CodeStale, CodeIfMatchRequired}, writes...),
		func(ctx context.Context, in *policyPublishInput) (*policyWriteOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PolicyView, error) {
				before, err := a.onePolicy(ctx, u, in.Key)
				if err != nil {
					return PolicyView{}, err
				}
				if err := checkIfMatch(in.IfMatch, draftETag(before.Draft.Revision)); err != nil {
					return PolicyView{}, err
				}
				if !before.Draft.Unpublished {
					return PolicyView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "key", Message: "has no change to publish",
						Fix: "Change the draft with savePolicyDraft first"}})
				}
				body, from := before.Draft.Body, "revision "+strconv.Itoa(before.Draft.Revision)
				if before.Draft.Source == "fyniq" {
					drafts, err := compliance.LibraryDrafts()
					if err != nil {
						return PolicyView{}, err
					}
					i := slices.IndexFunc(drafts, func(d compliance.PolicyDraft) bool { return d.Key == in.Key })
					body, from = drafts[i].Body, drafts[i].Draft
				}
				version := 1
				if before.Current != nil {
					version = before.Current.Version + 1
				}
				if _, err := u.Compliance().PublishPolicy(ctx, storage.PublishedPolicy{Key: in.Key, Version: version, Title: before.Title, Body: body,
					From: from, PublishedBy: uploader(ctx), PublishedAt: now}); err != nil {
					return PolicyView{}, err
				}
				return a.onePolicy(ctx, u, in.Key)
			})
			if err != nil {
				return nil, err
			}
			return policyAnswer(w), nil
		})

	register(a, authz.AcknowledgePolicyVersion, huma.Operation{
		OperationID: "acknowledgePolicyVersion", Method: http.MethodPost, Path: "/v1/compliance/policies/{key}/versions/{version}/acknowledgements",
		Tags: []string{"Compliance"}, Summary: "Acknowledge a policy",
		Description: "Records that the caller read and accepts the published version (CMP-07): each member for themselves, signed in to Fyniq, never an integration or the AI, and once per version. Side effects: the acknowledgement, the audit event policy.acknowledged. Needs If-Match with the version's ETag (\"v<version>\"). Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeUnknownPolicy, CodeStale, CodeIfMatchRequired, CodeSessionRequired}, writes...),
		func(ctx context.Context, in *policyAckInput) (*policyWriteOutput, error) {
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() {
				return nil, problem(CodeSessionRequired, "A member acknowledges a policy for themselves, signed in to Fyniq: not an integration or the AI.",
					"Sign in to Fyniq in the browser and acknowledge it there.")
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PolicyView, error) {
				before, err := a.onePolicy(ctx, u, in.Key)
				if err != nil {
					return PolicyView{}, err
				}
				if !compliance.Acknowledged(in.Key) {
					return PolicyView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "key", Message: "is the contacts' notice, which members do not acknowledge",
						Fix: "Its summary goes in what Fyniq sends to contacts; acknowledge the members' notice or a policy"}})
				}
				if before.Current == nil || in.Version != before.Current.Version {
					return PolicyView{}, problem(CodeUnknownPolicy, fmt.Sprintf("Version %d of %s is not the version in force.", in.Version, in.Key),
						"Acknowledge the current version listPolicies shows.")
				}
				if before.Mine != nil && before.Mine.AcknowledgedAt != nil {
					return PolicyView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "version", Message: "is acknowledged already",
						Fix: "Nothing to do: you acknowledged it on " + *before.Mine.AcknowledgedAt}})
				}
				if err := checkIfMatch(in.IfMatch, etag("v"+strconv.Itoa(in.Version))); err != nil {
					return PolicyView{}, err
				}
				if err := u.Compliance().AcknowledgePolicy(ctx, storage.PolicyAcknowledgement{Key: in.Key, Version: in.Version, By: uploader(ctx),
					At: now}); err != nil {
					return PolicyView{}, err
				}
				return a.onePolicy(ctx, u, in.Key)
			})
			if err != nil {
				return nil, err
			}
			return policyAnswer(w), nil
		})
}
