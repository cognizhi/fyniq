package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the fixed asset register (FR-FA-01; p1-fixed-assets)
//
// An asset is registered from the bill line or categorised bank line that bought it, which already put its cost on the
// asset account, or, bought before the books start, with the cost and accumulated depreciation the opening balances
// carry (design D1). Registering posts nothing. A change of its estimates is its next version, which carries what was
// posted and applies to the months not posted yet (design D2). Appendix D's "Fixed assets" row: the Accountant registers
// and changes (C), and disposes (P); the Owner, the Controller, the Bookkeeper, the Auditor and the Tax agent read (V).

// WarnAssetSource is an asset whose bill or bank line was voided or edited after it was registered: its cost may no
// longer be on the account as the register says.
const WarnAssetSource = "ASSET-SOURCE-CHANGED"

// AssetCodes are the codes an asset's own checks answer.
var AssetCodes = []string{asset.CodeSource, asset.CodeAccount, asset.CodeLife, asset.CodeResidual, asset.CodeStart, asset.CodeMethod, asset.CodeOpening,
	asset.CodeTooLong, asset.CodePosted}

// monthPattern is a month as the API writes it.
const monthPattern = "^[0-9]{4}-(0[1-9]|1[0-2])$"

// FixedAssetSourceRef names the line an asset is registered from.
type FixedAssetSourceRef struct {
	DocumentID string `json:"document_id" format:"uuid" doc:"The posted bill or categorised bank line (bank transaction) that bought it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Line       int    `json:"line" minimum:"1" maximum:"10000" doc:"The bill's line, or the bank line's part, from 1: one that put a cost on an asset account." example:"1"`
}

// FixedAssetOpening is an asset bought before the books start, as the opening balances carry it.
type FixedAssetOpening struct {
	AccountID               string `json:"account_id" format:"uuid" doc:"The asset account it sits on: one whose report group is Property, plant & equipment or Intangible assets." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1510"`
	PurchasedOn             string `json:"purchased_on" format:"date" doc:"The day it was bought: before the books start." example:"2025-03-14"`
	Cost                    Money  `json:"cost" doc:"What it cost, in the base currency, as the opening balances carry it on its account."`
	AccumulatedDepreciation Money  `json:"accumulated_depreciation" doc:"Its accumulated depreciation at the books start, in the base currency, as the opening balances carry it on 1590."`
}

// FixedAssetDraft is an asset as registered: from a line, or bought before the books start.
type FixedAssetDraft struct {
	Source     *FixedAssetSourceRef `json:"source,omitempty" doc:"The line it was bought on; its account, purchase date and cost come from that line as its entry posted it. Send this or opening."`
	Opening    *FixedAssetOpening   `json:"opening,omitempty" doc:"An asset bought before the books start, with its opening figures. Send this or source."`
	Name       string               `json:"name,omitempty" maxLength:"200" doc:"What it is called in the register; a line's description when left out." example:"MacBook Pro 14, Wei Ling"`
	Notes      string               `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it: its serial number, where it is." example:"Serial C02XG0FDH7JY"`
	Residual   *Money               `json:"residual,omitempty" doc:"What it is worth at the end of its life, in the base currency; zero when left out."`
	LifeMonths int                  `json:"life_months" minimum:"1" maximum:"600" doc:"Its useful life in months." example:"36"`
	Method     string               `json:"method,omitempty" enum:"straight_line" doc:"How it depreciates: straight-line, cost less residual evenly over its life (reducing balance follows in P2)." example:"straight_line"`
	Start      string               `json:"start,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month depreciation starts: the purchase month when left out, or a later one when it went into use later." example:"2026-03"`
}

// FixedAssetChange is a change of an asset: only what is sent changes.
type FixedAssetChange struct {
	Name                    *string `json:"name,omitempty" maxLength:"200" doc:"Its name in the register." example:"MacBook Pro 14, Arjun"`
	Notes                   *string `json:"notes,omitempty" maxLength:"2000" doc:"Its notes." example:"Moved to Arjun, 1 Nov 2026"`
	Residual                *Money  `json:"residual,omitempty" doc:"A new residual value: it applies to the months not posted yet."`
	LifeMonths              *int    `json:"life_months,omitempty" minimum:"1" maximum:"600" doc:"A new useful life in months, from its start: it applies to the months not posted yet, and ends in or after the first of them." example:"48"`
	Start                   *string `json:"start,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"A new start month: only while none of its depreciation has posted." example:"2026-04"`
	PurchasedOn             *string `json:"purchased_on,omitempty" format:"date" doc:"An asset bought before the books start: its purchase date, only while none of its depreciation has posted." example:"2025-03-14"`
	Cost                    *Money  `json:"cost,omitempty" doc:"An asset bought before the books start: its cost, only while none of its depreciation has posted."`
	AccumulatedDepreciation *Money  `json:"accumulated_depreciation,omitempty" doc:"An asset bought before the books start: its opening accumulated depreciation, only while none of its depreciation has posted."`
}

// FixedAssetSource is the line an asset came from.
type FixedAssetSource struct {
	DocumentID string `json:"document_id" format:"uuid" doc:"The bill or bank transaction." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Type       string `json:"type" enum:"bill,bank_transaction" doc:"bill, or bank_transaction: a categorised bank line." example:"bill"`
	Number     string `json:"number,omitempty" doc:"The bill's number." example:"BILL-0042"`
	Version    int    `json:"version" doc:"The version of it that posted the cost." example:"1"`
	Line       int    `json:"line" doc:"Its line, or part, from 1." example:"1"`
}

// FixedAsset is an asset in the register.
type FixedAsset struct {
	ID                      string            `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
	Number                  string            `json:"number,omitempty" doc:"Its number in the register (FR-ORG-04)." example:"FA-0001"`
	Status                  string            `json:"status" enum:"in_use,fully_depreciated,disposed" doc:"in_use while it depreciates; fully_depreciated once nothing is left to post; disposed once nothing of it is left." example:"in_use"`
	Version                 int               `json:"version" doc:"The version of its content; it makes the ETag." example:"1"`
	Name                    string            `json:"name" doc:"Its name." example:"MacBook Pro 14, Wei Ling"`
	AccountID               string            `json:"account_id" format:"uuid" doc:"The asset account it sits on: its category." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1510"`
	Source                  *FixedAssetSource `json:"source,omitempty" doc:"The line it was bought on; absent for one bought before the books start."`
	PurchasedOn             string            `json:"purchased_on" format:"date" doc:"The day it was bought." example:"2026-03-14"`
	Cost                    Money             `json:"cost" doc:"What it cost, in the base currency: what is left of it after a partial disposal."`
	Residual                Money             `json:"residual" doc:"Its residual value."`
	LifeMonths              int               `json:"life_months" doc:"Its useful life in months." example:"36"`
	Method                  string            `json:"method" enum:"straight_line" doc:"How it depreciates." example:"straight_line"`
	Start                   string            `json:"start" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month depreciation starts." example:"2026-03"`
	End                     string            `json:"end" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month it depreciates: start + life − 1." example:"2029-02"`
	OpeningAccumulated      *Money            `json:"opening_accumulated,omitempty" doc:"Bought before the books start: the accumulated depreciation the opening balances carry for it."`
	AccumulatedDepreciation Money             `json:"accumulated_depreciation" doc:"Its accumulated depreciation: what the opening balances carried for it and what has posted since."`
	NetBookValue            Money             `json:"net_book_value" doc:"Cost less accumulated depreciation."`
	DepreciatedTo           string            `json:"depreciated_to,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month whose depreciation has posted." example:"2026-09"`
	Notes                   string            `json:"notes,omitempty" doc:"Its notes." example:"Serial C02XG0FDH7JY"`
	Checks                  []BillCheck       `json:"checks" nullable:"false" doc:"Its warnings (⚠): the bill or bank line it came from was voided or edited since."`
	RecordedBy              *Person           `json:"recorded_by,omitempty" doc:"Who registered it, or saved its latest change."`
	RecordedAt              *time.Time        `json:"recorded_at,omitempty" doc:"When." example:"2026-03-20T09:30:00Z"`
}

// FixedAssetPage is a page of the register, newest purchase first.
type FixedAssetPage struct {
	Items      []FixedAsset `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMDMtMTR8MDE5MiJ9"`
}

// FixedAssetPath names an asset in the path.
type FixedAssetPath struct {
	AssetID string `path:"asset_id" format:"uuid" doc:"The asset's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
}

type assetListInput struct {
	PageQuery
	Status    string `query:"status" enum:"in_use,fully_depreciated,disposed" doc:"Only the assets in this state." example:"in_use"`
	AccountID string `query:"account_id" format:"uuid" doc:"Only the assets on this account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1510"`
}

type assetListOutput struct{ Body FixedAssetPage }

type registerAssetInput struct {
	KeyHeader
	PreferHeader
	Body FixedAssetDraft
}

type changeAssetInput struct {
	FixedAssetPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body FixedAssetChange
}

type assetOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     FixedAsset
}

type assetReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body FixedAsset
}

func assetETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

func monthText(t time.Time) string { return t.Format("2006-01") }

// monthIn reads a month the API was sent.
func monthIn(s, field string) (time.Time, *FieldProblem) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, &FieldProblem{Code: CodeInvalid, Field: field, Message: "is not a month", Fix: "Send the month as YYYY-MM"}
	}
	return t, nil
}

// assetOf is an asset as the API shows it: its figures as they stand with what has posted.
func (a *API) assetOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, doc storage.Document) (FixedAsset, error) {
	x, err := asset.Read(doc)
	if err != nil {
		return FixedAsset{}, err
	}
	posted, err := journal.PostedOf(ctx, u, doc.ID, base)
	if err != nil {
		return FixedAsset{}, err
	}
	acc := x.Carried
	if t, ok := posted.Total[doc.Version]; ok {
		acc, _ = acc.Add(t)
	}
	nbv, _ := x.Cost.Sub(acc)
	out := FixedAsset{ID: doc.ID.String(), Number: doc.Number, Status: "in_use", Version: doc.Version, Name: x.Name, AccountID: x.AccountID.String(),
		PurchasedOn: dateOf(x.PurchasedOn), Cost: moneyOut(x.Cost), Residual: moneyOut(x.Residual), LifeMonths: x.LifeMonths, Method: x.Method,
		Start: monthText(x.Start), End: monthText(x.End()), AccumulatedDepreciation: moneyOut(acc), NetBookValue: moneyOut(nbv), Notes: x.Notes,
		Checks: []BillCheck{}, RecordedBy: personOf(doc.PostedBy), RecordedAt: doc.PostedAt}
	if !posted.Last.IsZero() {
		out.DepreciatedTo = monthText(posted.Last)
	}
	left, _ := nbv.Sub(x.Residual)
	switch {
	case x.Disposed:
		out.Status = "disposed"
	case left.Sign() <= 0 || x.From.After(x.End()):
		out.Status = "fully_depreciated"
	}
	if x.Source == nil {
		first, err := u.Documents().Version(ctx, doc.ID, 1)
		if err != nil {
			return FixedAsset{}, err
		}
		v1, err := asset.Read(first)
		if err != nil {
			return FixedAsset{}, err
		}
		opening := moneyOut(v1.Carried)
		out.OpeningAccumulated = &opening
	} else {
		src, err := u.Documents().Get(ctx, x.Source.DocumentID)
		if err != nil {
			return FixedAsset{}, err
		}
		out.Source = &FixedAssetSource{DocumentID: src.ID.String(), Type: string(src.Type), Number: src.Number, Version: x.Source.Version, Line: x.Source.Line}
		if src.Status != storage.DocumentPosted || src.Version != x.Source.Version {
			what := "edited"
			if src.Status != storage.DocumentPosted {
				what = "voided"
			}
			out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: WarnAssetSource, Field: "source",
				Message: strings.TrimSpace(src.Number+" ") + " was " + what + " after the asset was registered from it, so its cost may no longer be on the account",
				Fix:     "Check the asset against the bill or bank line; dispose of it if it was never bought"})
		}
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

// assetBase is the organisation's base currency.
func assetBase(ctx context.Context, u storage.UnitOfWork) (money.Currency, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return money.Currency{}, err
	}
	return money.Lookup(o.BaseCurrency)
}

// assetDraftOf reads an asset to register from the request, with every invalid field at once.
func assetDraftOf(in FixedAssetDraft, base money.Currency) (journal.AssetDraft, error) {
	d := journal.AssetDraft{Name: in.Name, Notes: in.Notes, LifeMonths: in.LifeMonths, Method: in.Method, Residual: money.Zero(base)}
	var fields []FieldProblem
	switch {
	case (in.Source == nil) == (in.Opening == nil):
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.source", Message: "names neither, or both, of a source line and opening figures",
			Fix: "Send source for an asset bought on a bill or bank line, or opening for one bought before the books start"})
	case in.Source != nil:
		id, err := uuid.Parse(in.Source.DocumentID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.source.document_id", Message: "is not an ID", Fix: "Send the bill's or bank transaction's ID"})
		}
		d.SourceID, d.SourceLine = &id, in.Source.Line
	default:
		o := in.Opening
		id, err := uuid.Parse(o.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.opening.account_id", Message: "is not an ID", Fix: "Send the asset account's ID"})
		}
		d.AccountID = id
		if d.PurchasedOn, err = time.Parse(time.DateOnly, o.PurchasedOn); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.opening.purchased_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		var bad *FieldProblem
		if d.Cost, bad = moneyIn(o.Cost, "body.opening.cost"); bad != nil {
			fields = append(fields, *bad)
		}
		if d.OpeningAccumulated, bad = moneyIn(o.AccumulatedDepreciation, "body.opening.accumulated_depreciation"); bad != nil {
			fields = append(fields, *bad)
		}
	}
	if in.Residual != nil {
		var bad *FieldProblem
		if d.Residual, bad = moneyIn(*in.Residual, "body.residual"); bad != nil {
			fields = append(fields, *bad)
		}
	}
	if in.Start != "" {
		var bad *FieldProblem
		if d.Start, bad = monthIn(in.Start, "body.start"); bad != nil {
			fields = append(fields, *bad)
		}
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// assetChangeOf reads a change of an asset from the request, with every invalid field at once.
func assetChangeOf(in FixedAssetChange) (journal.AssetChange, error) {
	c := journal.AssetChange{Name: in.Name, Notes: in.Notes, LifeMonths: in.LifeMonths}
	var fields []FieldProblem
	for _, m := range []struct {
		in   *Money
		out  **money.Money
		path string
	}{{in.Residual, &c.Residual, "body.residual"}, {in.Cost, &c.Cost, "body.cost"}, {in.AccumulatedDepreciation, &c.OpeningAccumulated, "body.accumulated_depreciation"}} {
		if m.in == nil {
			continue
		}
		v, bad := moneyIn(*m.in, m.path)
		if bad != nil {
			fields = append(fields, *bad)
			continue
		}
		*m.out = &v
	}
	if in.Start != nil {
		t, bad := monthIn(*in.Start, "body.start")
		if bad != nil {
			fields = append(fields, *bad)
		}
		c.Start = &t
	}
	if in.PurchasedOn != nil {
		t, err := time.Parse(time.DateOnly, *in.PurchasedOn)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.purchased_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		c.PurchasedOn = &t
	}
	if len(fields) > 0 {
		return c, invalid(fields)
	}
	return c, nil
}

func registerFixedAssets(a *API) {
	readCodes := []string{CodeNotFound, asset.CodeNotAsset, asset.CodeBadContent}

	register(a, authz.ListFixedAssets, huma.Operation{
		OperationID: "listFixedAssets", Method: http.MethodGet, Path: "/v1/fixed-assets", Tags: []string{"Fixed assets"},
		Summary: "List the fixed asset register",
		Description: "The register's assets, newest purchase first, a page at a time, narrowed by state or account (FR-FA-01): each with its " +
			"category (the asset account it sits on), cost, residual value, life, start, accumulated depreciation and net book value " +
			"as its depreciation has posted, and ⚠ when the bill or bank line it came from was voided or edited since. Read-only; no " +
			"side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *assetListInput) (*assetListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := assetListOutput{Body: FixedAssetPage{Items: []FixedAsset{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := assetBase(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentFixedAsset, Status: storage.DocumentPosted,
					AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.PurchasedOn + "|" + last.ID)
						return nil
					}
					x, err := a.assetOf(ctx, u, base, doc)
					if err != nil {
						return err
					}
					if in.Status != "" && x.Status != in.Status || in.AccountID != "" && x.AccountID != in.AccountID {
						continue
					}
					out.Body.Items = append(out.Body.Items, x)
				}
				if len(docs) < 500 {
					return nil
				}
			}
		})
		return &out, err
	})

	register(a, authz.GetFixedAsset, huma.Operation{
		OperationID: "getFixedAsset", Method: http.MethodGet, Path: "/v1/fixed-assets/{asset_id}", Tags: []string{"Fixed assets"},
		Summary: "Get a fixed asset",
		Description: "Returns an asset of the register with its ETag: its category, source line or opening figures, cost, residual value, " +
			"life, start and end months, accumulated depreciation and net book value as its depreciation has posted. Read-only; no side " +
			"effects.",
	}, readCodes, func(ctx context.Context, in *FixedAssetPath) (*assetReadOutput, error) {
		id, err := parseID("asset_id", in.AssetID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var x FixedAsset
		var doc storage.Document
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if doc, err = u.Documents().Get(ctx, id); err != nil {
				return err
			}
			if !asset.IsAsset(doc) {
				return ports.ErrNotFound
			}
			base, err := assetBase(ctx, u)
			if err != nil {
				return err
			}
			x, err = a.assetOf(ctx, u, base, doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &assetReadOutput{ETag: assetETag(doc), Body: x}, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeAsset, storage.CodeDuplicate, storage.CodeRetry,
		storage.CodeLockTimeout}, AssetCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (FixedAsset, error) {
		base, err := assetBase(ctx, u)
		if err != nil {
			return FixedAsset{}, err
		}
		return a.assetOf(ctx, u, base, doc)
	}
	answer := func(w written[FixedAsset]) *assetOutput {
		if w.Previewed {
			return &assetOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &assetOutput{Status: w.Status, ETag: etag(w.Body.Version, "posted"), Location: "/v1/fixed-assets/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.RegisterFixedAsset, huma.Operation{
		OperationID: "registerFixedAsset", Method: http.MethodPost, Path: "/v1/fixed-assets", Tags: []string{"Fixed assets"},
		DefaultStatus: http.StatusCreated, Summary: "Register a fixed asset",
		Description: "Adds an asset to the register (FR-FA-01): from the line of a posted bill, or the part of a categorised bank line, that " +
			"put its cost on an asset account (one whose report group is Property, plant & equipment or Intangible assets), its cost " +
			"being what that line's entry put there in the base currency; or, bought before the books start, with the cost and " +
			"accumulated depreciation the opening balances carry for it, its depreciation posting from the books start (FR-FA-02). " +
			"A line is registered once. Registering posts nothing: the cost is already on the account. Side effects: the asset, its " +
			"number (FA-), audit events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"saves nothing and shows the asset.",
	}, writeCodes, func(ctx context.Context, in *registerAssetInput) (*assetOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FixedAsset, error) {
			base, err := assetBase(ctx, u)
			if err != nil {
				return FixedAsset{}, err
			}
			d, err := assetDraftOf(in.Body, base)
			if err != nil {
				return FixedAsset{}, err
			}
			doc, err := a.assets.Register(ctx, u, d)
			if err != nil {
				return FixedAsset{}, err
			}
			return shown(ctx, u, doc)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ChangeFixedAsset, huma.Operation{
		OperationID: "changeFixedAsset", Method: http.MethodPost, Path: "/v1/fixed-assets/{asset_id}/change", Tags: []string{"Fixed assets"},
		Summary: "Change a fixed asset",
		Description: "Saves the asset's next version with what is sent (FR-FA-01, FR-FA-02): its name and notes; a new residual value or " +
			"useful life, which apply to the months not posted yet, what has posted being carried into the version; and, only while " +
			"none of its depreciation has posted, its start month and, for an asset bought before the books start, its purchase date, " +
			"cost and opening accumulated depreciation. Posts nothing. Side effects: the new version, audit events. Every problem comes " +
			"back together. Needs If-Match with the asset's ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing " +
			"and shows the asset as it would be.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, asset.CodeNotAsset, asset.CodeBadContent, asset.CodeDisposed}, writeCodes...),
		func(ctx context.Context, in *changeAssetInput) (*assetOutput, error) {
			id, err := parseID("asset_id", in.AssetID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (FixedAsset, error) {
				doc, err := u.Documents().Lock(ctx, id)
				if err != nil {
					return FixedAsset{}, err
				}
				if !asset.IsAsset(doc) {
					return FixedAsset{}, ports.ErrNotFound
				}
				if err := checkIfMatch(in.IfMatch, assetETag(doc)); err != nil {
					return FixedAsset{}, err
				}
				c, err := assetChangeOf(in.Body)
				if err != nil {
					return FixedAsset{}, err
				}
				if doc, err = a.assets.Change(ctx, u, id, c); err != nil {
					return FixedAsset{}, err
				}
				return shown(ctx, u, doc)
			})
			if err != nil {
				return nil, err
			}
			return answer(w), nil
		})
}
