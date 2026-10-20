package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// ------------------------------------------------------------------ statutory packs (ENG-30, ENG-32, SOD-10, FR-ORG-05, rev 0.41)

// PackSource is an official source a pack version's values come from (ENG-30).
type PackSource struct {
	ID        string `json:"id" doc:"The source's ID in the pack, which each row cites." example:"iras-gst-rate"`
	Title     string `json:"title" doc:"Its title." example:"GST rate change"`
	URL       string `json:"url" format:"uri" doc:"The official page." example:"https://www.iras.gov.sg/taxes/goods-services-tax-(gst)"`
	Section   string `json:"section" doc:"The section of the page the values come from." example:"Current GST rate"`
	Kind      string `json:"kind" doc:"The kind of source: law, regulation, official guidance, contract term or product policy." example:"official guidance"`
	Retrieved string `json:"retrieved" format:"date" doc:"When it was read." example:"2026-10-20"`
}

// PackPinnedResult is a pinned statutory test of a version, run against its own tables (ENG-32).
type PackPinnedResult struct {
	Table string `json:"table" doc:"The table it tests." example:"rates"`
	Case  string `json:"case" doc:"The case's name." example:"G4-001"`
	Why   string `json:"why" doc:"What it checks, worked by hand from the official page." example:"The rate on 1 Jan 2024 is 9%"`
	Pass  bool   `json:"pass" doc:"true when the table gives what the case wants." example:"true"`
	Got   string `json:"got" doc:"What the table gives." example:"0.09"`
	Want  string `json:"want" doc:"What the case wants." example:"0.09"`
}

// PackActivation is the organisation's activation of a version: who proposed it, who approved it and the request.
type PackActivation struct {
	ProposedBy  string    `json:"proposed_by" format:"uuid" doc:"Who asked to activate it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ApprovedBy  string    `json:"approved_by" format:"uuid" doc:"Who approved it: never the proposer (SOD-10)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	ApprovalID  string    `json:"approval_id" format:"uuid" doc:"The approved request." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	ActivatedAt time.Time `json:"activated_at" doc:"When it was activated." example:"2026-10-20T09:30:00Z"`
}

// StatutoryPack is a statutory pack version the release loaded, and whether the organisation has activated it.
type StatutoryPack struct {
	ID            string          `json:"id" doc:"The version: <pack>@<year>.<n>." example:"sg-gst@2026.1"`
	Pack          string          `json:"pack" doc:"The pack." example:"sg-gst"`
	Title         string          `json:"title" doc:"What it holds." example:"Singapore GST rates and tax codes"`
	EffectiveFrom string          `json:"effective_from" format:"date" doc:"The first day it is in force." example:"2026-01-01"`
	EffectiveTo   string          `json:"effective_to,omitempty" format:"date" doc:"The last day it is in force; absent while no later version ends it." example:"2026-12-31"`
	ReviewDate    string          `json:"review_date" format:"date" doc:"When it is due for review (ENG-30)." example:"2027-01-31"`
	Digest        string          `json:"digest" doc:"The SHA-256 of the version's files: a loaded version never changes." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Readable      bool            `json:"readable" doc:"true when this release carries the version exactly as it was loaded, so its pinned tests can run; only then can it be activated." example:"true"`
	PinnedPassed  int             `json:"pinned_passed" doc:"Its pinned statutory tests that pass." example:"22"`
	PinnedTotal   int             `json:"pinned_total" doc:"Its pinned statutory tests." example:"22"`
	Activation    *PackActivation `json:"activation,omitempty" doc:"The organisation's activation; absent until it is activated (ENG-32)."`
}

// StatutoryPackDetail is a version with its sources and the result of each pinned test.
type StatutoryPackDetail struct {
	StatutoryPack
	Verifier   string             `json:"verifier" doc:"Who verified it against the sources." example:"Ah Kow Lim"`
	Conditions string             `json:"conditions" doc:"The conditions it holds under, as the pack states them." example:"GST-registered businesses"`
	Sources    []PackSource       `json:"sources" doc:"The official sources its values come from."`
	Pinned     []PackPinnedResult `json:"pinned" doc:"Each pinned statutory test and its result."`
}

// StatutoryPackPage is a page of the loaded versions, by ID.
type StatutoryPackPage struct {
	Items      []StatutoryPack `json:"items" doc:"The versions on this page."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6InNnLWdzdEAyMDI2LjEifQ"`
}

// StatutoryPackPath names a version.
type StatutoryPackPath struct {
	VersionID string `path:"version_id" maxLength:"60" pattern:"^[a-z][a-z0-9-]*@[0-9]{4}\\.[0-9]+$" doc:"The version: <pack>@<year>.<n>." example:"sg-gst@2026.1"`
}

type packListInput struct{ PageQuery }

type packGetInput struct{ StatutoryPackPath }

type packActivateInput struct {
	StatutoryPackPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type packListOutput struct{ Body StatutoryPackPage }

type packReadOutput struct {
	ETag string `header:"ETag" doc:"\"0\" until the organisation activates the version, then \"1\": send it as If-Match to ask to activate it."`
	Body StatutoryPackDetail
}

// packETag is a version's ETag in the organisation: whether it has activated it.
func packETag(act *storage.PackActivation) string {
	if act != nil {
		return etag(1)
	}
	return etag(0)
}

type packActivateOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

// PackCodes are the rules of activating a pack version.
var PackCodes = []string{pack.CodeTable, pack.CodePinned, pack.CodeApproval, pack.CodeActive}

// releasePacks are the pack versions this release carries (packs.FS), read once.
var releasePacks = sync.OnceValues(func() ([]pack.Version, error) { return pack.LoadAll(packs.FS) })

// readable returns the release's copy of a loaded version, when it carries it with the same digest.
func readable(v storage.PackVersion) (pack.Version, bool, error) {
	all, err := releasePacks()
	if err != nil {
		return pack.Version{}, false, err
	}
	i := slices.IndexFunc(all, func(r pack.Version) bool { return r.ID == v.ID && r.Digest == v.Digest })
	if i < 0 {
		return pack.Version{}, false, nil
	}
	return all[i], true, nil
}

// loadedPack returns a loaded version and the organisation's activation of it (nil until activated), or
// ports.ErrNotFound.
func loadedPack(ctx context.Context, u storage.UnitOfWork, id string) (storage.PackVersion, *storage.PackActivation, error) {
	vs, err := u.Packs().Versions(ctx)
	if err != nil {
		return storage.PackVersion{}, nil, err
	}
	i := slices.IndexFunc(vs, func(v storage.PackVersion) bool { return v.ID == id })
	if i < 0 {
		return storage.PackVersion{}, nil, ports.ErrNotFound
	}
	acts, err := u.Packs().Activations(ctx)
	if err != nil {
		return storage.PackVersion{}, nil, err
	}
	if j := slices.IndexFunc(acts, func(a storage.PackActivation) bool { return a.VersionID == id }); j >= 0 {
		return vs[i], &acts[j], nil
	}
	return vs[i], nil, nil
}

func packOf(v storage.PackVersion, act *storage.PackActivation) (StatutoryPack, pack.Version, error) {
	out := StatutoryPack{ID: v.ID, Pack: v.Pack, EffectiveFrom: dateOf(v.EffectiveFrom), ReviewDate: dateOf(v.ReviewDate), Digest: v.Digest}
	if v.EffectiveTo != nil {
		out.EffectiveTo = dateOf(*v.EffectiveTo)
	}
	if act != nil {
		out.Activation = &PackActivation{ProposedBy: act.ProposedBy.String(), ApprovedBy: act.ApprovedBy.String(), ApprovalID: act.Approval.String(),
			ActivatedAt: act.ActivatedAt.UTC()}
	}
	rv, ok, err := readable(v)
	if err != nil || !ok {
		return out, rv, err
	}
	out.Readable, out.Title = true, rv.Title
	results := pack.RunPinned(rv)
	out.PinnedTotal, out.PinnedPassed = len(results), len(results)-len(pack.Failed(results))
	return out, rv, nil
}

// packProblem is the first reason a version may not be activated, as an API problem.
func packProblem(x pack.Problem) error {
	return problem(x.Code, fmt.Sprintf("%s: %s.", x.Field, x.Message), x.Fix)
}

// packExecutable checks, as the approved activation is about to execute, that the release still reads the version
// and its pinned tests pass, and that the approver is not the proposer (ENG-32, SOD-10).
func (a *API) packExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	v, _, err := loadedPack(ctx, u, r.ObjectKey)
	if err != nil {
		return err
	}
	rv, ok, err := readable(v)
	if err != nil {
		return err
	}
	var x pack.Problems
	if !ok {
		x = pack.Problems{{Code: pack.CodeTable, Field: "version_id", Message: "this release does not carry " + v.ID + " as it was loaded",
			Fix: "Activate it from a release that carries it"}}
	} else {
		x = pack.CheckActivation(rv, pack.Activation{ProposedBy: r.RequestedBy, ApprovedBy: signer, Approval: r.ID})
	}
	if len(x) == 0 {
		return nil
	}
	return &blocked{id: r.ID, version: r.Version, status: approval.Void, reason: "the pack version can no longer be activated",
		problem: problem(x[0].Code, fmt.Sprintf("The pack version can no longer be activated: %s, so nothing was changed and the request is void.", x[0].Message),
			x[0].Fix)}
}

func registerPacks(a *API) {
	register(a, authz.ListStatutoryPacks, huma.Operation{
		OperationID: "listStatutoryPacks", Method: http.MethodGet, Path: "/v1/statutory-packs", Tags: []string{"Statutory packs"},
		Summary: "List the statutory pack versions",
		Description: "Every statutory pack version the release loaded (GST rates and codes, and later CPF, SDL and SHG), by ID: its effective " +
			"dates, review date and digest, how many of its pinned statutory tests pass, and the organisation's activation of it (ENG-30, " +
			"ENG-32, FR-ORG-05). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *packListInput) (*packListOutput, error) {
		p, _ := authn.From(ctx)
		var all []StatutoryPack
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			vs, err := u.Packs().Versions(ctx)
			if err != nil {
				return err
			}
			for _, v := range vs {
				_, act, err := loadedPack(ctx, u, v.ID)
				if err != nil {
					return err
				}
				sp, _, err := packOf(v, act)
				if err != nil {
					return err
				}
				all = append(all, sp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(v StatutoryPack) string { return v.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &packListOutput{Body: StatutoryPackPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.GetStatutoryPack, huma.Operation{
		OperationID: "getStatutoryPack", Method: http.MethodGet, Path: "/v1/statutory-packs/{version_id}", Tags: []string{"Statutory packs"},
		Summary: "Get a statutory pack version",
		Description: "One loaded version with its official sources, its verifier and conditions, each pinned statutory test and its result, " +
			"and the organisation's activation of it (ENG-30, ENG-32). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *packGetInput) (*packReadOutput, error) {
		p, _ := authn.From(ctx)
		var out packReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, act, err := loadedPack(ctx, u, in.VersionID)
			if errors.Is(err, ports.ErrNotFound) {
				return problem(CodeNotFound, "No statutory pack version "+in.VersionID+" is loaded.", "List the statutory packs to find their versions")
			}
			if err != nil {
				return err
			}
			sp, rv, err := packOf(v, act)
			if err != nil {
				return err
			}
			out.ETag = packETag(act)
			out.Body = StatutoryPackDetail{StatutoryPack: sp, Sources: []PackSource{}, Pinned: []PackPinnedResult{}}
			if !sp.Readable {
				return nil
			}
			out.Body.Verifier, out.Body.Conditions = rv.Verifier, rv.Conditions
			for _, s := range rv.Sources {
				out.Body.Sources = append(out.Body.Sources, PackSource{ID: s.ID, Title: s.Title, URL: s.URL, Section: s.Section, Kind: s.Kind,
					Retrieved: s.Retrieved})
			}
			for _, r := range pack.RunPinned(rv) {
				out.Body.Pinned = append(out.Body.Pinned, PackPinnedResult{Table: r.Table, Case: r.Case, Why: r.Why, Pass: r.Pass, Got: r.Got, Want: r.Want})
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ActivateStatutoryPack, huma.Operation{
		OperationID: "activateStatutoryPack", Method: http.MethodPost, Path: "/v1/statutory-packs/{version_id}/activate", Tags: []string{"Statutory packs"},
		Summary: "Ask to activate a statutory pack version", DefaultStatus: http.StatusAccepted,
		Description: "Asks for the organisation to adopt a loaded version (ENG-32). Its pinned statutory tests must pass (PACK-09), this release " +
			"must carry it as it was loaded (PACK-07), and the organisation must not have activated it already (PACK-13). It is never " +
			"activated at once: the answer is 202 with an approval request, which a member who holds A on \"Statutory parameters\" (the Owner " +
			"or the Controller) and is not the proposer approves (SOD-10, FM-1). On the approval the pinned tests run again and the " +
			"activation is recorded, naming the proposer, the approver and the request. Asking needs a person signed in who passed step-up " +
			"in the last 5 minutes, and If-Match with the version's ETag (\"0\" before it is activated). Side effects: the approval request " +
			"and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout,
		storage.CodeApproval, CodeSessionRequired, CodeStepUpRequired, approval.CodePending}, PackCodes...),
		func(ctx context.Context, in *packActivateInput) (*packActivateOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				v, act, err := loadedPack(ctx, u, in.VersionID)
				if errors.Is(err, ports.ErrNotFound) {
					return Approval{}, problem(CodeNotFound, "No statutory pack version "+in.VersionID+" is loaded.", "List the statutory packs to find their versions")
				}
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, packETag(act)); err != nil {
					return Approval{}, err
				}
				if act != nil {
					return Approval{}, problem(pack.CodeActive, "The organisation activated "+v.ID+" already, on "+dateOf(act.ActivatedAt)+".",
						"Nothing to do; activate a later version when one is loaded")
				}
				rv, ok, err := readable(v)
				if err != nil {
					return Approval{}, err
				}
				if !ok {
					return Approval{}, problem(pack.CodeTable, "This release does not carry "+v.ID+" as it was loaded, so its pinned tests cannot run.",
						"Activate it from a release that carries it")
				}
				if failed := pack.Failed(pack.RunPinned(rv)); len(failed) > 0 {
					f := failed[0]
					return Approval{}, packProblem(pack.Problem{Code: pack.CodePinned, Field: "pinned." + f.Table,
						Message: fmt.Sprintf("%s (%s) gives %s, not %s", f.Case, f.Why, f.Got, f.Want),
						Fix:     "Correct the table from the official page in a new version; never activate a version that fails"})
				}
				to := "open"
				if v.EffectiveTo != nil {
					to = dateOf(*v.EffectiveTo)
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPack, ObjectKey: v.ID, Operation: approval.OpPackActivate,
					Summary: fmt.Sprintf("Activate %s (%s), in force from %s", v.ID, rv.Title, dateOf(v.EffectiveFrom)),
					Execute: map[string]any{"version_id": v.ID, "digest": v.Digest},
					Diff: []ApprovalDiff{{Field: "activated", Before: "no", After: "yes"},
						{Field: "in_force", After: dateOf(v.EffectiveFrom) + " to " + to}}})
			})
			if err != nil {
				return nil, err
			}
			return &packActivateOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
