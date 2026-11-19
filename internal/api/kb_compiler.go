package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/kbcompile"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The knowledge-base compiler's four operations (PRD-001 AI-52, AI-57; p1-knowledge-base design D7): the whole of what the compiler job
// can ask of the API, under its own service identity and no other. It cannot publish, read the ledger or run a domain command, because
// none of these does: they list the notes that changed, ask the model for a page of a note (the API reads the note and asks, so the job
// never holds a provider key or the note's text beyond the lint's), store the page as a draft authored by the compiler, and raise a
// finding. A draft is never live: only a publication a person approves makes a snapshot, and the compiler's token opens nothing else.

// ServiceKBCompiler is the class of the compiler's service tokens: these four operations alone.
const ServiceKBCompiler = "kb_compiler"

// Codes of the compiler's refusals.
const (
	CodeKBCompilerNote = "KB-COMPILER-01" // the note is not there, or changed since the work was listed
	CodeKBCompilerPage = "KB-COMPILER-02" // the model's page is not a page
)

type (
	kbCompilerOrg struct {
		Organisation string `json:"organisation_id" format:"uuid" doc:"The organisation whose notes to compile." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	}
	kbCompilerWorkIn  struct{ Body kbCompilerOrg }
	kbCompilerWorkOut struct {
		Body struct {
			Items []kbcompile.Item `json:"items" nullable:"false" doc:"The general notes that changed since their page was compiled: only general notes compile, as the compiler's route handles know-how alone (AI-75)."`
		}
	}
	kbCompilerCompileIn struct {
		Body struct {
			Organisation string   `json:"organisation_id" format:"uuid" doc:"The organisation whose notes to compile." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
			Note         string   `json:"note_key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" doc:"The note." example:"expense-policy"`
			Version      int      `json:"note_version" minimum:"1" doc:"The version of the note the work listed." example:"2"`
			Feedback     []string `json:"feedback,omitempty" maxItems:"20" doc:"What the lint found of an earlier attempt, to fix."`
		}
	}
	kbCompilerDraft struct {
		Title   string   `json:"title" minLength:"1" maxLength:"200" example:"Expense policy"`
		Summary string   `json:"summary" minLength:"1" maxLength:"160" example:"How claims are made here."`
		Aliases []string `json:"aliases,omitempty" maxItems:"10" example:"[\"claims rule\"]"`
		Body    string   `json:"body" minLength:"1" maxLength:"60000" example:"# Expense policy"`
		Model   string   `json:"model,omitempty" doc:"The model that made it." example:"claude-opus-5-5"`
	}
	kbCompilerCompileOut struct{ Body kbCompilerDraft }
	kbCompilerDraftIn    struct {
		Body struct {
			Organisation string          `json:"organisation_id" format:"uuid" doc:"The organisation whose notes to compile." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
			Note         string          `json:"note_key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" example:"expense-policy"`
			Version      int             `json:"note_version" minimum:"1" example:"2"`
			Draft        kbCompilerDraft `json:"draft"`
		}
	}
	kbCompilerDraftOut struct {
		Body struct {
			Number int `json:"number" doc:"The draft's version of the page." example:"3"`
		}
	}
	kbCompilerFindingIn struct {
		Body struct {
			Organisation string `json:"organisation_id" format:"uuid" doc:"The organisation whose notes to compile." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
			Kind         string `json:"kind" enum:"stale,contradiction,number_not_in_source,source_without_page,orphan,human_edit_conflict,superseded,missing_cross_reference,citation,front_matter" example:"number_not_in_source"`
			PageKey      string `json:"page_key" pattern:"^[a-z0-9][a-z0-9.-]{0,99}$" example:"org.expense-policy"`
			Version      int    `json:"version,omitempty" minimum:"0" example:"2"`
			Detail       string `json:"detail" minLength:"1" maxLength:"1000" example:"the number 30 is in no source"`
		}
	}
	kbCompilerFindingOut struct {
		Body struct {
			Raised bool `json:"raised" example:"true"`
		}
	}
)

func kbCompilerOps(id string) bool {
	switch id {
	case "kbCompilerWork", "kbCompilerCompile", "kbCompilerDraft", "kbCompilerFinding":
		return true
	}
	return false
}

// kbNoteText is a note as the lint and the compiler read it: its title and body.
func kbNoteText(n storage.KBNote) string { return n.Title + "\n" + n.Body }

// kbPageNoteVersion is the version of the note a page version was compiled from; 0 when it was not compiled from a note.
func kbPageNoteVersion(v storage.KBVersion) int {
	var srcs []struct {
		Kind    string
		Version int
	}
	_ = json.Unmarshal(v.Sources, &srcs)
	for _, s := range srcs {
		if s.Kind == "note" {
			return s.Version
		}
	}
	return 0
}

func registerKBCompiler(a *API) {
	codes := []string{CodeKBCompilerNote, CodeKBCompilerPage, CodeNotFound, CodeScreenIncomplete, CodeScreened, CodeAIService, storage.CodeKnowledge}
	codes = append(codes, modelCallCodes...)
	org := func(s string) (uuid.UUID, error) {
		id, err := uuid.Parse(s)
		if err != nil {
			return uuid.Nil, problem(CodeInvalid, "organisation_id is not an organisation's ID.", "Send the organisation the work is for")
		}
		return id, nil
	}
	internalOp(a, huma.Operation{
		OperationID: "kbCompilerWork", Method: http.MethodPost, Path: internalBase + "/kb-compiler/work", Tags: []string{"Assistant"},
		Summary: "List the notes the knowledge-base compiler has to compile",
		Description: "The compiler job's first call (AI-52, AI-57), with its own service token and no other: the organisation's general notes that changed " +
			"since their page was compiled, each with its text for the lint, what its page is now, and the page keys a link may name. A note of another data " +
			"class is left out: the compiler's route handles know-how alone (AI-75). Read-only; no side effects.",
	}, codes, func(ctx context.Context, in *kbCompilerWorkIn) (*kbCompilerWorkOut, error) {
		o, err := org(in.Body.Organisation)
		if err != nil {
			return nil, err
		}
		out := &kbCompilerWorkOut{}
		out.Body.Items = []kbcompile.Item{}
		err = a.deps.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			k := u.OrganisationKnowledge()
			notes, err := k.Notes(ctx)
			if err != nil {
				return err
			}
			latest, err := k.Latest(ctx)
			if err != nil {
				return err
			}
			byKey := map[string]storage.KBVersion{}
			keys := map[string]bool{}
			for _, v := range latest {
				byKey[v.Key], keys[v.Key] = v, true
			}
			idx, err := u.KnowledgeBase().PlatformIndex(ctx, compilerReader)
			if err != nil {
				return err
			}
			for _, e := range idx {
				keys[e.Key] = true
			}
			all := make([]string, 0, len(keys))
			for k := range keys {
				all = append(all, k)
			}
			sort.Strings(all)
			for _, n := range notes {
				if n.DataClass != "general" {
					continue
				}
				it := kbcompile.Item{NoteKey: n.Key, NoteVersion: n.Version, Title: n.Title, Text: kbNoteText(n), Keys: all}
				if v, ok := byKey[kbcompile.PageKey(n.Key)]; ok {
					it.Latest = &kbcompile.Latest{Number: v.Number, Origin: v.Origin, HumanEdited: v.HumanEdited, NoteVersion: kbPageNoteVersion(v)}
					if it.Latest.NoteVersion >= n.Version {
						continue // compiled from this version already
					}
				}
				out.Body.Items = append(out.Body.Items, it)
			}
			return nil
		})
		return out, err
	})

	internalOp(a, huma.Operation{
		OperationID: "kbCompilerCompile", Method: http.MethodPost, Path: internalBase + "/kb-compiler/compile", Tags: []string{"Assistant"},
		Summary: "Ask the model for the page of a note",
		Description: "The compiler job's second call (AI-52, AI-57): the API reads the note (the job never sends its text), refuses a note that changed since the " +
			"work was listed, screens it, and asks the model through the route of the agent kb_compiler, its budget and the AI switch applying (AI-74, AI-86). " +
			"The note reaches the model as untrusted text (AI-18). The page comes back screened. Side effects: the model call's record in the AI activity log.",
	}, codes, func(ctx context.Context, in *kbCompilerCompileIn) (*kbCompilerCompileOut, error) {
		o, err := org(in.Body.Organisation)
		if err != nil {
			return nil, err
		}
		var note storage.KBNote
		err = a.deps.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.OrganisationKnowledge().Note(ctx, in.Body.Note)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && n.Version != in.Body.Version) {
				return problem(CodeKBCompilerNote, "The note is not there, or changed since the work was listed.", "List the work again")
			}
			note = n
			return err
		})
		if err != nil {
			return nil, err
		}
		if note.DataClass != "general" {
			return nil, problem(CodeKBCompilerNote, "Only a general note compiles: the compiler's route handles know-how alone.", "Leave it to a person to write its page")
		}
		prompt, err := assistant.DefaultPrompt("kb_compile")
		if err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(map[string]any{"note_key": note.Key, "kind": note.Kind, "title": map[string]string{"untrusted_text": note.Title},
			"text": map[string]string{"untrusted_text": note.Body}, "page_keys_you_may_link": []string{}})
		ask := "Compile the page of this note.\n" + string(payload)
		if fb := kbcompile.Instruction(in.Body.Feedback); fb != "" {
			ask += "\n" + fb
		}
		if err := a.screen(ctx, false, note.Title, note.Body, ask); err != nil {
			return nil, err
		}
		schema := &ports.ModelSchema{Name: "page", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["title","summary","body"],"properties":{` +
			`"title":{"type":"string"},"summary":{"type":"string","description":"One line, at most 160 characters."},` +
			`"aliases":{"type":"array","items":{"type":"string"},"description":"At most ten other words people use for the topic."},` +
			`"body":{"type":"string","description":"The page's Markdown, each section citing the note."}}}`)}
		res, err := a.router().Run(ctx, o, ai.Call{Agent: "kb_compiler", Classes: []string{"know-how"}, Actor: "service:kb-compiler",
			Request: ports.ModelRequest{System: prompt.Text, MaxTokens: 4000, Output: schema,
				Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: ask}}}}}})
		if err != nil {
			return nil, modelCallProblem(err)
		}
		var d kbCompilerDraft
		var text []string
		for _, p := range res.Response.Parts {
			if p.Kind == ports.PartText {
				text = append(text, p.Text)
			}
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.Join(text, ""))), &d); err != nil || strings.TrimSpace(d.Body) == "" || strings.TrimSpace(d.Title) == "" {
			return nil, problem(CodeKBCompilerPage, "The model's answer is not a page.", "Try again; a finding is raised if it never is")
		}
		if err := a.screen(ctx, true, d.Title, d.Summary, d.Body); err != nil {
			return nil, err
		}
		d.Model = res.Response.Model
		return &kbCompilerCompileOut{Body: d}, nil
	})

	internalOp(a, huma.Operation{
		OperationID: "kbCompilerDraft", Method: http.MethodPost, Path: internalBase + "/kb-compiler/drafts", Tags: []string{"Assistant"},
		Summary: "Store a compiled page as a draft",
		Description: "The compiler job's third call (AI-52, AI-56, AI-57): stores the page as the next version of the note's page, org.<the note's key>, authored by the " +
			"compiler, from that note alone, with the note's version and hash as its source. It is a draft: nothing the assistant reads changes until a person's " +
			"publication makes a snapshot, which this token cannot make. The page must pass the lint a publication is checked by, which the API runs again. Side effects: " +
			"the version, the audit event kb_page.version_added by the service kb-compiler.",
	}, codes, func(ctx context.Context, in *kbCompilerDraftIn) (*kbCompilerDraftOut, error) {
		o, err := org(in.Body.Organisation)
		if err != nil {
			return nil, err
		}
		out := &kbCompilerDraftOut{}
		err = a.deps.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			k := u.OrganisationKnowledge()
			n, err := k.Note(ctx, in.Body.Note)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && n.Version != in.Body.Version) {
				return problem(CodeKBCompilerNote, "The note is not there, or changed since the work was listed.", "List the work again")
			}
			if err != nil {
				return err
			}
			if n.DataClass != "general" {
				return problem(CodeKBCompilerNote, "Only a general note compiles.", "Leave it to a person to write its page")
			}
			key := kbcompile.PageKey(n.Key)
			keys := map[string]bool{key: true}
			latest, err := k.Latest(ctx)
			if err != nil {
				return err
			}
			for _, l := range latest {
				keys[l.Key] = true
			}
			if idx, err := u.KnowledgeBase().PlatformIndex(ctx, compilerReader); err == nil {
				for _, e := range idx {
					keys[e.Key] = true
				}
			}
			ref := "note:" + n.Key
			found := lintDraftPage(key, in.Body.Draft, ref, kbNoteText(n), keys)
			if len(found) > 0 {
				return kbGateProblem(found)
			}
			srcs, _ := json.Marshal([]map[string]any{{"kind": "note", "ref": n.Key, "title": n.Title, "version": n.Version}})
			typ := map[string]string{"policy": "policy", "decision": "decision"}[n.Kind]
			if typ == "" {
				typ = "concept"
			}
			v, err := k.AddVersion(ctx, storage.KBVersion{Key: key, Type: typ, Title: strings.TrimSpace(in.Body.Draft.Title), Summary: strings.TrimSpace(in.Body.Draft.Summary),
				Aliases: in.Body.Draft.Aliases, Body: in.Body.Draft.Body, Sources: srcs, DataClass: n.DataClass, Origin: storage.KBOriginCompiled, AuthorKind: "compiler",
				Compiler: in.Body.Draft.Model, PromptVersion: "kb_compile@1", CreatedAt: a.deps.Clock.Now()})
			out.Body.Number = v.Number
			return err
		})
		return out, err
	})

	internalOp(a, huma.Operation{
		OperationID: "kbCompilerFinding", Method: http.MethodPost, Path: internalBase + "/kb-compiler/findings", Tags: []string{"Assistant"},
		Summary: "Raise a finding the compiler could not resolve",
		Description: "The compiler job's last call (AI-52): records what it could not fix, such as a page that never passed the lint or a recompile that conflicts with a " +
			"person's edit, as a finding for the Owner or the Administrator to resolve (AI-56). One open finding of a kind per page. Side effects: the finding, the audit " +
			"event kb_finding.raised by the service kb-compiler.",
	}, codes, func(ctx context.Context, in *kbCompilerFindingIn) (*kbCompilerFindingOut, error) {
		o, err := org(in.Body.Organisation)
		if err != nil {
			return nil, err
		}
		out := &kbCompilerFindingOut{}
		err = a.deps.Store.Do(ctx, o, func(ctx context.Context, u storage.UnitOfWork) error {
			k := u.OrganisationKnowledge()
			open, err := k.OpenFindings(ctx, in.Body.PageKey)
			if err != nil {
				return err
			}
			for _, f := range open {
				if f.Kind == in.Body.Kind {
					return nil // one open finding of a kind per page
				}
			}
			_, err = k.AddFinding(ctx, storage.KBFinding{Kind: in.Body.Kind, PageKey: in.Body.PageKey, PageVersion: in.Body.Version, Detail: in.Body.Detail, CreatedAt: a.deps.Clock.Now()})
			out.Body.Raised = err == nil
			return err
		})
		return out, err
	})
}

// compilerReader is who the compiler's lists are read as: general pages of the platform, for the keys a link may name.
var compilerReader = storage.KBReader{Roles: []string{"owner"}, Classes: []string{"general"}}

// lintDraftPage is the findings of the lint a publication is checked by, for a draft of the page of a note.
func lintDraftPage(key string, d kbCompilerDraft, ref, text string, keys map[string]bool) []knowledge.Finding {
	return knowledge.LintOrgPage(knowledge.OrgPage{Key: key, Summary: strings.TrimSpace(d.Summary), Body: d.Body, Sources: []string{ref}},
		func(r string) (string, bool) { return text, r == ref }, func(k string) bool { return keys[k] })
}
