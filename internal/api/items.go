package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/item"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ items (FR-ITM-01)

// ItemPrice is an item's unit price in one currency.
type ItemPrice struct {
	Currency  string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The ISO 4217 currency code." example:"SGD"`
	UnitPrice string `json:"unit_price" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"The price per unit, an exact decimal with up to 6 decimals (NUMERIC(20,6)); shown with at least the currency's decimals. A document's line amount is rounded from it by a named policy." example:"180.00"`
}

// Item is a product or service the organisation sells.
type Item struct {
	ID               string      `json:"id" format:"uuid" doc:"The item's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
	Code             string      `json:"code" doc:"Its code; it never changes." example:"CONSULT-HR"`
	Name             string      `json:"name" doc:"Its name." example:"HR consulting"`
	Description      string      `json:"description,omitempty" doc:"What documents print on its lines." example:"HR consulting, by the hour"`
	Type             string      `json:"type" enum:"product,service" doc:"A product or a service." example:"service"`
	Unit             string      `json:"unit,omitempty" doc:"What a quantity of one is." example:"hour"`
	RevenueAccountID string      `json:"revenue_account_id,omitempty" format:"uuid" doc:"The revenue account its sales post to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	GSTCode          string      `json:"gst_code,omitempty" doc:"The GST code its lines start with." example:"SR"`
	Prices           []ItemPrice `json:"prices" doc:"Its unit price in each currency it is sold in, by currency."`
	Active           bool        `json:"active" doc:"An inactive item is kept for its history and offered for nothing new." example:"true"`
	Version          int         `json:"version" doc:"The item's version; it also makes the ETag." example:"1"`
}

// ItemDraft is a new item.
type ItemDraft struct {
	Code             string      `json:"code" maxLength:"30" doc:"1 to 30 letters, digits, dots, underscores, slashes or hyphens, starting with a letter or digit; it never changes." example:"CONSULT-HR"`
	Name             string      `json:"name" maxLength:"200" doc:"Its name." example:"HR consulting"`
	Description      string      `json:"description,omitempty" maxLength:"2000" doc:"What documents print on its lines." example:"HR consulting, by the hour"`
	Type             string      `json:"type" enum:"product,service" doc:"A product or a service." example:"service"`
	Unit             string      `json:"unit,omitempty" maxLength:"20" doc:"What a quantity of one is." example:"hour"`
	RevenueAccountID string      `json:"revenue_account_id,omitempty" doc:"An active revenue account that takes postings." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	GSTCode          string      `json:"gst_code,omitempty" doc:"The GST code its lines start with." example:"SR"`
	Prices           []ItemPrice `json:"prices,omitempty" maxItems:"50" doc:"Its unit price in each currency, one per currency; zero or more."`
}

// ItemChanges change an item: a field left out keeps its value, and "" clears a text.
type ItemChanges struct {
	Name             *string      `json:"name,omitempty" maxLength:"200" doc:"Its name." example:"HR advisory"`
	Description      *string      `json:"description,omitempty" maxLength:"2000" doc:"What documents print on its lines." example:"HR advisory, by the hour"`
	Type             *string      `json:"type,omitempty" enum:"product,service" doc:"A product or a service." example:"service"`
	Unit             *string      `json:"unit,omitempty" maxLength:"20" doc:"What a quantity of one is." example:"day"`
	RevenueAccountID *string      `json:"revenue_account_id,omitempty" doc:"The revenue account; \"\" clears it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	GSTCode          *string      `json:"gst_code,omitempty" doc:"The GST code." example:"SR"`
	Prices           *[]ItemPrice `json:"prices,omitempty" maxItems:"50" doc:"Every price the item now has: it replaces them all, and [] clears them."`
	Active           *bool        `json:"active,omitempty" doc:"false keeps it for its history and offers it for nothing new." example:"false"`
}

// ItemPage is a page of items, by code.
type ItemPage struct {
	Items      []Item `json:"items" doc:"The items on this page."`
	NextCursor string `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IkNPTlNVTFQtSFIifQ"`
}

// ItemPath names an item.
type ItemPath struct {
	ItemID string `path:"item_id" format:"uuid" doc:"The item's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e10"`
}

type itemListInput struct {
	PageQuery
}

type itemCreateInput struct {
	KeyHeader
	PreferHeader
	Body ItemDraft
}

type itemUpdateInput struct {
	ItemPath
	IfMatchHeader
	PreferHeader
	Body ItemChanges
}

type itemListOutput struct{ Body ItemPage }

type itemReadOutput struct {
	ETag string `header:"ETag" doc:"The item's version, for If-Match."`
	Body Item
}

type itemWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The item's version, for If-Match."`
	Location string `header:"Location" doc:"The item's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Item
}

// ItemCodes are an item's rules (internal/domain/item).
var ItemCodes = []string{item.CodeCode, item.CodeName, item.CodeType, item.CodeText, item.CodeCurrency, item.CodePrice, item.CodeGSTCode,
	item.CodeAccount}

// unitPrice is a stored price as the API shows it: its significant decimals, and at least its currency's.
func unitPrice(p storage.ItemPrice) string {
	if cur, err := money.Lookup(p.Currency); err == nil {
		if v, err := money.ParseUnitPrice(string(p.UnitPrice), cur, "unit_price"); err == nil {
			return v.String()
		}
	}
	return string(p.UnitPrice)
}

func itemOf(it storage.Item) Item {
	out := Item{ID: it.ID.String(), Code: it.Code, Name: it.Name, Description: it.Description, Type: string(it.Type), Unit: it.Unit,
		GSTCode: it.GSTCode, Prices: []ItemPrice{}, Active: it.Active, Version: it.Version}
	if it.RevenueAccountID != nil {
		out.RevenueAccountID = it.RevenueAccountID.String()
	}
	for _, p := range it.Prices {
		out.Prices = append(out.Prices, ItemPrice{Currency: p.Currency, UnitPrice: unitPrice(p)})
	}
	return out
}

func pricesOf(in []ItemPrice) []storage.ItemPrice {
	out := make([]storage.ItemPrice, len(in))
	for i, p := range in {
		out[i] = storage.ItemPrice{Currency: p.Currency, UnitPrice: storage.Decimal(p.UnitPrice)}
	}
	return out
}

// checkItem runs an item's rules, and checks its revenue account is an active revenue account that takes postings.
func checkItem(ctx context.Context, u storage.UnitOfWork, problems []item.Problem, d storage.ItemDetails, bad []FieldProblem) error {
	problems = append(problems, item.Check(d)...)
	if d.RevenueAccountID != nil {
		acc, err := u.Accounts().Get(ctx, *d.RevenueAccountID)
		if err != nil {
			bad = append(bad, FieldProblem{Code: item.CodeAccount, Field: "body.revenue_account_id", Message: "is not an account of this organisation",
				Fix: "Choose an active revenue account that takes postings"})
		} else {
			problems = append(problems, item.CheckAccount(acc)...)
		}
	}
	for _, x := range problems {
		bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return invalid(bad)
	}
	return nil
}

func registerItems(a *API) {
	codes := append([]string{storage.CodeRetry, storage.CodeLockTimeout}, ItemCodes...)

	register(a, authz.ListItems, huma.Operation{
		OperationID: "listItems", Method: http.MethodGet, Path: "/v1/items", Tags: []string{"Items"},
		Summary:     "List the items",
		Description: "The organisation's products and services by code, with their prices, a page at a time (FR-ITM-01). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *itemListInput) (*itemListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := itemListOutput{Body: ItemPage{Items: []Item{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			page, err := u.Items().Page(ctx, after, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(page) > in.PageSize {
				page = page[:in.PageSize]
				out.Body.NextCursor = encodeCursor(page[len(page)-1].Code)
			}
			for _, it := range page {
				out.Body.Items = append(out.Body.Items, itemOf(it))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreateItem, huma.Operation{
		OperationID: "createItem", Method: http.MethodPost, Path: "/v1/items", Tags: []string{"Items"},
		DefaultStatus: http.StatusCreated, Summary: "Add an item",
		Description: "Adds a product or service with its code, name, printed description, unit, revenue account, GST code and a unit " +
			"price per currency (FR-ITM-01). Side effects: the item and its audit event. Its quantity and sales for the financial year " +
			"come with the sales documents. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodeDuplicate}, codes...),
		func(ctx context.Context, in *itemCreateInput) (*itemWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Item, error) {
				b := in.Body
				code := strings.TrimSpace(b.Code)
				d := storage.ItemDetails{Name: strings.TrimSpace(b.Name), Description: strings.TrimSpace(b.Description), Type: storage.ItemType(b.Type),
					Unit: strings.TrimSpace(b.Unit), GSTCode: b.GSTCode, Prices: pricesOf(b.Prices)}
				var bad []FieldProblem
				if b.RevenueAccountID != "" {
					d.RevenueAccountID = parseAccountRef("body.revenue_account_id", b.RevenueAccountID, &bad)
				}
				if err := checkItem(ctx, u, item.CheckCode(code), d, bad); err != nil {
					return Item{}, err
				}
				it, err := u.Items().Create(ctx, code, d)
				return itemOf(it), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &itemWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &itemWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/items/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetItem, huma.Operation{
		OperationID: "getItem", Method: http.MethodGet, Path: "/v1/items/{item_id}", Tags: []string{"Items"},
		Summary:     "Get an item",
		Description: "Returns an item with its prices and its ETag (FR-ITM-01). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ItemPath) (*itemReadOutput, error) {
		id, err := parseID("item_id", in.ItemID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out itemReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			it, err := u.Items().Get(ctx, id)
			out.Body, out.ETag = itemOf(it), etag(it.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.UpdateItem, huma.Operation{
		OperationID: "updateItem", Method: http.MethodPatch, Path: "/v1/items/{item_id}", Tags: []string{"Items"},
		Summary: "Change an item",
		Description: "Changes the fields sent and keeps the rest; \"\" clears a text, and prices, when sent, replace every price. The code " +
			"never changes. active false keeps the item for its history and offers it for nothing new; an item is never deleted. Side " +
			"effects: the next version and its audit event, with the prices before and after. Needs If-Match with the item's ETag. " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeVersion}, codes...),
		func(ctx context.Context, in *itemUpdateInput) (*itemWriteOutput, error) {
			id, err := parseID("item_id", in.ItemID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Item, error) {
				cur, err := u.Items().Get(ctx, id)
				if err != nil {
					return Item{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
					return Item{}, err
				}
				b, d, active := in.Body, cur.ItemDetails, cur.Active
				text := func(dst *string, v *string) {
					if v != nil {
						*dst = strings.TrimSpace(*v)
					}
				}
				text(&d.Name, b.Name)
				text(&d.Description, b.Description)
				text(&d.Unit, b.Unit)
				text(&d.GSTCode, b.GSTCode)
				if b.Type != nil {
					d.Type = storage.ItemType(*b.Type)
				}
				if b.Prices != nil {
					d.Prices = pricesOf(*b.Prices)
				}
				if b.Active != nil {
					active = *b.Active
				}
				var bad []FieldProblem
				if b.RevenueAccountID != nil {
					d.RevenueAccountID = nil
					if strings.TrimSpace(*b.RevenueAccountID) != "" {
						d.RevenueAccountID = parseAccountRef("body.revenue_account_id", *b.RevenueAccountID, &bad)
					}
				}
				if err := checkItem(ctx, u, nil, d, bad); err != nil {
					return Item{}, err
				}
				it, err := u.Items().Update(ctx, id, cur.Version, d, active)
				return itemOf(it), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &itemWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &itemWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})
}
