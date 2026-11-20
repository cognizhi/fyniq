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
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes of the cap table grant (ENG-23).
const (
	CodeCapGrantOwner = "CAPGRANT-01" // only an Owner grants or ends a grant
	CodeCapGrantNone  = "CAPGRANT-02" // the viewer holds no grant of the full cap table
)

// CapTableGrant is an Owner's grant of the full cap table to a viewer (FR-EQ-24).
type CapTableGrant struct {
	ID           string       `json:"id" format:"uuid" doc:"The grant's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a10"`
	UserID       MemberUserID `json:"user_id" doc:"The viewer it was granted to."`
	Reason       string       `json:"reason" doc:"Why the Owner granted it." example:"Lead investor's board seat"`
	GrantedAt    time.Time    `json:"granted_at" doc:"When." example:"2026-11-23T03:00:00Z"`
	Live         bool         `json:"live" doc:"True until an Owner revokes it." example:"true"`
	RevokedAt    *time.Time   `json:"revoked_at,omitempty" doc:"When it ended." example:"2026-12-01T03:00:00Z"`
	RevokeReason string       `json:"revoke_reason,omitempty" doc:"Why it ended." example:"The seat ended"`
}

func capGrantOf(g storage.CapTableGrant) CapTableGrant {
	return CapTableGrant{ID: g.ID.String(), UserID: MemberUserID(g.UserID.String()), Reason: g.Reason, GrantedAt: g.GrantedAt.UTC(), Live: g.RevokedAt == nil, RevokedAt: g.RevokedAt, RevokeReason: g.RevokeReason}
}

type capGrantOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CapTableGrant
}

// CapTableGrantPath names a grant in the path.
type CapTableGrantPath struct {
	ID string `path:"id" format:"uuid" doc:"The grant's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a10"`
}

type grantCapTableInput struct {
	KeyHeader
	PreferHeader
	Body struct {
		UserID MemberUserID `json:"user_id" doc:"The Board or investor viewer: an active member holding the Board viewer role."`
		Reason string       `json:"reason" minLength:"3" maxLength:"500" doc:"Why the full cap table is opened to them." example:"Lead investor's board seat"`
	}
}

type revokeCapTableInput struct {
	CapTableGrantPath
	KeyHeader
	PreferHeader
	Body struct {
		Reason string `json:"reason" minLength:"3" maxLength:"500" doc:"Why the grant ends." example:"The seat ended"`
	}
}

func registerCapTableGrants(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, CodeCapGrantOwner,
		storage.CodeCapTableGrant, storage.CodeCapTableGrantLive}, locks...)

	register(a, authz.ListCapTableGrants, huma.Operation{
		OperationID: "listCapTableGrants", Method: http.MethodGet, Path: "/v1/cap-table-grants", Tags: []string{"Equity"},
		Summary:     "List the grants of the full cap table",
		Description: "The Owner's grants of the full cap table to Board or investor viewers (FR-EQ-24), live and ended, newest first, a page at a time. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *struct{ PageQuery }) (*struct {
		Body struct {
			Items      []CapTableGrant `json:"items" nullable:"false" doc:"The grants, newest first."`
			NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
		}
	}, error) {
		p, _ := authn.From(ctx)
		var all []storage.CapTableGrant
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.CapTableGrants().List(ctx)
			return err
		}); err != nil {
			return nil, err
		}
		page, next, err := pageOf(all, func(g storage.CapTableGrant) string { return g.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items      []CapTableGrant `json:"items" nullable:"false" doc:"The grants, newest first."`
				NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
			}
		}{}
		out.Body.Items, out.Body.NextCursor = []CapTableGrant{}, next
		for _, g := range page {
			out.Body.Items = append(out.Body.Items, capGrantOf(g))
		}
		return out, nil
	})

	register(a, authz.GrantCapTableAccess, huma.Operation{
		OperationID: "grantCapTableAccess", Method: http.MethodPost, Path: "/v1/cap-table-grants", Tags: []string{"Equity"}, DefaultStatus: http.StatusCreated,
		Summary: "Open the full cap table to a viewer",
		Description: "An Owner grants a Board or investor viewer the full cap table, with a reason (FR-EQ-24; CAPGRANT-01). By default a viewer reads their own holding only. The person must be an " +
			"active member holding the Board viewer role and may hold one live grant. Side effects: the grant and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *grantCapTableInput) (*capGrantOutput, error) {
		p, _ := authn.From(ctx)
		if p.Class != authn.ClassUser || !slices.Contains(p.Roles, authz.Owner) {
			return nil, problem(CodeCapGrantOwner, "Only an Owner opens the full cap table to a viewer.", "Ask an Owner")
		}
		user, err := uuid.Parse(string(in.Body.UserID))
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.user_id", Message: "is not a member's user ID", Fix: "Send the user ID listMembers gives"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CapTableGrant, error) {
			rs, err := u.Members().Roles(ctx, user, a.deps.Clock.Now())
			if err != nil {
				return CapTableGrant{}, err
			}
			board := false
			for _, r := range rs {
				board = board || authz.Role(r.Role) == authz.BoardViewer
			}
			if !board {
				return CapTableGrant{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.user_id", Message: "is not an active member with the Board viewer role", Fix: "Choose a Board or investor viewer"}})
			}
			g, err := u.CapTableGrants().Grant(ctx, user, in.Body.Reason, uploader(ctx), a.deps.Clock.Now())
			return capGrantOf(g), err
		})
		if err != nil {
			return nil, err
		}
		return &capGrantOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.RevokeCapTableAccess, huma.Operation{
		OperationID: "revokeCapTableAccess", Method: http.MethodPost, Path: "/v1/cap-table-grants/{id}/revoke", Tags: []string{"Equity"},
		Summary:     "End a viewer's access to the full cap table",
		Description: "An Owner ends a grant, with a reason (FR-EQ-24; CAPGRANT-01). It ends once and is never deleted. Side effects: the revocation and its audit event. Idempotent with Idempotency-Key.",
	}, writes, func(ctx context.Context, in *revokeCapTableInput) (*capGrantOutput, error) {
		p, _ := authn.From(ctx)
		if p.Class != authn.ClassUser || !slices.Contains(p.Roles, authz.Owner) {
			return nil, problem(CodeCapGrantOwner, "Only an Owner ends a viewer's access to the full cap table.", "Ask an Owner")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CapTableGrant, error) {
			id, err := parseID("id", in.ID)
			if err != nil {
				return CapTableGrant{}, err
			}
			if _, err := u.CapTableGrants().Get(ctx, id); errors.Is(err, ports.ErrNotFound) {
				return CapTableGrant{}, problem(CodeNotFound, "No grant with that ID exists.", "List the grants")
			} else if err != nil {
				return CapTableGrant{}, err
			}
			g, err := u.CapTableGrants().Revoke(ctx, id, in.Body.Reason, uploader(ctx), a.deps.Clock.Now())
			return capGrantOf(g), err
		})
		if err != nil {
			return nil, err
		}
		return &capGrantOutput{Status: http.StatusOK, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.GetGrantedCapTable, huma.Operation{
		OperationID: "getGrantedCapTable", Method: http.MethodGet, Path: "/v1/cap-table/granted", Tags: []string{"Equity"},
		Summary: "Get the whole cap table, as a viewer an Owner granted it to",
		Description: "The cap table as getCapTable gives it (FR-EQ-20), for a Board or investor viewer whose Appendix D cell reads 'all if granted' (FR-EQ-24): only while an Owner's grant stands, " +
			"otherwise CAPGRANT-02. A role that reads the whole cap table already reads it here too. Read-only; no side effects.",
	}, append([]string{CodeInvalid, CodeCapGrantNone}, locks...), func(ctx context.Context, in *capTableInput) (*capTableOutput, error) {
		p, _ := authn.From(ctx)
		var out capTableOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if !authz.Decide(authz.GetCapTable, p.Roles, p.AI()).Allowed {
				if _, err := u.CapTableGrants().LiveOf(ctx, p.UserID); errors.Is(err, ports.ErrNotFound) {
					return problem(CodeCapGrantNone, "An Owner has not opened the full cap table to you.", "You can read your own holding; ask an Owner for the full cap table")
				} else if err != nil {
					return err
				}
			}
			day, err := a.capDay(ctx, u, "at", in.At)
			if err != nil {
				return err
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			if out.Body.CapTableAt, err = a.capTableAt(ctx, u, v, day); err != nil {
				return err
			}
			if in.Compare != "" {
				other, err := a.capDay(ctx, u, "compare", in.Compare)
				if err != nil {
					return err
				}
				t, err := a.capTableAt(ctx, u, v, other)
				if err != nil {
					return err
				}
				out.Body.Compared = &t
			}
			return nil
		})
		return &out, err
	})
}
