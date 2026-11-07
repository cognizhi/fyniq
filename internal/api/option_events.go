package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// OptionEventDraft is a forfeiture or expiry of a recorded grant's options, as drafted (FR-EQ-19, design D9).
type OptionEventDraft struct {
	Kind   string `json:"kind" enum:"forfeiture,expiry" doc:"forfeiture: options not vested, given up when the holder leaves; expiry: options that lapse, at the grant's expiry or once a leaver's window to exercise closes." example:"forfeiture"`
	Number string `json:"number" pattern:"^[0-9]{1,14}$" doc:"The options it takes, a whole number above zero: at most what the grant leaves, and for a forfeiture at most what is not vested on its day." example:"30000"`
	On     string `json:"on" format:"date" doc:"The day the options are forfeited or lapse, on or after the grant date; from it they are back in the pool." example:"2027-04-15"`
	Note   string `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"Left the company on 15 Apr 2027"`
}

// OptionEventView is a grant's event as the API shows it.
type OptionEventView struct {
	OptionEventDraft
	ID         string      `json:"id" format:"uuid" doc:"The event." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6e01"`
	GrantID    string      `json:"grant_id" format:"uuid" doc:"The grant whose options it takes (getOptionGrant)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6d01"`
	HolderName string      `json:"holder_name" doc:"The grant's holder." example:"Priya Nair"`
	Status     string      `json:"status" enum:"draft,recorded" doc:"draft: waiting for its evidence and approval; recorded: in the register." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Evidence   int         `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, such as the leaver's notice." example:"1"`
	ApprovalID string      `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6e11"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2027-04-16T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

type (
	OptionEventPath struct {
		EventID string `path:"event_id" format:"uuid" doc:"The grant's event." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6e01"`
	}
	eventDraftInput struct {
		OptionGrantPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body OptionEventDraft
	}
	eventReviseInput struct {
		OptionEventPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body OptionEventDraft
	}
	eventAskInput struct {
		OptionEventPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	eventReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body OptionEventView
	}
	eventWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     OptionEventView
	}
)

// eventIn reads an event's fields, or what of them is wrong.
func eventIn(d OptionEventDraft, grant uuid.UUID) (equity.GrantEvent, error) {
	e := equity.GrantEvent{GrantID: grant, Kind: d.Kind, Number: strings.TrimSpace(d.Number), Note: strings.TrimSpace(d.Note)}
	var err error
	if e.On, err = time.Parse(time.DateOnly, d.On); err != nil {
		return e, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.on", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
	}
	return e, nil
}

func (a *API) optionEventOf(ctx context.Context, u storage.UnitOfWork, v optionView, doc storage.Document, holder string) (OptionEventView, error) {
	e, err := equity.ReadEvent(doc)
	if err != nil {
		return OptionEventView{}, err
	}
	out := OptionEventView{OptionEventDraft: OptionEventDraft{Kind: e.Kind, Number: e.Number, On: e.On.Format(time.DateOnly), Note: e.Note},
		ID: doc.ID.String(), GrantID: e.GrantID.String(), HolderName: holder, Status: "draft", Version: doc.Version}
	if doc.Status == storage.DocumentPosted {
		out.Status = "recorded"
	}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, v.eventAsked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the notice, such as the leaver's letter or the board's minute"
		}
	}
	return out, err
}

func (a *API) shownEvent(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (OptionEventView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionEvent, id)
	if err != nil {
		return OptionEventView{}, err
	}
	e, err := equity.ReadEvent(doc)
	if err != nil {
		return OptionEventView{}, err
	}
	g, err := equityRecordOfType(ctx, u, storage.DocumentOptionGrant, e.GrantID)
	if err != nil {
		return OptionEventView{}, err
	}
	grant, err := equity.ReadGrant(g)
	if err != nil {
		return OptionEventView{}, err
	}
	c, err := u.Contacts().Get(ctx, grant.HolderID)
	if err != nil {
		return OptionEventView{}, err
	}
	v, err := a.optionViewOf(ctx, u)
	if err != nil {
		return OptionEventView{}, err
	}
	return a.optionEventOf(ctx, u, v, doc, c.LegalName)
}

func eventAnswer(w written[OptionEventView]) *eventWriteOutput {
	if w.Previewed {
		return &eventWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &eventWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/option-events/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// eventGrant is an event's grant document and the grant, read for its envelope and its approver.
func eventGrant(ctx context.Context, u storage.UnitOfWork, key string) (storage.Document, equity.GrantEvent, equity.Grant, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return storage.Document{}, equity.GrantEvent{}, equity.Grant{}, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, equity.GrantEvent{}, equity.Grant{}, err
	}
	e, err := equity.ReadEvent(doc)
	if err != nil {
		return doc, e, equity.Grant{}, err
	}
	gd, err := u.Documents().Get(ctx, e.GrantID)
	if err != nil {
		return doc, e, equity.Grant{}, err
	}
	g, err := equity.ReadGrant(gd)
	return doc, e, g, err
}

// eventDependencies are an event's envelope values: its status, its grant and the grant's holder.
func eventDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	doc, e, g, err := eventGrant(ctx, u, key)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "grant": e.GrantID.String(), "holder": g.HolderID.String()}, nil
}

// eventExecutable refuses the grant's own holder (FM-1): another person records what is taken from a grant.
func eventExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	_, _, g, err := eventGrant(ctx, u, r.ObjectKey)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, g.HolderID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeNotApprover, "These are your own options, so another person approves their forfeiture or expiry (FM-1).",
			"Ask another person who holds the approval right")
	}
	return nil
}

func registerOptionEvents(a *API) {
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid},
		storage.CodeRetry, storage.CodeLockTimeout)
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.CreateOptionEvent, huma.Operation{
		OperationID: "createOptionEvent", Method: http.MethodPost, Path: "/v1/option-grants/{grant_id}/events", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a forfeiture or expiry of a grant's options",
		Description: "Drafts an event of a recorded grant (FR-EQ-19, design D9): a forfeiture of options not vested, when the holder " +
			"leaves, or an expiry of options that lapse, at the grant's expiry or once a leaver's window to exercise closes. It takes " +
			"at most what the grant leaves, and a forfeiture at most what is not vested on its day; once recorded, its options are " +
			"back in the pool from that day. Attach the notice with addOptionEventEvidence, then ask another person to approve it " +
			"with requestOptionEventApproval. Side effects: the draft, audit events. Every problem comes back together. Needs " +
			"If-Match with the grant's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), OptionCodes...),
		func(ctx context.Context, in *eventDraftInput) (*eventWriteOutput, error) {
			grant, err := parseID("grant_id", in.GrantID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (OptionEventView, error) {
				g, err := equityRecordOfType(ctx, u, storage.DocumentOptionGrant, grant)
				if err != nil {
					return OptionEventView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(g)); err != nil {
					return OptionEventView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return OptionEventView{}, err
				}
				e, err := eventIn(in.Body, grant)
				if err != nil {
					return OptionEventView{}, err
				}
				doc, err := equity.DraftEventIn(ctx, u, e, today)
				if err != nil {
					return OptionEventView{}, equityProblems(err)
				}
				return a.shownEvent(ctx, u, doc.ID)
			})
			if err != nil {
				return nil, err
			}
			return eventAnswer(w), nil
		})

	register(a, authz.GetOptionEvent, huma.Operation{
		OperationID: "getOptionEvent", Method: http.MethodGet, Path: "/v1/option-events/{event_id}", Tags: []string{"Equity"},
		Summary:     "Get a forfeiture or expiry of a grant's options",
		Description: "Returns a grant's event with its ETag: its kind, options, day, grant, status, files and the approval waiting (FR-EQ-19). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *OptionEventPath) (*eventReadOutput, error) {
		id, err := parseID("event_id", in.EventID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out eventReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionEvent, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownEvent(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceOptionEvent, huma.Operation{
		OperationID: "replaceOptionEvent", Method: http.MethodPut, Path: "/v1/option-events/{event_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted forfeiture or expiry",
		Description: "Saves a new version of a grant's event not recorded yet (FR-EQ-19); its grant stays. A recorded one changes no more " +
			"(EQUITY-05). Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), OptionCodes...),
		func(ctx context.Context, in *eventReviseInput) (*eventWriteOutput, error) {
			id, err := parseID("event_id", in.EventID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OptionEventView, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentOptionEvent, id)
				if err != nil {
					return OptionEventView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return OptionEventView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return OptionEventView{}, err
				}
				e, err := eventIn(in.Body, uuid.Nil)
				if err != nil {
					return OptionEventView{}, err
				}
				if _, err := equity.ReviseEventIn(ctx, u, id, e, today); err != nil {
					return OptionEventView{}, equityProblems(err)
				}
				return a.shownEvent(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return eventAnswer(w), nil
		})

	register(a, authz.RequestOptionEventApproval, huma.Operation{
		OperationID: "requestOptionEventApproval", Method: http.MethodPost, Path: "/v1/option-events/{event_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a forfeiture or expiry",
		Description: "Asks another person to approve a drafted event at the version read (FR-EQ-19, FM-1): a grant's options are " +
			"forfeited or lapse only this way, within what the grant leaves (FY293). It needs its notice attached (EQUITY-06). The " +
			"answer is 202 with an approval request, which a person who holds A on \"Cap table, holders, investors, equity " +
			"transactions\" approves, stepped up: never the one who asked, nor the grant's holder. The request holds the event's " +
			"version, status, grant and holder. Needs a person signed in who passed step-up, and If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), OptionCodes...),
		func(ctx context.Context, in *eventAskInput) (*equityAskOutput, error) {
			id, err := parseID("event_id", in.EventID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentOptionEvent, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the event has no evidence attached",
						Fix: "Attach the notice, such as the leaver's letter or the board's minute, then ask again"}
				}
				_, e, g, err := eventGrant(ctx, u, id.String())
				if err != nil {
					return Approval{}, err
				}
				c, err := u.Contacts().Get(ctx, g.HolderID)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "kind", After: e.Kind},
					{Field: "number", After: e.Number}, {Field: "on", After: e.On.Format(time.DateOnly)}}
				verb := map[string]string{equity.EventForfeiture: "Forfeit", equity.EventExpiry: "Let lapse"}[e.Kind]
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectOptionEvent, ObjectKey: doc.ID.String(),
					Operation: approval.OpOptionEventRecord, Summary: fmt.Sprintf("%s %s of %s's options", verb, e.Number, c.LegalName),
					Execute: map[string]any{"event_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "grant_id": e.GrantID.String()},
					Diff:    diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
