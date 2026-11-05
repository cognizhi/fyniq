package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/dimension"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ dimensions and their values (FR-DIM-01)
//
// Department, Project and up to 8 custom dimensions, each with its values, kept by those who keep the chart (Appendix D,
// "Chart of accounts, dimensions"). Neither a dimension nor a value is ever deleted, as the dimension sets lines carry
// name them: one is put out of use instead. A dimension's kind and place, and a value's dimension and code, never change.

// Dimension is one of the organisation's dimensions.
type Dimension struct {
	ID      string `json:"id" format:"uuid" doc:"The dimension's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Kind    string `json:"kind" enum:"department,project,custom" doc:"department and project once each; custom for up to 8 more, such as Region or Funding source (FR-DIM-01). It never changes." example:"department"`
	Place   int    `json:"place,omitempty" minimum:"1" maximum:"8" doc:"A custom dimension's place, 1 to 8, the first free when it was added; absent for department and project. It never changes." example:"1"`
	Name    string `json:"name" doc:"Its name, unique among the organisation's dimensions." example:"Department"`
	Active  bool   `json:"active" doc:"false once put out of use: kept, as lines name its values." example:"true"`
	Version int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// DimensionDraft is a dimension as added.
type DimensionDraft struct {
	Kind string `json:"kind" enum:"department,project,custom" doc:"department or project, once each; custom for up to 8 more, each taking the first free place." example:"custom"`
	Name string `json:"name" maxLength:"100" doc:"Its name, unique among the organisation's dimensions whatever the case." example:"Region"`
}

// DimensionChange is a dimension's name and use, as changed.
type DimensionChange struct {
	Name   string `json:"name" maxLength:"100" doc:"Its name, unique among the organisation's dimensions whatever the case." example:"Department"`
	Active *bool  `json:"active,omitempty" doc:"false puts it out of use; left out, in use." example:"true"`
}

// DimensionValue is a value of a dimension, such as the Engineering department.
type DimensionValue struct {
	ID          string `json:"id" format:"uuid" doc:"The value's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	DimensionID string `json:"dimension_id" format:"uuid" doc:"The dimension it is a value of. It never changes." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Code        string `json:"code" doc:"Its code, unique in its dimension whatever the case. It never changes." example:"ENG"`
	Name        string `json:"name" doc:"Its name." example:"Engineering"`
	Active      bool   `json:"active" doc:"false once put out of use: kept, as lines name it." example:"true"`
	Version     int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// DimensionValueDraft is a value as added.
type DimensionValueDraft struct {
	DimensionID string `json:"dimension_id" format:"uuid" doc:"The dimension it is a value of: one of the organisation's, in use." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Code        string `json:"code" maxLength:"30" doc:"Its code: a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, unique in its dimension whatever the case." example:"ENG"`
	Name        string `json:"name" maxLength:"200" doc:"Its name, unique in its dimension whatever the case." example:"Engineering"`
}

// DimensionValueChange is a value's name and use, as changed.
type DimensionValueChange struct {
	Name   string `json:"name" maxLength:"200" doc:"Its name, unique in its dimension whatever the case." example:"Engineering"`
	Active *bool  `json:"active,omitempty" doc:"false puts it out of use; left out, in use." example:"true"`
}

// DimensionPage is a page of the dimensions: Department, Project, then the custom ones by place.
type DimensionPage struct {
	Items      []Dimension `json:"items" nullable:"false"`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// DimensionValuePage is a page of the values, by dimension as the dimensions are listed, then by code.
type DimensionValuePage struct {
	Items      []DimensionValue `json:"items" nullable:"false"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type dimensionListInput struct{ PageQuery }

type dimensionListOutput struct{ Body DimensionPage }

type dimensionValueListInput struct {
	PageQuery
	DimensionID string `query:"dimension_id" format:"uuid" doc:"Only the values of this dimension; left out, the values of every dimension." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

type dimensionValueListOutput struct{ Body DimensionValuePage }

type createDimensionInput struct {
	KeyHeader
	PreferHeader
	Body DimensionDraft
}

type replaceDimensionInput struct {
	DimensionID string `path:"dimension_id" format:"uuid" doc:"The dimension's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	IfMatchHeader
	PreferHeader
	Body DimensionChange
}

type dimensionOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The dimension's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Dimension
}

type createDimensionValueInput struct {
	KeyHeader
	PreferHeader
	Body DimensionValueDraft
}

type replaceDimensionValueInput struct {
	ValueID string `path:"value_id" format:"uuid" doc:"The value's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	IfMatchHeader
	PreferHeader
	Body DimensionValueChange
}

type dimensionValueOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The value's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DimensionValue
}

// DimensionCodes are the codes a write of a dimension or a value may answer.
var DimensionCodes = []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, dimension.CodeKind, dimension.CodeName, dimension.CodeCode,
	dimension.CodeDimension, storage.CodeDuplicate, storage.CodeDimensionFixed, storage.CodeRetry, storage.CodeLockTimeout}

func dimensionOf(d storage.Dimension) Dimension {
	return Dimension{ID: d.ID.String(), Kind: d.Kind, Place: d.Place, Name: d.Name, Active: d.Active, Version: d.Version}
}

func dimensionValueOf(v storage.DimensionValue) DimensionValue {
	return DimensionValue{ID: v.ID.String(), DimensionID: v.DimensionID.String(), Code: v.Code, Name: v.Name, Active: v.Active, Version: v.Version}
}

// dimensionProblems are the domain's problems as the body's fields, every one together (ENG-22).
func dimensionProblems(p []dimension.Problem) error {
	if len(p) == 0 {
		return nil
	}
	bad := make([]FieldProblem, 0, len(p))
	for _, x := range p {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	return invalid(bad)
}

// pageAt is the part of n items a page shows, from a cursor that counts the items on the pages before: the first and
// last index, and the cursor of the next page ("" on the last).
func pageAt(n int, cur string, size int) (from, to int, next string, err error) {
	after, err := decodeCursor(cur)
	if err != nil {
		return 0, 0, "", err
	}
	from, _ = strconv.Atoi(after)
	from = min(max(from, 0), n)
	to = min(from+size, n)
	if to < n {
		next = encodeCursor(strconv.Itoa(to))
	}
	return from, to, next, nil
}

func registerDimensions(a *API) {
	register(a, authz.ListDimensions, huma.Operation{
		OperationID: "listDimensions", Method: http.MethodGet, Path: "/v1/dimensions", Tags: []string{"Chart of accounts"},
		Summary: "List the dimensions",
		Description: "The organisation's dimensions (FR-DIM-01): Department, Project, then the custom ones by place, those out of use " +
			"included, a page at a time. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *dimensionListInput) (*dimensionListOutput, error) {
		p, _ := authn.From(ctx)
		out := dimensionListOutput{Body: DimensionPage{Items: []Dimension{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ds, err := u.Dimensions().List(ctx)
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(ds), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, d := range ds[from:to] {
				out.Body.Items = append(out.Body.Items, dimensionOf(d))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateDimension, huma.Operation{
		OperationID: "createDimension", Method: http.MethodPost, Path: "/v1/dimensions", Tags: []string{"Chart of accounts"},
		DefaultStatus: http.StatusCreated, Summary: "Add a dimension",
		Description: "Adds Department or Project, once each, or a custom dimension in the first free of places 1 to 8 (FR-DIM-01), " +
			"named uniquely. Every problem comes back together. Side effects: the dimension and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, DimensionCodes...),
		func(ctx context.Context, in *createDimensionInput) (*dimensionOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Dimension, error) {
				others, err := u.Dimensions().List(ctx)
				if err != nil {
					return Dimension{}, err
				}
				n, probs := dimension.New(in.Body.Kind, in.Body.Name, others)
				if err := dimensionProblems(probs); err != nil {
					return Dimension{}, err
				}
				d, err := u.Dimensions().Create(ctx, n)
				return dimensionOf(d), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &dimensionOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &dimensionOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceDimension, huma.Operation{
		OperationID: "replaceDimension", Method: http.MethodPut, Path: "/v1/dimensions/{dimension_id}", Tags: []string{"Chart of accounts"},
		Summary: "Rename a dimension, or put it out of use",
		Description: "Changes a dimension's name, or puts it out of use or back in use; it is never deleted, as lines name its values, " +
			"and its kind and place never change. Side effects: its next version and its audit event. Needs If-Match with its ETag. " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, DimensionCodes...),
		func(ctx context.Context, in *replaceDimensionInput) (*dimensionOutput, error) {
			id, err := parseID("dimension_id", in.DimensionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Dimension, error) {
				before, err := u.Dimensions().Get(ctx, id)
				if err != nil {
					return Dimension{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return Dimension{}, err
				}
				others, err := u.Dimensions().List(ctx)
				if err != nil {
					return Dimension{}, err
				}
				c := storage.DimensionChange{Name: strings.TrimSpace(in.Body.Name), Active: in.Body.Active == nil || *in.Body.Active}
				if err := dimensionProblems(dimension.Change(before, c, others)); err != nil {
					return Dimension{}, err
				}
				d, err := u.Dimensions().Change(ctx, id, before.Version, c)
				return dimensionOf(d), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &dimensionOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &dimensionOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})

	register(a, authz.ListDimensionValues, huma.Operation{
		OperationID: "listDimensionValues", Method: http.MethodGet, Path: "/v1/dimension-values", Tags: []string{"Chart of accounts"},
		Summary: "List the dimensions' values",
		Description: "The values of one dimension, or of every dimension, those out of use included (FR-DIM-01): by dimension as " +
			"listDimensions orders them, then by code, a page at a time. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *dimensionValueListInput) (*dimensionValueListOutput, error) {
		var only *uuid.UUID
		if in.DimensionID != "" {
			id, err := parseID("dimension_id", in.DimensionID)
			if err != nil {
				return nil, err
			}
			only = &id
		}
		p, _ := authn.From(ctx)
		out := dimensionValueListOutput{Body: DimensionValuePage{Items: []DimensionValue{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			vs, err := u.Dimensions().Values(ctx, only)
			if err != nil {
				return err
			}
			from, to, next, err := pageAt(len(vs), in.Cursor, in.PageSize)
			if err != nil {
				return err
			}
			for _, v := range vs[from:to] {
				out.Body.Items = append(out.Body.Items, dimensionValueOf(v))
			}
			out.Body.NextCursor = next
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateDimensionValue, huma.Operation{
		OperationID: "createDimensionValue", Method: http.MethodPost, Path: "/v1/dimension-values", Tags: []string{"Chart of accounts"},
		DefaultStatus: http.StatusCreated, Summary: "Add a value to a dimension",
		Description: "Adds a value to a dimension in use (FR-DIM-01): its code, unique in the dimension and never changed, and its " +
			"name. Every problem comes back together. Side effects: the value, and an audit event of its dimension that names it. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, DimensionCodes...),
		func(ctx context.Context, in *createDimensionValueInput) (*dimensionValueOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DimensionValue, error) {
				n := storage.NewDimensionValue{Code: strings.TrimSpace(in.Body.Code), Name: strings.TrimSpace(in.Body.Name)}
				var dim *storage.Dimension
				var others []storage.DimensionValue
				if id, err := uuid.Parse(in.Body.DimensionID); err == nil {
					n.DimensionID = id
					switch d, err := u.Dimensions().Get(ctx, id); {
					case err == nil:
						dim = &d
						if others, err = u.Dimensions().Values(ctx, &id); err != nil {
							return DimensionValue{}, err
						}
					case !errors.Is(err, ports.ErrNotFound):
						return DimensionValue{}, err
					}
				}
				if err := dimensionProblems(dimension.NewValue(n, dim, others)); err != nil {
					return DimensionValue{}, err
				}
				v, err := u.Dimensions().CreateValue(ctx, n)
				return dimensionValueOf(v), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &dimensionValueOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &dimensionValueOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceDimensionValue, huma.Operation{
		OperationID: "replaceDimensionValue", Method: http.MethodPut, Path: "/v1/dimension-values/{value_id}", Tags: []string{"Chart of accounts"},
		Summary: "Rename a dimension's value, or put it out of use",
		Description: "Changes a value's name, or puts it out of use or back in use; it is never deleted, as lines name it, and its " +
			"dimension and code never change. Side effects: its next version, and an audit event of its dimension that names it. " +
			"Needs If-Match with its ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, DimensionCodes...),
		func(ctx context.Context, in *replaceDimensionValueInput) (*dimensionValueOutput, error) {
			id, err := parseID("value_id", in.ValueID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DimensionValue, error) {
				before, err := u.Dimensions().GetValue(ctx, id)
				if err != nil {
					return DimensionValue{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return DimensionValue{}, err
				}
				others, err := u.Dimensions().Values(ctx, &before.DimensionID)
				if err != nil {
					return DimensionValue{}, err
				}
				c := storage.DimensionChange{Name: strings.TrimSpace(in.Body.Name), Active: in.Body.Active == nil || *in.Body.Active}
				if err := dimensionProblems(dimension.ChangeValue(before, c, others)); err != nil {
					return DimensionValue{}, err
				}
				v, err := u.Dimensions().ChangeValue(ctx, id, before.Version, c)
				return dimensionValueOf(v), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &dimensionValueOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &dimensionValueOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
