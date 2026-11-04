package api

import (
	"context"
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
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ directors' fees (FR-EQ-04, §10.2; p1-founders-money)
//
// A director's fee the shareholders voted is recorded by an Owner or the Controller (A on "Director accounts") with the
// shareholders' resolution as its evidence, and posts only on another person's approval (director_fee.post), never the
// director it is paid to (FM-9's reading for fees) nor whoever asked: Dr 6050 Directors' fees · Cr 2120 Deferred
// remuneration payable naming the director, on the day they became entitled to it. The database refuses any other post
// (FY252).

// FeeCodes are the codes a fee's own checks answer.
var FeeCodes = []string{director.CodeFee, director.CodeNotPerson, director.CodeNotFee}

// DirectorFeeDraft is a director's fee as recorded.
type DirectorFeeDraft struct {
	ContactID  string `json:"contact_id" format:"uuid" doc:"The director: a contact marked director." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	EntitledOn string `json:"entitled_on" format:"date" doc:"The day the director became entitled to it: the general meeting that voted it. It posts on this day." example:"2026-11-30"`
	PeriodFrom string `json:"period_from" format:"date" doc:"The first day of the period it is for." example:"2026-01-01"`
	PeriodTo   string `json:"period_to" format:"date" doc:"The last day of that period." example:"2026-12-31"`
	Amount     Money  `json:"amount" doc:"The fee, in the base currency."`
	AccountID  string `json:"account_id,omitempty" format:"uuid" doc:"The account it is a cost on; left out, the chart's 6050 Directors' fees." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	Memo       string `json:"memo,omitempty" maxLength:"500" doc:"What it is, in a few words." example:"Directors' fee 2026, voted at the AGM"`
}

// DirectorFee is a director's fee, drafted, waiting for its approval, or posted.
type DirectorFee struct {
	ID            string      `json:"id" format:"uuid" doc:"Its ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	Number        string      `json:"number,omitempty" doc:"Its number, once posted (FR-ORG-04)." example:"DF-0001"`
	Status        string      `json:"status" enum:"draft,waiting,posted" doc:"draft; waiting for another person's approval; or posted to the director's account on 2120." example:"waiting"`
	Version       int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	ContactID     string      `json:"contact_id" format:"uuid" doc:"The director." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	LegalName     string      `json:"legal_name" doc:"Their name." example:"Mei Lin Tan"`
	EntitledOn    string      `json:"entitled_on" format:"date" doc:"The day they became entitled to it, which it posts on." example:"2026-11-30"`
	PeriodFrom    string      `json:"period_from" format:"date" doc:"The first day of the period it is for." example:"2026-01-01"`
	PeriodTo      string      `json:"period_to" format:"date" doc:"The last day of that period." example:"2026-12-31"`
	Amount        Money       `json:"amount" doc:"The fee."`
	AccountID     string      `json:"account_id" format:"uuid" doc:"The account it is a cost on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	AccountCode   string      `json:"account_code,omitempty" doc:"That account's code." example:"6050"`
	AccountName   string      `json:"account_name,omitempty" doc:"That account's name." example:"Directors' fees"`
	Memo          string      `json:"memo,omitempty" doc:"Its memo." example:"Directors' fee 2026, voted at the AGM"`
	Resolutions   int         `json:"resolutions" doc:"How many files are attached and not removed: asking for approval needs one, the shareholders' resolution." example:"1"`
	ApprovalAsked string      `json:"approval_asked,omitempty" format:"uuid" doc:"The pending request to post it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd1"`
	Checks        []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no shareholders' resolution is attached."`
	LedgerImpact  []EntryLine `json:"ledger_impact" nullable:"false" doc:"getDirectorFee only, once posted: the lines its entry wrote, debits positive."`
}

// DirectorFeePage is a page of fees, the latest entitlement first.
type DirectorFeePage struct {
	Items      []DirectorFee `json:"items" nullable:"false"`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMzB8MDE5MiJ9"`
}

// DirectorFeePath names a fee in the path.
type DirectorFeePath struct {
	FeeID string `path:"fee_id" format:"uuid" doc:"The fee's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
}

type feeListInput struct {
	PageQuery
	ContactID string `query:"contact_id" format:"uuid" doc:"Only this director's fees." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
}

type feeListOutput struct{ Body DirectorFeePage }

type createFeeInput struct {
	KeyHeader
	PreferHeader
	Body DirectorFeeDraft
}

type replaceFeeInput struct {
	DirectorFeePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body DirectorFeeDraft
}

type feeCommandInput struct {
	DirectorFeePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type feeOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
	Location string `header:"Location" doc:"Its URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DirectorFee
}

type feeReadOutput struct {
	ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
	Body DirectorFee
}

type feeRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

func feeETag(doc storage.Document) string { return etag(doc.Version, string(doc.Status)) }

// feesAccount is the chart's 6050 Directors' fees, the account a fee is a cost on by default; uuid.Nil when the chart
// has none.
func feesAccount(ctx context.Context, u storage.UnitOfWork) (uuid.UUID, error) {
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, a := range all {
		if a.Code == director.FeesAccountCode && !a.Group {
			return a.ID, nil
		}
	}
	return uuid.Nil, nil
}

// feeIn reads a fee as sent, with every invalid field at once; the account defaults to 6050.
func feeIn(ctx context.Context, u storage.UnitOfWork, d DirectorFeeDraft) (director.Fee, error) {
	f := director.Fee{Memo: strings.TrimSpace(d.Memo)}
	var fields []FieldProblem
	var err error
	if f.ContactID, err = uuid.Parse(d.ContactID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.contact_id", Message: "is not an ID", Fix: "Send the director's contact ID"})
	}
	for _, x := range []struct {
		text, field string
		into        *time.Time
	}{{d.EntitledOn, "body.entitled_on", &f.EntitledOn}, {d.PeriodFrom, "body.period_from", &f.PeriodFrom}, {d.PeriodTo, "body.period_to", &f.PeriodTo}} {
		if *x.into, err = time.Parse(time.DateOnly, x.text); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
	}
	m, bad := moneyIn(d.Amount, "body.amount")
	if bad != nil {
		fields = append(fields, *bad)
	}
	f.Amount = m
	switch d.AccountID {
	case "":
		if f.AccountID, err = feesAccount(ctx, u); err != nil {
			return f, err
		}
	default:
		if f.AccountID, err = uuid.Parse(d.AccountID); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.account_id", Message: "is not an ID", Fix: "Send the account's ID, or leave it out for 6050"})
		}
	}
	if len(fields) > 0 {
		return f, invalid(fields)
	}
	return f, nil
}

// feeView is what showing fees needs once: the directors' names, the chart, and each fee's pending request to post it.
type feeView struct {
	names    map[uuid.UUID]string
	accounts map[uuid.UUID]storage.Account
	asked    map[string]string
}

func (a *API) feeView(ctx context.Context, u storage.UnitOfWork) (feeView, error) {
	v := feeView{accounts: map[uuid.UUID]storage.Account{}, asked: map[string]string{}}
	var err error
	if v.names, err = directorNames(ctx, u); err != nil {
		return v, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.accounts[x.ID] = x
	}
	requests, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return v, err
	}
	now := a.deps.Clock.Now()
	for _, r := range requests {
		if r.ObjectType == approval.ObjectDirectorFee && approval.StatusAt(requestOf(r), now) == approval.Pending {
			v.asked[r.ObjectKey] = r.ID.String()
		}
	}
	return v, nil
}

// feeOf is a fee as the API shows it; withDetail fills a posted fee's ledger impact (getDirectorFee).
func (a *API) feeOf(ctx context.Context, u storage.UnitOfWork, v feeView, doc storage.Document, withDetail bool) (DirectorFee, error) {
	f, err := director.ReadFee(doc)
	if err != nil {
		return DirectorFee{}, err
	}
	acc := v.accounts[f.AccountID]
	out := DirectorFee{ID: doc.ID.String(), Number: doc.Number, Status: "draft", Version: doc.Version, ContactID: f.ContactID.String(),
		LegalName: v.names[f.ContactID], EntitledOn: dayText(f.EntitledOn), PeriodFrom: dayText(f.PeriodFrom), PeriodTo: dayText(f.PeriodTo),
		Amount: moneyOut(f.Amount), AccountID: f.AccountID.String(), AccountCode: acc.Code, AccountName: acc.Name, Memo: f.Memo,
		ApprovalAsked: v.asked[doc.ID.String()], Checks: []BillCheck{}, LedgerImpact: []EntryLine{}}
	if out.Resolutions, err = director.Attached(ctx, u, doc.ID); err != nil {
		return DirectorFee{}, err
	}
	switch {
	case doc.Status == storage.DocumentPosted:
		out.Status = "posted"
		if withDetail {
			e, err := u.Ledger().ByIdentity(ctx, ledger.DirectorFeeIdentity(doc.ID, doc.Version, ledger.RuleDirectorFee))
			if err != nil {
				return DirectorFee{}, err
			}
			out.LedgerImpact = entryOf(e).Lines
		}
		return out, nil
	case out.ApprovalAsked != "":
		out.Status = "waiting"
	}
	if out.Resolutions == 0 {
		out.Checks = append(out.Checks, BillCheck{Severity: "error", Code: director.CodeNoVote, Field: "resolutions",
			Message: "it has no shareholders' resolution attached, so it cannot be sent for approval", Fix: "Attach the resolution or the minutes of the meeting that voted it"})
	}
	return out, nil
}

// readableFee is a fee the caller may read: not found for one whose right on "Director accounts" is qualified.
func readableFee(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Document, error) {
	if !authz.Decide(authz.GetDirectorFee, p.Roles, p.AI()).Allowed {
		return storage.Document{}, ports.ErrNotFound
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return doc, err
	}
	if !director.IsFee(doc) {
		return doc, ports.ErrNotFound
	}
	return doc, nil
}

// feeDependencies are what a fee's envelope depends on: its version and status, and its amount.
func feeDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	f, err := director.ReadFee(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "amount": f.Amount.String() + " " + f.Amount.Currency().Code}, nil
}

// feeExecutable refuses the director a fee is paid to: they never approve their own fee (FM-9's reading for fees).
func feeExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	f, err := director.ReadFee(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, f.ContactID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(director.CodeOwnFee, "The fee is paid to you, so another person approves it (FM-9).",
			"Ask another person who holds the approval right on director accounts")
	}
	return nil
}

func registerFees(a *API) {
	readCodes := []string{CodeNotFound, director.CodeNotFee, director.CodeFee}

	register(a, authz.ListDirectorFees, huma.Operation{
		OperationID: "listDirectorFees", Method: http.MethodGet, Path: "/v1/director-fees", Tags: []string{"Founders"},
		Summary: "List directors' fees",
		Description: "The directors' fees the shareholders voted, drafted, waiting for approval and posted, the latest entitlement first, a " +
			"page at a time, narrowed by director (FR-EQ-04). Needs the right to read every director's account. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeInvalid}, readCodes...), func(ctx context.Context, in *feeListInput) (*feeListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		afterDate, afterID, err := journalCursor(after)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := feeListOutput{Body: DirectorFeePage{Items: []DirectorFee{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.feeView(ctx, u)
			if err != nil {
				return err
			}
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorFee, AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if len(out.Body.Items) == in.PageSize {
						last := out.Body.Items[len(out.Body.Items)-1]
						out.Body.NextCursor = encodeCursor(last.EntitledOn + "|" + last.ID)
						return nil
					}
					x, err := a.feeOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					if in.ContactID != "" && x.ContactID != in.ContactID {
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

	register(a, authz.GetDirectorFee, huma.Operation{
		OperationID: "getDirectorFee", Method: http.MethodGet, Path: "/v1/director-fees/{fee_id}", Tags: []string{"Founders"},
		Summary: "Get a director's fee",
		Description: "Returns a fee with its ETag: the director, the day they became entitled to it, the period, the amount and account, its " +
			"resolutions, the pending request to post it, and once posted the lines its entry wrote (FR-EQ-04). Read-only; no side effects.",
	}, readCodes, func(ctx context.Context, in *DirectorFeePath) (*feeReadOutput, error) {
		id, err := parseID("fee_id", in.FeeID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out feeReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := readableFee(ctx, u, p, id)
			if err != nil {
				return err
			}
			v, err := a.feeView(ctx, u)
			if err != nil {
				return err
			}
			out.ETag = feeETag(doc)
			out.Body, err = a.feeOf(ctx, u, v, doc, true)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &out, nil
	})

	writeCodes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency, storage.CodeRetry, storage.CodeLockTimeout}, FeeCodes...)
	shown := func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (DirectorFee, error) {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return DirectorFee{}, err
		}
		v, err := a.feeView(ctx, u)
		if err != nil {
			return DirectorFee{}, err
		}
		return a.feeOf(ctx, u, v, doc, false)
	}
	answer := func(w written[DirectorFee]) *feeOutput {
		if w.Previewed {
			return &feeOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		status := "draft"
		if w.Body.Status == "posted" {
			status = "posted"
		}
		return &feeOutput{Status: w.Status, ETag: etag(w.Body.Version, status), Location: "/v1/director-fees/" + w.Body.ID, Replayed: replayedHeader(w.Replayed),
			Body: w.Body}
	}

	register(a, authz.CreateDirectorFee, huma.Operation{
		OperationID: "createDirectorFee", Method: http.MethodPost, Path: "/v1/director-fees", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Record a director's fee",
		Description: "Records, as a draft, a director's fee the shareholders voted (FR-EQ-04): the director, the day they became entitled to " +
			"it, the period it is for, the amount, and the account (6050 Directors' fees when left out). Attach the shareholders' " +
			"resolution with addDirectorFeeEvidence, then ask for approval with requestDirectorFeePost. Side effects: the draft, audit " +
			"events. Every problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writeCodes, func(ctx context.Context, in *createFeeInput) (*feeOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DirectorFee, error) {
			f, err := feeIn(ctx, u, in.Body)
			if err != nil {
				return DirectorFee{}, err
			}
			doc, err := a.fees.CreateIn(ctx, u, f)
			if err != nil {
				return DirectorFee{}, err
			}
			return shown(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.ReplaceDirectorFee, huma.Operation{
		OperationID: "replaceDirectorFee", Method: http.MethodPut, Path: "/v1/director-fees/{fee_id}", Tags: []string{"Founders"},
		Summary: "Change a drafted director's fee",
		Description: "Saves a new version of a fee not posted yet, checked as recording it is (FR-EQ-04); a pending request to post it then " +
			"no longer matches, and nothing executes until it is asked for again. A posted fee changes no more (FOUNDER-18). Side " +
			"effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeFeePosted}, writeCodes...), func(ctx context.Context, in *replaceFeeInput) (*feeOutput, error) {
		id, err := parseID("fee_id", in.FeeID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DirectorFee, error) {
			doc, _, err := journal.DraftFee(ctx, u, id)
			if err != nil {
				return DirectorFee{}, err
			}
			if err := checkIfMatch(in.IfMatch, feeETag(doc)); err != nil {
				return DirectorFee{}, err
			}
			f, err := feeIn(ctx, u, in.Body)
			if err != nil {
				return DirectorFee{}, err
			}
			if _, err := a.fees.ReviseIn(ctx, u, id, f); err != nil {
				return DirectorFee{}, err
			}
			return shown(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.RequestDirectorFeePost, huma.Operation{
		OperationID: "requestDirectorFeePost", Method: http.MethodPost, Path: "/v1/director-fees/{fee_id}/post-approval", Tags: []string{"Founders"},
		Summary: "Ask for approval to post a director's fee", DefaultStatus: http.StatusAccepted,
		Description: "Asks another person to approve a drafted fee at the version read (FR-EQ-04, FR-APR-01): a fee posts only this way, in " +
			"every team mode. It needs the shareholders' resolution attached (FOUNDER-17) and must pass the posting rule. The answer is " +
			"202 with an approval request, which a person who holds A on \"Director accounts, settlements, share issues\" approves, " +
			"stepped up: never the one who asked, nor the director the fee is paid to (FOUNDER-19, FM-9; the database's FY252). On the " +
			"approval it posts director_fee@1 as the approver, numbered DF-: Dr 6050 Directors' fees, Cr 2120 Deferred remuneration " +
			"payable naming the director, on the day they became entitled to it. The request holds the fee's version, status and " +
			"amount, so a change voids it. Needs If-Match with the fee's ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, director.CodeFeePosted, director.CodeNoVote, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout, ledger.CodeDirectorFee,
		ledger.CodeParty, ledger.CodeSystemRole, ledger.CodeUnknownAccount, ledger.CodeNoDate}, FeeCodes...),
		func(ctx context.Context, in *feeCommandInput) (*feeRequestOutput, error) {
			id, err := parseID("fee_id", in.FeeID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, f, err := journal.DraftFee(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, feeETag(doc)); err != nil {
					return Approval{}, err
				}
				if _, err := a.fees.Check(ctx, u, doc); err != nil {
					return Approval{}, err
				}
				names, err := directorNames(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				amount := f.Amount.String() + " " + f.Amount.Currency().Code
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectDirectorFee, ObjectKey: doc.ID.String(), Operation: approval.OpDirectorFeePost,
					Summary: fmt.Sprintf("Post %s's director's fee of %s, entitled on %s", names[f.ContactID], amount, dayText(f.EntitledOn)),
					Execute: map[string]any{"fee_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "contact_id": f.ContactID.String()},
					Diff:    []ApprovalDiff{{Field: "status", Before: "draft", After: "posted"}, {Field: "amount", After: amount}},
					Amounts: []approval.Amount{{Amount: f.Amount.String(), Currency: f.Amount.Currency().Code}}})
			})
			if err != nil {
				return nil, err
			}
			return &feeRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	registerFeeEvidence(a)
}
