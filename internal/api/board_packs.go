package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes of the board pack and the investor update (ENG-23).
const (
	CodePackRole      = "BPACK-01" // building a pack is for the Owner, Controller and Accountant, publishing and sending for the Owner and Controller
	CodePackNotDraft  = "BPACK-02" // the pack is published already: a correction is a new version
	CodePackNotReady  = "BPACK-03" // an update is built from a published pack
	CodePackRecipient = "BPACK-04" // a recipient who is no active member who may read the pack
	CodePackSent      = "BPACK-05" // the update is sent already
)

// BoardPack is one version of a month's board pack (FR-RPT-06).
type BoardPack struct {
	ID          string         `json:"id" format:"uuid" doc:"The pack's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
	Month       string         `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month it is for." example:"2026-10"`
	Version     int            `json:"version" doc:"The pack's version of that month: a correction is the next." example:"1"`
	Status      string         `json:"status" enum:"draft,published" doc:"draft: its texts are still written; published: frozen with its figures." example:"published"`
	Highlights  string         `json:"highlights" doc:"What a person wrote as the month's highlights." example:"Revenue passed S$50,000 for the first time."`
	Commentary  string         `json:"commentary" doc:"What a person wrote or accepted as commentary." example:"Hosting rose with the new region."`
	Asks        string         `json:"asks" doc:"What the board is asked for." example:"An introduction to two banks."`
	Edit        int            `json:"edit" doc:"A draft's edits, which is its ETag." example:"3"`
	Figures     map[string]any `json:"figures,omitempty" doc:"The statements (pl, bs, cf) and KPIs of the month as they were read at ledger_revision when it was published; for a draft, as they read now (preview)."`
	Preview     bool           `json:"preview,omitempty" doc:"True when figures are read now for a draft and will be frozen when it is published." example:"false"`
	Revision    int64          `json:"ledger_revision,omitempty" doc:"The ledger revision a published pack read its figures at (FR-RPT-08)." example:"42"`
	Supersedes  string         `json:"supersedes,omitempty" format:"uuid" doc:"The earlier version this corrects." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a00"`
	PeriodState string         `json:"period_status,omitempty" enum:"open,soft_locked,closed" doc:"The month's status when the figures were read: a pack of a month that is not closed says so." example:"closed"`
	PublishedAt *time.Time     `json:"published_at,omitempty" doc:"When it was published." example:"2026-11-04T03:00:00Z"`
	UpdatedAt   time.Time      `json:"updated_at" doc:"When it last changed." example:"2026-11-04T02:00:00Z"`
}

// BoardPackPage is a page of packs.
type BoardPackPage struct {
	Items      []BoardPack `json:"items" nullable:"false" doc:"The packs, latest month first, without their figures: open one to read them."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
}

// BoardPackPath names a pack in the path.
type BoardPackPath struct {
	ID string `path:"id" format:"uuid" doc:"The pack's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
}

type boardPackOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	ETag     string `header:"ETag" doc:"The draft's edit, for If-Match; a published pack's stays."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BoardPack
}

type getBoardPackInput struct{ BoardPackPath }

type createBoardPackInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		Month string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The month the pack is for, YYYY-MM; the books must have a period for it." example:"2026-10"`
	}
}

// BoardPackTexts are the texts a person writes.
type BoardPackTexts struct {
	Highlights string `json:"highlights" maxLength:"5000" doc:"The month's highlights." example:"Revenue passed S$50,000 for the first time."`
	Commentary string `json:"commentary" maxLength:"20000" doc:"The commentary: what a person wrote, or accepted from an AI draft and edited." example:"Hosting rose with the new region."`
	Asks       string `json:"asks" maxLength:"5000" doc:"What the board is asked for." example:"An introduction to two banks."`
}

type editBoardPackInput struct {
	BoardPackPath
	IfMatchHeader
	PreferHeader
	Body BoardPackTexts
}

type publishBoardPackInput struct {
	BoardPackPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

func editETag(edit int) string { return `"` + strconv.Itoa(edit) + `"` }

func boardPackOf(p storage.BoardPack, withFigures bool) BoardPack {
	out := BoardPack{ID: p.ID.String(), Month: p.Month.Format(statementMonthForm), Version: p.Version, Status: p.Status, Highlights: p.Highlights, Commentary: p.Commentary,
		Asks: p.Asks, Edit: p.Edit, Revision: p.LedgerRevision, PublishedAt: p.PublishedAt, UpdatedAt: p.UpdatedAt.UTC()}
	if p.Supersedes != nil {
		out.Supersedes = p.Supersedes.String()
	}
	if withFigures && len(p.Figures) > 0 {
		var f map[string]any
		if json.Unmarshal(p.Figures, &f) == nil {
			out.Figures = f
			if s, ok := f["period_status"].(string); ok {
				out.PeriodState = s
			}
		}
	}
	return out
}

// wholeLedgerReader says whether the caller reads the whole ledger's reports: a reader limited to the board pack sees published packs only.
func wholeLedgerReader(p authn.Principal) bool { return mayReadReports(p.Roles) }

func mayBuildPacks(p authn.Principal) bool {
	return p.Class == authn.ClassUser && hasAnyRole(p.Roles, authz.Owner, authz.Controller, authz.Accountant)
}

func mayPublishPacks(p authn.Principal) bool {
	return p.Class == authn.ClassUser && hasAnyRole(p.Roles, authz.Owner, authz.Controller)
}

// packFigures reads the statements and the KPIs of a month in u, as a pack freezes them (D4).
func (a *API) packFigures(ctx context.Context, u storage.UnitOfWork, month time.Time) (json.RawMessage, int64, string, error) {
	m := month.Format(statementMonthForm)
	figs := map[string]any{}
	var revision int64
	for _, kind := range []string{"pl", "bs", "cf"} {
		s, err := a.financialStatement(ctx, u, &financialStatementInput{Kind: kind, From: m, To: m, Compare: "prior_period", Columns: "total"})
		if err != nil {
			return nil, 0, "", err
		}
		figs[kind], revision = s, s.Revision
	}
	k, err := a.kpis(ctx, u, &kpisInput{Month: m, Revision: revision})
	if err != nil {
		return nil, 0, "", err
	}
	figs["kpis"] = k
	state := "open"
	ps, err := u.Periods().List(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	for _, x := range ps {
		if x.StartsOn.Equal(month) {
			state = string(x.Status)
		}
	}
	figs["period_status"] = state
	raw, err := json.Marshal(figs)
	return raw, revision, state, err
}

func registerBoardPacks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	packRead := authz.Decide

	register(a, authz.ListBoardPacks, huma.Operation{
		OperationID: "listBoardPacks", Method: http.MethodGet, Path: "/v1/board-packs", Tags: []string{"Reports"},
		Summary: "List the board packs",
		Description: "The monthly board packs, latest month first, a page at a time, without their figures (FR-RPT-06). A reader whose view of the reports is the board pack " +
			"sees the published packs only; the roles that read the whole ledger's reports see the drafts too. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *struct{ PageQuery }) (*struct{ Body BoardPackPage }, error) {
		p, _ := authn.From(ctx)
		var all []storage.BoardPack
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.BoardPacks().List(ctx, !wholeLedgerReader(p))
			return err
		}); err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(b storage.BoardPack) string { return b.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := BoardPackPage{Items: []BoardPack{}, NextCursor: next}
		for _, b := range items {
			out.Items = append(out.Items, boardPackOf(b, false))
		}
		return &struct{ Body BoardPackPage }{Body: out}, nil
	})

	register(a, authz.GetBoardPack, huma.Operation{
		OperationID: "getBoardPack", Method: http.MethodGet, Path: "/v1/board-packs/{id}", Tags: []string{"Reports"},
		Summary: "Get a board pack",
		Description: "A board pack with its texts and figures (FR-RPT-06): the statements (profit and loss, balance sheet, cash flow, each beside the period before) and the KPIs " +
			"of the month, frozen as they were read at one ledger revision when it was published. A draft shows its figures as they read now, marked preview. A reader whose view " +
			"is the board pack is not found a draft. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeInvalid, CodeNoPeriod, CodeLaterRevision}, locks...), func(ctx context.Context, in *getBoardPackInput) (*boardPackOutput, error) {
		p, _ := authn.From(ctx)
		var out BoardPack
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			id, err := parseID("id", in.ID)
			if err != nil {
				return err
			}
			b, err := u.BoardPacks().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) || err == nil && b.Status != storage.PackPublished && !wholeLedgerReader(p) {
				return problem(CodeNotFound, "No board pack with that ID is open to you.", "List the board packs")
			}
			if err != nil {
				return err
			}
			out = boardPackOf(b, true)
			if b.Status == storage.PackDraft {
				raw, _, _, err := a.packFigures(ctx, u, b.Month)
				if err != nil {
					return err
				}
				var f map[string]any
				_ = json.Unmarshal(raw, &f)
				out.Figures, out.Preview = f, true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &boardPackOutput{Status: http.StatusOK, ETag: editETag(out.Edit), Body: out}, nil
	})

	register(a, authz.CreateBoardPackDraft, huma.Operation{
		OperationID: "createBoardPackDraft", Method: http.MethodPost, Path: "/v1/board-packs", Tags: []string{"Reports"}, DefaultStatus: http.StatusCreated,
		Summary: "Start a month's board pack",
		Description: "Makes the draft of a month's pack (FR-RPT-06): version 1, or the next version of a month already published, which it supersedes. A month has one draft at a time. " +
			"The books must have a period for the month. Only the Owner, Controller and Accountant build a pack (BPACK-01). Side effects: the draft and its audit event. Idempotent with Idempotency-Key.",
	}, append(slices.Clone(writes), CodePackRole, CodeNoPeriod, storage.CodeBoardPack, storage.CodeBoardPackDraft), func(ctx context.Context, in *createBoardPackInput) (*boardPackOutput, error) {
		p, _ := authn.From(ctx)
		if !mayBuildPacks(p) {
			return nil, problem(CodePackRole, "Only an Owner, Controller or Accountant builds a board pack.", "Ask one of them")
		}
		month, err := time.Parse(statementMonthForm, in.Body.Month)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.month", Message: "is not a month", Fix: "Send the month as YYYY-MM"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BoardPack, error) {
			ps, err := u.Periods().List(ctx)
			if err != nil {
				return BoardPack{}, err
			}
			if !slices.ContainsFunc(ps, func(x storage.Period) bool { return x.StartsOn.Equal(month) }) {
				return BoardPack{}, invalid([]FieldProblem{{Code: CodeNoPeriod, Field: "body.month", Message: "the books have no period for " + in.Body.Month, Fix: "Choose a month of a financial year of the books"}})
			}
			b, err := u.BoardPacks().CreateDraft(ctx, month, uploader(ctx), a.deps.Clock.Now())
			return boardPackOf(b, false), err
		})
		if err != nil {
			return nil, err
		}
		return &boardPackOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.EditBoardPackDraft, huma.Operation{
		OperationID: "editBoardPackDraft", Method: http.MethodPut, Path: "/v1/board-packs/{id}", Tags: []string{"Reports"},
		Summary: "Write a pack's highlights, commentary and asks",
		Description: "Saves a draft's texts as its next edit (FR-RPT-06): what a person writes or accepts from an AI draft and edits; the pack never needs the AI. A published pack never changes " +
			"(BPACK-02): a correction is a new version. Needs If-Match with the draft's ETag. Only the Owner, Controller and Accountant (BPACK-01). Side effects: the next edit and its audit event, with the texts before and after.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodePackRole, CodePackNotDraft, storage.CodeVersion, storage.CodeBoardPack),
		func(ctx context.Context, in *editBoardPackInput) (*boardPackOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if !mayBuildPacks(p) {
				return nil, problem(CodePackRole, "Only an Owner, Controller or Accountant builds a board pack.", "Ask one of them")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BoardPack, error) {
				id, err := parseID("id", in.ID)
				if err != nil {
					return BoardPack{}, err
				}
				cur, err := u.BoardPacks().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return BoardPack{}, problem(CodeNotFound, "No board pack with that ID is open to you.", "List the board packs")
				}
				if err != nil {
					return BoardPack{}, err
				}
				if cur.Status != storage.PackDraft {
					return BoardPack{}, problem(CodePackNotDraft, "That pack is published: it never changes.", "Start a corrected version for the month")
				}
				if err := checkIfMatch(in.IfMatch, editETag(cur.Edit)); err != nil {
					return BoardPack{}, err
				}
				b, err := u.BoardPacks().EditDraft(ctx, id, in.Body.Highlights, in.Body.Commentary, in.Body.Asks, uploader(ctx), a.deps.Clock.Now(), cur.Edit)
				return boardPackOf(b, false), err
			})
			if err != nil {
				return nil, err
			}
			return &boardPackOutput{Status: http.StatusOK, ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.PublishBoardPack, huma.Operation{
		OperationID: "publishBoardPack", Method: http.MethodPost, Path: "/v1/board-packs/{id}/publish", Tags: []string{"Reports"},
		Summary: "Publish a board pack",
		Description: "Freezes a draft (FR-RPT-06): the statements and KPIs of its month are read at the ledger's revision now and kept with the texts, so the pack a board read is the pack it keeps " +
			"reading; a month that is not closed is published all the same and says so (period_status). Only the Owner and Controller publish (BPACK-01). Needs If-Match with the draft's ETag. " +
			"Side effects: the published pack and its audit event. Idempotent with Idempotency-Key.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodePackRole, CodePackNotDraft, CodeNoPeriod, storage.CodeVersion, storage.CodeBoardPack),
		func(ctx context.Context, in *publishBoardPackInput) (*boardPackOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if !mayPublishPacks(p) {
				return nil, problem(CodePackRole, "Only an Owner or Controller publishes a board pack.", "Ask one of them")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BoardPack, error) {
				id, err := parseID("id", in.ID)
				if err != nil {
					return BoardPack{}, err
				}
				cur, err := u.BoardPacks().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return BoardPack{}, problem(CodeNotFound, "No board pack with that ID is open to you.", "List the board packs")
				}
				if err != nil {
					return BoardPack{}, err
				}
				if cur.Status != storage.PackDraft {
					return BoardPack{}, problem(CodePackNotDraft, "That pack is published already.", "Start a corrected version for the month")
				}
				if err := checkIfMatch(in.IfMatch, editETag(cur.Edit)); err != nil {
					return BoardPack{}, err
				}
				raw, revision, _, err := a.packFigures(ctx, u, cur.Month)
				if err != nil {
					return BoardPack{}, err
				}
				b, err := u.BoardPacks().Publish(ctx, id, raw, revision, uploader(ctx), a.deps.Clock.Now(), cur.Edit)
				return boardPackOf(b, true), err
			})
			if err != nil {
				return nil, err
			}
			return &boardPackOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
		})
	_ = packRead
}

// ------------------------------------------------------------------ investor updates (FR-EQ-23)

// InvestorUpdate is an update built from a published pack.
type InvestorUpdate struct {
	ID         string         `json:"id" format:"uuid" doc:"The update's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b10"`
	PackID     string         `json:"pack_id" format:"uuid" doc:"The published pack it is built from: its KPIs, cash, runway and statements are the pack's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
	Kind       string         `json:"kind" enum:"monthly,quarterly" doc:"monthly or quarterly." example:"monthly"`
	Title      string         `json:"title" doc:"Its title." example:"October 2026 investor update"`
	Highlights string         `json:"highlights" doc:"What a person wrote as the highlights." example:"Revenue passed S$50,000."`
	Asks       string         `json:"asks" doc:"What investors are asked for." example:"Introductions to two banks."`
	Recipients []MemberUserID `json:"recipients" nullable:"false" doc:"The members it was sent to; fixed when it is sent."`
	Status     string         `json:"status" enum:"draft,sent" doc:"draft or sent; a sent update never changes." example:"sent"`
	Edit       int            `json:"edit" doc:"A draft's edits, which is its ETag." example:"2"`
	SentAt     *time.Time     `json:"sent_at,omitempty" doc:"When it was sent." example:"2026-11-05T03:00:00Z"`
	Read       *bool          `json:"read,omitempty" doc:"For a recipient: whether they have opened it." example:"false"`
	ReadBy     []MemberUserID `json:"read_by,omitempty" doc:"For the people who send it: the recipients who have opened it, so far."`
	Pack       *BoardPack     `json:"pack,omitempty" doc:"The pack it is built from, with its figures, when the update is opened."`
}

// InvestorUpdatePage is a page of updates.
type InvestorUpdatePage struct {
	Items      []InvestorUpdate `json:"items" nullable:"false" doc:"The updates, newest first: a reader limited to the board pack sees those sent to them."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
}

// InvestorUpdatePath names an update in the path.
type InvestorUpdatePath struct {
	ID string `path:"id" format:"uuid" doc:"The update's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6b10"`
}

type investorUpdateOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	ETag     string `header:"ETag" doc:"The draft's edit, for If-Match."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     InvestorUpdate
}

type getInvestorUpdateInput struct{ InvestorUpdatePath }

type createInvestorUpdateInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		PackID string `json:"pack_id" format:"uuid" doc:"The published pack to build it from." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a10"`
		Kind   string `json:"kind" enum:"monthly,quarterly" doc:"monthly or quarterly." example:"monthly"`
		Title  string `json:"title" minLength:"1" maxLength:"120" doc:"Its title." example:"October 2026 investor update"`
	}
}

type editInvestorUpdateInput struct {
	InvestorUpdatePath
	IfMatchHeader
	PreferHeader
	Body struct {
		Title      string `json:"title" minLength:"1" maxLength:"120" doc:"Its title." example:"October 2026 investor update"`
		Highlights string `json:"highlights" maxLength:"5000" doc:"The highlights." example:"Revenue passed S$50,000."`
		Asks       string `json:"asks" maxLength:"5000" doc:"The asks." example:"Introductions to two banks."`
	}
}

type sendInvestorUpdateInput struct {
	InvestorUpdatePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body struct {
		Recipients []MemberUserID `json:"recipients" minItems:"1" maxItems:"200" nullable:"false" doc:"Member user IDs: each an active member who may read the board pack, such as a Board viewer (BPACK-04)."`
	}
}

func updateOf(x storage.InvestorUpdate, caller uuid.UUID, reads []storage.InvestorUpdateRead, forSenders bool) InvestorUpdate {
	out := InvestorUpdate{ID: x.ID.String(), PackID: x.PackID.String(), Kind: x.Kind, Title: x.Title, Highlights: x.Highlights, Asks: x.Asks, Recipients: []MemberUserID{}, Status: x.Status,
		Edit: x.Edit, SentAt: x.SentAt}
	for _, r := range x.Recipients {
		out.Recipients = append(out.Recipients, MemberUserID(r.String()))
	}
	if x.Status == storage.UpdateSent {
		mine := slices.Contains(x.Recipients, caller)
		if mine {
			read := slices.ContainsFunc(reads, func(r storage.InvestorUpdateRead) bool { return r.UserID == caller })
			out.Read = &read
		}
		if forSenders {
			for _, r := range reads {
				out.ReadBy = append(out.ReadBy, MemberUserID(r.UserID.String()))
			}
		}
	}
	return out
}

// mayReceivePack says whether people holding these roles may read a board pack: the board pack's own view, or the whole ledger's reports.
func mayReceivePack(roles []authz.Role) bool {
	return authz.Decide(authz.GetBoardPack, roles, false).Allowed
}

func registerInvestorUpdates(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)

	register(a, authz.ListInvestorUpdates, huma.Operation{
		OperationID: "listInvestorUpdates", Method: http.MethodGet, Path: "/v1/investor-updates", Tags: []string{"Reports"},
		Summary: "List the investor updates",
		Description: "The investor updates, newest first, a page at a time (FR-EQ-23). A reader whose view of the reports is the board pack sees the updates sent to them, " +
			"with whether they have opened each; the roles that read the whole ledger's reports see every update, drafts included. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *struct{ PageQuery }) (*struct{ Body InvestorUpdatePage }, error) {
		p, _ := authn.From(ctx)
		var all []storage.InvestorUpdate
		reads := map[uuid.UUID][]storage.InvestorUpdateRead{}
		whole := wholeLedgerReader(p)
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			xs, err := u.InvestorUpdates().List(ctx, !whole)
			if err != nil {
				return err
			}
			for _, x := range xs {
				if !whole && !slices.Contains(x.Recipients, p.UserID) {
					continue
				}
				all = append(all, x)
				if x.Status == storage.UpdateSent {
					if reads[x.ID], err = u.InvestorUpdates().Reads(ctx, x.ID); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(x storage.InvestorUpdate) string { return x.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := InvestorUpdatePage{Items: []InvestorUpdate{}, NextCursor: next}
		for _, x := range items {
			out.Items = append(out.Items, updateOf(x, p.UserID, reads[x.ID], whole))
		}
		return &struct{ Body InvestorUpdatePage }{Body: out}, nil
	})

	register(a, authz.GetInvestorUpdate, huma.Operation{
		OperationID: "getInvestorUpdate", Method: http.MethodGet, Path: "/v1/investor-updates/{id}", Tags: []string{"Reports"},
		Summary: "Open an investor update",
		Description: "An update with the published pack it is built from (FR-EQ-23). A recipient who opens a sent update has their first open recorded as read: that is a side effect of this read, " +
			"audited. A reader limited to the board pack is not found an update that was not sent to them.",
	}, append([]string{CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *getInvestorUpdateInput) (*investorUpdateOutput, error) {
		p, _ := authn.From(ctx)
		var out InvestorUpdate
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			id, err := parseID("id", in.ID)
			if err != nil {
				return err
			}
			x, err := u.InvestorUpdates().Get(ctx, id)
			whole := wholeLedgerReader(p)
			if errors.Is(err, ports.ErrNotFound) || err == nil && !whole && !(x.Status == storage.UpdateSent && slices.Contains(x.Recipients, p.UserID)) {
				return problem(CodeNotFound, "No investor update with that ID is open to you.", "List the investor updates")
			}
			if err != nil {
				return err
			}
			if x.Status == storage.UpdateSent && slices.Contains(x.Recipients, p.UserID) {
				if _, err := u.InvestorUpdates().MarkRead(ctx, x.ID, p.UserID, a.deps.Clock.Now()); err != nil {
					return err
				}
			}
			reads, err := u.InvestorUpdates().Reads(ctx, x.ID)
			if err != nil {
				return err
			}
			out = updateOf(x, p.UserID, reads, whole)
			if pk, err := u.BoardPacks().Get(ctx, x.PackID); err == nil {
				b := boardPackOf(pk, true)
				out.Pack = &b
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &investorUpdateOutput{Status: http.StatusOK, ETag: editETag(out.Edit), Body: out}, nil
	})

	register(a, authz.CreateInvestorUpdate, huma.Operation{
		OperationID: "createInvestorUpdate", Method: http.MethodPost, Path: "/v1/investor-updates", Tags: []string{"Reports"}, DefaultStatus: http.StatusCreated,
		Summary: "Start an investor update",
		Description: "Makes the draft of an update from a published board pack (FR-EQ-23, BPACK-03): the pack's KPIs, cash, runway and statements are the update's figures, with its own highlights and asks. " +
			"Only the Owner and Controller (BPACK-01). Side effects: the draft and its audit event. Idempotent with Idempotency-Key.",
	}, append(slices.Clone(writes), CodePackRole, CodePackNotReady, storage.CodeInvestorUpdate), func(ctx context.Context, in *createInvestorUpdateInput) (*investorUpdateOutput, error) {
		p, _ := authn.From(ctx)
		if !mayPublishPacks(p) {
			return nil, problem(CodePackRole, "Only an Owner or Controller makes an investor update.", "Ask one of them")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InvestorUpdate, error) {
			pid, err := parseID("pack_id", in.Body.PackID)
			if err != nil {
				return InvestorUpdate{}, err
			}
			pk, err := u.BoardPacks().Get(ctx, pid)
			if errors.Is(err, ports.ErrNotFound) || err == nil && pk.Status != storage.PackPublished {
				return InvestorUpdate{}, problem(CodePackNotReady, "An update is built from a published board pack.", "Publish the pack first")
			}
			if err != nil {
				return InvestorUpdate{}, err
			}
			x, err := u.InvestorUpdates().CreateDraft(ctx, pid, in.Body.Kind, strings.TrimSpace(in.Body.Title), uploader(ctx), a.deps.Clock.Now())
			return updateOf(x, p.UserID, nil, true), err
		})
		if err != nil {
			return nil, err
		}
		return &investorUpdateOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.EditInvestorUpdate, huma.Operation{
		OperationID: "editInvestorUpdate", Method: http.MethodPut, Path: "/v1/investor-updates/{id}", Tags: []string{"Reports"},
		Summary: "Write an investor update",
		Description: "Saves a draft's title, highlights and asks as its next edit (FR-EQ-23); a sent update never changes (BPACK-05). Needs If-Match. Only the Owner and Controller (BPACK-01). " +
			"Side effects: the next edit and its audit event.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodePackRole, CodePackSent, storage.CodeVersion, storage.CodeInvestorUpdate),
		func(ctx context.Context, in *editInvestorUpdateInput) (*investorUpdateOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if !mayPublishPacks(p) {
				return nil, problem(CodePackRole, "Only an Owner or Controller makes an investor update.", "Ask one of them")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvestorUpdate, error) {
				id, err := parseID("id", in.ID)
				if err != nil {
					return InvestorUpdate{}, err
				}
				cur, err := u.InvestorUpdates().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return InvestorUpdate{}, problem(CodeNotFound, "No investor update with that ID is open to you.", "List the investor updates")
				}
				if err != nil {
					return InvestorUpdate{}, err
				}
				if cur.Status != storage.UpdateDraft {
					return InvestorUpdate{}, problem(CodePackSent, "That update is sent: it never changes.", "Send a new update")
				}
				if err := checkIfMatch(in.IfMatch, editETag(cur.Edit)); err != nil {
					return InvestorUpdate{}, err
				}
				x, err := u.InvestorUpdates().EditDraft(ctx, id, strings.TrimSpace(in.Body.Title), in.Body.Highlights, in.Body.Asks, uploader(ctx), a.deps.Clock.Now(), cur.Edit)
				return updateOf(x, p.UserID, nil, true), err
			})
			if err != nil {
				return nil, err
			}
			return &investorUpdateOutput{Status: http.StatusOK, ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.SendInvestorUpdate, huma.Operation{
		OperationID: "sendInvestorUpdate", Method: http.MethodPost, Path: "/v1/investor-updates/{id}/send", Tags: []string{"Reports"},
		Summary: "Send an investor update",
		Description: "Sends a draft once, to the members named (FR-EQ-23): each must be an active member who may read the board pack, such as a Board viewer (BPACK-04). Each is told by a notification " +
			"that links to the signed-in update: it carries no figure and no file. The recipients are fixed and the update never changes again; each recipient's first open is recorded. Only the Owner and " +
			"Controller (BPACK-01). Needs If-Match. Side effects: the sent update, a notification per recipient and the audit event. Idempotent with Idempotency-Key.",
	}, append(slices.Clone(writes), CodeStale, CodeIfMatchRequired, CodePackRole, CodePackSent, CodePackRecipient, storage.CodeVersion, storage.CodeInvestorUpdate),
		func(ctx context.Context, in *sendInvestorUpdateInput) (*investorUpdateOutput, error) {
			p, _ := authn.From(ctx)
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			if !mayPublishPacks(p) {
				return nil, problem(CodePackRole, "Only an Owner or Controller sends an investor update.", "Ask one of them")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InvestorUpdate, error) {
				id, err := parseID("id", in.ID)
				if err != nil {
					return InvestorUpdate{}, err
				}
				cur, err := u.InvestorUpdates().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return InvestorUpdate{}, problem(CodeNotFound, "No investor update with that ID is open to you.", "List the investor updates")
				}
				if err != nil {
					return InvestorUpdate{}, err
				}
				if cur.Status != storage.UpdateDraft {
					return InvestorUpdate{}, problem(CodePackSent, "That update is sent already.", "Send a new update")
				}
				if err := checkIfMatch(in.IfMatch, editETag(cur.Edit)); err != nil {
					return InvestorUpdate{}, err
				}
				var ids []uuid.UUID
				var bad []FieldProblem
				for i, r := range in.Body.Recipients {
					field := "body.recipients[" + strconv.Itoa(i) + "]"
					uid, err := uuid.Parse(string(r))
					if err != nil {
						bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a member's user ID", Fix: "Send the user IDs listMembers gives"})
						continue
					}
					if slices.Contains(ids, uid) {
						continue
					}
					ids = append(ids, uid)
					rs, err := u.Members().Roles(ctx, uid, a.deps.Clock.Now())
					roles := make([]authz.Role, 0, len(rs))
					for _, x := range rs {
						roles = append(roles, authz.Role(x.Role))
					}
					if err != nil || len(rs) == 0 || !mayReceivePack(roles) {
						bad = append(bad, FieldProblem{Code: CodePackRecipient, Field: field, Message: "is not an active member who may read the board pack", Fix: "Choose a Board viewer or another member who reads the reports"})
					}
				}
				if len(bad) > 0 {
					return InvestorUpdate{}, invalid(bad)
				}
				now := a.deps.Clock.Now()
				x, err := u.InvestorUpdates().Send(ctx, id, ids, uploader(ctx), now, cur.Edit)
				if err != nil {
					return InvestorUpdate{}, err
				}
				if a.deps.Outbox != nil {
					if _, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, notify.Event{Type: notify.InvestorUpdateType, RecordType: "investor_update", RecordID: &x.ID, At: now,
						Title: "An investor update is ready: " + x.Title, Link: "/board?update=" + x.ID.String(), Audience: notify.Audience{Users: ids}}); err != nil {
						return InvestorUpdate{}, err
					}
				}
				return updateOf(x, p.UserID, nil, true), nil
			})
			if err != nil {
				return nil, err
			}
			return &investorUpdateOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), ETag: editETag(w.Body.Edit), Applied: applied(w), Body: w.Body}, nil
		})
}
