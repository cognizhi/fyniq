package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ opening balances (FR-GL-06)

// OpeningLine is one opening balance: an account and its balance at the books start, in the base currency.
type OpeningLine struct {
	AccountID   string `json:"account_id" format:"uuid" doc:"The account: a leaf account, active. Share capital may take a line (INV-13)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Amount      Money  `json:"amount" doc:"The balance in the base currency: a debit balance positive, a credit balance negative."`
	Description string `json:"description,omitempty" maxLength:"500" doc:"What the balance is." example:"DBS current account at the books start"`
}

// OpeningDraft is the opening balances as entered.
type OpeningDraft struct {
	Memo  string        `json:"memo,omitempty" maxLength:"1000" doc:"A note on where the balances come from." example:"Balances from the 31 March 2026 trial balance"`
	Lines []OpeningLine `json:"lines" maxItems:"1000" doc:"One line per account with a balance; together they net to zero."`
}

// OpeningBalances are the organisation's opening balances: none yet, a draft, or posted.
type OpeningBalances struct {
	ID         string        `json:"id,omitempty" format:"uuid" doc:"The document's ID, once saved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d72"`
	Status     string        `json:"status" enum:"none,draft,posted,reversed" doc:"none until they are first saved; then a draft (✎) until posted." example:"draft"`
	Version    int           `json:"version" doc:"The version of their content; 0 while none is saved." example:"2"`
	Date       string        `json:"date,omitempty" format:"date" doc:"The books start: the date they post on." example:"2026-04-01"`
	Memo       string        `json:"memo,omitempty" doc:"Where the balances come from." example:"Balances from the 31 March 2026 trial balance"`
	Lines      []OpeningLine `json:"lines" nullable:"false" doc:"The balances as entered."`
	PreparedBy *Person       `json:"prepared_by,omitempty" doc:"Who saved the current version."`
	PostedBy   *Person       `json:"posted_by,omitempty" doc:"Who posted them."`
	PostedAt   *time.Time    `json:"posted_at,omitempty" doc:"When they were posted." example:"2026-10-21T09:30:00Z"`
}

// OpeningPosting is the opening balances after posting, with the entry it made.
type OpeningPosting struct {
	OpeningBalances OpeningBalances `json:"opening_balances"`
	Entry           Entry           `json:"entry"`
}

type openingReadOutput struct {
	ETag string `header:"ETag" doc:"Their version, for If-Match; \"0-none\" while none is saved."`
	Body OpeningBalances
}

type replaceOpeningInput struct {
	IfMatchHeader
	PreferHeader
	Body OpeningDraft
}

type replaceOpeningOutput struct {
	ETag    string `header:"ETag" doc:"The new version, for If-Match."`
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    OpeningBalances
}

type postOpeningInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type postOpeningOutput struct {
	ETag     string `header:"ETag" doc:"Their version after posting, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved, and the entry shows what posting would write (FR-INT-14)."`
	Body     OpeningPosting
}

func openingOf(doc storage.Document) (OpeningBalances, error) {
	saved, err := journal.ReadOpening(doc)
	if err != nil {
		return OpeningBalances{}, err
	}
	out := OpeningBalances{ID: doc.ID.String(), Status: string(doc.Status), Version: doc.Version, Date: saved.Date, Memo: saved.Memo,
		Lines: make([]OpeningLine, 0, len(saved.Lines)), PreparedBy: personOf(doc.SavedBy), PostedBy: personOf(doc.PostedBy), PostedAt: utc(doc.PostedAt)}
	for _, l := range saved.Lines {
		out.Lines = append(out.Lines, OpeningLine{AccountID: l.AccountID.String(), Amount: Money{Amount: l.Amount.String(), Currency: l.Amount.Currency().Code},
			Description: l.Description})
	}
	return out, nil
}

func openingETag(o OpeningBalances) string { return etag(o.Version, o.Status) }

// currentOpening reads the opening balances, or none, with their people named.
func (a *API) currentOpening(ctx context.Context, u storage.UnitOfWork, lock bool) (OpeningBalances, error) {
	doc, err := u.Documents().OpeningBalance(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return OpeningBalances{Status: "none", Lines: []OpeningLine{}}, nil
	}
	if err != nil {
		return OpeningBalances{}, err
	}
	if lock {
		if doc, err = u.Documents().Lock(ctx, doc.ID); err != nil {
			return OpeningBalances{}, err
		}
	}
	o, err := openingOf(doc)
	if err != nil {
		return OpeningBalances{}, err
	}
	return o, a.name(ctx, u, o.PreparedBy, o.PostedBy)
}

// openingLines reads the lines from the request, with every invalid field at once.
func openingLines(in OpeningDraft, base money.Currency) ([]ledger.JournalLine, error) {
	var fields []FieldProblem
	out := make([]ledger.JournalLine, 0, len(in.Lines))
	for i, l := range in.Lines {
		field := func(name string) string { return "body.lines[" + strconv.Itoa(i) + "]." + name }
		id, err := uuid.Parse(l.AccountID)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: field("account_id"), Message: "is not an account ID", Fix: "Choose an account from the chart of accounts"})
		}
		cur, err := money.Lookup(l.Amount.Currency)
		if err != nil {
			fields = append(fields, FieldProblem{Code: money.CodeCurrency, Field: field("amount.currency"), Message: "is not a currency Fyniq knows", Fix: "Enter a three-letter ISO 4217 code such as " + base.Code})
			continue
		}
		amount, err := money.ParseField(l.Amount.Amount, cur, field("amount.amount"))
		if err != nil {
			var me *money.Error
			if errors.As(err, &me) {
				fields = append(fields, FieldProblem{Code: me.Code, Field: me.Field, Message: me.Message, Fix: me.Fix})
				continue
			}
			return nil, err
		}
		out = append(out, ledger.JournalLine{AccountID: id, Amount: amount, Description: strings.TrimSpace(l.Description)})
	}
	if len(fields) > 0 {
		return nil, invalid(fields)
	}
	return out, nil
}

func registerOpening(a *API) {
	register(a, authz.GetOpeningBalances, huma.Operation{
		OperationID: "getOpeningBalances", Method: http.MethodGet, Path: "/v1/opening-balances", Tags: []string{"Journals"},
		Summary: "Get the opening balances",
		Description: "The organisation's opening balances at the books start (FR-GL-06): none until first saved, then a draft until " +
			"posted, with who prepared and posted them and their ETag (\"0-none\" while none is saved). Read-only; no side effects.",
	}, []string{journal.CodeNotOpening, journal.CodeBadDraft}, func(ctx context.Context, _ *struct{}) (*openingReadOutput, error) {
		p, _ := authn.From(ctx)
		var out openingReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out.Body, err = a.currentOpening(ctx, u, false)
			out.ETag = openingETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceOpeningBalances, huma.Operation{
		OperationID: "replaceOpeningBalances", Method: http.MethodPut, Path: "/v1/opening-balances", Tags: []string{"Journals"},
		Summary: "Save the opening balances as a draft",
		Description: "Saves the opening balances as a draft dated at the books start (FR-GL-06; PRD-001 rev 0.49, provisional): the " +
			"first save creates them, each later one their next version, recording who saved it. Each line is an account and its " +
			"balance in the base currency, a debit balance positive and a credit balance negative; share capital may take a line " +
			"(INV-13). Like a journal's draft, they are checked only for their amounts; posting checks the rest. Only a draft " +
			"changes. Side effects: the document or its new version, and the audit event. Needs If-Match with their ETag " +
			"(\"0-none\" before the first save). Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, journal.CodeNotOpening, journal.CodeNotDraft,
		journal.CodeBadDraft, journal.CodeNoBooksStart, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency,
		storage.CodeDuplicate, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *replaceOpeningInput) (*replaceOpeningOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OpeningBalances, error) {
				before, err := a.currentOpening(ctx, u, true)
				if err != nil {
					return OpeningBalances{}, err
				}
				if err := checkIfMatch(in.IfMatch, openingETag(before)); err != nil {
					return OpeningBalances{}, err
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return OpeningBalances{}, err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return OpeningBalances{}, err
				}
				lines, err := openingLines(in.Body, base)
				if err != nil {
					return OpeningBalances{}, err
				}
				if _, err := a.journals.SaveOpening(ctx, p.Organisation, strings.TrimSpace(in.Body.Memo), lines); err != nil {
					return OpeningBalances{}, err
				}
				return a.currentOpening(ctx, u, false)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &replaceOpeningOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &replaceOpeningOutput{ETag: openingETag(w.Body), Body: w.Body}, nil
		})

	register(a, authz.PostOpeningBalances, huma.Operation{
		OperationID: "postOpeningBalances", Method: http.MethodPost, Path: "/v1/opening-balances/post", Tags: []string{"Journals"},
		Summary: "Post the opening balances",
		Description: "Posts the opening balances' current version as entered at the books start, once they net to zero (FR-GL-06, " +
			"posting rule \"Opening balances\"): every line in the base currency, share capital allowed (INV-13), the rest of " +
			"ENG-24 as for a journal. Side effects: the entry, a new ledger revision, balances, audit events. Needs If-Match with " +
			"their ETag. Posted opening balances do not change until edits with a second person's approval arrive (FM-1). " +
			"Idempotent with Idempotency-Key; Prefer: return=preview shows the entry and saves nothing.",
	}, append([]string{journal.CodeNotOpening, journal.CodeNotDraft, journal.CodeNoBooksStart, ledger.CodeForeignOpening}, postingCodes()...),
		func(ctx context.Context, in *postOpeningInput) (*postOpeningOutput, error) {
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OpeningPosting, error) {
				before, err := a.currentOpening(ctx, u, true)
				if err != nil {
					return OpeningPosting{}, err
				}
				if before.Status == "none" {
					return OpeningPosting{}, problem(CodeNotFound, "No opening balances are saved yet.", "Save them as a draft first.")
				}
				if err := checkIfMatch(in.IfMatch, openingETag(before)); err != nil {
					return OpeningPosting{}, err
				}
				if err := checkConflicts(ctx, u, p, "Posting"); err != nil {
					return OpeningPosting{}, err
				}
				entry, err := a.journals.PostOpening(ctx, p.Organisation)
				if err != nil {
					return OpeningPosting{}, err
				}
				after, err := a.currentOpening(ctx, u, false)
				return OpeningPosting{OpeningBalances: after, Entry: entryOf(entry)}, err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &postOpeningOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &postOpeningOutput{ETag: openingETag(w.Body.OpeningBalances), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})
}
