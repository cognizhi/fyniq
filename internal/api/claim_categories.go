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
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the expense policy's categories (FR-AP-15; design D6)
//
// A category names what a claim line is for and the account it costs, with its limits in the base currency: per claim
// line (above it, finance approves too), a receipt required above, and attendees required above. Those who approve
// claims change them; a category is never deleted, as claims name it: it is put out of use instead.

// ClaimCategory is one of the expense policy's categories.
type ClaimCategory struct {
	ID             string `json:"id" format:"uuid" doc:"The category's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
	Name           string `json:"name" doc:"What its claims are for." example:"Meals & entertainment"`
	AccountID      string `json:"account_id" format:"uuid" doc:"The account its claim lines cost." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d65"`
	AccountCode    string `json:"account_code,omitempty" doc:"That account's code." example:"6530"`
	Mileage        bool   `json:"mileage" doc:"true for the mileage category: trips in a member's own vehicle (FR-AP-16). It never changes." example:"false"`
	Limit          *Money `json:"limit,omitempty" doc:"The most a line of it is, in the base currency; above it the line shows ⚠ and finance approves the claim too. Absent: no limit."`
	ReceiptAbove   *Money `json:"receipt_above,omitempty" doc:"Above it, a line needs its receipt attached (✖ until it is). Absent: never required."`
	AttendeesAbove *Money `json:"attendees_above,omitempty" doc:"Above it, a line names who attended (⚠ until it does). Absent: never asked."`
	Active         bool   `json:"active" doc:"false once put out of use: kept, as claims name it." example:"true"`
	Version        int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// ClaimCategoryDraft is a category as entered.
type ClaimCategoryDraft struct {
	Name           string `json:"name" maxLength:"80" doc:"What its claims are for, unique among the categories in use." example:"Meals & entertainment"`
	AccountID      string `json:"account_id" format:"uuid" doc:"The account its claim lines cost: an active, postable expense or asset account that is not a money, control or system account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d65"`
	Mileage        bool   `json:"mileage,omitempty" doc:"Make it the mileage category (one in use at most); never changes once made." example:"false"`
	Limit          *Money `json:"limit,omitempty" doc:"The most a line of it is, in the base currency; left out, no limit."`
	ReceiptAbove   *Money `json:"receipt_above,omitempty" doc:"Above it, a line needs its receipt; left out, never required."`
	AttendeesAbove *Money `json:"attendees_above,omitempty" doc:"Above it, a line names who attended; left out, never asked."`
	Active         *bool  `json:"active,omitempty" doc:"false puts it out of use; left out, in use." example:"true"`
}

// ClaimCategoryPage is a page of the categories, those in use first, by name.
type ClaimCategoryPage struct {
	Items      []ClaimCategory `json:"items" nullable:"false"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

// ClaimCategoryPath names a category.
type ClaimCategoryPath struct {
	CategoryID string `path:"category_id" format:"uuid" doc:"The category's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
}

// StarterCategoriesLoaded is what loading the starter categories did.
type StarterCategoriesLoaded struct {
	Categories int `json:"categories" doc:"How many starter categories were added." example:"7"`
}

type categoryListInput struct{ PageQuery }

type categoryListOutput struct{ Body ClaimCategoryPage }

type createCategoryInput struct {
	KeyHeader
	PreferHeader
	Body ClaimCategoryDraft
}

type replaceCategoryInput struct {
	ClaimCategoryPath
	IfMatchHeader
	PreferHeader
	Body ClaimCategoryDraft
}

type loadStarterInput struct {
	KeyHeader
	PreferHeader
}

type categoryOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The category's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ClaimCategory
}

type starterOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     StarterCategoriesLoaded
}

func decimalOut(d *storage.Decimal, cur string) *Money {
	if d == nil {
		return nil
	}
	m := storedOut(*d, cur)
	return &m
}

func (a *API) categoryOf(ctx context.Context, u storage.UnitOfWork, c storage.ClaimCategory) (ClaimCategory, error) {
	out := ClaimCategory{ID: c.ID.String(), Name: c.Name, AccountID: c.AccountID.String(), Mileage: c.Mileage, Limit: decimalOut(c.LineLimit, c.Currency),
		ReceiptAbove: decimalOut(c.ReceiptAbove, c.Currency), AttendeesAbove: decimalOut(c.AttendeesAbove, c.Currency), Active: c.Active, Version: c.Version}
	acc, err := u.Accounts().Get(ctx, c.AccountID)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	out.AccountCode = acc.Code
	return out, nil
}

// categoryDraftOf reads a category from the request and checks it whole (ENG-22), as self (uuid.Nil for a new one).
func (a *API) categoryDraftOf(ctx context.Context, u storage.UnitOfWork, in ClaimCategoryDraft, self uuid.UUID) (storage.NewClaimCategory, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return storage.NewClaimCategory{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return storage.NewClaimCategory{}, err
	}
	n := storage.NewClaimCategory{Name: strings.TrimSpace(in.Name), Mileage: in.Mileage, Currency: base.Code, Active: in.Active == nil || *in.Active,
		At: a.deps.Clock.Now()}
	var fields []FieldProblem
	for _, x := range []struct {
		in    *Money
		to    **storage.Decimal
		field string
	}{{in.Limit, &n.LineLimit, "limit"}, {in.ReceiptAbove, &n.ReceiptAbove, "receipt_above"}, {in.AttendeesAbove, &n.AttendeesAbove, "attendees_above"}} {
		if x.in == nil {
			continue
		}
		if x.in.Currency != base.Code {
			fields = append(fields, FieldProblem{Code: claim.CodeCategoryLimit, Field: "body." + x.field + ".currency", Message: "is not the base currency " + base.Code,
				Fix: "Send the amount in " + base.Code})
			continue
		}
		d := storage.Decimal(x.in.Amount)
		*x.to = &d
	}
	var acc *storage.Account
	if id, err := uuid.Parse(in.AccountID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.account_id", Message: "is not an ID", Fix: "Send the account's ID"})
	} else {
		n.AccountID = id
		switch x, err := u.Accounts().Get(ctx, id); {
		case err == nil:
			acc = &x
		case !errors.Is(err, ports.ErrNotFound):
			return n, err
		}
	}
	others, err := u.ClaimCategories().List(ctx)
	if err != nil {
		return n, err
	}
	for _, p := range claim.CheckCategory(n, acc, base, others, self) {
		if p.Field == "account_id" && len(fields) > 0 && fields[len(fields)-1].Field == "body.account_id" {
			continue // not an ID: said once
		}
		fields = append(fields, FieldProblem{Code: p.Code, Field: "body." + p.Field, Message: p.Message, Fix: p.Fix})
	}
	if len(fields) > 0 {
		return n, invalid(fields)
	}
	return n, nil
}

func registerClaimCategories(a *API) {
	writes := []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, claim.CodeCategoryName, claim.CodeCategoryAccount, claim.CodeCategoryLimit,
		storage.CodeClaim, storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListClaimCategories, huma.Operation{
		OperationID: "listClaimCategories", Method: http.MethodGet, Path: "/v1/claim-categories", Tags: []string{"Expense claims"},
		Summary: "List the expense policy's categories",
		Description: "The categories a claim line is for (FR-AP-15), those in use first, by name, a page at a time: each with the account " +
			"it costs and its limits in the base currency. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *categoryListInput) (*categoryListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		skip, _ := strconv.Atoi(after) // a cursor is the number of categories on the pages before
		p, _ := authn.From(ctx)
		out := categoryListOutput{Body: ClaimCategoryPage{Items: []ClaimCategory{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			cats, err := u.ClaimCategories().List(ctx)
			if err != nil {
				return err
			}
			for i, c := range cats {
				switch {
				case i < skip:
					continue
				case len(out.Body.Items) == in.PageSize:
					out.Body.NextCursor = encodeCursor(strconv.Itoa(i))
					return nil
				}
				x, err := a.categoryOf(ctx, u, c)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateClaimCategory, huma.Operation{
		OperationID: "createClaimCategory", Method: http.MethodPost, Path: "/v1/claim-categories", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Add a category to the expense policy",
		Description: "Adds a category (FR-AP-15): its name, the account its lines cost, and its limits in the base currency. Every problem " +
			"comes back together. Side effects: the category and its audit event. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, writes...),
		func(ctx context.Context, in *createCategoryInput) (*categoryOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ClaimCategory, error) {
				n, err := a.categoryDraftOf(ctx, u, in.Body, uuid.Nil)
				if err != nil {
					return ClaimCategory{}, err
				}
				c, err := u.ClaimCategories().Create(ctx, n)
				if err != nil {
					return ClaimCategory{}, err
				}
				return a.categoryOf(ctx, u, c)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &categoryOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &categoryOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceClaimCategory, huma.Operation{
		OperationID: "replaceClaimCategory", Method: http.MethodPut, Path: "/v1/claim-categories/{category_id}", Tags: []string{"Expense claims"},
		Summary: "Change a category of the expense policy",
		Description: "Changes a category's name, account or limits, or puts it out of use; it is never deleted, as claims name it, and " +
			"whether it is the mileage category never changes. A claim not yet posted is checked against the category as it is when " +
			"submitted. Side effects: its next version and its audit event. Needs If-Match with its ETag. Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, writes...),
		func(ctx context.Context, in *replaceCategoryInput) (*categoryOutput, error) {
			id, err := parseID("category_id", in.CategoryID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimCategory, error) {
				before, err := u.ClaimCategories().Get(ctx, id)
				if err != nil {
					return ClaimCategory{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return ClaimCategory{}, err
				}
				n, err := a.categoryDraftOf(ctx, u, in.Body, id)
				if err != nil {
					return ClaimCategory{}, err
				}
				if n.Mileage != before.Mileage {
					return ClaimCategory{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.mileage", Message: "never changes once the category is made",
						Fix: "Send it as it is, and make another category for the other use"}})
				}
				c, err := u.ClaimCategories().Change(ctx, id, before.Version, n)
				if err != nil {
					return ClaimCategory{}, err
				}
				return a.categoryOf(ctx, u, c)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &categoryOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &categoryOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})

	register(a, authz.LoadStarterClaimCategories, huma.Operation{
		OperationID: "loadStarterClaimCategories", Method: http.MethodPost, Path: "/v1/claim-categories:load-starter", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Load the starter categories",
		Description: "Adds the starter categories (local and overseas travel, meals and entertainment, software and subscriptions, telephone " +
			"and internet, office supplies, mileage; design D6) to an organisation that has none, as loading the standard chart does: " +
			"each on the standard chart's account of its code when the chart has it, with no limits. Refused once the organisation " +
			"has any category (CLAIM-23). Side effects: the categories and their audit events. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, claim.CodeStarterLoaded, storage.CodeClaim, storage.CodeRetry,
		storage.CodeLockTimeout},
		func(ctx context.Context, in *loadStarterInput) (*starterOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (StarterCategoriesLoaded, error) {
				have, err := u.ClaimCategories().List(ctx)
				if err != nil {
					return StarterCategoriesLoaded{}, err
				}
				if len(have) > 0 {
					return StarterCategoriesLoaded{}, problem(claim.CodeStarterLoaded, "The expense policy has categories already.",
						"Change them one by one; the starter set loads only into an organisation with none.")
				}
				n, err := claim.LoadStarter(ctx, u, a.deps.Clock.Now())
				return StarterCategoriesLoaded{Categories: n}, err
			})
			if err != nil {
				return nil, err
			}
			return &starterOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
