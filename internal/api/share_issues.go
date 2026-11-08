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
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the register of share issues (FR-EQ-08; p1-founders-money)
//
// Every allotment of shares is in the register (design D11), and share capital moves only with it or the opening
// balances (INV-13; the database's FY007 and FY255). Shares issued before the books start, such as the incorporation
// subscription, are recorded here without an entry, by an Owner or the Controller stepped up, since the opening balances
// hold their share capital; a conversion of a founder's balance posts its issue from the register when it becomes
// effective.

// ShareIssue is one allotment in the register.
type ShareIssue struct {
	ID             string `json:"id" format:"uuid" doc:"The issue's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f10"`
	HolderID       string `json:"holder_id" format:"uuid" doc:"The contact the shares were allotted to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName     string `json:"holder_name" doc:"Their name." example:"Mei Lin Tan"`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The class: its share-capital account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassCode      string `json:"class_code,omitempty" doc:"The account's code: 3010 ordinary, 3020 preference." example:"3010"`
	ClassID        string `json:"class_id,omitempty" format:"uuid" doc:"Its share class (FR-EQ-16), once its account has a recorded class." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Shares         string `json:"shares" pattern:"^[1-9][0-9]*$" doc:"The number of shares, a whole number." example:"10000"`
	Price          string `json:"price" doc:"The price per share, in the currency, as a decimal string." example:"1.00"`
	Amount         Money  `json:"amount" doc:"What the shares are paid up with: shares × price for cash, RND-SHARE's converted amount for a conversion."`
	Consideration  string `json:"consideration" enum:"cash,debt_capitalised,services,safe_or_note" doc:"What paid for them." example:"cash"`
	ResolutionRefs string `json:"resolution_refs" doc:"The shareholders' approval and the board resolution, by reference." example:"Incorporation subscription, 2 Jan 2025"`
	LodgementRef   string `json:"lodgement_ref,omitempty" doc:"ACRA's lodgement reference for the allotment." example:"AA2025010200123"`
	EffectiveOn    string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows the allotment." example:"2025-01-02"`
	BeforeBooks    bool   `json:"before_books" doc:"Issued before the books start, so recorded without an entry: the opening balances hold its share capital." example:"true"`
	SettlementID   string `json:"settlement_id,omitempty" format:"uuid" doc:"A conversion: the settlement it decides." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa9"`
	EntryID        string `json:"entry_id,omitempty" format:"uuid" doc:"The entry it posted, from the source share_issue." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f20"`
	RecordedAt     string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-11-03T09:30:00Z"`
}

// ShareIssueRequest is an issue made before the books start, as recorded.
type ShareIssueRequest struct {
	HolderID       string `json:"holder_id" format:"uuid" doc:"The contact the shares were allotted to: a director or shareholder." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The class: an active share-capital account (3010 ordinary or 3020 preference)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassID        string `json:"class_id,omitempty" format:"uuid" doc:"The share class, a recorded class of that account (FR-EQ-16). Left out: the account's first recorded class, if it has one." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e01"`
	Shares         string `json:"shares" doc:"The number of shares, a whole number above zero." example:"10000"`
	Price          string `json:"price" doc:"The price per share in the base currency, a decimal string above zero with at most 6 decimals." example:"1.00"`
	Consideration  string `json:"consideration" enum:"cash,debt_capitalised,services,safe_or_note" doc:"What paid for them." example:"cash"`
	ResolutionRefs string `json:"resolution_refs" maxLength:"500" doc:"The resolutions or subscription that allotted them, by reference." example:"Incorporation subscription, 2 Jan 2025"`
	LodgementRef   string `json:"lodgement_ref,omitempty" maxLength:"100" doc:"ACRA's lodgement reference, if any." example:"AA2025010200123"`
	EffectiveOn    string `json:"effective_on" format:"date" doc:"The day ACRA's register of members shows the allotment: before the books start." example:"2025-01-02"`
}

// ShareIssuePage is the register, earliest effective date first.
type ShareIssuePage struct {
	Items []ShareIssue `json:"items" nullable:"false" doc:"The issues on this page, earliest effective date first."`
	// NextCursor is the next page's cursor (FR-INT-10).
	NextCursor string `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

type shareIssueListInput struct {
	PageQuery
}

type shareIssueRecordInput struct {
	KeyHeader
	PreferHeader
	Body ShareIssueRequest
}

type shareIssueListOutput struct{ Body ShareIssuePage }

type shareIssueOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ShareIssue
}

// shareIssueOf is an issue as the API shows it: its amount at its currency's scale, its price without trailing zeros
// beyond two decimals.
func shareIssueOf(s storage.ShareIssue, holder string, classCode string) ShareIssue {
	amount := Money{Amount: string(s.Amount), Currency: s.Currency}
	if cur, err := money.Lookup(s.Currency); err == nil {
		if m, err := money.ParseStored(string(s.Amount), cur); err == nil {
			amount = moneyOut(m)
		}
	}
	out := ShareIssue{ID: s.ID.String(), HolderID: s.HolderID.String(), HolderName: holder, ClassAccountID: s.ClassAccountID.String(), ClassCode: classCode,
		Shares: string(s.Shares), Price: strings.TrimRight(strings.TrimRight(string(s.Price), "0"), "."), Amount: amount,
		Consideration: s.Consideration, ResolutionRefs: s.ResolutionRefs, LodgementRef: s.LodgementRef, EffectiveOn: dateOf(s.EffectiveOn),
		BeforeBooks: s.EntryID == nil, RecordedAt: s.RecordedAt.UTC().Format(time.RFC3339)}
	if whole, frac, _ := strings.Cut(out.Price, "."); len(frac) < 2 {
		out.Price = whole + "." + (frac + "00")[:2]
	}
	if s.SettlementID != nil {
		out.SettlementID = s.SettlementID.String()
	}
	if s.ClassID != nil {
		out.ClassID = s.ClassID.String()
	}
	if s.EntryID != nil {
		out.EntryID = s.EntryID.String()
	}
	return out
}

// shareIssuesOf is the register as the API shows it.
func shareIssuesOf(ctx context.Context, u storage.UnitOfWork) ([]ShareIssue, error) {
	all, err := u.ShareIssues().List(ctx)
	if err != nil {
		return nil, err
	}
	out := []ShareIssue{}
	for _, s := range all {
		c, err := u.Contacts().Get(ctx, s.HolderID)
		if err != nil {
			return nil, err
		}
		a, err := u.Accounts().Get(ctx, s.ClassAccountID)
		if err != nil {
			return nil, err
		}
		out = append(out, shareIssueOf(s, c.LegalName, a.Code))
	}
	return out, nil
}

func registerShareIssues(a *API) {
	register(a, authz.ListShareIssues, huma.Operation{
		OperationID: "listShareIssues", Method: http.MethodGet, Path: "/v1/share-issues", Tags: []string{"Founders"},
		Summary: "List the register of share issues",
		Description: "Every allotment of shares, earliest effective date first (FR-EQ-08): the holder, class, number, price, amount, " +
			"consideration, resolutions, ACRA's lodgement reference and effective date, and the entry it posted, if any. Share capital " +
			"moves only with the register or the opening balances (INV-13). A page at a time. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *shareIssueListInput) (*shareIssueListOutput, error) {
		p, _ := authn.From(ctx)
		var out shareIssueListOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			items, err := shareIssuesOf(ctx, u)
			if err != nil {
				return err
			}
			page, next, err := pageOf(items, func(x ShareIssue) string { return x.ID }, in.PageQuery)
			out.Body = ShareIssuePage{Items: page, NextCursor: next}
			if out.Body.Items == nil {
				out.Body.Items = []ShareIssue{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.RecordShareIssueBeforeBooks, huma.Operation{
		OperationID: "recordShareIssueBeforeBooks", Method: http.MethodPost, Path: "/v1/share-issues", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Record shares issued before the books start",
		Description: "Records in the register an allotment made before the books start, such as the incorporation subscription (FR-EQ-08, " +
			"design D11): the holder, a director or shareholder; the class, an active share-capital account; the whole number of shares; " +
			"the price per share in the base currency; the consideration; the resolutions; ACRA's lodgement reference; and the " +
			"effective date, before the books start. Its amount is shares × price under RND-LINE. It posts no entry: the opening " +
			"balances hold its share capital. Later allotments come from the register when a conversion becomes effective. Needs a " +
			"person signed in who passed step-up. Side effects: the issue, append-only, and its audit event. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{equity.CodeIssue, storage.CodeShareIssue, CodeSessionRequired, CodeStepUpRequired, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *shareIssueRecordInput) (*shareIssueOutput, error) {
			var fields []FieldProblem
			holder, err := uuid.Parse(in.Body.HolderID)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.holder_id", Message: "is not a contact's ID", Fix: "Send the shareholder's contact ID"})
			}
			class, err := uuid.Parse(in.Body.ClassAccountID)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.class_account_id", Message: "is not an account's ID", Fix: "Send the share-capital account's ID"})
			}
			on, err := time.Parse(time.DateOnly, in.Body.EffectiveOn)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.effective_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
			}
			var classID *uuid.UUID
			if in.Body.ClassID != "" {
				if id, err := uuid.Parse(in.Body.ClassID); err != nil {
					fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.class_id", Message: "is not a share class's ID", Fix: "Send a recorded class's ID, or leave it out"})
				} else {
					classID = &id
				}
			}
			if len(fields) > 0 {
				return nil, invalid(fields)
			}
			p, err := a.steppedUpPerson(ctx, "Recording shares issued before the books start")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ShareIssue, error) {
				settings, err := journal.LoadSettings(ctx, u)
				if err != nil {
					return ShareIssue{}, err
				}
				o, err := u.Organisation(ctx)
				if err != nil {
					return ShareIssue{}, err
				}
				c, err := equity.CheckBeforeBooks(ctx, u, equity.Issue{HolderID: holder, ClassAccountID: class, Shares: in.Body.Shares, Price: in.Body.Price,
					Consideration: in.Body.Consideration, ResolutionRefs: strings.TrimSpace(in.Body.ResolutionRefs), LodgementRef: strings.TrimSpace(in.Body.LodgementRef),
					EffectiveOn: on}, settings, o.Profile.BooksStart)
				if err != nil {
					return ShareIssue{}, err
				}
				if classID == nil { // the account's first recorded class, if it has one (FR-EQ-16, FY287)
					if classID, err = equity.DefaultClass(ctx, u, class); err != nil {
						return ShareIssue{}, err
					}
				}
				s, err := equity.RecordIssue(ctx, u, storage.ShareIssue{ID: uuid.Must(uuid.NewV7()), HolderID: holder, ClassAccountID: class, ClassID: classID,
					Shares: storage.Decimal(c.Shares.Text('f')), Price: storage.Decimal(c.Price.String()), Currency: c.Amount.Currency().Code,
					Amount: storage.Decimal(c.Amount.String()), Consideration: c.Issue.Consideration, ResolutionRefs: c.Issue.ResolutionRefs,
					LodgementRef: c.Issue.LodgementRef, EffectiveOn: on, RecordedBy: p.UserID, RecordedAt: a.deps.Clock.Now()}, nil)
				if err != nil {
					return ShareIssue{}, err
				}
				hc, err := u.Contacts().Get(ctx, holder)
				if err != nil {
					return ShareIssue{}, err
				}
				return shareIssueOf(s, hc.LegalName, settings.Accounts[class].Code), nil
			})
			if err != nil {
				return nil, err
			}
			return &shareIssueOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
