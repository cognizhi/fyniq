package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Editing, rolling back and publishing the organisation's pages (PRD-001 AI-55, AI-56, FR-APR-01, FM-3; p1-knowledge-base design
// D8). A page's versions are kept as they were made: an edit is a new human-edited version (a draft, which a recompile never
// overwrites), an edit of a platform page an overlay for this organisation alone, a rollback a new version restoring an old
// one's content. Nothing the assistant reads changes until a publication makes the next snapshot, and a publication is an
// approval object: in Founder mode the Owner publishes alone and it waits in the weekly review (open question 18, FM-3); in Team
// mode another Owner signs a request. Before either, the lint (the platform's rules for what an organisation page is made of) and
// the page's open findings are checked, so a page that fails is not published.

// Codes of a knowledge-base page's refusal.
const (
	CodeKBLint     = "KB-LINT-01"     // a page version fails the lint, or has an open finding
	CodeKBTeam     = "KB-TEAM-01"     // in Team mode a publication is asked for, not made alone
	CodeKBPage     = "KB-PAGE-01"     // the page, version or key is not what a publication takes
	CodeKBSnapshot = "KB-SNAPSHOT-01" // the live snapshot moved since the publication was asked for
)

// KBSourceInput is a source a page is made from.
type KBSourceInput struct {
	Kind  string `json:"kind" enum:"note,setting,platform" doc:"note: one of the organisation's notes; setting: an organisation setting; platform: the platform page an overlay replaces." example:"note"`
	Ref   string `json:"ref" pattern:"^[a-z0-9][a-z0-9._-]{0,99}$" doc:"The note's key, the setting's key or the platform page's key." example:"expense-policy"`
	Title string `json:"title,omitempty" maxLength:"200" doc:"What it is called, for the chip a reader sees; left out, its key." example:"Expense policy"`
}

// KBPageInput is a page's next version, as a person writes it.
type KBPageInput struct {
	Type    string          `json:"type" enum:"concept,task,reference,policy,decision" doc:"What the page is." example:"policy"`
	Title   string          `json:"title" minLength:"1" maxLength:"200" example:"Expense policy"`
	Summary string          `json:"summary" minLength:"1" maxLength:"160" doc:"One line the index shows." example:"How claims are made here."`
	Aliases []string        `json:"aliases,omitempty" nullable:"false" doc:"Other words people use for it, at most 30 of at most 60 characters, which search matches." example:"[\"claims rule\",\"expense rule\"]"`
	Body    string          `json:"body" minLength:"1" maxLength:"60000" example:"# Expense policy" doc:"Markdown: a title, then sections, each citing its source as (src: note:<key>#<anchor>), (src: setting:<key>#<anchor>) or (src: platform:<key>#<anchor>). Every number in it must be in a source it cites."`
	Sources []KBSourceInput `json:"sources" maxItems:"50" doc:"What the page is made from; each section cites one of these."`
}

// KBPageVersion is a version of an organisation page.
type KBPageVersion struct {
	Key         string          `json:"key" example:"org.expense-policy"`
	Number      int             `json:"number" doc:"Its version; the page's ETag carries the newest for If-Match." example:"2"`
	State       string          `json:"state" enum:"published,draft,superseded" doc:"published when the live snapshot lists it, draft when it is newer than the published one, superseded when an earlier one." example:"draft"`
	Origin      string          `json:"origin" enum:"rendered,compiled,human_edited,overlay,rollback" example:"human_edited"`
	Type        string          `json:"type" example:"policy"`
	Title       string          `json:"title" example:"Expense policy"`
	Summary     string          `json:"summary" example:"How claims are made here."`
	Aliases     []string        `json:"aliases" nullable:"false" example:"[\"claims rule\",\"expense rule\"]"`
	Body        string          `json:"body" example:"# Expense policy"`
	Sources     []KBSourceInput `json:"sources" nullable:"false"`
	DataClass   string          `json:"data_class" example:"general"`
	BaseKey     string          `json:"base_key,omitempty" doc:"An overlay: the platform page it replaces." example:"claims.approve-team-claims"`
	Restores    int             `json:"restores,omitempty" doc:"A rollback: the version whose content it restores." example:"1"`
	Compiler    string          `json:"compiler,omitempty" doc:"The model that compiled it." example:"claude-opus-5-5"`
	Author      string          `json:"author" doc:"user:<id>, kb-compiler or system." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	CreatedAt   string          `json:"created_at" format:"date-time" example:"2026-11-19T08:00:00Z"`
	HumanEdited bool            `json:"human_edited" doc:"true when a person wrote it: a recompile never overwrites it." example:"true"`
}

// KBPublicationInput names the versions to publish.
type KBPublicationInput struct {
	Pages  []KBPublicationPage `json:"pages" minItems:"1" maxItems:"50" doc:"Each page and the version to publish."`
	Reason string              `json:"reason,omitempty" maxLength:"1000" doc:"Why, for the snapshot and the weekly review." example:"The expense policy changed."`
}

// KBPublicationPage is a page version to publish.
type KBPublicationPage struct {
	Key     string `json:"key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" example:"org.expense-policy"`
	Version int    `json:"version" minimum:"1" example:"2"`
}

// KBSnapshot is a numbered snapshot of what is published.
type KBSnapshot struct {
	Number    int    `json:"number" example:"3"`
	Reason    string `json:"reason" example:"The expense policy changed."`
	Approval  string `json:"approval_id,omitempty" format:"uuid" doc:"The approval request that signed it; none when the Owner published alone in Founder mode, which waits in the weekly review." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2e"`
	CreatedBy string `json:"created_by,omitempty" example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	CreatedAt string `json:"created_at" format:"date-time" example:"2026-11-19T08:00:00Z"`
}

type (
	// KBPagePath is a page's path parameter (exported: huma reads only an exported embedded struct).
	KBPagePath struct {
		Key string `path:"key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" doc:"The page's key: org.<name> for the organisation's own, or a platform page's key for an overlay." example:"org.expense-policy"`
	}
	kbPageEditIn struct {
		KBPagePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body KBPageInput
	}
	kbRollbackIn struct {
		KBPagePath
		KeyHeader
		PreferHeader
		Body struct {
			Version int `json:"version" minimum:"1" doc:"The version whose content to restore, as a new version." example:"1"`
		}
	}
	kbVersionOut struct {
		Status   int
		ETag     string `header:"ETag" doc:"The page's newest version, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     KBPageVersion
	}
	kbPublishIn struct {
		KeyHeader
		PreferHeader
		Body KBPublicationInput
	}
	kbPublishOut struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     KBSnapshot
	}
	kbRequestOut struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
)

func kbPageETag(number int) string { return etag("v" + strconv.Itoa(number)) }

func kbVersionView(v storage.KBVersion, published map[string]int, newest int) KBPageVersion {
	state := "superseded"
	switch {
	case published[v.Key] == v.Number:
		state = "published"
	case v.Number > published[v.Key] && v.Number == newest:
		state = "draft"
	}
	out := KBPageVersion{Key: v.Key, Number: v.Number, State: state, Origin: v.Origin, Type: v.Type, Title: v.Title, Summary: v.Summary, Aliases: nonNilStrings(v.Aliases),
		Body: v.Body, Sources: []KBSourceInput{}, DataClass: v.DataClass, BaseKey: v.BaseKey, Restores: v.Restores, Compiler: v.Compiler,
		CreatedAt: v.CreatedAt.UTC().Format(time.RFC3339), HumanEdited: v.HumanEdited}
	var srcs []struct{ Kind, Ref, Title string }
	_ = json.Unmarshal(v.Sources, &srcs)
	for _, s := range srcs {
		out.Sources = append(out.Sources, KBSourceInput{Kind: s.Kind, Ref: s.Ref, Title: s.Title})
	}
	switch v.AuthorKind {
	case "person":
		out.Author = "user:" + v.AuthorUser.String()
	case "compiler":
		out.Author = "kb-compiler"
	default:
		out.Author = "system"
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// adminReader is who reads the knowledge base to administer it: the Owner and the Administrator read every class but identity
// (AI-02), whatever the AI may.
func adminReader(p authn.Principal) storage.KBReader {
	roles := make([]string, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, string(r))
	}
	return storage.KBReader{Roles: roles, Classes: []string{"general", "personal", "payroll", "equity"}}
}

// kbSources is what a page's sources are as text, for the lint and the page's citations.
type kbSources struct {
	a *API
	u storage.UnitOfWork
	p authn.Principal
}

func (s kbSources) text(ctx context.Context, ref string) (string, bool) {
	kind, key, _ := strings.Cut(ref, ":")
	switch kind {
	case "note":
		n, err := s.u.OrganisationKnowledge().Note(ctx, key)
		if err != nil {
			return "", false
		}
		return n.Title + "\n" + n.Body, true
	case "setting":
		def, ok := settings.Lookup(key)
		if !ok {
			return "", false
		}
		today, err := orgToday(ctx, s.a, s.u)
		if err != nil {
			return "", false
		}
		v, err := settingOn(ctx, s.u, key, today)
		if err != nil {
			return "", false
		}
		return def.Label + "\n" + v + "\n" + def.Reason + "\n" + def.Recommended, true
	case "platform":
		pg, ok, err := s.u.KnowledgeBase().PlatformPage(ctx, key, adminReader(s.p))
		if err != nil || !ok {
			return "", false
		}
		return pg.Title + "\n" + pg.Body, true
	}
	return "", false
}

// kbGate is what a version must pass before it is published: the lint, and no open finding of the kinds that stop a publication
// (a contradiction with a setting, a pack or a newer page, or a recompile that conflicts with a person's edit); the weekly lint's advice
// (knowledge.BlocksPublication) does not stop one.
func (a *API) kbGate(ctx context.Context, u storage.UnitOfWork, p authn.Principal, v storage.KBVersion) ([]knowledge.Finding, error) {
	var srcs []struct{ Kind, Ref, Title string }
	_ = json.Unmarshal(v.Sources, &srcs)
	refs := make([]string, 0, len(srcs))
	for _, s := range srcs {
		refs = append(refs, s.Kind+":"+s.Ref)
	}
	src := kbSources{a: a, u: u, p: p}
	latest, err := u.OrganisationKnowledge().Latest(ctx)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, l := range latest {
		keys[l.Key] = true
	}
	idx, err := u.KnowledgeBase().PlatformIndex(ctx, adminReader(p))
	if err != nil {
		return nil, err
	}
	for _, e := range idx {
		keys[e.Key] = true
	}
	// A source's text is read once per ref: the lint asks for each cited source.
	texts := map[string]string{}
	have := map[string]bool{}
	for _, r := range refs {
		if t, ok := src.text(ctx, r); ok {
			texts[r], have[r] = t, true
		}
	}
	found := knowledge.LintOrgPage(knowledge.OrgPage{Key: v.Key, Summary: v.Summary, Body: v.Body, Sources: refs},
		func(ref string) (string, bool) { t, ok := texts[ref]; return t, ok && have[ref] }, func(k string) bool { return keys[k] })
	open, err := u.OrganisationKnowledge().OpenFindings(ctx, v.Key)
	if err != nil {
		return nil, err
	}
	for _, f := range open {
		if !knowledge.BlocksPublication(f.Kind) {
			continue // the weekly pass's advice: the new version is the answer to it
		}
		found = append(found, knowledge.Finding{Rule: "KB-OPEN-FINDING", Where: v.Key, Detail: "an open finding stops the publication: " + f.Kind + ": " + f.Detail})
	}
	return found, nil
}

func kbGateProblem(found []knowledge.Finding) error {
	fields := make([]FieldProblem, 0, len(found))
	for _, f := range found {
		fields = append(fields, FieldProblem{Code: CodeKBLint, Field: "body.pages[" + f.Where + "]", Message: f.Detail, Fix: "Fix the page, or resolve its finding, and publish again"})
	}
	return problem(CodeKBLint, fmt.Sprintf("%d things stop this publication.", len(found)), "Fix what each field names; the page's text and sources are checked as in the platform's guide", fields...)
}

// kbPublication is a publication's pages, as the request keeps them and the envelope signs them.
func kbPublicationPages(in KBPublicationInput) map[string]int {
	out := map[string]int{}
	for _, p := range in.Pages {
		out[p.Key] = p.Version
	}
	return out
}

// kbPagesDigest is the envelope's hash of what is to be published: each page, its version and the digest of that version's content,
// so a version changed (it cannot be) or replaced by another number voids the request.
func kbPagesDigest(ctx context.Context, u storage.UnitOfWork, pages map[string]int) (string, error) {
	keys := make([]string, 0, len(pages))
	for k := range pages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		vs, err := u.OrganisationKnowledge().Versions(ctx, k)
		if err != nil {
			return "", err
		}
		i := slices.IndexFunc(vs, func(v storage.KBVersion) bool { return v.Number == pages[k] })
		if i < 0 {
			return "", problem(CodeKBPage, fmt.Sprintf("%s has no version %d.", k, pages[k]), "Publish a version the page has")
		}
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%s\n", k, pages[k], vs[i].Title, vs[i].Summary, vs[i].Body)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func liveSnapshot(ctx context.Context, u storage.UnitOfWork) (int, error) {
	snaps, err := u.OrganisationKnowledge().Snapshots(ctx, 1)
	if err != nil || len(snaps) == 0 {
		return 0, err
	}
	return snaps[0].Number, nil
}

// publishKB makes the next snapshot: the versions are checked once more, then the snapshot is made. approval names the request
// that signed it; none for the Owner's own publication in Founder mode.
func (a *API) publishKB(ctx context.Context, u storage.UnitOfWork, p authn.Principal, pages map[string]int, approvalID *uuid.UUID, by uuid.UUID, reason string) (storage.KBSnapshot, error) {
	k := u.OrganisationKnowledge()
	var all []knowledge.Finding
	keys := make([]string, 0, len(pages))
	for key := range pages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		vs, err := k.Versions(ctx, key)
		if err != nil {
			return storage.KBSnapshot{}, err
		}
		i := slices.IndexFunc(vs, func(v storage.KBVersion) bool { return v.Number == pages[key] })
		if i < 0 {
			return storage.KBSnapshot{}, problem(CodeKBPage, fmt.Sprintf("%s has no version %d.", key, pages[key]), "Publish a version the page has")
		}
		found, err := a.kbGate(ctx, u, p, vs[i])
		if err != nil {
			return storage.KBSnapshot{}, err
		}
		all = append(all, found...)
	}
	if len(all) > 0 {
		return storage.KBSnapshot{}, kbGateProblem(all)
	}
	return k.Publish(ctx, pages, approvalID, &by, reason, a.deps.Clock.Now())
}

func kbSnapshotView(s storage.KBSnapshot) KBSnapshot {
	out := KBSnapshot{Number: s.Number, Reason: s.Reason, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339)}
	if s.ApprovalID != nil {
		out.Approval = s.ApprovalID.String()
	}
	if s.CreatedBy != nil {
		out.CreatedBy = "user:" + s.CreatedBy.String()
	}
	return out
}

// executeKBPublication is what an approved publication does: the snapshot, with the request that signed it.
func (a *API) executeKBPublication(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored, signer uuid.UUID) error {
	var pages map[string]int
	raw, _ := s.Execute["pages"].(string)
	if err := json.Unmarshal([]byte(raw), &pages); err != nil {
		return fmt.Errorf("approval: a publication's pages: %w", err)
	}
	reason, _ := s.Execute["reason"].(string)
	// The signer's rights are the Owner's; the lint reads as them.
	p := authn.Principal{Roles: []authz.Role{authz.Owner}}
	_, err := a.publishKB(ctx, u, p, pages, &r.ID, signer, reason)
	return err
}

func (a *API) kbPublicationDependencies(ctx context.Context, u storage.UnitOfWork, c pendingChange) (int, map[string]any, error) {
	var pages map[string]int
	raw, _ := c.Execute["pages"].(string)
	if err := json.Unmarshal([]byte(raw), &pages); err != nil {
		return 0, nil, err
	}
	digest, err := kbPagesDigest(ctx, u, pages)
	if err != nil {
		return 0, nil, err
	}
	n, err := liveSnapshot(ctx, u)
	if err != nil {
		return 0, nil, err
	}
	return n, map[string]any{"pages": digest, "snapshot": n}, nil
}

func registerKnowledgePublishing(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeKnowledge, CodeSessionRequired}, locks...)
	person := func(ctx context.Context) (authn.Principal, error) {
		p, _ := authn.From(ctx)
		if p.Class != authn.ClassUser || p.UserID == uuid.Nil {
			return p, problem(CodeSessionRequired, "A person signed in writes the organisation's pages.", "Sign in to Fyniq")
		}
		return p, nil
	}
	versionOut := func(w written[KBPageVersion], key string) *kbVersionOut {
		if w.Previewed {
			return &kbVersionOut{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &kbVersionOut{Status: w.Status, ETag: kbPageETag(w.Body.Number), Location: "/v1/knowledge-base/pages/" + key, Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}
	// fields checks a page's input and returns the sources as JSON.
	fields := func(in KBPageInput) ([]FieldProblem, []byte) {
		var problems []FieldProblem
		seen := map[string]bool{}
		for i, s := range in.Sources {
			ref := s.Kind + ":" + s.Ref
			if seen[ref] {
				problems = append(problems, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.sources[%d]", i), Message: "is listed twice", Fix: "List each source once"})
			}
			seen[ref] = true
		}
		if len(in.Aliases) > 30 {
			problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.aliases", Message: "lists more than 30 aliases", Fix: "Keep the words people really use"})
		}
		for i, al := range in.Aliases {
			if n := len([]rune(al)); n == 0 || n > 60 {
				problems = append(problems, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.aliases[%d]", i), Message: "is empty or longer than 60 characters", Fix: "Give a short phrase"})
			}
		}
		if m := secretShape.FindString(in.Body + " " + in.Title + " " + in.Summary); m != "" {
			problems = append(problems, FieldProblem{Code: CodeInvalid, Field: "body.body", Message: "holds what looks like an identity number, a bank account number or a secret",
				Fix: "Take it out: the assistant reads pages, and these never reach it (AI-02)"})
		}
		b, _ := json.Marshal(in.Sources)
		return problems, b
	}

	register(a, authz.EditKBPage, huma.Operation{
		OperationID: "editKBPage", Method: http.MethodPut, Path: "/v1/knowledge-base/pages/{key}", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusCreated,
		Summary: "Edit a knowledge-base page",
		Description: "Saves the page's next version as written by the caller (AI-55, AI-56): a human-edited version of an organisation page (key " +
			"org.<name>), or an overlay of a platform page for this organisation alone. It is a draft: nothing the assistant reads changes until it is " +
			"published. A recompile never overwrites it; it is flagged for a person instead. Needs If-Match with the page's newest version's ETag (v0 " +
			"for a page with none). Side effects: the version, the audit event kb_page.version_added. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing. On the never-list for MCP and agent clients (§9.6).",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeKBPage}, writes...), func(ctx context.Context, in *kbPageEditIn) (*kbVersionOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		problems, srcJSON := fields(in.Body)
		if len(problems) > 0 {
			return nil, invalid(problems)
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (KBPageVersion, error) {
			k := u.OrganisationKnowledge()
			v := storage.KBVersion{Key: in.Key, Type: in.Body.Type, Title: strings.TrimSpace(in.Body.Title), Summary: strings.TrimSpace(in.Body.Summary), Aliases: in.Body.Aliases,
				Body: in.Body.Body, Sources: srcJSON, Origin: storage.KBOriginHumanEdited, HumanEdited: true, AuthorKind: "person", AuthorUser: p.UserID, CreatedAt: now}
			if base, ok, err := u.KnowledgeBase().PlatformPage(ctx, in.Key, adminReader(p)); err != nil {
				return KBPageVersion{}, err
			} else if ok {
				v.Origin, v.BaseKey, v.BaseDigest, v.DataClass = storage.KBOriginOverlay, in.Key, base.Digest, base.DataClass
			} else if !strings.HasPrefix(in.Key, "org.") {
				return KBPageVersion{}, invalid([]FieldProblem{{Code: CodeKBPage, Field: "key", Message: "is neither a platform page's key nor org.<name>", Fix: "Name the organisation's own page org.<name>, or edit a platform page by its key"}})
			}
			vs, err := k.Versions(ctx, in.Key)
			if err != nil {
				return KBPageVersion{}, err
			}
			newest := 0
			if len(vs) > 0 {
				newest = vs[0].Number
			}
			if err := checkIfMatch(in.IfMatch, kbPageETag(newest)); err != nil {
				return KBPageVersion{}, err
			}
			saved, err := k.AddVersion(ctx, v)
			if err != nil {
				return KBPageVersion{}, err
			}
			published, err := k.Published(ctx)
			return kbVersionView(saved, published, saved.Number), err
		})
		if err != nil {
			return nil, err
		}
		return versionOut(w, in.Key), nil
	})

	register(a, authz.RollbackKBPage, huma.Operation{
		OperationID: "rollbackKBPage", Method: http.MethodPost, Path: "/v1/knowledge-base/pages/{key}/rollback", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusCreated,
		Summary: "Roll a knowledge-base page back to an earlier version",
		Description: "Saves a new version that restores an earlier one's content, as a human-edited draft (AI-56): earlier versions are never changed. It is " +
			"published like any change, by a publication a person approves. Side effects: the version, the audit event kb_page.version_added. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing. On the never-list for MCP and agent clients (§9.6).",
	}, append([]string{CodeNotFound, CodeKBPage}, writes...), func(ctx context.Context, in *kbRollbackIn) (*kbVersionOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (KBPageVersion, error) {
			k := u.OrganisationKnowledge()
			vs, err := k.Versions(ctx, in.Key)
			if err != nil {
				return KBPageVersion{}, err
			}
			i := slices.IndexFunc(vs, func(v storage.KBVersion) bool { return v.Number == in.Body.Version })
			if len(vs) == 0 || i < 0 {
				return KBPageVersion{}, ports.ErrNotFound
			}
			old := vs[i]
			if old.Number >= vs[0].Number && old.Number == vs[0].Number {
				return KBPageVersion{}, invalid([]FieldProblem{{Code: CodeKBPage, Field: "body.version", Message: "is the newest version: there is nothing to roll back", Fix: "Name an earlier version"}})
			}
			v := storage.KBVersion{Key: old.Key, Type: old.Type, Title: old.Title, Summary: old.Summary, Aliases: old.Aliases, Body: old.Body, Sources: old.Sources,
				DataClass: old.DataClass, Origin: storage.KBOriginRollback, HumanEdited: true, BaseKey: old.BaseKey, BaseDigest: old.BaseDigest, Restores: old.Number,
				AuthorKind: "person", AuthorUser: p.UserID, CreatedAt: now}
			saved, err := k.AddVersion(ctx, v)
			if err != nil {
				return KBPageVersion{}, err
			}
			published, err := k.Published(ctx)
			return kbVersionView(saved, published, saved.Number), err
		})
		if err != nil {
			return nil, err
		}
		return versionOut(w, in.Key), nil
	})

	register(a, authz.PublishKBPages, huma.Operation{
		OperationID: "publishKBPages", Method: http.MethodPost, Path: "/v1/knowledge-base/publications", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusCreated,
		Summary: "Publish knowledge-base pages",
		Description: "Makes the next snapshot: the live pages with these versions in their place, which the assistant reads from then on (AI-56). In Founder mode " +
			"an Owner publishes alone and the publication is listed for the weekly review (FM-3); in Team mode it is refused, and requestKBPublication asks " +
			"another Owner to sign it. Every version is linted first (its summary, that each section cites a source it lists, that each number is in a source it " +
			"cites, that links match a page) and a page with an open finding is refused. Side effects: the snapshot, the audit event kb_snapshot.published, " +
			"and the weekly review's entry. Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing. On the never-list for MCP " +
			"and agent clients (§9.6).",
	}, append([]string{CodeKBLint, CodeKBTeam, CodeKBPage}, writes...), func(ctx context.Context, in *kbPublishIn) (*kbPublishOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (KBSnapshot, error) {
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return KBSnapshot{}, err
			}
			if mode != team.Founder {
				return KBSnapshot{}, problem(CodeKBTeam, "In Team mode another Owner signs a publication.", "Ask with requestKBPublication")
			}
			snap, err := a.publishKB(ctx, u, p, kbPublicationPages(in.Body), nil, p.UserID, strings.TrimSpace(in.Body.Reason))
			if err != nil {
				return KBSnapshot{}, err
			}
			what := fmt.Sprintf("Published %d page(s) of the knowledge base alone, as snapshot %d", len(in.Body.Pages), snap.Number)
			if r := strings.TrimSpace(in.Body.Reason); r != "" {
				what += ": " + r
			}
			if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectKBPublication,
				ObjectKey: fmt.Sprintf("%s@%d", approval.KBPublicationKey, snap.Number), Summary: what, Actor: &p.UserID, LoggedAt: a.deps.Clock.Now()}); err != nil {
				return KBSnapshot{}, err
			}
			return kbSnapshotView(snap), nil
		})
		if err != nil {
			return nil, err
		}
		return &kbPublishOut{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	register(a, authz.RequestKBPublication, huma.Operation{
		OperationID: "requestKBPublication", Method: http.MethodPost, Path: "/v1/knowledge-base/publications/approval", Tags: []string{"Knowledge base"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask for approval to publish knowledge-base pages",
		Description: "Asks another Owner to sign a publication of these page versions (AI-56, FR-APR-01): the request holds each version and a hash of its content and the " +
			"snapshot it replaces, so a change to any voids it. The versions are linted first, as publishKBPages does. On approval the snapshot is made, naming the request. " +
			"Asking needs a person signed in who passed step-up. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing. On the never-list for MCP " +
			"and agent clients (§9.6).",
	}, append([]string{CodeKBLint, CodeKBPage, CodeStepUpRequired, approval.CodePending, storage.CodeApproval}, writes...), func(ctx context.Context, in *kbPublishIn) (*kbRequestOut, error) {
		p, err := person(ctx)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			pages := kbPublicationPages(in.Body)
			var all []knowledge.Finding
			keys := make([]string, 0, len(pages))
			for k := range pages {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var diff []ApprovalDiff
			live, err := u.OrganisationKnowledge().Published(ctx)
			if err != nil {
				return Approval{}, err
			}
			for _, key := range keys {
				vs, err := u.OrganisationKnowledge().Versions(ctx, key)
				if err != nil {
					return Approval{}, err
				}
				i := slices.IndexFunc(vs, func(v storage.KBVersion) bool { return v.Number == pages[key] })
				if i < 0 {
					return Approval{}, problem(CodeKBPage, fmt.Sprintf("%s has no version %d.", key, pages[key]), "Publish a version the page has")
				}
				found, err := a.kbGate(ctx, u, p, vs[i])
				if err != nil {
					return Approval{}, err
				}
				all = append(all, found...)
				diff = append(diff, ApprovalDiff{Field: key, Before: fmt.Sprint(live[key]), After: fmt.Sprint(pages[key])})
			}
			if len(all) > 0 {
				return Approval{}, kbGateProblem(all)
			}
			raw, _ := json.Marshal(pages)
			summary := fmt.Sprintf("Publish %d page(s) of the knowledge base", len(pages))
			if r := strings.TrimSpace(in.Body.Reason); r != "" {
				summary += ": " + r
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectKBPublication, ObjectKey: approval.KBPublicationKey, Operation: approval.OpKBPublish,
				Summary: summary, Execute: map[string]any{"pages": string(raw), "reason": strings.TrimSpace(in.Body.Reason)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &kbRequestOut{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// TransformSchema implements huma.SchemaTransformer: an example for each alias (NFR-API-01).
func (KBPageInput) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"aliases": "claims rule"})
	return s
}

// TransformSchema implements huma.SchemaTransformer: an example for each alias (NFR-API-01).
func (KBPageVersion) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"aliases": "claims rule"})
	return s
}
