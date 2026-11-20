package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// LinkLog reads the link's event log by identity from Nemura with the read-only client's second scope, link.sync.read (the owner's decision of 21 Nov 2026): linksend.HoldAsker in a deployment, a fake in tests;
// nil reads nothing, and the link's health says it could not look.
type LinkLog interface {
	Sync(ctx context.Context, key storage.PeopleLinkAskKey, cursor string) (linksend.SyncPage, error)
}

// SequenceRange is a run of Nemura's sequence numbers Fyniq never received.
type SequenceRange struct {
	From int64 `json:"from" doc:"The first sequence number missing." example:"12"`
	To   int64 `json:"to" doc:"The last sequence number missing of this run." example:"14"`
}

// PeopleLinkHealth is how the link stands, by identity: what Nemura says it queued against what Fyniq's inbox received (PRD-001 FR-PPL-08, FR-PPL-15; PRD-002 NM-INT-04). It holds no payload, name or figure.
type PeopleLinkHealth struct {
	State                   string          `json:"state" enum:"none,active,suspended,unlinking,unlinked" doc:"The link's state." example:"active"`
	Reachable               bool            `json:"reachable" doc:"True when Nemura's event log was read just now. False with a note when the link is not active, this deployment does not know where Nemura is, the link was made before the log could be read (it is approved again to hold link.sync.read), or Nemura did not answer: the other numbers are then Fyniq's own." example:"true"`
	Note                    string          `json:"note,omitempty" doc:"Why Nemura's log could not be read, in plain words." example:"Nemura did not answer: try again in a minute."`
	CheckedAt               time.Time       `json:"checked_at" doc:"When this was worked out." example:"2026-11-21T09:00:00Z"`
	NemuraHighestSequence   int64           `json:"nemura_highest_sequence" doc:"The highest sequence Nemura has queued for Fyniq (0 when it could not be read)." example:"57"`
	ReceivedHighestSequence int64           `json:"received_highest_sequence" doc:"The highest of Nemura's sequence numbers Fyniq's inbox holds." example:"55"`
	Waiting                 int             `json:"waiting" doc:"Events Nemura queued and has not delivered yet: not a gap, they are on their way." example:"2"`
	Failing                 int             `json:"failing" doc:"Events Nemura tried and could not deliver: it tries again." example:"0"`
	Refused                 int             `json:"refused" doc:"Events Fyniq refused for good, with the code on Nemura's page." example:"0"`
	Missing                 []SequenceRange `json:"missing" nullable:"false" doc:"The runs of sequence numbers Nemura says it delivered and Fyniq's inbox never received: a gap. Ask Nemura to send them again with repairPeopleLink."`
	Truncated               bool            `json:"truncated,omitempty" doc:"True when Nemura's log is longer than one look reads; the oldest part was left out." example:"false"`
}

// logPages is how many pages of 500 events one look at Nemura's log reads.
const logPages = 40

// linkHealth works out how the link stands: it reads Nemura's log by identity (outside any transaction) and sets it against Fyniq's inbox. Whatever it cannot read it says so and never guesses.
func (a *API) linkHealth(ctx context.Context, org authn.Principal) (PeopleLinkHealth, storage.PeopleLinkRow, error) {
	out := PeopleLinkHealth{State: "none", Missing: []SequenceRange{}, CheckedAt: a.deps.Clock.Now()}
	var l storage.PeopleLinkRow
	var ask storage.PeopleLinkAskKey
	var haveAsk bool
	if err := a.deps.Store.Do(ctx, org.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if l, err = u.PeopleLinks().Live(ctx); errors.Is(err, ports.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		out.State = l.State
		if ask, err = u.PeopleLinks().AskKey(ctx, l.ID); err == nil {
			haveAsk = true
		} else if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		return nil
	}); err != nil {
		return out, l, err
	}
	switch {
	case l.ID == uuid.Nil:
		out.Note = "The organisation is not linked to the people platform."
		return out, l, nil
	case l.State != storage.LinkActive:
		out.Note = "The link is " + l.State + ": Nemura's log is read while it is active."
		return out, l, nil
	case a.deps.LinkLog == nil || !haveAsk:
		out.Note = "This deployment cannot read Nemura's log: it is not told where Nemura is, or the link was made before the log could be read (approve the link again)."
		return out, l, nil
	}
	var events []linksend.SyncEvent
	cursor := ""
	for i := 0; i < logPages; i++ {
		page, err := a.deps.LinkLog.Sync(ctx, ask, cursor)
		if err != nil {
			out.Note = "Nemura did not answer: try again in a minute."
			return out, l, nil
		}
		if page.Epoch != l.Epoch {
			out.Note = "Nemura answered for another epoch of the link: it is not the link this organisation has."
			return out, l, nil
		}
		out.NemuraHighestSequence = page.HighestSequence
		events = append(events, page.Events...)
		if page.NextCursor == "" {
			cursor = ""
			break
		}
		cursor = page.NextCursor
	}
	out.Reachable, out.Truncated = true, cursor != ""
	var sent []int64
	for _, e := range events {
		switch e.State {
		case "queued":
			out.Waiting++
		case "failed":
			out.Failing++
		case "refused":
			out.Refused++
		case "sent":
			sent = append(sent, e.Sequence)
		}
	}
	if err := a.deps.Store.Do(ctx, org.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		have, err := u.PeopleLinks().InboxSequences(ctx, l.ID, 1, max(out.NemuraHighestSequence, 1))
		if err != nil {
			return err
		}
		if len(have) > 0 {
			out.ReceivedHighestSequence = have[len(have)-1]
		}
		out.Missing = missingRuns(sent, have)
		return nil
	}); err != nil {
		return out, l, err
	}
	return out, l, nil
}

// missingRuns are the runs of the sequence numbers Nemura says it sent that the inbox does not hold, no run wider than a replay request may ask for.
func missingRuns(sent, have []int64) []SequenceRange {
	out := []SequenceRange{}
	for _, s := range sent {
		if _, found := slices.BinarySearch(have, s); found {
			continue
		}
		if n := len(out); n > 0 && out[n-1].To == s-1 && s-out[n-1].From < linksend.MaxReplaySpan {
			out[n-1].To = s
			continue
		}
		out = append(out, SequenceRange{From: s, To: s})
	}
	return out
}

type (
	peopleLinkHealthOutput struct{ Body PeopleLinkHealth }
	peopleLinkRepairInput  struct {
		KeyHeader
		PreferHeader
	}
	peopleLinkRepairOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PeopleLinkRepair
	}
)

// PeopleLinkRepair is what asking Nemura to send again queued.
type PeopleLinkRepair struct {
	Requested []SequenceRange  `json:"requested" nullable:"false" doc:"The runs of sequence numbers Nemura was asked to send again: one request each. Empty when there was nothing missing."`
	Health    PeopleLinkHealth `json:"health" doc:"How the link stood when it was asked."`
}

func registerPeopleLinkHealth(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetPeopleLinkHealth, huma.Operation{
		OperationID: "getPeopleLinkHealth", Method: http.MethodGet, Path: "/v1/people-link/health", Tags: []string{"People"},
		Summary: "Show how the link to the people platform stands",
		Description: "Reads Nemura's event log by identity (sequence, type, object and state, never a payload) with the link's read-only client and sets it against what Fyniq's inbox received (PRD-001 FR-PPL-08, FR-PPL-15): " +
			"how many events wait, fail or were refused, the highest sequence on each side and the runs of sequence numbers Nemura says it delivered and Fyniq never received, which is a gap. When Nemura cannot " +
			"be read it says why and the numbers are Fyniq's own. Needs People and HR's right. Read-only; no side effects. Nothing here reaches the AI or MCP (§9.6).",
	}, locks, func(ctx context.Context, _ *struct{}) (*peopleLinkHealthOutput, error) {
		p, _ := authn.From(ctx)
		h, _, err := a.linkHealth(ctx, p)
		if err != nil {
			return nil, err
		}
		return &peopleLinkHealthOutput{Body: h}, nil
	})
	register(a, authz.RepairPeopleLink, huma.Operation{
		OperationID: "repairPeopleLink", Method: http.MethodPost, Path: "/v1/people-link/repair", Tags: []string{"People"},
		Summary: "Ask Nemura to send again what Fyniq missed",
		Description: "Finds the gaps in the link as getPeopleLinkHealth does and sends Nemura one link.replay.requested for each run of missing sequence numbers (at most 10,000 wide), signed like every event, through the link's one " +
			"event scope. Nemura answers by sending the current state of the objects the run names, at the version they have now, never an old payload; Fyniq applies a version it missed and ignores one it holds. " +
			"Side effects: one event queued for each run and its audit event (the range, never a payload). Idempotent with Idempotency-Key; Prefer: return=preview shows what it would ask and queues nothing. " +
			"Needs People and HR's right. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodePeopleLink}, locks...), func(ctx context.Context, in *peopleLinkRepairInput) (*peopleLinkRepairOutput, error) {
		p, _ := authn.From(ctx)
		health, link, err := a.linkHealth(ctx, p)
		if err != nil {
			return nil, err
		}
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PeopleLinkRepair, error) {
			out := PeopleLinkRepair{Requested: []SequenceRange{}, Health: health}
			if !health.Reachable {
				return out, nil // nothing known to ask for: the health says why
			}
			for _, r := range health.Missing {
				id, err := linksend.Queue(ctx, u, linksend.TypeReplayRequested, "link", link.ID.String(), 0, linksend.ReplayRequested{FromSequence: r.From, ToSequence: r.To})
				if err != nil {
					return out, err
				}
				if id != "" {
					out.Requested = append(out.Requested, r)
				}
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &peopleLinkRepairOutput{Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})
}
