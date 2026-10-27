package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ disposals of fixed assets (FR-FA-03; p1-fixed-assets)
//
// The sale or write-off of an asset, or of a share of its cost, posts asset_disposal@1 at once: the share's accumulated
// depreciation off 1590, its cost off the asset account, the proceeds (money received, with output GST when the
// organisation is GST-registered; or an issued invoice's line on the disposal clearing account; or none), and the gain
// or loss to assets.disposal_account (the owner's decisions of 27 Oct 2026, rev 0.149). A partial disposal makes the
// asset's next version, the rest depreciating on. Appendix D's "Fixed assets" row: the Accountant disposes (P); in
// Founder mode a void is asked for and approved by another person who holds A there (the Controller, FM-1).

// CodeDisposalVoidApproval is a void of a disposal that needs a second person in Founder mode (FM-1).
const CodeDisposalVoidApproval = "ASSET-19"

// WarnDisposalInvoice is a disposal whose invoice was voided after it: the clearing account holds its proceeds alone.
const WarnDisposalInvoice = "ASSET-INVOICE-VOIDED"

// DisposalCodes are the codes a disposal's own checks and its rule answer.
var DisposalCodes = []string{asset.CodeDisposalDate, asset.CodeDisposalShare, asset.CodeProceeds, asset.CodeDepreciationDue, asset.CodeDisposed, asset.CodeVoided,
	asset.CodeTooLong, ledger.CodeDisposal, ledger.CodeNotMoneyAccount, ledger.CodeUnknownAccount, ledger.CodeGroupAccount, ledger.CodeInactiveAccount,
	ledger.CodeSystemRole, ledger.CodeGSTRate, ledger.CodeNoDate}

// DisposalApprovalCodes are the codes an approval of a disposal's void may answer.
func DisposalApprovalCodes() []string {
	return []string{asset.CodeNotDisposal, asset.CodeDisposalVoid, asset.CodeBadContent}
}

// AssetDisposalDraft is a disposal as recorded.
type AssetDisposalDraft struct {
	Date           string `json:"date" format:"date" doc:"The day the asset was sold or written off: on or after its purchase, and not after today." example:"2027-03-15"`
	Cost           *Money `json:"cost,omitempty" doc:"A partial disposal: the part of the asset's cost disposed of, in the base currency, above zero and below the cost; its accumulated depreciation and residual value go with it pro rata (ALLOC-PRORATA). Leave it out to dispose of the whole asset."`
	Proceeds       string `json:"proceeds" enum:"money,invoice,none" doc:"money: received into a money account, recorded here and matched by bank import; invoice: an issued sales invoice to the buyer whose line is on the disposal clearing account; none: a write-off." example:"money"`
	MoneyAccountID string `json:"money_account_id,omitempty" format:"uuid" doc:"money: the bank, cash or wallet account it came into: an active money account in the base currency." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Amount         *Money `json:"amount,omitempty" doc:"money: what came in, in the base currency, GST included."`
	GSTCode        string `json:"gst_code,omitempty" maxLength:"10" doc:"money, when the organisation is GST-registered on the date: the sale's GST code for supplies, usually SR." example:"SR"`
	InvoiceID      string `json:"invoice_id,omitempty" format:"uuid" doc:"invoice: the issued invoice to the buyer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	InvoiceLine    int    `json:"invoice_line,omitempty" minimum:"1" maximum:"200" doc:"invoice: its line for the asset, from 1, on the disposal clearing account." example:"1"`
	Notes          string `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with it: who bought it, why it was written off." example:"Sold to a staff member"`
}

// AssetDisposalView is a disposal of an asset.
type AssetDisposalView struct {
	ID                      string      `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6fa1"`
	Number                  string      `json:"number,omitempty" doc:"Its number (FR-ORG-04)." example:"DSP-0001"`
	Status                  string      `json:"status" enum:"posted,reversed" doc:"posted once recorded; reversed by a void." example:"posted"`
	Version                 int         `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	AssetID                 string      `json:"asset_id" format:"uuid" doc:"The asset." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
	AssetNumber             string      `json:"asset_number,omitempty" doc:"The asset's number." example:"FA-0001"`
	AssetName               string      `json:"asset_name" doc:"The asset's name when it was disposed of." example:"MacBook Pro 14, Wei Ling"`
	Date                    string      `json:"date" format:"date" doc:"The day it was sold or written off." example:"2027-03-15"`
	Whole                   bool        `json:"whole" doc:"true when the whole asset went; false for a part of its cost." example:"true"`
	Cost                    Money       `json:"cost" doc:"The cost it took off the asset account."`
	AccumulatedDepreciation Money       `json:"accumulated_depreciation" doc:"That cost's accumulated depreciation through the month before, taken off 1590."`
	NetBookValue            Money       `json:"net_book_value" doc:"Cost less accumulated depreciation: what the asset was still worth."`
	Proceeds                string      `json:"proceeds" enum:"money,invoice,none" doc:"How it was paid for, or none for a write-off." example:"money"`
	MoneyAccountID          string      `json:"money_account_id,omitempty" format:"uuid" doc:"money: the account it came into." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	Amount                  *Money      `json:"amount,omitempty" doc:"money: what came in, GST included."`
	GSTCode                 string      `json:"gst_code,omitempty" doc:"money: its GST code." example:"SR"`
	InvoiceID               string      `json:"invoice_id,omitempty" format:"uuid" doc:"invoice: the invoice." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e70"`
	InvoiceNumber           string      `json:"invoice_number,omitempty" doc:"invoice: its number." example:"INV-0042"`
	InvoiceLine             int         `json:"invoice_line,omitempty" doc:"invoice: its line." example:"1"`
	NetProceeds             Money       `json:"net_proceeds" doc:"What it fetched, net of GST: zero for a write-off."`
	GainOrLoss              Money       `json:"gain_or_loss" doc:"Net proceeds less net book value: a gain above zero, a loss below, posted to the disposal account."`
	Notes                   string      `json:"notes,omitempty" doc:"Its notes." example:"Sold to a staff member"`
	Checks                  []BillCheck `json:"checks" nullable:"false" doc:"Its warnings (⚠): the invoice of its proceeds was voided after it."`
	LedgerImpact            []EntryLine `json:"ledger_impact" nullable:"false" doc:"getAssetDisposal and the commands only: the lines its entry wrote: debits positive."`
	RecordedBy              *Person     `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt              *time.Time  `json:"recorded_at,omitempty" doc:"When." example:"2027-03-15T09:30:00Z"`
	ApprovalAsked           string      `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to void it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
}

// AssetDisposalPage is a page of disposals, newest first.
type AssetDisposalPage struct {
	Items      []AssetDisposalView `json:"items" nullable:"false"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjctMDMtMTV8MDE5MiJ9"`
}

// AssetDisposalPath names a disposal in the path.
type AssetDisposalPath struct {
	DisposalID string `path:"disposal_id" format:"uuid" doc:"The disposal's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6fa1"`
}

type disposalListInput struct {
	PageQuery
	AssetID string `query:"asset_id" format:"uuid" doc:"Only the disposals of this asset." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
}

type disposalListOutput struct{ Body AssetDisposalPage }

type disposeInput struct {
	FixedAssetPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body AssetDisposalDraft
}

type disposalCommandInput struct {
	AssetDisposalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body Reversal
}

type disposalOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and it shows what it would post (FR-INT-14)."`
	Body     AssetDisposalView
}

type disposalReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body AssetDisposalView
}

func disposalETag(x AssetDisposalView) string { return etag(x.Version, x.Status) }

// disposalAsked are the disposals with a pending request to void them, by ID.
func (a *API) disposalAsked(ctx context.Context, u storage.UnitOfWork) (map[string]string, error) {
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectAssetDisposal && approval.StatusAt(requestOf(r), now) == approval.Pending {
			out[r.ObjectKey] = r.ID.String()
		}
	}
	return out, nil
}

// disposalOf is a disposal as the API shows it. withDetail fills its ledger impact.
func (a *API) disposalOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, asked map[string]string, doc storage.Document, withDetail bool) (AssetDisposalView, error) {
	x, err := asset.ReadDisposal(doc)
	if err != nil {
		return AssetDisposalView{}, err
	}
	nbv, _ := x.Cost.Sub(x.Accumulated)
	out := AssetDisposalView{ID: doc.ID.String(), Number: doc.Number, Status: string(doc.Status), Version: doc.Version, AssetID: x.AssetID.String(),
		Date: dateOf(x.Date), Whole: x.Whole, Cost: moneyOut(x.Cost), AccumulatedDepreciation: moneyOut(x.Accumulated), NetBookValue: moneyOut(nbv),
		Proceeds: x.Proceeds, GSTCode: x.GSTCode, InvoiceLine: x.InvoiceLine, Notes: x.Notes, Checks: []BillCheck{}, LedgerImpact: []EntryLine{},
		RecordedBy: personOf(doc.PostedBy), RecordedAt: doc.PostedAt, ApprovalAsked: asked[doc.ID.String()]}
	if was, err := u.Documents().Version(ctx, x.AssetID, x.AssetVersion); err == nil {
		out.AssetNumber = was.Number
		if a, err := asset.Read(was); err == nil {
			out.AssetName = a.Name
		}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return AssetDisposalView{}, err
	}
	net := money.Zero(base)
	switch x.Proceeds {
	case asset.ProceedsMoney:
		if x.MoneyAccountID != nil {
			out.MoneyAccountID = x.MoneyAccountID.String()
		}
		if x.Gross != nil {
			gross := moneyOut(*x.Gross)
			out.Amount = &gross
			net = *x.Gross
			if x.GSTRate != "" {
				if gst, err := rounding.GSTIncl(*x.Gross, x.GSTRate); err == nil {
					net, _ = x.Gross.Sub(gst)
				}
			}
		}
	case asset.ProceedsInvoice:
		if x.InvoiceID != nil {
			out.InvoiceID = x.InvoiceID.String()
			if inv, err := u.Documents().Get(ctx, *x.InvoiceID); err == nil {
				out.InvoiceNumber = inv.Number
				if inv.Status != storage.DocumentPosted && doc.Status == storage.DocumentPosted {
					out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: WarnDisposalInvoice, Field: "invoice_id",
						Message: inv.Number + " was voided after the disposal, so the clearing account holds its proceeds alone",
						Fix:     "Issue the invoice again to the buyer on the clearing account, or void the disposal"})
				}
			}
		}
		if x.InvoiceNet != nil {
			net = *x.InvoiceNet
		}
	}
	out.NetProceeds = moneyOut(net)
	gain, _ := net.Sub(nbv)
	out.GainOrLoss = moneyOut(gain)
	if withDetail {
		e, err := u.Ledger().ByIdentity(ctx, ledger.AssetDisposalIdentity(doc.ID, doc.Version, ledger.RuleAssetDisposal))
		if err != nil {
			return AssetDisposalView{}, err
		}
		out.LedgerImpact = entryOf(e).Lines
	}
	return out, a.name(ctx, u, out.RecordedBy)
}

// disposalDraftOf reads a disposal from the request, with every invalid field at once.
func disposalDraftOf(assetID uuid.UUID, in AssetDisposalDraft) (journal.DisposalDraft, error) {
	d := journal.DisposalDraft{AssetID: assetID, Proceeds: in.Proceeds, GSTCode: strings.TrimSpace(in.GSTCode), InvoiceLine: in.InvoiceLine, Notes: in.Notes}
	var fields []FieldProblem
	date, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	d.Date = date
	for _, m := range []struct {
		in   *Money
		out  **money.Money
		path string
	}{{in.Cost, &d.Cost, "body.cost"}, {in.Amount, &d.Gross, "body.amount"}} {
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
	for _, x := range []struct {
		raw   string
		out   **uuid.UUID
		field string
	}{{in.MoneyAccountID, &d.MoneyAccountID, "body.money_account_id"}, {in.InvoiceID, &d.InvoiceID, "body.invoice_id"}} {
		id, bad := optionalID(x.field, x.raw)
		if bad != nil {
			fields = append(fields, *bad)
		}
		*x.out = id
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// disposalDependencies are what a disposal's envelope depends on: its status, the asset's version it made, and the cost
// it took off the asset.
func (a *API) disposalDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	x, err := asset.ReadDisposal(doc)
	if err != nil {
		return 0, nil, err
	}
	of, err := u.Documents().Get(ctx, x.AssetID)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "asset_version": of.Version, "cost": x.Cost.String() + " " + x.Cost.Currency().Code}, nil
}

// executeDisposalVoid voids the disposal as asked, on the approval.
func (a *API) executeDisposalVoid(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	date, err := time.Parse(time.DateOnly, fmt.Sprint(s.Execute["date"]))
	if err != nil {
		return err
	}
	reason := fmt.Sprint(s.Execute["reason"])
	_, err = a.disposals.Void(storage.WithReason(ctx, reason), u, id, date, reason)
	return err
}

func registerDisposals(a *API) {
	readCodes := []string{CodeNotFound, asset.CodeNotDisposal, asset.CodeBadContent}

	register(a, authz.ListAssetDisposals, huma.Operation{
		OperationID: "listAssetDisposals", Method: http.MethodGet, Path: "/v1/asset-disposals", Tags: []string{"Fixed assets"},
		Summary: "List disposals of fixed assets",
		Description: "The sales and write-offs of assets, newest first, a page at a time, narrowed to one asset (FR-FA-03): each with the " +
			"cost and accumulated depreciation it took off, what it fetched net of GST and the gain or loss. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *disposalListInput) (*disposalListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := disposalListOutput{Body: AssetDisposalPage{Items: []AssetDisposalView{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := assetBase(ctx, u)
			if err != nil {
				return err
			}
			asked, err := a.disposalAsked(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentAssetDisposal, AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID)
						return nil
					}
					x, err := a.disposalOf(ctx, u, base, asked, doc, false)
					if err != nil {
						return err
					}
					if in.AssetID != "" && x.AssetID != in.AssetID {
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

	register(a, authz.GetAssetDisposal, huma.Operation{
		OperationID: "getAssetDisposal", Method: http.MethodGet, Path: "/v1/asset-disposals/{disposal_id}", Tags: []string{"Fixed assets"},
		Summary: "Get a disposal of a fixed asset",
		Description: "Returns a disposal with its ETag: the asset, the cost and accumulated depreciation it took off, its proceeds, the gain " +
			"or loss, the lines its entry wrote and the pending request to void it. Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *AssetDisposalPath) (*disposalReadOutput, error) {
		id, err := parseID("disposal_id", in.DisposalID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var x AssetDisposalView
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if !asset.IsDisposal(doc) {
				return ports.ErrNotFound
			}
			base, err := assetBase(ctx, u)
			if err != nil {
				return err
			}
			asked, err := a.disposalAsked(ctx, u)
			if err != nil {
				return err
			}
			x, err = a.disposalOf(ctx, u, base, asked, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &disposalReadOutput{ETag: disposalETag(x), Body: x}, nil
	})

	shown := func(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (AssetDisposalView, error) {
		base, err := assetBase(ctx, u)
		if err != nil {
			return AssetDisposalView{}, err
		}
		return a.disposalOf(ctx, u, base, map[string]string{}, doc, true)
	}
	disposeCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed,
		CodeTooLarge, CodeMediaType, CodeInvalid, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, asset.CodeNotAsset,
		asset.CodeBadContent, team.CodeAction}, append(postingCodes(), DisposalCodes...)...)
	register(a, authz.DisposeFixedAsset, huma.Operation{
		OperationID: "disposeFixedAsset", Method: http.MethodPost, Path: "/v1/fixed-assets/{asset_id}/disposals", Tags: []string{"Fixed assets"},
		DefaultStatus: http.StatusCreated, Summary: "Sell or write off a fixed asset",
		Description: "Records the sale or write-off of an asset, or of a part of its cost, and posts it at once under asset_disposal@1 " +
			"(FR-FA-03; rev 0.149): Dr 1590 with that cost's accumulated depreciation through the month before; Dr the money account " +
			"with what came in and Cr 2200 with its output GST (RND-GST-INCL) when the organisation is GST-registered, or Dr the " +
			"disposal clearing account with the invoice line's net, or nothing for a write-off; Cr the asset account with the cost; " +
			"and the difference to the disposal account (assets.disposal_account, 7050): Dr a loss, Cr a gain. Every month of " +
			"depreciation before the disposal's must have posted (ASSET-17); those of its month and after, posted before it was " +
			"recorded, are reversed on their own dates. A partial disposal makes the asset's next version, the rest depreciating " +
			"from the disposal's month. Bank import matches money received to it. Side effects: the disposal, its number (DSP-), " +
			"its entry, the reversals, the asset's next version, audit events. Every problem comes back together. Needs If-Match " +
			"with the asset's ETag. Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, disposeCodes, func(ctx context.Context, in *disposeInput) (*disposalOutput, error) {
		id, err := parseID("asset_id", in.AssetID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		d, err := disposalDraftOf(id, in.Body)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		if !wantsPreview(in.Prefer) { // the months due by today post first, as the hourly job would (design D3)
			var today time.Time
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				today, err = orgToday(ctx, a, u)
				return err
			}); err != nil {
				return nil, err
			}
			if _, err := (journal.Depreciations{Store: a.deps.Store}).Depreciate(ctx, p.Organisation, id, today); err != nil {
				return nil, err
			}
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AssetDisposalView, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return AssetDisposalView{}, err
			}
			if !asset.IsAsset(doc) {
				return AssetDisposalView{}, ports.ErrNotFound
			}
			if err := checkIfMatch(in.IfMatch, assetETag(doc)); err != nil {
				return AssetDisposalView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return AssetDisposalView{}, err
			}
			rec, err := a.disposals.Record(ctx, u, d, today)
			if err != nil {
				return AssetDisposalView{}, err
			}
			return shown(ctx, u, rec.Disposal)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &disposalOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &disposalOutput{Status: w.Status, ETag: disposalETag(w.Body), Location: "/v1/asset-disposals/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
			Body: w.Body}, nil
	})

	voidCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeDisposalVoidApproval, team.CodeAction}, append(postingCodes(), DisposalApprovalCodes()...)...)
	locked := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, AssetDisposalView, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return doc, AssetDisposalView{}, err
		}
		if !asset.IsDisposal(doc) {
			return doc, AssetDisposalView{}, ports.ErrNotFound
		}
		x, err := shown(ctx, u, doc)
		if err != nil {
			return doc, x, err
		}
		if doc.Status != storage.DocumentPosted {
			return doc, x, &asset.Error{Code: asset.CodeDisposalVoid, Message: fmt.Sprintf("the disposal is %s, not posted", doc.Status),
				Fix: "Only a posted disposal is voided"}
		}
		return doc, x, checkIfMatch(ifMatch, disposalETag(x))
	}
	register(a, authz.VoidAssetDisposal, huma.Operation{
		OperationID: "voidAssetDisposal", Method: http.MethodPost, Path: "/v1/asset-disposals/{disposal_id}/void", Tags: []string{"Fixed assets"},
		Summary: "Void a disposal of a fixed asset",
		Description: "Voids a disposal with a reversing entry on the date given, with a reason (FR-GL-12), and restores the asset as it " +
			"stood before it, carrying what had posted before the disposal's month; its months from then post again. Refused " +
			"(ASSET-18) once the asset changed again or, after a partial disposal, a month of what was left posted. In Founder mode " +
			"voiding needs a second person (FM-1): it is refused (ASSET-19), and requestAssetDisposalVoid asks for the approval. " +
			"Side effects: the reversing entry, the asset's next version, audit events with the reason. Needs If-Match with its " +
			"ETag. Idempotent with Idempotency-Key; Prefer: return=preview writes nothing.",
	}, voidCodes, func(ctx context.Context, in *disposalCommandInput) (*disposalOutput, error) {
		id, err := parseID("disposal_id", in.DisposalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		date, err := time.Parse(time.DateOnly, in.Body.Date)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AssetDisposalView, error) {
			if _, _, err := locked(ctx, u, id, in.IfMatch); err != nil {
				return AssetDisposalView{}, err
			}
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return AssetDisposalView{}, err
			}
			if mode == team.Founder {
				return AssetDisposalView{}, problem(CodeDisposalVoidApproval, "In Founder mode, voiding a disposal needs a second person (FM-1).",
					"Ask for another person's approval to void it")
			}
			rec, err := a.disposals.Void(storage.WithReason(ctx, in.Body.Reason), u, id, date, in.Body.Reason)
			if err != nil {
				return AssetDisposalView{}, err
			}
			return shown(ctx, u, rec.Disposal)
		})
		if err != nil {
			return nil, err
		}
		return &disposalOutput{Status: http.StatusOK, ETag: disposalETag(w.Body), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	requestCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, DisposalApprovalCodes()...)
	register(a, authz.RequestAssetDisposalVoid, huma.Operation{
		OperationID: "requestAssetDisposalVoid", Method: http.MethodPost, Path: "/v1/asset-disposals/{disposal_id}/void-approval",
		Tags: []string{"Fixed assets"}, Summary: "Ask for approval to void a disposal of a fixed asset", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to void a disposal (FM-1): the answer is 202 with an approval request, which a " +
			"person who holds A on \"Fixed assets\" (the Controller) and did not ask approves, stepped up; on the approval it is voided on " +
			"the date given, as voidAssetDisposal does. Needs a person signed in who passed step-up, and If-Match with its ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, requestCodes, func(ctx context.Context, in *disposalCommandInput) (*billRequestOutput, error) {
		id, err := parseID("disposal_id", in.DisposalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		if _, err := time.Parse(time.DateOnly, in.Body.Date); err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			doc, x, err := locked(ctx, u, id, in.IfMatch)
			if err != nil {
				return Approval{}, err
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAssetDisposal, ObjectKey: id.String(), Operation: approval.OpAssetDisposalVoid,
				Summary: fmt.Sprintf("Void disposal %s of %s %s, cost %s %s", doc.Number, x.AssetNumber, x.AssetName, x.Cost.Amount, x.Cost.Currency),
				Execute: map[string]any{"disposal_id": id.String(), "date": in.Body.Date, "reason": in.Body.Reason},
				Diff:    []ApprovalDiff{{Field: "status", Before: "posted", After: "reversed"}, {Field: "reason", After: in.Body.Reason}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
