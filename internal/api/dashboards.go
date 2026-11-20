package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/dashboard"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeDashboardRole refuses a role's layout from anyone but an Owner or Administrator (DASH-04).
const CodeDashboardRole = "DASH-04"

var dashboardCodes = []string{dashboard.CodeTile, dashboard.CodeGrid, dashboard.CodeTooMany}

// DashboardTile is a tile placed on the grid.
type DashboardTile struct {
	Tile string `json:"tile" maxLength:"40" doc:"The tile: a KPI's key, from listDashboardTiles." example:"cash"`
	X    int    `json:"x" minimum:"0" maximum:"11" doc:"Its column, from 0." example:"0"`
	Y    int    `json:"y" minimum:"0" maximum:"100" doc:"Its row, from 0." example:"0"`
	W    int    `json:"w" minimum:"1" maximum:"12" doc:"Its width in columns, so that x + w is at most 12." example:"4"`
	H    int    `json:"h" minimum:"1" maximum:"4" doc:"Its height in rows." example:"1"`
}

// DashboardTileKind is a tile that can be placed.
type DashboardTileKind struct {
	Tile       string `json:"tile" doc:"The tile's key: a KPI's key." example:"cash"`
	Name       string `json:"name" doc:"What it is called." example:"Cash"`
	Definition string `json:"definition" doc:"How it is worked out." example:"Cash in the money accounts at the month end"`
}

// Dashboard is the layout that applies to the caller.
type Dashboard struct {
	Source      string          `json:"source" enum:"user,role,default" doc:"user: arranged by the caller; role: arranged for their role; default: the workbook's headline KPIs." example:"default"`
	Role        string          `json:"role,omitempty" doc:"The role whose layout it is, when source is role." example:"controller"`
	Tiles       []DashboardTile `json:"tiles" nullable:"false" doc:"The tiles, as placed. A tile shows only what the viewer's own rights read: if the caller may not read the KPIs the list is empty."`
	Version     int             `json:"version" doc:"The caller's own layout's version, 0 when they have none: it is the ETag of saveMyDashboard." example:"0"`
	GeneratedAt time.Time       `json:"generated_at" doc:"When the server read it: the tiles' figures are read then, from the books as they stand (the KPI's own ledger revision says which)." example:"2026-11-23T03:00:00Z"`
}

type dashboardOutput struct {
	ETag string `header:"ETag" doc:"The caller's own layout's version, for If-Match when saving it."`
	Body Dashboard
}

type dashboardTilesOutput struct {
	Body struct {
		Items      []DashboardTileKind `json:"items" nullable:"false" doc:"Every tile that can be placed, a page at a time."`
		NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
	}
}

// DashboardLayoutIn is a layout to save.
type DashboardLayoutIn struct {
	Tiles []DashboardTile `json:"tiles" maxItems:"24" nullable:"false" doc:"The tiles; an empty list clears the layout, so the role's, or the default, shows again."`
}

// DashboardRolePath names a role in the path.
type DashboardRolePath struct {
	Role string `path:"role" pattern:"^[a-z_]{3,40}$" doc:"The role, as a role is named in Fyniq." example:"controller"`
}

type saveMyDashboardInput struct {
	IfMatchHeader
	PreferHeader
	Body DashboardLayoutIn
}

type saveRoleDashboardInput struct {
	DashboardRolePath
	IfMatchHeader
	PreferHeader
	Body DashboardLayoutIn
}

type getRoleDashboardInput struct{ DashboardRolePath }

func tilesOf(raw json.RawMessage) []DashboardTile {
	out := []DashboardTile{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []DashboardTile{}
	}
	return out
}

func domainTiles(in []DashboardTile) []dashboard.Tile {
	out := make([]dashboard.Tile, len(in))
	for i, t := range in {
		out[i] = dashboard.Tile(t)
	}
	return out
}

func checkLayout(in DashboardLayoutIn) (json.RawMessage, error) {
	var bad []FieldProblem
	for _, p := range dashboard.Validate(domainTiles(in.Tiles)) {
		bad = append(bad, FieldProblem{Code: p.Code, Field: "body." + p.Field, Message: p.Message, Fix: p.Fix})
	}
	if len(bad) > 0 {
		return nil, invalid(bad)
	}
	raw, _ := json.Marshal(in.Tiles)
	return raw, nil
}

func registerDashboards(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion, storage.CodeDashboardLayout}, locks...)

	register(a, authz.ListDashboardTiles, huma.Operation{
		OperationID: "listDashboardTiles", Method: http.MethodGet, Path: "/v1/dashboard-tiles", Tags: []string{"Reports"},
		Summary:     "List the tiles a dashboard can hold",
		Description: "Every tile that can be placed on a dashboard (FR-VIZ-09): each KPI of the catalogue, by key, with its definition. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *struct{ PageQuery }) (*dashboardTilesOutput, error) {
		items, next, err := pageOf(kpi.Definitions, func(d kpi.Definition) string { return d.Key }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := &dashboardTilesOutput{}
		out.Body.Items, out.Body.NextCursor = []DashboardTileKind{}, next
		for _, d := range items {
			out.Body.Items = append(out.Body.Items, DashboardTileKind{Tile: d.Key, Name: d.Name, Definition: d.Formula})
		}
		return out, nil
	})

	register(a, authz.GetMyDashboard, huma.Operation{
		OperationID: "getMyDashboard", Method: http.MethodGet, Path: "/v1/dashboards/mine", Tags: []string{"Reports"},
		Summary: "Get the dashboard that applies to you",
		Description: "The caller's own arrangement of tiles if they made one, else the layout set for the first of their roles that has one, else the default (FR-VIZ-09, FR-KPI-05). " +
			"The layout holds no figure and no right: a caller who may not read the KPIs gets no tiles. Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*dashboardOutput, error) {
		p, _ := authn.From(ctx)
		out := Dashboard{Source: "default", Tiles: []DashboardTile{}, GeneratedAt: a.deps.Clock.Now().UTC()}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			own, err := u.DashboardLayouts().OfUser(ctx, p.UserID)
			switch {
			case err == nil:
				out.Version = own.Version
				if t := tilesOf(own.Tiles); len(t) > 0 {
					out.Source, out.Tiles = "user", t
					return nil
				}
			case !errors.Is(err, ports.ErrNotFound):
				return err
			}
			for _, r := range p.Roles {
				l, err := u.DashboardLayouts().OfRole(ctx, string(r))
				if errors.Is(err, ports.ErrNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				if t := tilesOf(l.Tiles); len(t) > 0 {
					out.Source, out.Role, out.Tiles = "role", string(r), t
					return nil
				}
			}
			for _, t := range dashboard.Default() {
				out.Tiles = append(out.Tiles, DashboardTile(t))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !authz.Decide(authz.GetKpis, p.Roles, p.AI()).Allowed {
			out.Tiles = []DashboardTile{} // a tile shows only what the viewer's own rights read
		}
		return &dashboardOutput{ETag: reportETag(out.Version), Body: out}, nil
	})

	register(a, authz.SaveMyDashboard, huma.Operation{
		OperationID: "saveMyDashboard", Method: http.MethodPut, Path: "/v1/dashboards/mine", Tags: []string{"Reports"},
		Summary: "Arrange your own dashboard",
		Description: "Saves the caller's own arrangement of tiles (FR-VIZ-09): add, remove and resize tiles on the 12-column grid, each a KPI of the catalogue, placed once (DASH-01 to DASH-03). " +
			"An empty list clears it, so the role's layout, or the default, shows again. Needs If-Match with the layout's ETag (\"0\" before the first save). Side effects: the layout at its next " +
			"version and its audit event. Prefer: return=preview checks it and saves nothing.",
	}, append(slices.Clone(writes), dashboardCodes...), func(ctx context.Context, in *saveMyDashboardInput) (*dashboardOutput, error) {
		p, _ := authn.From(ctx)
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeSavedNoUser, "Only a person who is a member has a dashboard of their own.", "Sign in as yourself")
		}
		raw, err := checkLayout(in.Body)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Dashboard, error) {
			version := 0
			cur, err := u.DashboardLayouts().OfUser(ctx, p.UserID)
			switch {
			case errors.Is(err, ports.ErrNotFound):
			case err != nil:
				return Dashboard{}, err
			default:
				version = cur.Version
			}
			if err := checkIfMatch(in.IfMatch, reportETag(version)); err != nil {
				return Dashboard{}, err
			}
			saved, err := u.DashboardLayouts().SaveUser(ctx, p.UserID, raw, uploader(ctx), a.deps.Clock.Now(), version)
			return Dashboard{Source: "user", Tiles: tilesOf(saved.Tiles), Version: saved.Version, GeneratedAt: a.deps.Clock.Now().UTC()}, err
		})
		if err != nil {
			return nil, err
		}
		return &dashboardOutput{ETag: reportETag(w.Body.Version), Body: w.Body}, nil
	})

	register(a, authz.GetRoleDashboard, huma.Operation{
		OperationID: "getRoleDashboard", Method: http.MethodGet, Path: "/v1/dashboards/roles/{role}", Tags: []string{"Reports"},
		Summary:     "Get the dashboard set for a role",
		Description: "The layout an Owner or Administrator set for a role (FR-KPI-05), with its version; none yet is an empty list at version 0. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *getRoleDashboardInput) (*dashboardOutput, error) {
		p, _ := authn.From(ctx)
		out := Dashboard{Source: "role", Role: in.Role, Tiles: []DashboardTile{}, GeneratedAt: a.deps.Clock.Now().UTC()}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			l, err := u.DashboardLayouts().OfRole(ctx, in.Role)
			if errors.Is(err, ports.ErrNotFound) {
				return nil
			}
			out.Tiles, out.Version = tilesOf(l.Tiles), l.Version
			return err
		})
		if err != nil {
			return nil, err
		}
		return &dashboardOutput{ETag: reportETag(out.Version), Body: out}, nil
	})

	register(a, authz.SaveRoleDashboard, huma.Operation{
		OperationID: "saveRoleDashboard", Method: http.MethodPut, Path: "/v1/dashboards/roles/{role}", Tags: []string{"Reports"},
		Summary: "Arrange the dashboard of a role",
		Description: "Saves the layout shown to the people who hold a role and have arranged none of their own (FR-KPI-05, FR-VIZ-09). Only an Owner or Administrator (DASH-04). The role must be one Fyniq " +
			"knows. Needs If-Match (\"0\" before the first save). Side effects: the layout at its next version and its audit event. Prefer: return=preview checks it and saves nothing.",
	}, append(slices.Clone(writes), append([]string{CodeDashboardRole}, dashboardCodes...)...), func(ctx context.Context, in *saveRoleDashboardInput) (*dashboardOutput, error) {
		p, _ := authn.From(ctx)
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		if p.Class != authn.ClassUser || !hasAnyRole(p.Roles, authz.Owner, authz.Administrator) {
			return nil, problem(CodeDashboardRole, "Only an Owner or Administrator arranges a role's dashboard.", "Ask one of them")
		}
		if _, known := authz.Roles[authz.Role(in.Role)]; !known {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "path.role", Message: "is not a role Fyniq knows", Fix: "Use a role's name, such as controller"}})
		}
		raw, err := checkLayout(in.Body)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Dashboard, error) {
			version := 0
			cur, err := u.DashboardLayouts().OfRole(ctx, in.Role)
			switch {
			case errors.Is(err, ports.ErrNotFound):
			case err != nil:
				return Dashboard{}, err
			default:
				version = cur.Version
			}
			if err := checkIfMatch(in.IfMatch, reportETag(version)); err != nil {
				return Dashboard{}, err
			}
			saved, err := u.DashboardLayouts().SaveRole(ctx, in.Role, raw, uploader(ctx), a.deps.Clock.Now(), version)
			return Dashboard{Source: "role", Role: in.Role, Tiles: tilesOf(saved.Tiles), Version: saved.Version, GeneratedAt: a.deps.Clock.Now().UTC()}, err
		})
		if err != nil {
			return nil, err
		}
		return &dashboardOutput{ETag: reportETag(w.Body.Version), Body: w.Body}, nil
	})
}
