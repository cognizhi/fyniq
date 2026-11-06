package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/member"
	"github.com/cognizhi/fyniq/internal/domain/succession"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Owner succession (FR-ADM-09, rev 0.181)
//
// When another Owner is permanently unavailable, an Owner starts a succession with a reason and the evidence it rests
// on, attached as files; the outgoing Owner is told at once and for 7 days may object, which ends it, and the one who
// started it may withdraw it. From the 7th day a Super admin, any one, completes it: the member it names, if any,
// becomes an Owner, and the outgoing Owner's membership ends. Every Owner is told of each step (security.owners).

// SuccessionMember is a member a succession names.
type SuccessionMember struct {
	MemberID string `json:"member_id" format:"uuid" doc:"The membership." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	UserID   string `json:"user_id" format:"uuid" doc:"The user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Name     string `json:"name,omitempty" doc:"Their name, while they are a member." example:"Ben Ong"`
}

// OwnerSuccession is a succession of an Owner who is permanently unavailable (FR-ADM-09).
type OwnerSuccession struct {
	ID             string            `json:"id" format:"uuid" doc:"The succession's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	Outgoing       SuccessionMember  `json:"outgoing" doc:"The Owner whose membership it ends."`
	Successor      *SuccessionMember `json:"successor,omitempty" doc:"The member who becomes an Owner when it completes; absent when it names none."`
	Reason         string            `json:"reason" doc:"Why, as the Owner who started it gave it." example:"Ben left the company on 31 October; the board resolved on his removal"`
	EvidenceKind   string            `json:"evidence_kind" enum:"board_resolution,accountant_confirmation" doc:"What it rests on: a board resolution, or the external accountant's recorded confirmation (FR-DOC-05)." example:"board_resolution"`
	Files          int               `json:"files" doc:"How many evidence files are attached: completing it needs at least one." example:"1"`
	StartedBy      Person            `json:"started_by" doc:"The Owner who started it."`
	StartedAt      time.Time         `json:"started_at" doc:"When; the outgoing Owner was told then." example:"2026-11-06T09:00:00Z"`
	DueAt          time.Time         `json:"due_at" doc:"When its 7 days of notice end and a Super admin may complete it." example:"2026-11-13T09:00:00Z"`
	Status         string            `json:"status" enum:"notice,objected,withdrawn,completed" doc:"notice while it waits; objected by the outgoing Owner; withdrawn by the one who started it; completed by a Super admin." example:"notice"`
	EndedBy        *Person           `json:"ended_by,omitempty" doc:"Who objected, withdrew or completed it."`
	EndedAt        *time.Time        `json:"ended_at,omitempty" doc:"When." example:"2026-11-13T10:00:00Z"`
	Version        int               `json:"version" doc:"Its version, as its ETag." example:"1"`
	YouMayObject   bool              `json:"you_may_object" doc:"Whether you, the outgoing Owner, may object to it now." example:"false"`
	YouMayWithdraw bool              `json:"you_may_withdraw" doc:"Whether you, who started it, may withdraw it now." example:"true"`
	YouMayAttach   bool              `json:"you_may_attach" doc:"Whether you may attach evidence to it now: an Owner other than the outgoing one, while it waits." example:"true"`
	YouMayComplete bool              `json:"you_may_complete" doc:"Whether you, a Super admin, may complete it now: its notice is over and a file is attached." example:"false"`
}

// OwnerSuccessionRequest starts a succession.
type OwnerSuccessionRequest struct {
	Reason            string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the Owner is permanently unavailable: departure, incapacity or death, and what was decided. The outgoing Owner and every Owner read it." example:"Ben left the company on 31 October; the board resolved on his removal"`
	EvidenceKind      string `json:"evidence_kind" enum:"board_resolution,accountant_confirmation" doc:"What it rests on: attach the file with addOwnerSuccessionEvidence before it is completed." example:"board_resolution"`
	SuccessorMemberID string `json:"successor_member_id,omitempty" format:"uuid" doc:"A member who becomes an Owner when it completes, such as a new director, so the company keeps two Owners; leave it out to name none." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f03"`
}

type SuccessionPath struct {
	SuccessionID string `path:"succession_id" format:"uuid" doc:"The succession's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
}

type startSuccessionInput struct {
	MemberPath
	KeyHeader
	PreferHeader
	Body OwnerSuccessionRequest
}

type successionActInput struct {
	SuccessionPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type successionEvidenceInput struct {
	SuccessionPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	RawBody huma.MultipartFormFiles[EvidenceUpload]
}

type successionEvidenceListInput struct {
	SuccessionPath
	PageQuery
}

type successionOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The succession's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     OwnerSuccession
}

type successionListInput struct {
	PageQuery
}

type successionListOutput struct {
	Body struct {
		Items      []OwnerSuccession `json:"items" nullable:"false" doc:"The successions on this page, latest first."`
		NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
	}
}

// SuccessionCodes are the codes a succession may answer.
func SuccessionCodes() []string { return append([]string{storage.CodeSuccession}, succession.Codes...) }

// successionView is what a succession needs to be shown to p: the members' names, the files attached, and whether p is
// a Super admin.
type successionView struct {
	names map[uuid.UUID]string
	files map[uuid.UUID]int
	super bool
}

func (a *API) successionView(ctx context.Context, u storage.UnitOfWork, p authn.Principal, all []storage.OwnerSuccession) (successionView, error) {
	names, err := memberNames(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return successionView{}, err
	}
	v := successionView{names: names, files: map[uuid.UUID]int{}}
	for _, s := range all {
		n, err := attachedFiles(ctx, u, s.DocumentID)
		if err != nil {
			return v, err
		}
		v.files[s.ID] = n
	}
	if p.Issuer != "" && a.deps.Installation != nil {
		if _, err := a.deps.Installation.SuperAdminFor(ctx, p.Issuer, p.Subject); err == nil {
			v.super = true
		} else if !errors.Is(err, ports.ErrNotFound) {
			return v, err
		}
	}
	return v, nil
}

// attachedFiles counts a succession's files that are not removed.
func attachedFiles(ctx context.Context, u storage.UnitOfWork, doc uuid.UUID) (int, error) {
	all, err := u.Evidence().OfDocument(ctx, doc, uuid.Nil, 1000)
	n := 0
	for _, e := range all {
		if e.State != storage.EvidenceRemoved {
			n++
		}
	}
	return n, err
}

func (a *API) successionOf(s storage.OwnerSuccession, p authn.Principal, v successionView) OwnerSuccession {
	now := a.deps.Clock.Now()
	user := func(id uuid.UUID) Person { return Person{Kind: "user", ID: id.String(), Name: v.names[id]} }
	out := OwnerSuccession{ID: s.ID.String(), Outgoing: SuccessionMember{MemberID: s.OutgoingMembershipID.String(), UserID: s.OutgoingUserID.String(),
		Name: v.names[s.OutgoingUserID]}, Reason: s.Reason, EvidenceKind: s.EvidenceKind, Files: v.files[s.ID], StartedBy: user(s.StartedBy),
		StartedAt: s.StartedAt.UTC(), DueAt: s.DueAt.UTC(), Status: s.Status, EndedAt: s.EndedAt, Version: s.Version}
	if s.SuccessorMembershipID != nil {
		out.Successor = &SuccessionMember{MemberID: s.SuccessorMembershipID.String()}
	}
	if s.EndedBy != nil {
		who := user(*s.EndedBy)
		out.EndedBy = &who
	}
	open := s.Status == storage.SuccessionNotice
	out.YouMayObject = open && p.UserID == s.OutgoingUserID
	out.YouMayWithdraw = open && p.UserID == s.StartedBy
	out.YouMayAttach = open && p.UserID != s.OutgoingUserID && slices.Contains(p.Roles, authz.Owner)
	out.YouMayComplete = open && v.super && !now.Before(s.DueAt) && out.Files > 0
	return out
}

// withSuccessor fills in the successor's user and name from the members.
func withSuccessor(out OwnerSuccession, members []storage.Member) OwnerSuccession {
	if out.Successor == nil {
		return out
	}
	for _, m := range members {
		if m.ID.String() == out.Successor.MemberID {
			out.Successor.UserID, out.Successor.Name = m.UserID.String(), m.DisplayName
		}
	}
	return out
}

// tellOwners notifies every Owner, and the outgoing Owner, of a step of succession s (FR-ADM-09): a security notice,
// always on and emailed at once.
func (a *API) tellOwners(ctx context.Context, u storage.UnitOfWork, s storage.OwnerSuccession, title string, except uuid.UUID) error {
	if err := a.notify(ctx, u, notify.Event{Type: notify.OwnersType, RecordType: "owner_succession", RecordID: &s.ID, Title: title,
		Link: "/settings/members", Audience: notify.Audience{Users: []uuid.UUID{s.OutgoingUserID}}}); err != nil {
		return err
	}
	return a.notify(ctx, u, notify.Event{Type: notify.OwnersType, RecordType: "owner_succession", RecordID: &s.ID, Title: title,
		Link: "/settings/members", Audience: notify.Audience{Capability: string(authz.UsersAndRoles), Need: "A", Except: []uuid.UUID{except, s.OutgoingUserID}}})
}

func registerOwnerSuccession(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		CodeNotFound, CodeSessionRequired, CodeStepUpRequired, storage.CodeVersion}, append(locks, SuccessionCodes()...)...)

	register(a, authz.ListOwnerSuccessions, huma.Operation{
		OperationID: "listOwnerSuccessions", Method: http.MethodGet, Path: "/v1/owner-successions", Tags: []string{"Members"},
		Summary: "List the Owner successions",
		Description: "Every succession of an Owner who is permanently unavailable, latest first (FR-ADM-09): its outgoing Owner and successor, " +
			"its reason and evidence, its 7 days of notice and how it ended, with what you may do with it, a page at a time. Read-only.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *successionListInput) (*successionListOutput, error) {
		p, _ := authn.From(ctx)
		out := &successionListOutput{}
		out.Body.Items = []OwnerSuccession{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.OwnerSuccessions().List(ctx)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(x storage.OwnerSuccession) string { return x.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			v, err := a.successionView(ctx, u, p, all)
			if err != nil {
				return err
			}
			members, err := u.Members().List(ctx, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			for _, s := range all {
				out.Body.Items = append(out.Body.Items, withSuccessor(a.successionOf(s, p, v), members))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.StartOwnerSuccession, huma.Operation{
		OperationID: "startOwnerSuccession", Method: http.MethodPost, Path: "/v1/members/{member_id}/succession", Tags: []string{"Members"},
		DefaultStatus: http.StatusCreated, Summary: "Start the succession of an Owner who is permanently unavailable",
		Description: "Starts a succession of another Owner who is permanently unavailable, such as after a departure, incapacity or death " +
			"(FR-ADM-09; PRD-001 rev 0.181, the owner's decisions of 6 Nov 2026). Needs an Owner signed in who passed step-up in the last 5 " +
			"minutes, a reason and the kind of evidence it rests on: a board resolution, or the external accountant's recorded " +
			"confirmation, attached with addOwnerSuccessionEvidence. It may name a member who becomes an Owner when it completes. The " +
			"outgoing Owner is told at once, by email, and for 7 days may object (objectToOwnerSuccession), which ends it; you may " +
			"withdraw it. From the 7th day a Super admin, any one, completes it (completeOwnerSuccession). Refused for a member who is no " +
			"Owner or is you (SUCC-01), a successor who is not an active member other than the outgoing Owner (SUCC-06), and while another " +
			"succession of the same Owner waits (STORAGE-SUCCESSION-01). Side effects: the succession and the document that holds its " +
			"files, audited; notices to the outgoing Owner and every Owner. Idempotent with Idempotency-Key; Prefer: return=preview checks " +
			"it and saves nothing.",
	}, writes, func(ctx context.Context, in *startSuccessionInput) (*successionOutput, error) {
		id, err := parseID("member_id", in.MemberID)
		if err != nil {
			return nil, err
		}
		reason := strings.TrimSpace(in.Body.Reason)
		var bad []FieldProblem
		if reason == "" {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.reason", Message: "is blank", Fix: "Say why the Owner is permanently unavailable"})
		}
		if !slices.Contains(succession.EvidenceKinds, in.Body.EvidenceKind) {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.evidence_kind", Message: "is not a kind of evidence",
				Fix: "Send board_resolution or accountant_confirmation"})
		}
		var successor *uuid.UUID
		if s := strings.TrimSpace(in.Body.SuccessorMemberID); s != "" {
			x, err := uuid.Parse(s)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.successor_member_id", Message: "is not a member ID", Fix: "Send a member's ID, or leave it out"})
			}
			successor = &x
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		p, err := a.steppedUpPerson(ctx, "Starting a succession")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (OwnerSuccession, error) {
			now := a.deps.Clock.Now()
			members, err := u.Members().List(ctx, now)
			if err != nil {
				return OwnerSuccession{}, err
			}
			i := slices.IndexFunc(members, func(m storage.Member) bool { return m.ID == id })
			if i < 0 {
				return OwnerSuccession{}, problem(CodeNotFound, "No member with that ID exists in this organisation.", "List the members to find their IDs")
			}
			out := members[i]
			owner := func(m storage.Member) bool {
				return slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == string(authz.Owner) })
			}
			if !owner(out) || out.UserID == p.UserID {
				return OwnerSuccession{}, problem(succession.CodeNotOwner, out.DisplayName+" is not another Owner, so there is no Owner to succeed.",
					"Start a succession only for another Owner who is permanently unavailable; remove other members as usual")
			}
			open, err := u.OwnerSuccessions().List(ctx)
			if err != nil {
				return OwnerSuccession{}, err
			}
			if slices.ContainsFunc(open, func(x storage.OwnerSuccession) bool {
				return x.OutgoingMembershipID == out.ID && x.Status == storage.SuccessionNotice
			}) {
				return OwnerSuccession{}, problem(storage.CodeSuccession, "A succession of "+out.DisplayName+" waits already.",
					"Withdraw it, or wait for it to end, before starting another")
			}
			if successor != nil && (*successor == out.ID || !slices.ContainsFunc(members, func(m storage.Member) bool { return m.ID == *successor })) {
				return OwnerSuccession{}, problem(succession.CodeSuccessor, "The successor must be an active member other than the outgoing Owner.",
					"Name another member, or leave the successor out")
			}
			b, err := json.Marshal(map[string]string{"outgoing_membership_id": out.ID.String()})
			if err != nil {
				return OwnerSuccession{}, err
			}
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOwnerSuccession, Payload: b})
			if err != nil {
				return OwnerSuccession{}, err
			}
			if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil { // its files are append-only
				return OwnerSuccession{}, err
			}
			s, err := u.OwnerSuccessions().Start(ctx, storage.NewOwnerSuccession{DocumentID: doc.ID, OutgoingMembershipID: out.ID, OutgoingUserID: out.UserID,
				SuccessorMembershipID: successor, Reason: reason, EvidenceKind: in.Body.EvidenceKind, StartedBy: p.UserID, StartedAt: now})
			if err != nil {
				return OwnerSuccession{}, err
			}
			if err := a.tellOwners(ctx, u, s, fmt.Sprintf("A succession of %s as Owner started: it ends their membership from %s unless they object",
				out.DisplayName, s.DueAt.UTC().Format("2 Jan 2006")), p.UserID); err != nil {
				return OwnerSuccession{}, err
			}
			v, err := a.successionView(ctx, u, p, []storage.OwnerSuccession{s})
			return withSuccessor(a.successionOf(s, p, v), members), err
		})
		if err != nil {
			return nil, err
		}
		return &successionOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	// act ends a waiting succession at the version read, after check, as p, and tells every Owner.
	act := func(ctx context.Context, in *successionActInput, what, status string,
		check func(context.Context, storage.UnitOfWork, storage.OwnerSuccession, authn.Principal) error,
		then func(context.Context, storage.UnitOfWork, storage.OwnerSuccession) error, tell string) (*successionOutput, error) {
		id, err := parseID("succession_id", in.SuccessionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		var p authn.Principal
		if status == storage.SuccessionCompleted {
			if _, err := a.superAdmin(ctx, what); err != nil {
				return nil, err
			}
			p, _ = authn.From(ctx)
			if p.Organisation == uuid.Nil {
				return nil, problem(CodeSessionRequired, what+" is done in the organisation you are signed in to.", "Sign in to the organisation, then complete it there")
			}
		} else if p, err = a.steppedUpPerson(ctx, what); err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OwnerSuccession, error) {
			s, err := u.OwnerSuccessions().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				return OwnerSuccession{}, problem(CodeNotFound, "No succession with that ID exists in this organisation.", "List the successions to find their IDs")
			}
			if err != nil {
				return OwnerSuccession{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(s.Version)); err != nil {
				return OwnerSuccession{}, err
			}
			if s.Status != storage.SuccessionNotice {
				return OwnerSuccession{}, problem(succession.CodeEnded, "The succession was "+s.Status+" already, so nothing changes.", "Read it again")
			}
			if err := check(ctx, u, s, p); err != nil {
				return OwnerSuccession{}, err
			}
			ended, err := u.OwnerSuccessions().End(ctx, s.ID, s.Version, status, p.UserID, a.deps.Clock.Now())
			if err != nil {
				return OwnerSuccession{}, err
			}
			if err := a.tellOwners(ctx, u, ended, tell, p.UserID); err != nil {
				return OwnerSuccession{}, err
			}
			if then != nil {
				if err := then(ctx, u, ended); err != nil {
					return OwnerSuccession{}, err
				}
			}
			members, err := u.Members().List(ctx, a.deps.Clock.Now())
			if err != nil {
				return OwnerSuccession{}, err
			}
			v, err := a.successionView(ctx, u, p, []storage.OwnerSuccession{ended})
			return withSuccessor(a.successionOf(ended, p, v), members), err
		})
		if err != nil {
			return nil, err
		}
		return &successionOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	}
	actDoc := " Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing."

	register(a, authz.ObjectToOwnerSuccession, huma.Operation{
		OperationID: "objectToOwnerSuccession", Method: http.MethodPost, Path: "/v1/owner-successions/{succession_id}/object", Tags: []string{"Members"},
		Summary: "Object to a succession of you as Owner",
		Description: "Ends a waiting succession of you as Owner (FR-ADM-09): you are not permanently unavailable. Only the outgoing Owner, " +
			"signed in and stepped up in the last 5 minutes (SUCC-04 otherwise); every Owner is told." + actDoc,
	}, writes, func(ctx context.Context, in *successionActInput) (*successionOutput, error) {
		return act(ctx, in, "Objecting to a succession", storage.SuccessionObjected, func(_ context.Context, _ storage.UnitOfWork, s storage.OwnerSuccession, p authn.Principal) error {
			if p.UserID != s.OutgoingUserID {
				return problem(succession.CodeNotYours, "Only the outgoing Owner objects to a succession.", "Ask them to sign in and object, or withdraw it if you started it")
			}
			return nil
		}, nil, "The outgoing Owner objected to the succession, so it ended")
	})

	register(a, authz.WithdrawOwnerSuccession, huma.Operation{
		OperationID: "withdrawOwnerSuccession", Method: http.MethodPost, Path: "/v1/owner-successions/{succession_id}/withdraw", Tags: []string{"Members"},
		Summary: "Withdraw a succession you started",
		Description: "Ends a waiting succession you started (FR-ADM-09). Only the Owner who started it, stepped up in the last 5 minutes " +
			"(SUCC-04 otherwise); the outgoing Owner and every Owner are told." + actDoc,
	}, writes, func(ctx context.Context, in *successionActInput) (*successionOutput, error) {
		return act(ctx, in, "Withdrawing a succession", storage.SuccessionWithdrawn, func(_ context.Context, _ storage.UnitOfWork, s storage.OwnerSuccession, p authn.Principal) error {
			if p.UserID != s.StartedBy {
				return problem(succession.CodeNotYours, "Only the Owner who started a succession withdraws it.", "Ask them to withdraw it")
			}
			return nil
		}, nil, "The succession was withdrawn, so it ended")
	})

	register(a, authz.CompleteOwnerSuccession, huma.Operation{
		OperationID: "completeOwnerSuccession", Method: http.MethodPost, Path: "/v1/owner-successions/{succession_id}/complete", Tags: []string{"Members"},
		Summary: "Complete a succession, as a Super admin",
		Description: "Completes a succession once its 7 days of notice are over (FR-ADM-09; the owner's decisions of 6 Nov 2026): a Super " +
			"admin, any one, the Owner who started it included, stepped up in the last 5 minutes, in the organisation they are signed in " +
			"to. Refused before its due time (SUCC-02) and without an evidence file attached (SUCC-03). In one transaction: the successor " +
			"it names, if any, becomes an Owner; the outgoing Owner's membership ends, each role revoked and their tokens ended; the " +
			"succession completed, audited; every Owner and the outgoing Owner told." + actDoc,
	}, append(writes, CodeNotSuperAdmin, member.CodeLastOwner, member.CodeHeld), func(ctx context.Context, in *successionActInput) (*successionOutput, error) {
		return act(ctx, in, "Completing a succession", storage.SuccessionCompleted, func(ctx context.Context, u storage.UnitOfWork, s storage.OwnerSuccession, _ authn.Principal) error {
			if a.deps.Clock.Now().Before(s.DueAt) {
				return problem(succession.CodeNotDue, fmt.Sprintf("The succession's 7 days of notice end at %s.", s.DueAt.UTC().Format(time.RFC3339)),
					"Wait until then: the outgoing Owner may still object")
			}
			if n, err := attachedFiles(ctx, u, s.DocumentID); err != nil || n == 0 {
				if err != nil {
					return err
				}
				return problem(succession.CodeNoFile, "No evidence file is attached to the succession.",
					"Attach the board resolution or the accountant's confirmation, then complete it")
			}
			return nil
		}, func(ctx context.Context, u storage.UnitOfWork, s storage.OwnerSuccession) error {
			if s.SuccessorMembershipID != nil {
				m, err := findMember(ctx, u, *s.SuccessorMembershipID, a.deps.Clock.Now())
				if err != nil {
					return err
				}
				if !slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == string(authz.Owner) }) {
					if _, err := a.assignRoleNow(ctx, u, m.ID, string(authz.Owner), nil); err != nil {
						return err
					}
				}
			}
			_, err := a.removeMemberNow(ctx, u, s.OutgoingMembershipID)
			return err
		}, "The succession completed: the outgoing Owner's membership ended")
	})

	register(a, authz.AddOwnerSuccessionEvidence, huma.Operation{
		OperationID: "addOwnerSuccessionEvidence", Method: http.MethodPost, Path: "/v1/owner-successions/{succession_id}/evidence", Tags: []string{"Evidence"},
		DefaultStatus: http.StatusCreated, Summary: "Attach the evidence of a succession",
		Description: "Attaches a file to a waiting succession, as multipart/form-data: the board resolution, or the external accountant's " +
			"recorded confirmation (FR-ADM-09, FR-DOC-05). An Owner other than the outgoing one. The file is checked and scanned as any " +
			"evidence is (FR-DOC-08) and committed to the retention-locked store at once: it is append-only (FR-DOC-13). Needs If-Match " +
			"with the succession's ETag. Idempotent with Idempotency-Key; Prefer: return=preview runs every check and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, succession.CodeEnded, succession.CodeNotYours}, append(locks, EvidenceCodes...)...),
		func(ctx context.Context, in *successionEvidenceInput) (*evidenceWriteOutput, error) {
			id, err := parseID("succession_id", in.SuccessionID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			file, d, err := a.upload(ctx, in.RawBody.Data())
			if err != nil {
				return nil, err
			}
			request := struct {
				Succession, IfMatch, SHA256, Name string
				Details                           evidence.Details
			}{in.SuccessionID, in.IfMatch, file.OriginalSHA256, file.Name, d}
			key := evidence.StagingKey(p.Organisation, uuid.Must(uuid.NewV7()))
			if !wantsPreview(in.Prefer) {
				// Staged before the unit of work: if it rolls back, the sweeper deletes the file (FR-DOC-13).
				if err := a.deps.Staging.Put(ctx, key, bytes.NewReader(file.Data)); err != nil {
					logFailure(ctx, err)
					return nil, problem(CodeUnavailable, "The file could not be stored.", "Try again in a few minutes.")
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, request, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Evidence, error) {
				s, err := u.OwnerSuccessions().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return Evidence{}, problem(CodeNotFound, "No succession with that ID exists in this organisation.", "List the successions to find their IDs")
				}
				if err != nil {
					return Evidence{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(s.Version)); err != nil {
					return Evidence{}, err
				}
				if s.Status != storage.SuccessionNotice {
					return Evidence{}, problem(succession.CodeEnded, "The succession was "+s.Status+" already, so it takes no more files.", "Read it again")
				}
				if p.UserID == s.OutgoingUserID {
					return Evidence{}, problem(succession.CodeNotYours, "The outgoing Owner does not attach the evidence of their own succession.",
						"Object to it instead, if it is wrong")
				}
				e, err := u.Evidence().Attach(ctx, storage.NewEvidence{DocumentID: s.DocumentID, EvidenceType: d.EvidenceType, Note: d.Note, RelatesOn: d.RelatesOn,
					FileName: file.Name, MediaType: file.MediaType, Size: int64(len(file.Data)), SHA256: file.SHA256, OriginalSHA256: file.OriginalSHA256,
					Source: d.Source, DataClass: d.DataClass, StagingKey: key, UploadedBy: uploader(ctx)})
				if err != nil {
					return Evidence{}, err
				}
				if a.deps.Outbox != nil { // committed now, not at the next sweep
					if _, err := a.deps.Outbox.Enqueue(ctx, u, p.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker,
						Kind: EvidenceCommitKind, Payload: []byte(`{}`)}); err != nil {
						return Evidence{}, err
					}
				}
				return evidenceOf(e), nil
			})
			if (err != nil || w.Replayed) && !wantsPreview(in.Prefer) {
				_ = a.deps.Staging.Delete(context.WithoutCancel(ctx), key) // no row uses it; the sweeper would delete it a day later
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &evidenceWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &evidenceWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
	if op := a.OpenAPI().Paths["/v1/owner-successions/{succession_id}/evidence"].Post; op != nil && op.RequestBody != nil {
		if mt := op.RequestBody.Content["multipart/form-data"]; mt != nil && mt.Schema != nil && mt.Schema.Properties["file"] != nil {
			mt.Schema.Properties["file"].Examples = []any{"%PDF-1.7 … (the file's bytes)"}
		}
	}

	register(a, authz.ListOwnerSuccessionEvidence, huma.Operation{
		OperationID: "listOwnerSuccessionEvidence", Method: http.MethodGet, Path: "/v1/owner-successions/{succession_id}/evidence", Tags: []string{"Evidence"},
		Summary:     "List a succession's evidence",
		Description: "The files attached to a succession, oldest first, a page at a time (FR-ADM-09, FR-DOC-05). Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *successionEvidenceListInput) (*evidenceListOutput, error) {
		id, err := parseID("succession_id", in.SuccessionID)
		if err != nil {
			return nil, err
		}
		cur, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		after := uuid.Nil
		if cur != "" {
			if after, err = uuid.Parse(cur); err != nil {
				return nil, problem(CodeCursor, "The cursor is not one this list gave.", "Start again without a cursor.")
			}
		}
		size := in.PageSize
		if size <= 0 {
			size = 100
		}
		p, _ := authn.From(ctx)
		out := evidenceListOutput{Body: EvidencePage{Items: []Evidence{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			s, err := u.OwnerSuccessions().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				return problem(CodeNotFound, "No succession with that ID exists in this organisation.", "List the successions to find their IDs")
			}
			if err != nil {
				return err
			}
			page, err := u.Evidence().OfDocument(ctx, s.DocumentID, after, size+1)
			if err != nil {
				return err
			}
			if len(page) > size {
				page = page[:size]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].ID.String())
			}
			for _, e := range page {
				out.Body.Items = append(out.Body.Items, evidenceOf(e))
			}
			return nil
		})
		return &out, err
	})
}
