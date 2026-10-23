package api

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ purchase orders (FR-AP-01; PRD-001 rev 0.87)
//
// An order to a supplier, with a quantity and a unit price on each line (the owner's decisions of 23 Oct 2026). It
// posts nothing. It is approved as a bill is posted: at or below the self-approval threshold whoever prepares it
// approves it alone, logged as self-approved in Founder mode (FM-2, FM-3); above it another person who holds A on
// "Bills, vendor credits" approves it, never its preparer (SOD-3). Bills are made from what is left of its lines.

// The API's codes for purchase orders' approvals.
const (
	CodeOrderNeedsApproval = "PO-14" // the order's total is above the self-approval threshold: ask for approval (FM-2)
	CodeOwnOrder           = "PO-15" // whoever prepared the order never approves it (SOD-3)
)

// OrderCodes are the codes an order's checks answer.
var OrderCodes = []string{purchaseorder.CodeSupplier, purchaseorder.CodeNoDate, purchaseorder.CodeLines, purchaseorder.CodeAccount,
	purchaseorder.CodeQuantity, purchaseorder.CodeGSTCode, purchaseorder.CodeTooLong}

// OrderApprovalCodes are the codes an approval of an order may answer: SOD-3, and the approval's own.
func OrderApprovalCodes() []string {
	return append([]string{CodeOwnOrder, purchaseorder.CodeNotOrder, purchaseorder.CodeNotDraft, purchaseorder.CodeBadDraft}, OrderCodes...)
}

// PurchaseOrderLineDraft is one line of an order as entered.
type PurchaseOrderLineDraft struct {
	AccountID   string `json:"account_id" format:"uuid" doc:"The expense or asset account the line costs." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Description string `json:"description,omitempty" maxLength:"500" doc:"What is ordered." example:"Laptop, 14 inch"`
	Quantity    string `json:"quantity" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"How many, an exact decimal with up to 6 decimals." example:"3"`
	UnitPrice   string `json:"unit_price" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"The price of one before GST, in the order's currency, an exact decimal with up to 6 decimals. The line's amount is RND-LINE: quantity × unit price rounded to the currency's minor units, half away from zero." example:"1850.00"`
	GSTCode     string `json:"gst_code" pattern:"^[A-Z]{2,4}$" doc:"The GST code for purchases the supplier is expected to charge." example:"TX"`
}

// PurchaseOrderDraft is an order as entered.
type PurchaseOrderDraft struct {
	SupplierID string                   `json:"supplier_id,omitempty" format:"uuid" doc:"The supplier: a contact marked as a vendor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Reference  string                   `json:"reference,omitempty" maxLength:"60" doc:"A reference, such as the supplier's quotation number." example:"Q-2026-118"`
	Date       string                   `json:"date,omitempty" format:"date" doc:"The order's date." example:"2026-10-14"`
	ExpectedOn string                   `json:"expected_on,omitempty" format:"date" doc:"When the goods or services are expected." example:"2026-10-31"`
	Currency   string                   `json:"currency" pattern:"^[A-Z]{3}$" doc:"The order's currency." example:"SGD"`
	Lines      []PurchaseOrderLineDraft `json:"lines" maxItems:"200" doc:"What is ordered."`
	Notes      string                   `json:"notes,omitempty" maxLength:"2000" doc:"Notes kept with the order." example:"Deliver to level 3"`
}

// PurchaseOrderLine is a line of an order as it stands: what it comes to and what bills take of it.
type PurchaseOrderLine struct {
	PurchaseOrderLineDraft
	Amount   Money  `json:"amount" doc:"RND-LINE of quantity × unit price, in the order's currency."`
	GST      Money  `json:"gst" doc:"The GST expected: RND-GST-EXCL of the amount at the rate on the order's date, for a standard-rated code."`
	Billed   string `json:"billed" doc:"The quantity posted bills take of it." example:"1"`
	OnDrafts string `json:"on_drafts" doc:"The quantity draft bills take of it." example:"0"`
	Left     string `json:"left" doc:"What no bill takes yet: the quantity less what posted and draft bills take." example:"2"`
}

// PurchaseOrderStates are an order's separate states.
type PurchaseOrderStates struct {
	Approval string `json:"approval" enum:"draft,awaiting_approval,approved,rejected" doc:"draft until it is approved or asked for; awaiting_approval while a request waits; approved; rejected when the latest request was rejected and it is still a draft." example:"approved"`
	Billing  string `json:"billing" enum:"not_billed,part_billed,billed" doc:"What posted bills take of its lines: none, some, or every line in full." example:"part_billed"`
	Closed   bool   `json:"closed" doc:"Closed by a person: no more bills are made from it; those made already stand." example:"false"`
}

// PurchaseOrderBill is a bill made from an order.
type PurchaseOrderBill struct {
	ID     string `json:"id" format:"uuid" doc:"The bill's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Number string `json:"number,omitempty" doc:"Its number, once it has one." example:"BILL-0012"`
	Status string `json:"status" enum:"draft,posted" doc:"draft or posted." example:"posted"`
	Date   string `json:"date,omitempty" format:"date" doc:"Its date." example:"2026-10-20"`
}

// PurchaseOrder is a purchase order at its current version (FR-AP-01; PRD-001 rev 0.87).
type PurchaseOrder struct {
	ID            string              `json:"id" format:"uuid" doc:"The order's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e50"`
	Number        string              `json:"number,omitempty" doc:"Its number from the purchase order sequence: given when it is approved, or when it is saved if the sequence is not gap-free." example:"PO-0007"`
	Status        string              `json:"status" enum:"draft,approved,closed" doc:"draft (✎), approved, or closed." example:"approved"`
	Version       int                 `json:"version" doc:"The version of its content; with the status it makes the ETag." example:"1"`
	Supplier      *BillSupplier       `json:"supplier,omitempty" doc:"The supplier, with its name."`
	Reference     string              `json:"reference,omitempty" doc:"Its reference." example:"Q-2026-118"`
	Date          string              `json:"date,omitempty" format:"date" doc:"The order's date." example:"2026-10-14"`
	ExpectedOn    string              `json:"expected_on,omitempty" format:"date" doc:"When the goods or services are expected." example:"2026-10-31"`
	Currency      string              `json:"currency" doc:"The order's currency." example:"SGD"`
	Lines         []PurchaseOrderLine `json:"lines" nullable:"false" doc:"Its lines, with what they come to and what bills take of them."`
	Notes         string              `json:"notes,omitempty" doc:"Its notes." example:"Deliver to level 3"`
	Net           Money               `json:"net" doc:"Every line's amount, in its currency."`
	GST           Money               `json:"gst" doc:"The GST expected, in its currency."`
	Total         Money               `json:"total" doc:"Net and GST, in its currency."`
	TotalBase     *Money              `json:"total_base,omitempty" doc:"The total in the base currency at the rate for its date, on which its approval is judged (FM-2); absent while it has problems."`
	States        PurchaseOrderStates `json:"states" doc:"Its approval and billing states, and whether it is closed."`
	Checks        []BillCheck         `json:"checks" nullable:"false" doc:"getPurchaseOrder only: what stops a draft being approved (✖)."`
	Bills         []PurchaseOrderBill `json:"bills" nullable:"false" doc:"getPurchaseOrder only: the bills made from it, drafts and posted, not voided."`
	PreparedBy    *Person             `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	ApprovedBy    *Person             `json:"approved_by,omitempty" doc:"Who approved it."`
	ApprovedAt    *time.Time          `json:"approved_at,omitempty" doc:"When it was approved." example:"2026-10-14T09:30:00Z"`
	ApprovalAsked string              `json:"approval_asked,omitempty" format:"uuid" doc:"getPurchaseOrder only: the pending request to approve it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd2"`
}

// PurchaseOrderPage is a page of orders, newest date first.
type PurchaseOrderPage struct {
	Items      []PurchaseOrder `json:"items" nullable:"false"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTAtMzF8MDE5MiJ9"`
}

// PurchaseOrderPath names an order.
type PurchaseOrderPath struct {
	PurchaseOrderID string `path:"purchase_order_id" format:"uuid" doc:"The order's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e50"`
}

// OrderBillLine is the quantity of one of an order's lines a bill takes.
type OrderBillLine struct {
	Line     int    `json:"line" minimum:"1" doc:"The order's line, from 1." example:"1"`
	Quantity string `json:"quantity" pattern:"^(0|[1-9][0-9]{0,13})(\\.[0-9]{1,6})?$" doc:"How much of it the bill takes." example:"2"`
}

// OrderBillRequest says what a bill made from an order takes: some of its lines, or, left out, all that is left.
type OrderBillRequest struct {
	Lines []OrderBillLine `json:"lines,omitempty" maxItems:"200" doc:"The lines and quantities the bill takes; left out, everything no bill takes yet."`
}

// OrderCloseRequest is why an order is closed.
type OrderCloseRequest struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is closed, kept in the audit trail." example:"The supplier could not deliver the rest"`
}

type orderListInput struct {
	PageQuery
	Status string `query:"status" enum:"draft,approved,closed" doc:"Only orders with this status." example:"approved"`
}

type orderListOutput struct{ Body PurchaseOrderPage }

type orderCreateInput struct {
	KeyHeader
	PreferHeader
	Body PurchaseOrderDraft
}

type orderReplaceInput struct {
	PurchaseOrderPath
	IfMatchHeader
	PreferHeader
	Body PurchaseOrderDraft
}

type orderCommandInput struct {
	PurchaseOrderPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type orderCloseInput struct {
	PurchaseOrderPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body OrderCloseRequest
}

type orderBillInput struct {
	PurchaseOrderPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body *OrderBillRequest `required:"false"`
}

type orderReadOutput struct {
	ETag string `header:"ETag" doc:"The order's version and status, for If-Match."`
	Body PurchaseOrder
}

type orderWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The order's version and status, for If-Match."`
	Location string `header:"Location" doc:"The order's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PurchaseOrder
}

type orderBillOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The new bill's version and status, for If-Match."`
	Location string `header:"Location" doc:"The new bill's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Bill
}

type orderRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

func orderETag(o PurchaseOrder) string { return etag(o.Version, o.Status) }

func orderStatus(s storage.DocumentStatus) string {
	switch s {
	case storage.DocumentPosted:
		return "approved"
	case storage.DocumentReversed:
		return "closed"
	}
	return "draft"
}

// orderDraftOf reads an order from the request, with every invalid field at once.
func orderDraftOf(in PurchaseOrderDraft) (purchaseorder.Draft, error) {
	var fields []FieldProblem
	d := purchaseorder.Draft{Reference: strings.TrimSpace(in.Reference), Notes: in.Notes}
	if in.SupplierID != "" {
		id, err := uuid.Parse(in.SupplierID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.supplier_id", Message: "is not an ID", Fix: "Send the supplier's ID"})
		}
		d.SupplierID = id
	}
	for _, x := range []struct {
		text, field string
		to          *time.Time
	}{{in.Date, "body.date", &d.Date}, {in.ExpectedOn, "body.expected_on", &d.ExpectedOn}} {
		if x.text == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		*x.to = t
	}
	cur, err := money.Lookup(in.Currency)
	if err != nil {
		return d, invalid(append(fields, FieldProblem{Code: money.CodeCurrency, Field: "body.currency", Message: "is not a currency Fyniq knows",
			Fix: "Send an ISO 4217 code such as SGD"}))
	}
	d.Currency = cur
	for i, l := range in.Lines {
		at := func(f string) string { return "body.lines[" + strconv.Itoa(i) + "]." + f }
		id, err := uuid.Parse(l.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: at("account_id"), Message: "is not an ID", Fix: "Send the account's ID"})
		}
		q, err := money.ParseQuantity(l.Quantity, at("quantity"))
		if err != nil {
			fields = append(fields, fieldOf(err, at("quantity")))
			continue
		}
		p, err := money.ParseUnitPrice(l.UnitPrice, cur, at("unit_price"))
		if err != nil {
			fields = append(fields, fieldOf(err, at("unit_price")))
			continue
		}
		d.Lines = append(d.Lines, purchaseorder.Line{AccountID: id, Description: l.Description, Quantity: q, UnitPrice: p, GSTCode: l.GSTCode})
	}
	if len(fields) > 0 {
		return d, invalid(fields)
	}
	return d, nil
}

// orderView is what showing orders needs: who asked for or was refused approval, and the base currency.
type orderView struct {
	asked    map[string]string
	rejected map[string]int
	base     money.Currency
}

func (a *API) orderView(ctx context.Context, u storage.UnitOfWork) (orderView, error) {
	v := orderView{asked: map[string]string{}, rejected: map[string]int{}}
	var err error
	if v.base, err = baseOf(ctx, u); err != nil {
		return v, err
	}
	requests, err := u.Approvals().List(ctx, "")
	if err != nil {
		return v, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType != approval.ObjectOrder {
			continue
		}
		switch approval.StatusAt(requestOf(r), now) {
		case approval.Pending:
			v.asked[r.ObjectKey] = r.ID.String()
		case approval.Rejected:
			var c struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(r.Change, &c) == nil {
				if n, err := strconv.Atoi(c.Version); err == nil && n >= v.rejected[r.ObjectKey] {
					v.rejected[r.ObjectKey] = n
				}
			}
		}
	}
	return v, nil
}

// orderOf is an order as the API shows it. withChecks fills its checks and bills (getPurchaseOrder).
func (a *API) orderOf(ctx context.Context, u storage.UnitOfWork, v orderView, doc storage.Document, withChecks bool) (PurchaseOrder, error) {
	p, probs, err := a.orders.Check(ctx, u, doc)
	if err != nil {
		return PurchaseOrder{}, err
	}
	d := p.Draft
	out := PurchaseOrder{ID: doc.ID.String(), Number: doc.Number, Status: orderStatus(doc.Status), Version: doc.Version, Reference: d.Reference,
		Date: dayText(d.Date), ExpectedOn: dayText(d.ExpectedOn), Currency: d.Currency.Code, Notes: d.Notes, Lines: []PurchaseOrderLine{},
		Net: moneyOut(p.Net), GST: moneyOut(p.GST), Total: moneyOut(p.Total), Checks: []BillCheck{}, Bills: []PurchaseOrderBill{},
		PreparedBy: personOf(doc.SavedBy), ApprovedBy: personOf(doc.PostedBy), ApprovedAt: utc(doc.PostedAt)}
	if d.SupplierID != uuid.Nil {
		out.Supplier = &BillSupplier{ID: d.SupplierID.String()}
		if c, err := u.Contacts().Get(ctx, d.SupplierID); err == nil {
			out.Supplier.Name = c.LegalName
		}
	}
	if len(probs) == 0 {
		tb := moneyOut(p.TotalBase)
		out.TotalBase = &tb
	}
	states, bills, err := purchaseorder.Billing(ctx, u, doc.ID, d, v.base, uuid.Nil)
	if err != nil {
		return PurchaseOrder{}, err
	}
	amounts := map[int]purchaseorder.PreparedLine{}
	for i, l := range p.Lines {
		amounts[i] = l
	}
	billed, full := false, len(d.Lines) > 0
	for i, l := range d.Lines {
		line := PurchaseOrderLine{PurchaseOrderLineDraft: PurchaseOrderLineDraft{AccountID: l.AccountID.String(), Description: l.Description,
			Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.String(), GSTCode: l.GSTCode},
			Amount: moneyOut(money.Zero(d.Currency)), GST: moneyOut(money.Zero(d.Currency)), Billed: states[i].Billed.String(),
			OnDrafts: states[i].Drafted.String(), Left: states[i].Left().String()}
		if x, ok := amounts[i]; ok && len(p.Lines) == len(d.Lines) {
			line.Amount, line.GST = moneyOut(x.Amount), moneyOut(x.GST)
		}
		billed = billed || states[i].Billed.Sign() > 0
		full = full && states[i].Billed.Cmp(states[i].Ordered) >= 0
		out.Lines = append(out.Lines, line)
	}
	out.States = PurchaseOrderStates{Approval: "draft", Billing: "not_billed", Closed: doc.Status == storage.DocumentReversed}
	switch {
	case full:
		out.States.Billing = "billed"
	case billed:
		out.States.Billing = "part_billed"
	}
	if doc.Status != storage.DocumentDraft {
		out.States.Approval = "approved"
	} else if out.ApprovalAsked = v.asked[out.ID]; out.ApprovalAsked != "" {
		out.States.Approval = "awaiting_approval"
	} else if n, ok := v.rejected[out.ID]; ok && n == doc.Version {
		out.States.Approval = "rejected"
	}
	if !withChecks {
		out.ApprovalAsked = ""
	} else {
		for _, x := range probs {
			out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
		}
		for _, b := range bills {
			out.Bills = append(out.Bills, PurchaseOrderBill{ID: b.ID.String(), Number: b.Number, Status: string(b.Status), Date: b.Date})
		}
	}
	return out, a.name(ctx, u, out.PreparedBy, out.ApprovedBy)
}

// orderProblems turns an order's problems into the API's.
func orderProblems(err error) error {
	var lp ledger.Problems
	if !errors.As(err, &lp) || len(lp) == 0 {
		return err
	}
	fields := make([]FieldProblem, 0, len(lp))
	for _, x := range lp {
		fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
	}
	return problem(lp[0].Code, fmt.Sprintf("The order cannot go ahead: %d problem(s), each listed in errors.", len(lp)),
		"Correct the order and try again.", fields...)
}

// lockOrder reads an order for a command at the version read.
func (a *API) lockOrder(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.Document, PurchaseOrder, orderView, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, PurchaseOrder{}, orderView{}, err
	}
	if doc.Type != storage.DocumentPurchaseOrder {
		return doc, PurchaseOrder{}, orderView{}, problem(purchaseorder.CodeNotOrder, "That record is not a purchase order.", "Use the order's ID.")
	}
	v, err := a.orderView(ctx, u)
	if err != nil {
		return doc, PurchaseOrder{}, v, err
	}
	o, err := a.orderOf(ctx, u, v, doc, false)
	if err != nil {
		return doc, o, v, err
	}
	return doc, o, v, checkIfMatch(ifMatch, orderETag(o))
}

// gateOrderApproval refuses approving an order above the threshold (FM-2), and says whether approving one at or below
// it is a Founder-mode self-approval to log.
func (a *API) gateOrderApproval(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (bool, money.Money, error) {
	p, probs, err := a.orders.Check(ctx, u, doc)
	if err != nil {
		return false, money.Money{}, err
	}
	if len(probs) > 0 {
		return false, money.Money{}, orderProblems(probs)
	}
	threshold, err := selfApprovalThreshold(ctx, a, u, p.TotalBase.Currency())
	if err != nil {
		return false, money.Money{}, err
	}
	if over, err := p.TotalBase.Cmp(threshold); err != nil {
		return false, money.Money{}, err
	} else if over > 0 {
		return false, money.Money{}, problem(CodeOrderNeedsApproval,
			fmt.Sprintf("The order's total is %s %s, above the self-approval threshold of %s.", p.TotalBase, p.TotalBase.Currency().Code, threshold),
			"Ask for another person's approval")
	}
	mode, _, _, err := teamModeOf(ctx, u)
	return mode == team.Founder, p.TotalBase, err
}

func registerPurchaseOrders(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListPurchaseOrders, huma.Operation{
		OperationID: "listPurchaseOrders", Method: http.MethodGet, Path: "/v1/purchase-orders", Tags: []string{"Purchases"},
		Summary:     "List the purchase orders",
		Description: "The organisation's purchase orders, newest date first, a page at a time (FR-AP-01; PRD-001 rev 0.87): each one's states, total and what bills take of its lines. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *orderListInput) (*orderListOutput, error) {
		cur, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		f := storage.DocumentFilter{Type: storage.DocumentPurchaseOrder, Limit: in.PageSize + 1}
		switch in.Status {
		case "draft":
			f.Status = storage.DocumentDraft
		case "approved":
			f.Status = storage.DocumentPosted
		case "closed":
			f.Status = storage.DocumentReversed
		}
		if cur != "" {
			date, id, ok := strings.Cut(cur, "|")
			uid, err := uuid.Parse(id)
			if !ok || err != nil {
				return nil, badCursor()
			}
			f.AfterDate, f.AfterID = date, &uid
		}
		p, _ := authn.From(ctx)
		out := orderListOutput{Body: PurchaseOrderPage{Items: []PurchaseOrder{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, f)
			if err != nil {
				return err
			}
			if len(docs) > in.PageSize {
				docs = docs[:in.PageSize]
				last := docs[len(docs)-1]
				out.Body.NextCursor = encodeCursor(last.Date + "|" + last.ID.String())
			}
			v, err := a.orderView(ctx, u)
			if err != nil {
				return err
			}
			for _, doc := range docs {
				o, err := a.orderOf(ctx, u, v, doc, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, o)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.CreatePurchaseOrder, huma.Operation{
		OperationID: "createPurchaseOrder", Method: http.MethodPost, Path: "/v1/purchase-orders", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusCreated, Summary: "Raise a purchase order",
		Description: "Saves a draft purchase order (FR-AP-01; PRD-001 rev 0.87): the supplier, a reference, its date, when the goods are " +
			"expected, the currency, and lines with a quantity, a unit price and a GST code. It may be incomplete: getPurchaseOrder says " +
			"what stops it being approved. It posts nothing, ever. Side effects: the document and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid,
		purchaseorder.CodeTooLong, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency}, locks...),
		func(ctx context.Context, in *orderCreateInput) (*orderWriteOutput, error) {
			d, err := orderDraftOf(in.Body)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PurchaseOrder, error) {
				doc, err := a.orders.Create(ctx, p.Organisation, d)
				if err != nil {
					return PurchaseOrder{}, err
				}
				v, err := a.orderView(ctx, u)
				if err != nil {
					return PurchaseOrder{}, err
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return PurchaseOrder{}, err
				}
				return a.orderOf(ctx, u, v, doc, true)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &orderWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &orderWriteOutput{Status: w.Status, ETag: orderETag(w.Body), Location: "/v1/purchase-orders/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetPurchaseOrder, huma.Operation{
		OperationID: "getPurchaseOrder", Method: http.MethodGet, Path: "/v1/purchase-orders/{purchase_order_id}", Tags: []string{"Purchases"},
		Summary:     "Get a purchase order",
		Description: "Returns a purchase order with its states, what stops a draft being approved, what bills take of each line and the bills made from it, with its ETag (FR-AP-01). Read-only; no side effects.",
	}, []string{CodeNotFound, purchaseorder.CodeNotOrder}, func(ctx context.Context, in *PurchaseOrderPath) (*orderReadOutput, error) {
		id, err := parseID("purchase_order_id", in.PurchaseOrderID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out orderReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if doc.Type != storage.DocumentPurchaseOrder {
				return problem(purchaseorder.CodeNotOrder, "That record is not a purchase order.", "Use the order's ID.")
			}
			v, err := a.orderView(ctx, u)
			if err != nil {
				return err
			}
			out.Body, err = a.orderOf(ctx, u, v, doc, true)
			out.ETag = orderETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplacePurchaseOrder, huma.Operation{
		OperationID: "replacePurchaseOrder", Method: http.MethodPut, Path: "/v1/purchase-orders/{purchase_order_id}", Tags: []string{"Purchases"},
		Summary: "Change a draft purchase order",
		Description: "Saves a draft order's new content as its next version (FR-AP-01). An approved order never changes: close it and " +
			"raise another (PO-10). Side effects: the new version and its audit event. Needs If-Match with the order's ETag. Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, purchaseorder.CodeNotOrder,
		purchaseorder.CodeNotDraft, purchaseorder.CodeTooLong, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency}, locks...),
		func(ctx context.Context, in *orderReplaceInput) (*orderWriteOutput, error) {
			id, err := parseID("purchase_order_id", in.PurchaseOrderID)
			if err != nil {
				return nil, err
			}
			d, err := orderDraftOf(in.Body)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PurchaseOrder, error) {
				if _, _, _, err := a.lockOrder(ctx, u, id, in.IfMatch); err != nil {
					return PurchaseOrder{}, err
				}
				doc, err := a.orders.Revise(ctx, p.Organisation, id, d)
				if err != nil {
					return PurchaseOrder{}, err
				}
				v, err := a.orderView(ctx, u)
				if err != nil {
					return PurchaseOrder{}, err
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return PurchaseOrder{}, err
				}
				return a.orderOf(ctx, u, v, doc, true)
			})
			if err != nil {
				return nil, err
			}
			return &orderWriteOutput{Status: w.Status, ETag: orderETag(w.Body), Applied: applied(w), Body: w.Body}, nil
		})

	commandCodes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		purchaseorder.CodeNotOrder, purchaseorder.CodeNotDraft, purchaseorder.CodeNotApproved, purchaseorder.CodeBadDraft}, locks...)
	command := func(ctx context.Context, rawID, key, ifMatch, prefer string, request any, status int,
		run func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document) (storage.Document, error)) (*orderWriteOutput, error) {
		id, err := parseID("purchase_order_id", rawID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(ifMatch) == "" {
			return nil, checkIfMatch(ifMatch, "")
		}
		p, _ := authn.From(ctx)
		w, err := write(ctx, a, key, prefer, request, status, func(ctx context.Context, u storage.UnitOfWork) (PurchaseOrder, error) {
			doc, _, v, err := a.lockOrder(ctx, u, id, ifMatch)
			if err != nil {
				return PurchaseOrder{}, err
			}
			if doc, err = run(ctx, u, p, doc); err != nil {
				return PurchaseOrder{}, orderProblems(err)
			}
			if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
				return PurchaseOrder{}, err
			}
			return a.orderOf(ctx, u, v, doc, true)
		})
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &orderWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		}
		return &orderWriteOutput{Status: w.Status, ETag: orderETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	}

	register(a, authz.ApprovePurchaseOrder, huma.Operation{
		OperationID: "approvePurchaseOrder", Method: http.MethodPost, Path: "/v1/purchase-orders/{purchase_order_id}/approve", Tags: []string{"Purchases"},
		Summary: "Approve a purchase order",
		Description: "Approves a draft order as a bill is posted (FR-AP-01, FM-2; the owner's decision of 23 Oct 2026): it takes its number " +
			"and is approved by the caller, and posts nothing. Every problem comes back together. An order whose total in the base " +
			"currency is above the self-approval threshold is refused (PO-14): ask with requestPurchaseOrderApproval; at or below it, " +
			"an approval in Founder mode is logged as self-approved for the weekly review (FM-3). Side effects: the document's change " +
			"to approved and its audit events. Needs If-Match with the draft's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview approves nothing.",
	}, append([]string{CodeOrderNeedsApproval, CodeSessionRequired}, append(commandCodes, OrderCodes...)...),
		func(ctx context.Context, in *orderCommandInput) (*orderWriteOutput, error) {
			return command(ctx, in.PurchaseOrderID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, http.StatusOK,
				func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document) (storage.Document, error) {
					if doc.Status != storage.DocumentDraft {
						return doc, problem(purchaseorder.CodeNotDraft, "The order is "+orderStatus(doc.Status)+".", "Only a draft is approved")
					}
					self, total, err := a.gateOrderApproval(ctx, u, doc)
					if err != nil {
						return doc, err
					}
					out, err := a.orders.Approve(ctx, p.Organisation, doc.ID)
					if err == nil && self {
						var actor *uuid.UUID
						if p.UserID != uuid.Nil {
							actor = &p.UserID
						}
						_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectOrder,
							ObjectKey: doc.ID.String(), Summary: fmt.Sprintf("Approved purchase order %s of %s %s alone, at or below the self-approval threshold",
								cmpText(out.Number, "(a draft)"), total, total.Currency().Code), Actor: actor, LoggedAt: a.deps.Clock.Now()})
					}
					return out, err
				})
		})

	register(a, authz.RequestPurchaseOrderApproval, huma.Operation{
		OperationID: "requestPurchaseOrderApproval", Method: http.MethodPost, Path: "/v1/purchase-orders/{purchase_order_id}/approval", Tags: []string{"Purchases"},
		Summary: "Ask for approval of a purchase order", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval of a draft order at the version read (FM-2, SOD-3): what approvePurchaseOrder asks " +
			"for when the order's total is above the self-approval threshold (PO-14). The answer is 202 with an approval request, which a " +
			"person who holds A on \"Bills, vendor credits\", did not ask and did not prepare the order approves, stepped up; on the " +
			"approval the order is approved, by the approver. The request holds the order's version, status and total in the base " +
			"currency, so a change to the draft voids it. Asking needs a person signed in who passed step-up, and If-Match with the " +
			"order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, approval.CodePending, storage.CodeApproval, purchaseorder.CodeNotOrder, purchaseorder.CodeNotDraft}, append(locks, OrderCodes...)...),
		func(ctx context.Context, in *orderCommandInput) (*orderRequestOutput, error) {
			id, err := parseID("purchase_order_id", in.PurchaseOrderID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, o, _, err := a.lockOrder(ctx, u, id, in.IfMatch)
				if err != nil {
					return Approval{}, err
				}
				if doc.Status != storage.DocumentDraft {
					return Approval{}, problem(purchaseorder.CodeNotDraft, "The order is "+o.Status+".", "Ask for approval of a draft")
				}
				if o.TotalBase == nil {
					_, probs, err := a.orders.Check(ctx, u, doc)
					if err != nil {
						return Approval{}, err
					}
					return Approval{}, orderProblems(probs)
				}
				name := cmpText(o.Number, "the draft purchase order")
				if o.Supplier != nil && o.Supplier.Name != "" {
					name += " to " + o.Supplier.Name
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectOrder, ObjectKey: doc.ID.String(), Operation: approval.OpOrderApprove,
					Summary: fmt.Sprintf("Approve %s, total %s %s", name, o.Total.Amount, o.Total.Currency),
					Execute: map[string]any{"purchase_order_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
					Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "approved"}, {Field: "total", After: o.TotalBase.Amount}}})
			})
			if err != nil {
				return nil, err
			}
			return &orderRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ClosePurchaseOrder, huma.Operation{
		OperationID: "closePurchaseOrder", Method: http.MethodPost, Path: "/v1/purchase-orders/{purchase_order_id}/close", Tags: []string{"Purchases"},
		Summary: "Close a purchase order",
		Description: "Closes an approved order, with a reason (FR-AP-01): no more bills are made from it, and a draft bill made from it no " +
			"longer posts; the bills posted already stand. Side effects: the document's change to closed and its audit event with the " +
			"reason. Needs If-Match with the order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview closes nothing.",
	}, append([]string{CodeInvalid}, commandCodes...),
		func(ctx context.Context, in *orderCloseInput) (*orderWriteOutput, error) {
			return command(ctx, in.PurchaseOrderID, in.IdempotencyKey, in.IfMatch, in.Prefer, in, http.StatusOK,
				func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, doc storage.Document) (storage.Document, error) {
					return a.orders.Close(ctx, p.Organisation, doc.ID, strings.TrimSpace(in.Body.Reason))
				})
		})

	register(a, authz.BillPurchaseOrder, huma.Operation{
		OperationID: "billPurchaseOrder", Method: http.MethodPost, Path: "/v1/purchase-orders/{purchase_order_id}/bill", Tags: []string{"Purchases"},
		DefaultStatus: http.StatusCreated, Summary: "Make a bill from a purchase order",
		Description: "Makes a draft bill from an approved order (FR-AP-01; the owner's decision of 23 Oct 2026: billed by quantity): of each " +
			"line, the quantity asked for, or everything no bill takes yet. Each bill line names its order line and quantity; its " +
			"amount is the part by cumulative RND-LINE, so a line's parts add up to its amount, and its GST is what the supplier is " +
			"expected to charge. Enter the supplier's invoice date and number, correct the amounts to the invoice, and post it as any " +
			"bill: a posted bill never takes more of a line than is left (PO-13). Side effects: the draft bill and its audit event. " +
			"Needs If-Match with the order's ETag. Idempotent with Idempotency-Key; Prefer: return=preview makes nothing.",
	}, append([]string{CodeInvalid, purchaseorder.CodeLeft, money.CodeNotDecimal, money.CodePrecision, money.CodeRange}, commandCodes...),
		func(ctx context.Context, in *orderBillInput) (*orderBillOutput, error) {
			id, err := parseID("purchase_order_id", in.PurchaseOrderID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			var quantities map[int]money.Quantity
			var bad []FieldProblem
			var lines []OrderBillLine
			if in.Body != nil {
				lines = in.Body.Lines
			}
			for i, l := range lines {
				q, err := money.ParseQuantity(l.Quantity, "body.lines["+strconv.Itoa(i)+"].quantity")
				if err != nil {
					bad = append(bad, fieldOf(err, "body.lines["+strconv.Itoa(i)+"].quantity"))
					continue
				}
				if quantities == nil {
					quantities = map[int]money.Quantity{}
				}
				quantities[l.Line] = q
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Bill, error) {
				if _, _, _, err := a.lockOrder(ctx, u, id, in.IfMatch); err != nil {
					return Bill{}, err
				}
				doc, err := a.orders.Bill(ctx, p.Organisation, id, quantities)
				if err != nil {
					return Bill{}, orderProblems(err)
				}
				v, err := a.billView(ctx, u)
				if err != nil {
					return Bill{}, err
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return Bill{}, err
				}
				return a.billOf(ctx, u, v, doc, true)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &orderBillOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &orderBillOutput{Status: w.Status, ETag: billETag(w.Body), Location: "/v1/bills/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
				Body: w.Body}, nil
		})
}
