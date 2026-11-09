package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DPIAView is a data protection impact assessment (CMP-PD-08, rev 0.244): Fyniq's draft or the organisation's latest
// version, its approval, and whether the processing it assesses needs it now.
type DPIAView struct {
	Key        string             `json:"key" doc:"The assessment: payroll, ai-assistant, or other-<uuid> for the organisation's own." example:"payroll"`
	Title      string             `json:"title" doc:"Its title." example:"Data protection impact assessment of the payroll link"`
	Required   bool               `json:"required" doc:"true when the processing it assesses goes on now: its feature is switched on, or the inventory entry it assesses is open and needs one." example:"true"`
	Why        string             `json:"why" doc:"Why it is required, or when it will be." example:"Payroll is switched on."`
	EntryID    string             `json:"entry_id,omitempty" format:"uuid" doc:"The inventory entry it assesses, if any." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Version    int                `json:"version" doc:"Its latest version; 0 while it is Fyniq's draft. The ETag carries it for If-Match." example:"2"`
	From       string             `json:"from" doc:"What the latest version came from: fyniq-dpia-draft@1, or organisation." example:"organisation"`
	Approved   *DPIAApprovalRef   `json:"approved,omitempty" doc:"Its latest approval; none before the first."`
	Unapproved bool               `json:"unapproved" doc:"true while its latest version (or Fyniq's draft) is not approved." example:"false"`
	Body       string             `json:"body,omitempty" doc:"With getDPIA: the latest version's text, or Fyniq's draft, as Markdown." example:"# Data protection impact assessment of the payroll link\n..."`
	Sources    []PolicySourceView `json:"sources,omitempty" doc:"With getDPIA: the official sources Fyniq's draft was written from."`
}

// DPIAApprovalRef is an assessment's approval.
type DPIAApprovalRef struct {
	Version int    `json:"version" doc:"The version approved." example:"2"`
	By      string `json:"by" doc:"Who approved it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	At      string `json:"at" format:"date-time" doc:"When." example:"2026-11-09T02:00:00Z"`
}

// DPIAInput is an assessment's text.
type DPIAInput struct {
	Title   string `json:"title,omitempty" maxLength:"200" doc:"Its title; left out, it keeps its own." example:"Data protection impact assessment of the mailing list"`
	Body    string `json:"body" minLength:"1" maxLength:"100000" doc:"Its text, as Markdown." example:"# Data protection impact assessment of the mailing list\n..."`
	EntryID string `json:"entry_id,omitempty" format:"uuid" doc:"With createDPIA: the inventory entry it assesses." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

type (
	dpiasOutput struct {
		Body struct {
			Items []DPIAView `json:"items" nullable:"false" doc:"Fyniq's two assessments, then the organisation's own, oldest first."`
		}
	}
	// DPIAPath names an assessment (exported: huma reads only an exported embedded struct).
	DPIAPath struct {
		Key string `path:"key" pattern:"^(payroll|ai-assistant|other-[0-9a-f-]{36})$" doc:"The assessment." example:"payroll"`
	}
	dpiaOutput struct {
		ETag string `header:"ETag" doc:"Its latest version, for If-Match."`
		Body DPIAView
	}
	dpiaCreateInput struct {
		KeyHeader
		PreferHeader
		Body DPIAInput
	}
	dpiaSaveInput struct {
		DPIAPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body DPIAInput
	}
	dpiaApproveInput struct {
		DPIAPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	dpiaWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its latest version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     DPIAView
	}
)

func dpiaETag(version int) string { return etag("v" + strconv.Itoa(version)) }

// dpiaViews are the organisation's assessments as of today, with their text when full.
func (a *API) dpiaViews(ctx context.Context, u storage.UnitOfWork, full bool) ([]DPIAView, error) {
	drafts, err := compliance.DPIADrafts()
	if err != nil {
		return nil, err
	}
	versions, approvals, err := u.Compliance().DPIAs(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := u.Compliance().InventoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	latest := map[string]storage.DPIAVersion{}
	var order []string
	for _, v := range versions { // by key, then version
		if _, ok := latest[v.Key]; !ok && !slices.Contains(compliance.DPIAKeys, v.Key) {
			order = append(order, v.Key)
		}
		latest[v.Key] = v
	}
	slices.SortStableFunc(order, func(x, y string) int { return strings.Compare(x, y) }) // other-<v7 uuid>: by time
	approved := map[string]storage.DPIAApproval{}
	for _, ap := range approvals {
		approved[ap.Key] = ap
	}
	view := func(key, title, body, from string, sources []compliance.PolicySource) DPIAView {
		v := DPIAView{Key: key, Title: title, From: from}
		if l, ok := latest[key]; ok {
			v.Title, v.Version, v.From, body = l.Title, l.Version, l.From, l.Body
			if l.EntryID != nil {
				v.EntryID = l.EntryID.String()
			}
		}
		if ap, ok := approved[key]; ok {
			v.Approved = &DPIAApprovalRef{Version: ap.Version, By: ap.ApprovedBy, At: ap.ApprovedAt.UTC().Format(time.RFC3339)}
		}
		v.Unapproved = v.Approved == nil || v.Approved.Version != v.Version
		if full {
			v.Body = body
			for _, s := range sources {
				v.Sources = append(v.Sources, PolicySourceView{Title: s.Title, URL: s.URL, Retrieved: s.Retrieved})
			}
		}
		return v
	}
	out := []DPIAView{}
	for _, d := range drafts {
		v := view(d.Key, d.Title, d.Body, d.Draft, d.Sources)
		setting := compliance.DPIAFeatures[d.Key]
		on, err := settingOn(ctx, u, setting, today)
		if err != nil {
			return nil, err
		}
		v.Required = on == "true"
		if v.Required {
			v.Why = fmt.Sprintf("Required now: %s is switched on (%s).", controls.DPIAName[d.Key], setting)
		} else {
			v.Why = fmt.Sprintf("Required before %s is switched on (%s).", controls.DPIAName[d.Key], setting)
		}
		out = append(out, v)
	}
	for _, key := range order {
		v := view(key, "", "", "organisation", nil)
		v.Why = "Added by the organisation."
		if v.EntryID != "" {
			i := slices.IndexFunc(entries, func(e storage.InventoryEntry) bool { return e.EntryID.String() == v.EntryID })
			if i >= 0 && entries[i].NeedsDPIA && entries[i].State == "open" {
				v.Required, v.Why = true, "The inventory's entry \""+entries[i].What+"\" needs one."
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (a *API) oneDPIA(ctx context.Context, u storage.UnitOfWork, key string) (DPIAView, error) {
	all, err := a.dpiaViews(ctx, u, true)
	if err != nil {
		return DPIAView{}, err
	}
	for _, v := range all {
		if v.Key == key {
			return v, nil
		}
	}
	return DPIAView{}, problem(CodeNotFound, "There is no impact assessment "+key+".", "Use a key from listDPIAs.")
}

func dpiaAnswer(w written[DPIAView]) *dpiaWriteOutput {
	if w.Previewed {
		return &dpiaWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &dpiaWriteOutput{Status: w.Status, ETag: dpiaETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func registerDPIAs(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeDPIA}, locks...)
	register(a, authz.ListDPIAs, exempt(huma.Operation{
		OperationID: "listDPIAs", Method: http.MethodGet, Path: "/v1/compliance/dpias", Tags: []string{"Compliance"},
		Summary: "List the data protection impact assessments",
		Description: "The impact assessments (CMP-PD-08): Fyniq's drafts for the payroll link and the AI assistant, each required once its " +
			"feature is switched on, and the organisation's own, required while the inventory entry they assess is open and needs one; " +
			"each with its latest version and approval. Read-only; no side effects.",
	}, "cursor", "An organisation has a handful of impact assessments, read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*dpiasOutput, error) {
			p, _ := authn.From(ctx)
			out := &dpiasOutput{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out.Body.Items, err = a.dpiaViews(ctx, u, false)
				return err
			})
			return out, err
		})

	register(a, authz.GetDPIA, exempt(huma.Operation{
		OperationID: "getDPIA", Method: http.MethodGet, Path: "/v1/compliance/dpias/{key}", Tags: []string{"Compliance"},
		Summary:     "Get a data protection impact assessment",
		Description: "One assessment as listDPIAs shows it, with the text of its latest version or Fyniq's draft and the official sources the draft was written from. Read-only; no side effects.",
	}, "cursor", "One assessment, not a list."), append([]string{CodeNotFound}, locks...),
		func(ctx context.Context, in *DPIAPath) (*dpiaOutput, error) {
			p, _ := authn.From(ctx)
			var v DPIAView
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				v, err = a.oneDPIA(ctx, u, in.Key)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &dpiaOutput{ETag: dpiaETag(v.Version), Body: v}, nil
		})

	register(a, authz.CreateDPIA, huma.Operation{
		OperationID: "createDPIA", Method: http.MethodPost, Path: "/v1/compliance/dpias", Tags: []string{"Compliance"},
		Summary:     "Write an impact assessment for other processing",
		Description: "Adds the organisation's own impact assessment, at version 1 (CMP-PD-08), for a new processing of personal data, such as an inventory entry that needs one. Side effects: the version, the audit event dpia.saved. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *dpiaCreateInput) (*dpiaWriteOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DPIAView, error) {
			title, body := strings.TrimSpace(in.Body.Title), in.Body.Body
			if title == "" || strings.TrimSpace(body) == "" {
				return DPIAView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.title", Message: "is empty", Fix: "Name the processing it assesses, and write its text"}})
			}
			var entry *uuid.UUID
			if in.Body.EntryID != "" {
				id, err := uuid.Parse(in.Body.EntryID)
				if err != nil {
					return DPIAView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.entry_id", Message: "is not an entry", Fix: "Use an entry_id from getDataInventory"}})
				}
				entry = &id
			}
			key := "other-" + uuid.Must(uuid.NewV7()).String()
			if _, err := u.Compliance().SaveDPIA(ctx, storage.DPIAVersion{Key: key, Version: 1, Title: title, Body: body, From: "organisation", EntryID: entry,
				SavedBy: uploader(ctx), SavedAt: now}); err != nil {
				return DPIAView{}, err
			}
			return a.oneDPIA(ctx, u, key)
		})
		if err != nil {
			return nil, err
		}
		return dpiaAnswer(w), nil
	})

	register(a, authz.SaveDPIA, huma.Operation{
		OperationID: "saveDPIA", Method: http.MethodPut, Path: "/v1/compliance/dpias/{key}", Tags: []string{"Compliance"},
		Summary:     "Save an impact assessment's next version",
		Description: "Saves the assessment's next version from Fyniq's draft or its last (CMP-PD-08). It needs approving again. Side effects: the version, the audit event dpia.saved. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *dpiaSaveInput) (*dpiaWriteOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DPIAView, error) {
			before, err := a.oneDPIA(ctx, u, in.Key)
			if err != nil {
				return DPIAView{}, err
			}
			if err := checkIfMatch(in.IfMatch, dpiaETag(before.Version)); err != nil {
				return DPIAView{}, err
			}
			if strings.TrimSpace(in.Body.Body) == "" {
				return DPIAView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.body", Message: "is empty", Fix: "Write the assessment's text"}})
			}
			var entry *uuid.UUID
			if before.EntryID != "" {
				id := uuid.MustParse(before.EntryID)
				entry = &id
			}
			if _, err := u.Compliance().SaveDPIA(ctx, storage.DPIAVersion{Key: in.Key, Version: before.Version + 1, Title: cmpOr(strings.TrimSpace(in.Body.Title), before.Title),
				Body: in.Body.Body, From: "organisation", EntryID: entry, SavedBy: uploader(ctx), SavedAt: now}); err != nil {
				return DPIAView{}, err
			}
			return a.oneDPIA(ctx, u, in.Key)
		})
		if err != nil {
			return nil, err
		}
		return dpiaAnswer(w), nil
	})

	register(a, authz.ApproveDPIA, huma.Operation{
		OperationID: "approveDPIA", Method: http.MethodPost, Path: "/v1/compliance/dpias/{key}/approvals", Tags: []string{"Compliance"},
		Summary:     "Approve an impact assessment",
		Description: "The Compliance officer approves the assessment's latest version, stepped up (CMP-PD-08). Approving Fyniq's draft adopts it as version 1 first. Side effects: the approval (and the adopted version), the audit events dpia.approved (and dpia.saved). Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired}, writes...),
		func(ctx context.Context, in *dpiaApproveInput) (*dpiaWriteOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if _, err := a.steppedUpPerson(ctx, "Approving an impact assessment"); err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DPIAView, error) {
				before, err := a.oneDPIA(ctx, u, in.Key)
				if err != nil {
					return DPIAView{}, err
				}
				if err := checkIfMatch(in.IfMatch, dpiaETag(before.Version)); err != nil {
					return DPIAView{}, err
				}
				if !before.Unapproved {
					return DPIAView{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "key", Message: "is approved already at its latest version",
						Fix: "Save a new version first if the processing changed"}})
				}
				version := before.Version
				if version == 0 { // Fyniq's draft: adopted as version 1
					if _, err := u.Compliance().SaveDPIA(ctx, storage.DPIAVersion{Key: in.Key, Version: 1, Title: before.Title, Body: before.Body, From: before.From,
						SavedBy: uploader(ctx), SavedAt: now}); err != nil {
						return DPIAView{}, err
					}
					version = 1
				}
				if err := u.Compliance().ApproveDPIA(ctx, storage.DPIAApproval{Key: in.Key, Version: version, ApprovedBy: uploader(ctx), ApprovedAt: now}); err != nil {
					return DPIAView{}, err
				}
				return a.oneDPIA(ctx, u, in.Key)
			})
			if err != nil {
				return nil, err
			}
			return dpiaAnswer(w), nil
		})
}
