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
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ costs before the books start (FR-EQ-03; p1-founders-money)
//
// A cost a director or shareholder paid before the books start is recorded with its original date and its receipt by an
// Owner or the Controller (A on "Director accounts"), and needs the accountant's decision (⚠): it posts nothing until
// then. The accountant (P on the row) decides its income-tax treatment, its account and whether its GST is claimed, and
// the decision posts it under pre_incorporation_cost@1 (Dr the account · Cr 2500, naming the person, paid personally) on
// the books start, or on the day of the decision when the books start's period is closed (design D7). The person who
// paid it never decides it.

// CodeOwnCost is a decision by the member who signs in as the person who paid the cost.
const CodeOwnCost = "FOUNDER-10"

// PreCostCodes are the codes a cost's own checks answer.
var PreCostCodes = []string{director.CodePreCost, director.CodeNotPerson, director.CodeNotPreCost}

// PreIncorporationCostDraft is a cost as recorded.
type PreIncorporationCostDraft struct {
	Date        string `json:"date" format:"date" doc:"The date on the receipt: before the books start." example:"2025-08-28"`
	Supplier    string `json:"supplier" maxLength:"200" doc:"Who it was paid to." example:"Corporate secretary"`
	Description string `json:"description" maxLength:"500" doc:"What it was for." example:"Incorporation: ACRA fee and constitution"`
	ContactID   string `json:"contact_id" format:"uuid" doc:"The director or shareholder who paid it: a contact marked so." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Gross       Money  `json:"gross" doc:"What they paid, GST included, in the base currency."`
	GST         *Money `json:"gst,omitempty" doc:"The GST the receipt shows, in the base currency; left out when it shows none."`
}

// PreIncorporationCostDecisionDraft is the accountant's decision.
type PreIncorporationCostDecisionDraft struct {
	Treatment string `json:"treatment" enum:"deductible,capital,not_deductible" doc:"The income-tax treatment: deductible (a revenue cost from up to 12 months before the deemed start of business, Income Tax Act s14R), capital (such as incorporation costs or equipment), or not_deductible." example:"capital"`
	AccountID string `json:"account_id" format:"uuid" doc:"The account the cost posts to: an active account that is not a group, share capital, a money account, a director's account or GST control." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	ClaimGST  bool   `json:"claim_gst,omitempty" doc:"Claim the GST shown as input tax: only while the organisation is GST-registered on the posting date. A claim from before registration is made in the first GST return with IRAS's approval [verify]; otherwise the GST stays in the cost." example:"false"`
	Note      string `json:"note,omitempty" maxLength:"1000" doc:"Why, in the accountant's words." example:"Incorporation cost: capital, not deductible."`
}

// PreIncorporationCostDecision is the accountant's decision on a cost, as recorded.
type PreIncorporationCostDecision struct {
	Treatment   string  `json:"treatment" enum:"deductible,capital,not_deductible" doc:"The income-tax treatment." example:"capital"`
	AccountID   string  `json:"account_id" format:"uuid" doc:"The account it posted to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	AccountCode string  `json:"account_code,omitempty" doc:"That account's code." example:"6910"`
	AccountName string  `json:"account_name,omitempty" doc:"That account's name." example:"Incorporation costs"`
	ClaimGST    bool    `json:"claim_gst" doc:"Whether the GST shown was claimed as input tax." example:"false"`
	Note        string  `json:"note,omitempty" doc:"The accountant's note." example:"Incorporation cost: capital, not deductible."`
	PostOn      string  `json:"post_on" format:"date" doc:"The day it posted: the books start, or the day of the decision when the books start's period was closed." example:"2026-01-01"`
	DecidedOn   string  `json:"decided_on" format:"date" doc:"The day it was decided." example:"2026-11-03"`
	DecidedBy   *Person `json:"decided_by,omitempty" doc:"Who decided it."`
}

// PreIncorporationCost is a cost a director or shareholder paid before the books start.
type PreIncorporationCost struct {
	ID           string                        `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb1"`
	Number       string                        `json:"number,omitempty" doc:"Its number (FR-ORG-04)." example:"PIC-0001"`
	Status       string                        `json:"status" enum:"needs_decision,decided" doc:"needs_decision (⚠): recorded, posting nothing until the accountant decides it; decided: posted." example:"needs_decision"`
	Version      int                           `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Date         string                        `json:"date" format:"date" doc:"The date on the receipt." example:"2025-08-28"`
	Supplier     string                        `json:"supplier" doc:"Who it was paid to." example:"Corporate secretary"`
	Description  string                        `json:"description" doc:"What it was for." example:"Incorporation: ACRA fee and constitution"`
	ContactID    string                        `json:"contact_id" format:"uuid" doc:"The director or shareholder who paid it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	PaidBy       string                        `json:"paid_by" doc:"Their legal name." example:"Mei Lin Tan"`
	Gross        Money                         `json:"gross" doc:"What they paid, GST included."`
	GST          *Money                        `json:"gst,omitempty" doc:"The GST the receipt shows."`
	Receipts     int                           `json:"receipts" doc:"How many files are attached and not removed: the decision needs one." example:"1"`
	Decision     *PreIncorporationCostDecision `json:"decision,omitempty" doc:"Once decided: the accountant's decision."`
	Checks       []BillCheck                   `json:"checks" nullable:"false" doc:"Its statuses: ⚠ while it needs a decision, ✖ while it has no receipt."`
	LedgerImpact []EntryLine                   `json:"ledger_impact" nullable:"false" doc:"getPreIncorporationCost only, once decided: the lines its entry wrote, debits positive."`
}

// PreIncorporationCostOwed is what the costs come to for one person.
type PreIncorporationCostOwed struct {
	ContactID     string `json:"contact_id" format:"uuid" doc:"The director or shareholder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName     string `json:"legal_name" doc:"Their legal name." example:"Mei Lin Tan"`
	Decided       Money  `json:"decided" doc:"The costs decided and posted to their account on 2500."`
	NeedsDecision Money  `json:"needs_decision" doc:"The costs still waiting for the accountant."`
	Total         Money  `json:"total" doc:"Both together."`
}

// PreIncorporationCostPage is a page of costs, newest date first, with what every cost comes to for each person.
type PreIncorporationCostPage struct {
	Items      []PreIncorporationCost     `json:"items" nullable:"false"`
	Owed       []PreIncorporationCostOwed `json:"owed" nullable:"false" doc:"Every cost, whatever the page or filter, by the person who paid it, in the order they were added."`
	Total      Money                      `json:"total" doc:"Every cost the founders paid, whatever the page or filter."`
	BooksStart string                     `json:"books_start,omitempty" format:"date" doc:"The books start: every cost is dated before it, and a decided one posts on it." example:"2026-01-01"`
	NextCursor string                     `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjUtMDgtMjh8MDE5MiJ9"`
}

// PreIncorporationCostPath names a cost in the path.
type PreIncorporationCostPath struct {
	CostID string `path:"cost_id" format:"uuid" doc:"The cost's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb1"`
}

type preCostListInput struct {
	PageQuery
	Status string `query:"status" enum:"needs_decision,decided" doc:"Only the costs with this status." example:"needs_decision"`
}

type preCostListOutput struct{ Body PreIncorporationCostPage }

type recordPreCostInput struct {
	KeyHeader
	PreferHeader
	Body PreIncorporationCostDraft
}

type replacePreCostInput struct {
	PreIncorporationCostPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body PreIncorporationCostDraft
}

type decidePreCostInput struct {
	PreIncorporationCostPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body PreIncorporationCostDecisionDraft
}

type preCostOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and it shows what it would post (FR-INT-14)."`
	Body     PreIncorporationCost
}

type preCostReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body PreIncorporationCost
}

func preCostETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// preCostIn reads a cost as sent, with every invalid field at once.
func preCostIn(d PreIncorporationCostDraft) (director.PreCost, error) {
	c := director.PreCost{Supplier: strings.TrimSpace(d.Supplier), Description: strings.TrimSpace(d.Description)}
	var fields []FieldProblem
	day, err := time.Parse(time.DateOnly, d.Date)
	if err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
	}
	c.Date = day
	if c.ContactID, err = uuid.Parse(d.ContactID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.contact_id", Message: "is not an ID", Fix: "Send the contact's ID"})
	}
	gross, bad := moneyIn(d.Gross, "body.gross")
	if bad != nil {
		fields = append(fields, *bad)
	}
	c.Gross = gross
	if d.GST != nil {
		g, bad := moneyIn(*d.GST, "body.gst")
		if bad != nil {
			fields = append(fields, *bad)
		}
		c.GST = &g
	}
	if len(fields) > 0 {
		return c, invalid(fields)
	}
	return c, nil
}

// preCostView is what showing costs needs once: the base currency, the chart, and the persons' names.
type preCostView struct {
	base     money.Currency
	accounts map[uuid.UUID]storage.Account
	names    map[uuid.UUID]string
}

func (a *API) preCostView(ctx context.Context, u storage.UnitOfWork) (preCostView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return preCostView{}, err
	}
	v := preCostView{accounts: map[uuid.UUID]storage.Account{}, names: map[uuid.UUID]string{}}
	if v.base, err = money.Lookup(o.BaseCurrency); err != nil {
		return preCostView{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return preCostView{}, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	for after := uuid.Nil; ; {
		page, err := u.Contacts().Page(ctx, after, "director", 500)
		if err != nil {
			return preCostView{}, err
		}
		for _, c := range page {
			v.names[c.ID] = c.LegalName
		}
		if len(page) < 500 {
			return v, nil
		}
		after = page[len(page)-1].ID
	}
}

// preCostOf is a cost as the API shows it; withDetail fills its ledger impact (getPreIncorporationCost).
func (a *API) preCostOf(ctx context.Context, u storage.UnitOfWork, v preCostView, doc storage.Document, withDetail bool) (PreIncorporationCost, error) {
	c, err := director.ReadPreCost(doc)
	if err != nil {
		return PreIncorporationCost{}, err
	}
	out := PreIncorporationCost{ID: doc.ID.String(), Number: doc.Number, Status: "needs_decision", Version: doc.Version, Date: dayText(c.Date),
		Supplier: c.Supplier, Description: c.Description, ContactID: c.ContactID.String(), PaidBy: v.names[c.ContactID], Gross: moneyOut(c.Gross),
		Checks: []BillCheck{}, LedgerImpact: []EntryLine{}}
	if c.GST != nil {
		g := moneyOut(*c.GST)
		out.GST = &g
	}
	files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 100)
	if err != nil {
		return PreIncorporationCost{}, err
	}
	for _, f := range files {
		if f.State != storage.EvidenceRemoved {
			out.Receipts++
		}
	}
	if d := c.Decision; d != nil && doc.Status == storage.DocumentPosted {
		out.Status = "decided"
		acc := v.accounts[d.AccountID]
		out.Decision = &PreIncorporationCostDecision{Treatment: d.Treatment, AccountID: d.AccountID.String(), AccountCode: acc.Code, AccountName: acc.Name,
			ClaimGST: d.ClaimGST, Note: d.Note, PostOn: dayText(d.PostOn), DecidedOn: dayText(d.DecidedOn), DecidedBy: personOf(d.DecidedBy)}
		if withDetail {
			e, err := u.Ledger().ByIdentity(ctx, ledger.PreIncorporationCostIdentity(doc.ID, doc.Version, ledger.RulePreIncorporationCost))
			if err != nil {
				return PreIncorporationCost{}, err
			}
			out.LedgerImpact = entryOf(e).Lines
		}
		if out.Decision.DecidedBy != nil {
			if err := a.name(ctx, u, out.Decision.DecidedBy); err != nil {
				return PreIncorporationCost{}, err
			}
		}
		return out, nil
	}
	out.Checks = append(out.Checks, BillCheck{Severity: "warning", Code: director.CodePreCost, Field: "decision",
		Message: "it needs the accountant's decision, and posts nothing until then", Fix: "Ask the accountant to decide its treatment and account"})
	if out.Receipts == 0 {
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: director.CodeNoReceipt, Field: "receipts",
			Message: "it has no receipt, so it cannot be decided", Fix: "Attach the receipt or invoice the person paid"})
	}
	return out, nil
}

// readablePreCost is a cost the caller may read: not found for one whose right on "Director accounts" is qualified (an
// Employee's own), whatever the operation's own policy.
func readablePreCost(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, error) {
	if !authz.Decide(authz.GetPreIncorporationCost, p.Roles, p.AI()).Allowed {
		return storage.Document{}, ports.ErrNotFound
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !director.IsPreCost(doc) {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

func registerPreCosts(a *API) {
	readCodes := []string{CodeNotFound, director.CodeNotPreCost, director.CodePreCost}

	register(a, authz.ListPreIncorporationCosts, huma.Operation{
		OperationID: "listPreIncorporationCosts", Method: http.MethodGet, Path: "/v1/pre-incorporation-costs", Tags: []string{"Founders"},
		Summary: "List costs paid before the books start",
		Description: "The costs directors and shareholders paid before the books start, newest date first, a page at a time, narrowed by " +
			"status (FR-EQ-03): each with its person, its amount and GST, its receipts, and the accountant's decision once made (⚠ while " +
			"it needs one). With them, what every cost comes to for each person, decided and waiting, and the books start. Needs the " +
			"right to read every director's account. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *preCostListInput) (*preCostListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := preCostListOutput{Body: PreIncorporationCostPage{Items: []PreIncorporationCost{}, Owed: []PreIncorporationCostOwed{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.preCostView(ctx, u)
			if err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			if o.Profile.BooksStart != nil {
				out.Body.BooksStart = dateOf(*o.Profile.BooksStart)
			}
			type owed struct{ decided, waiting money.Money }
			byPerson, order, total := map[uuid.UUID]*owed{}, []uuid.UUID{}, money.Zero(v.base)
			full := false
			var lastDate, lastID string
			for cur, curID := "", (*uuid.UUID)(nil); ; {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentPreIncorporationCost, AfterDate: cur, AfterID: curID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					cur, curID = doc.Date, &doc.ID
					c, err := director.ReadPreCost(doc)
					if err != nil {
						return err
					}
					o, ok := byPerson[c.ContactID]
					if !ok {
						o = &owed{money.Zero(v.base), money.Zero(v.base)}
						byPerson[c.ContactID], order = o, append(order, c.ContactID)
					}
					decided := doc.Status == storage.DocumentPosted
					if decided {
						o.decided, _ = o.decided.Add(c.Gross)
					} else {
						o.waiting, _ = o.waiting.Add(c.Gross)
					}
					total, _ = total.Add(c.Gross)
					// The page: after the cursor, of the status asked for, up to its size.
					if afterID != nil && (doc.Date > afterDate || doc.Date == afterDate && doc.ID.String() >= afterID.String()) || full {
						continue
					}
					if in.Status == "decided" && !decided || in.Status == "needs_decision" && decided {
						continue
					}
					if len(out.Body.Items) == in.PageSize {
						full = true
						out.Body.NextCursor = encodeCursor(lastDate + "|" + lastID)
						continue
					}
					x, err := a.preCostOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					out.Body.Items = append(out.Body.Items, x)
					lastDate, lastID = doc.Date, doc.ID.String()
				}
				if len(docs) < 500 {
					break
				}
			}
			for _, id := range order {
				o := byPerson[id]
				sum, _ := o.decided.Add(o.waiting)
				out.Body.Owed = append(out.Body.Owed, PreIncorporationCostOwed{ContactID: id.String(), LegalName: v.names[id], Decided: moneyOut(o.decided),
					NeedsDecision: moneyOut(o.waiting), Total: moneyOut(sum)})
			}
			out.Body.Total = moneyOut(total)
			return nil
		})
		return &out, err
	})

	register(a, authz.GetPreIncorporationCost, huma.Operation{
		OperationID: "getPreIncorporationCost", Method: http.MethodGet, Path: "/v1/pre-incorporation-costs/{cost_id}", Tags: []string{"Founders"},
		Summary: "Get a cost paid before the books start",
		Description: "Returns a cost with its ETag: its person, amount and GST, its receipts, its statuses, and once decided the " +
			"accountant's decision and the lines its entry wrote (FR-EQ-03). Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *PreIncorporationCostPath) (*preCostReadOutput, error) {
		id, err := parseID("cost_id", in.CostID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out preCostReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := readablePreCost(ctx, u, p, id)
			if err != nil {
				return err
			}
			v, err := a.preCostView(ctx, u)
			if err != nil {
				return err
			}
			out.ETag = preCostETag(doc)
			out.Body, err = a.preCostOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeRetry, storage.CodeLockTimeout}, PreCostCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (PreIncorporationCost, storage.Document, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return PreIncorporationCost{}, doc, err
		}
		v, err := a.preCostView(ctx, u)
		if err != nil {
			return PreIncorporationCost{}, doc, err
		}
		x, err := a.preCostOf(ctx, u, v, doc, true)
		return x, doc, err
	}
	answer := func(w written[PreIncorporationCost]) *preCostOutput {
		if w.Previewed {
			return &preCostOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status == "decided" {
			status = "posted"
		}
		return &preCostOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/pre-incorporation-costs/" + w.Body.ID,
			Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.RecordPreIncorporationCost, huma.Operation{
		OperationID: "recordPreIncorporationCost", Method: http.MethodPost, Path: "/v1/pre-incorporation-costs", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Record a cost paid before the books start",
		Description: "Records a cost a director or shareholder paid before the books start, with its original date, for the accountant's " +
			"decision (FR-EQ-03): it posts nothing until decidePreIncorporationCost. Attach its receipt with " +
			"addPreIncorporationCostEvidence; the decision needs one. The date is before the books start, the person a contact marked " +
			"director or shareholder, and the amount and GST in the base currency. Side effects: the cost, its number (PIC-), audit " +
			"events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writeCodes, func(ctx context.Context, in *recordPreCostInput) (*preCostOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PreIncorporationCost, error) {
			c, err := preCostIn(in.Body)
			if err != nil {
				return PreIncorporationCost{}, err
			}
			rec, err := a.preCosts.RecordIn(ctx, u, c)
			if err != nil {
				return PreIncorporationCost{}, err
			}
			x, _, err := shown(ctx, u, rec.Doc.ID)
			return x, err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ReplacePreIncorporationCost, huma.Operation{
		OperationID: "replacePreIncorporationCost", Method: http.MethodPut, Path: "/v1/pre-incorporation-costs/{cost_id}", Tags: []string{"Founders"},
		Summary: "Change a cost that needs a decision",
		Description: "Saves a new version of a cost that still needs the accountant's decision, checked as recording it is (FR-EQ-03). A " +
			"decided cost changes no more (FOUNDER-07): correct it with a journal to the person's account. Side effects: the new " +
			"version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodePreCostDone}, writeCodes...), func(ctx context.Context, in *replacePreCostInput) (*preCostOutput, error) {
		id, err := parseID("cost_id", in.CostID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PreIncorporationCost, error) {
			doc, _, err := journal.Undecided(ctx, u, id)
			if err != nil {
				return PreIncorporationCost{}, err
			}
			if err := checkIfMatch(in.IfMatch, preCostETag(doc)); err != nil {
				return PreIncorporationCost{}, err
			}
			c, err := preCostIn(in.Body)
			if err != nil {
				return PreIncorporationCost{}, err
			}
			if _, err := a.preCosts.ReviseIn(ctx, u, id, c); err != nil {
				return PreIncorporationCost{}, err
			}
			x, _, err := shown(ctx, u, id)
			return x, err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.DecidePreIncorporationCost, huma.Operation{
		OperationID: "decidePreIncorporationCost", Method: http.MethodPost, Path: "/v1/pre-incorporation-costs/{cost_id}/decide", Tags: []string{"Founders"},
		Summary: "Decide a cost paid before the books start, and post it",
		Description: "Records the accountant's decision on a cost (FR-EQ-03): its income-tax treatment, the account it posts to, and " +
			"whether its GST is claimed (only while GST-registered on the posting date). The decision posts it at once under " +
			"pre_incorporation_cost@1: Dr the account with the amount (less the GST when claimed), Dr 2200 with the GST when claimed, " +
			"Cr 2500 Loan from director / shareholder naming the person who paid it (paid personally), on the books start, or on today's " +
			"date when the books start's period is closed. The cost needs a receipt (FOUNDER-08). The person who paid it never decides it " +
			"(FOUNDER-10). Side effects: the cost's new version with the decision, its entry, audit events. Needs If-Match with its ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview posts nothing and shows the entry.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodePreCostDone, director.CodeNoReceipt, director.CodeTreatment, CodeOwnCost,
		ledger.CodePreIncorporationCost, ledger.CodeParty, ledger.CodeSystemRole}, append(writeCodes, postingCodes()...)...),
		func(ctx context.Context, in *decidePreCostInput) (*preCostOutput, error) {
			id, err := parseID("cost_id", in.CostID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			account, err := uuid.Parse(in.Body.AccountID)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.account_id", Message: "is not an ID", Fix: "Send the account's ID"}})
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PreIncorporationCost, error) {
				doc, c, err := journal.Undecided(ctx, u, id)
				if err != nil {
					return PreIncorporationCost{}, err
				}
				if err := checkIfMatch(in.IfMatch, preCostETag(doc)); err != nil {
					return PreIncorporationCost{}, err
				}
				if paid, err := u.Contacts().Get(ctx, c.ContactID); err == nil && paid.MemberUserID != nil && *paid.MemberUserID == p.UserID {
					return PreIncorporationCost{}, problem(CodeOwnCost, "You paid this cost, so another person decides it.",
						"Ask the accountant to decide it.")
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return PreIncorporationCost{}, err
				}
				d := director.Decision{Treatment: in.Body.Treatment, AccountID: account, ClaimGST: in.Body.ClaimGST, Note: strings.TrimSpace(in.Body.Note)}
				if _, err := a.preCosts.DecideIn(ctx, u, id, d, uploader(ctx), today); err != nil {
					return PreIncorporationCost{}, err
				}
				x, _, err := shown(ctx, u, id)
				return x, err
			})
			if err != nil {
				return nil, err
			}
			return answer(w), nil
		})

	registerPreCostEvidence(a)
}
