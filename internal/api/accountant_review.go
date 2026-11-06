package api

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accountantreview"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the accountant's review (FM-7, PRD-001 rev 0.176)
//
// A review covers one closed month: the close it reviews, with that close's snapshot (AUD-04), normally the quarter's
// last month. The Accountant opens it, raises queries and links adjusting journals (each posts through FR-GL-01
// approval; a closed month follows FR-GL-08); an Owner or the Controller answers each query, once; the Accountant signs
// it off, stepped up, once every query has its answer. Reopening the month supersedes it, and both are kept. In Founder
// mode a quarter is overdue 60 days after it ends without a signed-off review of its last month (C.2 #61).

// AccountantReview is a review as the API shows it.
type AccountantReview struct {
	ID            string              `json:"id" format:"uuid" doc:"The review's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f10"`
	Status        string              `json:"status" enum:"in_review,signed_off,superseded" doc:"in_review until the Accountant signs it off; superseded once its month reopens, kept as it was." example:"in_review"`
	Period        ReviewPeriod        `json:"period" doc:"The month it covers."`
	Close         ReviewClose         `json:"close" doc:"The close it reviews (AUD-04)."`
	OpenedBy      Person              `json:"opened_by" doc:"The Accountant who opened it."`
	OpenedAt      time.Time           `json:"opened_at" doc:"When." example:"2026-10-20T02:00:00Z"`
	Queries       []ReviewQueryItem   `json:"queries" nullable:"false" doc:"The Accountant's queries, oldest first, each with management's answer once given."`
	Unanswered    int                 `json:"unanswered" doc:"How many queries wait for an answer: it is signed off only at none." example:"1"`
	Journals      []ReviewJournalItem `json:"journals" nullable:"false" doc:"The adjusting journals it raised, oldest first. Each posts through approval (FR-GL-01); a closed month reopens for one first (FR-GL-08), which supersedes this review."`
	SignedBy      *Person             `json:"signed_by,omitempty" doc:"Who signed it off."`
	SignedAt      *time.Time          `json:"signed_at,omitempty" doc:"When." example:"2026-10-24T02:00:00Z"`
	Statement     string              `json:"statement,omitempty" doc:"What the Accountant signed off." example:"Reviewed the September close: no further adjustments"`
	SupersededAt  *time.Time          `json:"superseded_at,omitempty" doc:"When its month reopened." example:"2026-11-02T02:00:00Z"`
	ReopenID      string              `json:"reopen_id,omitempty" format:"uuid" doc:"The reopening that superseded it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f20"`
	YouMayAsk     bool                `json:"you_may_ask" doc:"true when you, the Accountant, may raise a query or link a journal now." example:"true"`
	YouMayAnswer  bool                `json:"you_may_answer" doc:"true when you, an Owner or the Controller, may answer its queries now." example:"false"`
	YouMaySignOff bool                `json:"you_may_sign_off" doc:"true when you, the Accountant, may sign it off now: every query has its answer." example:"false"`
	Version       int                 `json:"version" doc:"Its version: send it as If-Match with each change." example:"3"`
}

// ReviewPeriod is the month a review covers.
type ReviewPeriod struct {
	ID       string `json:"id" format:"uuid" doc:"The period's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Name     string `json:"name" doc:"The month." example:"September 2026"`
	StartsOn string `json:"starts_on" format:"date" doc:"Its first day." example:"2026-09-01"`
	EndsOn   string `json:"ends_on" format:"date" doc:"Its last day." example:"2026-09-30"`
}

// ReviewClose is the close a review covers.
type ReviewClose struct {
	ID             string    `json:"id" format:"uuid" doc:"The close's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f30"`
	AttestedAt     time.Time `json:"attested_at" doc:"When the month was attested and closed." example:"2026-10-12T02:00:00Z"`
	SnapshotSHA256 string    `json:"snapshot_sha256" doc:"The SHA-256 of the close's snapshot, the trial balance at the month end (AUD-04), in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// ReviewQueryItem is a query with its answer.
type ReviewQueryItem struct {
	ID         string     `json:"id" format:"uuid" doc:"The query's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f40"`
	AskedBy    Person     `json:"asked_by" doc:"The Accountant who asked."`
	AskedAt    time.Time  `json:"asked_at" doc:"When." example:"2026-10-20T03:00:00Z"`
	Question   string     `json:"question" doc:"The query." example:"Why is the September rent on 6010 twice?"`
	Answer     string     `json:"answer,omitempty" doc:"Management's answer, once given." example:"The October rent was paid early; the journal JNL-0214 moves it to prepayments"`
	AnsweredBy *Person    `json:"answered_by,omitempty" doc:"Who answered: an Owner or the Controller."`
	AnsweredAt *time.Time `json:"answered_at,omitempty" doc:"When." example:"2026-10-21T03:00:00Z"`
}

// ReviewJournalItem is an adjusting journal a review raised.
type ReviewJournalItem struct {
	JournalID string    `json:"journal_id" format:"uuid" doc:"The journal." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	Number    string    `json:"number,omitempty" doc:"Its number, once posted." example:"JNL-0214"`
	Status    string    `json:"status" doc:"Its status: draft until it posts, through approval when above the threshold." example:"posted"`
	Memo      string    `json:"memo,omitempty" doc:"Its memo." example:"Move the October rent paid early to prepayments"`
	LinkedBy  Person    `json:"linked_by" doc:"The Accountant who linked it."`
	LinkedAt  time.Time `json:"linked_at" doc:"When." example:"2026-10-21T04:00:00Z"`
}

// ReviewQuarter is a calendar quarter that ended, with the review that covers it (C.2 #61).
type ReviewQuarter struct {
	Key      string `json:"key" doc:"The quarter." example:"2026-Q3"`
	Label    string `json:"label" doc:"As people say it." example:"Jul–Sep 2026"`
	EndsOn   string `json:"ends_on" format:"date" doc:"Its last day." example:"2026-09-30"`
	DueBy    string `json:"due_by" format:"date" doc:"60 days after it ends: in Founder mode Checks warns after it while no signed-off review covers its last month." example:"2026-11-29"`
	Status   string `json:"status" enum:"reviewed,in_review,due,overdue" doc:"reviewed: a signed-off review covers its last month; in_review: one is open; due: none yet, within 60 days of its end; overdue: none after." example:"due"`
	PeriodID string `json:"period_id,omitempty" format:"uuid" doc:"Its last month's period; absent when the books have none." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	ReviewID string `json:"review_id,omitempty" format:"uuid" doc:"The review that covers it, or the one open." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f10"`
}

// AccountantReviewPage is the reviews, newest first, the quarters and the closed months not yet reviewed.
type AccountantReviewPage struct {
	Items      []AccountantReview `json:"items" doc:"Every review, newest first, superseded ones too."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
	Quarters   []ReviewQuarter    `json:"quarters" nullable:"false" doc:"The calendar quarters that ended since the books started, newest first, each with its review."`
	ToReview   []ReviewPeriod     `json:"to_review" nullable:"false" doc:"The closed months whose latest close has no review yet, newest first: what the Accountant may open a review of."`
	YouMayOpen bool               `json:"you_may_open" doc:"true when you, the Accountant, may open a review." example:"true"`
}

// ReviewQuestion raises a query.
type ReviewQuestion struct {
	Question string `json:"question" minLength:"1" maxLength:"2000" doc:"The query, for an Owner or the Controller to answer." example:"Why is the September rent on 6010 twice?"`
}

// ReviewAnswer answers a query.
type ReviewAnswer struct {
	Answer string `json:"answer" minLength:"1" maxLength:"2000" doc:"Management's answer, given once." example:"The October rent was paid early; the journal JNL-0214 moves it to prepayments"`
}

// ReviewJournalLink links an adjusting journal.
type ReviewJournalLink struct {
	JournalID string `json:"journal_id" format:"uuid" doc:"The journal the review raised: a draft, or one posted already." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
}

// ReviewSignOff signs a review off.
type ReviewSignOff struct {
	Statement string `json:"statement" minLength:"1" maxLength:"1000" doc:"What you sign off." example:"Reviewed the September close: no further adjustments"`
}

// AccountantReviewPath names a review.
type AccountantReviewPath struct {
	ReviewID string `path:"review_id" format:"uuid" doc:"The review's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f10"`
}

type reviewListInput struct{ PageQuery }

type reviewOpenInput struct {
	PeriodPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type reviewAskInput struct {
	AccountantReviewPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReviewQuestion
}

type reviewAnswerInput struct {
	AccountantReviewPath
	QueryID string `path:"query_id" format:"uuid" doc:"The query's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5f40"`
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReviewAnswer
}

type reviewLinkInput struct {
	AccountantReviewPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReviewJournalLink
}

type reviewSignInput struct {
	AccountantReviewPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReviewSignOff
}

type reviewListOutput struct{ Body AccountantReviewPage }

type reviewOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The review's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AccountantReview
}

// AccountantReviewCodes are the rules of the accountant's review.
func AccountantReviewCodes() []string {
	return []string{accountantreview.CodeNotClosed, accountantreview.CodeReviewed, accountantreview.CodeNotInReview, accountantreview.CodeAnswered,
		accountantreview.CodeNotJournal, accountantreview.CodeOpenQueries, storage.CodeAccountantReview}
}

// reviewOf is r as p sees it.
func reviewOf(ctx context.Context, u storage.UnitOfWork, p authn.Principal, r storage.AccountantReview, names map[uuid.UUID]string) (AccountantReview, error) {
	per, err := u.Periods().Get(ctx, r.PeriodID)
	if err != nil {
		return AccountantReview{}, err
	}
	user := func(id uuid.UUID) Person { return Person{Kind: "user", ID: id.String(), Name: names[id]} }
	out := AccountantReview{ID: r.ID.String(), Status: r.Status, Period: periodRef(per),
		Close:    ReviewClose{ID: r.CloseID.String(), AttestedAt: r.ClosedAt.UTC(), SnapshotSHA256: hex.EncodeToString(r.CloseHash)},
		OpenedBy: user(r.OpenedBy), OpenedAt: r.OpenedAt.UTC(), Queries: []ReviewQueryItem{}, Journals: []ReviewJournalItem{}, Statement: r.Statement,
		Version: r.Version}
	for _, q := range r.Queries {
		x := ReviewQueryItem{ID: q.ID.String(), AskedBy: user(q.AskedBy), AskedAt: q.AskedAt.UTC(), Question: q.Question, Answer: q.Answer}
		if q.AnsweredBy != nil {
			who, at := user(*q.AnsweredBy), q.AnsweredAt.UTC()
			x.AnsweredBy, x.AnsweredAt = &who, &at
		} else {
			out.Unanswered++
		}
		out.Queries = append(out.Queries, x)
	}
	for _, j := range r.Journals {
		doc, err := u.Documents().Get(ctx, j.JournalID)
		if err != nil {
			return AccountantReview{}, err
		}
		x := ReviewJournalItem{JournalID: j.JournalID.String(), Number: doc.Number, Status: string(doc.Status), LinkedBy: user(j.LinkedBy), LinkedAt: j.LinkedAt.UTC()}
		if jr, err := journalOf(doc); err == nil {
			x.Memo = jr.Memo
		}
		out.Journals = append(out.Journals, x)
	}
	if r.SignedBy != nil {
		who, at := user(*r.SignedBy), r.SignedAt.UTC()
		out.SignedBy, out.SignedAt = &who, &at
	}
	if r.SupersededAt != nil {
		at := r.SupersededAt.UTC()
		out.SupersededAt = &at
	}
	if r.ReopenID != nil {
		out.ReopenID = r.ReopenID.String()
	}
	if r.Status == storage.AccountantReviewInReview && !p.AI() && p.UserID != uuid.Nil {
		accountant := authz.Decide(authz.AskAccountantReviewQuery, p.Roles, false).Allowed
		out.YouMayAsk = accountant
		out.YouMayAnswer = out.Unanswered > 0 && authz.Decide(authz.AnswerAccountantReviewQuery, p.Roles, false).Allowed
		out.YouMaySignOff = accountant && out.Unanswered == 0
	}
	return out, nil
}

func periodRef(p storage.Period) ReviewPeriod {
	return ReviewPeriod{ID: p.ID.String(), Name: monthName(p), StartsOn: p.StartsOn.Format(time.DateOnly), EndsOn: p.EndsOn.Format(time.DateOnly)}
}

// signedIn is the person signed in to Fyniq who does what, never an OAuth client or the AI.
func signedIn(ctx context.Context, what string) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
		return p, problem(CodeSessionRequired, what+" needs a person with a Fyniq user, signed in to Fyniq.", "Sign in to Fyniq in the browser and do it there.")
	}
	return p, nil
}

// reviewIn reads review id in u for a change at ifMatch: it must exist and be in review.
func reviewIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, ifMatch string) (storage.AccountantReview, error) {
	r, err := u.AccountantReviews().Get(ctx, id)
	if err != nil {
		return r, err
	}
	if err := checkIfMatch(ifMatch, etag(r.Version)); err != nil {
		return r, err
	}
	if r.Status != storage.AccountantReviewInReview {
		return r, problem(accountantreview.CodeNotInReview, "The review is "+strings.ReplaceAll(r.Status, "_", " ")+", so it does not change.",
			"Open a review of the month as it is closed now")
	}
	return r, nil
}

func registerAccountantReviews(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		CodeSessionRequired, CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion, storage.CodeAccountantReview}, locks...)
	view := func(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (AccountantReview, error) {
		r, err := u.AccountantReviews().Get(ctx, id)
		if err != nil {
			return AccountantReview{}, err
		}
		names, err := memberNames(ctx, u, a.deps.Clock.Now())
		if err != nil {
			return AccountantReview{}, err
		}
		return reviewOf(ctx, u, p, r, names)
	}
	answer := func(w written[AccountantReview], status int) *reviewOutput {
		out := &reviewOutput{Status: status, Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.Status, out.ETag, out.Replayed = w.Status, etag(w.Body.Version), replayedHeader(w.Replayed)
		}
		return out
	}

	register(a, authz.ListAccountantReviews, huma.Operation{
		OperationID: "listAccountantReviews", Method: http.MethodGet, Path: "/v1/accountant-reviews", Tags: []string{"Periods"},
		Summary: "List the accountant's reviews",
		Description: "Every accountant's review, newest first, superseded ones too (FM-7; PRD-001 rev 0.176): the closed month each covers " +
			"and its close snapshot, the queries and answers, the adjusting journals and the sign-off. Also each calendar quarter since " +
			"the books started with the review that covers its last month (due 60 days after it ends; C.2 #61), and the closed months " +
			"whose latest close has no review yet. For the Accountant, the Owner, the Controller, the Auditor and the Tax agent; never " +
			"the AI. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *reviewListInput) (*reviewListOutput, error) {
		p, _ := authn.From(ctx)
		page := AccountantReviewPage{Quarters: []ReviewQuarter{}, ToReview: []ReviewPeriod{}}
		var all []AccountantReview
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.AccountantReviews().List(ctx)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			for i := len(rs) - 1; i >= 0; i-- {
				v, err := reviewOf(ctx, u, p, rs[i], names)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			periods, err := u.Periods().List(ctx)
			if err != nil {
				return err
			}
			for i := len(periods) - 1; i >= 0; i-- {
				per := periods[i]
				if per.Status != storage.PeriodClosed {
					continue
				}
				c, err := u.Closes().Latest(ctx, per.ID)
				if err != nil && !errors.Is(err, ports.ErrNotFound) {
					return err
				}
				if err == nil && !slices.ContainsFunc(rs, func(r storage.AccountantReview) bool { return r.CloseID == c.ID }) {
					page.ToReview = append(page.ToReview, periodRef(per))
				}
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			var from time.Time
			switch {
			case o.Profile.BooksStart != nil:
				from = *o.Profile.BooksStart
			case len(periods) > 0:
				from = periods[0].StartsOn
			}
			if !from.IsZero() {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				for _, q := range accountantreview.Quarters(periods, rs, from, today) {
					x := ReviewQuarter{Key: q.Key(), Label: q.Label(), EndsOn: q.EndsOn.Format(time.DateOnly), DueBy: q.DueBy.Format(time.DateOnly), Status: q.Status}
					if q.Period != nil {
						x.PeriodID = q.Period.ID.String()
					}
					if q.Review != nil {
						x.ReviewID = q.Review.ID.String()
					}
					page.Quarters = append(page.Quarters, x)
				}
			}
			page.YouMayOpen = !p.AI() && p.UserID != uuid.Nil && authz.Decide(authz.OpenAccountantReview, p.Roles, false).Allowed && len(page.ToReview) > 0
			return nil
		})
		if err != nil {
			return nil, err
		}
		if all == nil {
			all = []AccountantReview{}
		}
		page.Items, page.NextCursor, err = pageOf(all, func(v AccountantReview) string { return v.ID }, in.PageQuery)
		return &reviewListOutput{Body: page}, err
	})

	register(a, authz.GetAccountantReview, huma.Operation{
		OperationID: "getAccountantReview", Method: http.MethodGet, Path: "/v1/accountant-reviews/{review_id}", Tags: []string{"Periods"},
		Summary: "Get an accountant's review",
		Description: "A review with the month and close it covers, its queries and answers, its adjusting journals and its sign-off, and its " +
			"ETag (FM-7). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AccountantReviewPath) (*reviewOutput, error) {
		id, err := parseID("review_id", in.ReviewID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out AccountantReview
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			out, err = view(ctx, u, p, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &reviewOutput{Status: http.StatusOK, ETag: etag(out.Version), Body: out}, nil
	})

	register(a, authz.OpenAccountantReview, huma.Operation{
		OperationID: "openAccountantReview", Method: http.MethodPost, Path: "/v1/periods/{period_id}/accountant-review", Tags: []string{"Periods"},
		Summary: "Open the accountant's review of a closed month", DefaultStatus: http.StatusCreated,
		Description: "Opens the Accountant's review of a closed month's latest close, whose snapshot it covers (FM-7, AUD-04; PRD-001 rev " +
			"0.176): normally the quarter's last month, whose snapshot is the trial balance at the quarter end. Refused for a month " +
			"that is not closed (REVIEW-01) and for a close reviewed already (REVIEW-02); the database refuses both too. Needs a " +
			"person signed in to Fyniq, and If-Match with the period's ETag. Side effects: the review, audited. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and opens nothing.",
	}, append([]string{accountantreview.CodeNotClosed, accountantreview.CodeReviewed}, writes...), func(ctx context.Context, in *reviewOpenInput) (*reviewOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := signedIn(ctx, "Opening a review")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AccountantReview, error) {
			per, err := u.Periods().Get(ctx, id)
			if err != nil {
				return AccountantReview{}, err
			}
			cal, err := calendar(ctx, u)
			if err != nil {
				return AccountantReview{}, err
			}
			if err := checkIfMatch(in.IfMatch, periodETag(periodOf(per, cal))); err != nil {
				return AccountantReview{}, err
			}
			if per.Status != storage.PeriodClosed {
				return AccountantReview{}, problem(accountantreview.CodeNotClosed, monthName(per)+" is not closed, so it is not reviewed yet.",
					"Close the month first: the review covers its close snapshot")
			}
			c, err := u.Closes().Latest(ctx, id)
			if err != nil {
				return AccountantReview{}, err
			}
			rs, err := u.AccountantReviews().List(ctx)
			if err != nil {
				return AccountantReview{}, err
			}
			if slices.ContainsFunc(rs, func(r storage.AccountantReview) bool { return r.CloseID == c.ID }) {
				return AccountantReview{}, problem(accountantreview.CodeReviewed, monthName(per)+"'s close has a review already.", "Open that review")
			}
			r, err := u.AccountantReviews().Open(ctx, id, c.ID, p.UserID, a.deps.Clock.Now())
			if err != nil {
				return AccountantReview{}, err
			}
			return view(ctx, u, p, r.ID)
		})
		if err != nil {
			return nil, err
		}
		return answer(w, http.StatusOK), nil
	})

	register(a, authz.AskAccountantReviewQuery, huma.Operation{
		OperationID: "askAccountantReviewQuery", Method: http.MethodPost, Path: "/v1/accountant-reviews/{review_id}/queries", Tags: []string{"Periods"},
		Summary: "Raise a query in a review",
		Description: "Adds the Accountant's query to a review in review, for an Owner or the Controller to answer (FM-7). REVIEW-03 once it is " +
			"signed off or superseded. Needs a person signed in to Fyniq, and If-Match with the review's ETag. Side effects: the query, " +
			"audited, and the review's next version. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{accountantreview.CodeNotInReview}, writes...), func(ctx context.Context, in *reviewAskInput) (*reviewOutput, error) {
		id, err := parseID("review_id", in.ReviewID)
		if err != nil {
			return nil, err
		}
		question := strings.TrimSpace(in.Body.Question)
		if question == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.question", Message: "is empty", Fix: "Write the query"}})
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := signedIn(ctx, "Raising a query")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountantReview, error) {
			r, err := reviewIn(ctx, u, id, in.IfMatch)
			if err != nil {
				return AccountantReview{}, err
			}
			if _, err := u.AccountantReviews().Ask(ctx, id, r.Version, p.UserID, a.deps.Clock.Now(), question); err != nil {
				return AccountantReview{}, err
			}
			return view(ctx, u, p, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w, http.StatusOK), nil
	})

	register(a, authz.AnswerAccountantReviewQuery, huma.Operation{
		OperationID: "answerAccountantReviewQuery", Method: http.MethodPost, Path: "/v1/accountant-reviews/{review_id}/queries/{query_id}/answer",
		Tags: []string{"Periods"}, Summary: "Answer a review's query",
		Description: "Records management's answer to a query of a review in review, once: an Owner or the Controller (FM-7). REVIEW-04 when " +
			"it is answered already, REVIEW-03 once the review is signed off or superseded. Needs a person signed in to Fyniq, and " +
			"If-Match with the review's ETag. Side effects: the answer, audited, and the review's next version. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{accountantreview.CodeNotInReview, accountantreview.CodeAnswered}, writes...), func(ctx context.Context, in *reviewAnswerInput) (*reviewOutput, error) {
		id, err := parseID("review_id", in.ReviewID)
		if err != nil {
			return nil, err
		}
		query, err := parseID("query_id", in.QueryID)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(in.Body.Answer)
		if text == "" {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.answer", Message: "is empty", Fix: "Write the answer"}})
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := signedIn(ctx, "Answering a query")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountantReview, error) {
			r, err := reviewIn(ctx, u, id, in.IfMatch)
			if err != nil {
				return AccountantReview{}, err
			}
			i := slices.IndexFunc(r.Queries, func(q storage.ReviewQuery) bool { return q.ID == query })
			switch {
			case i < 0:
				return AccountantReview{}, problem(CodeNotFound, "The review has no query with that ID.", "Read the review to find its queries")
			case r.Queries[i].AnsweredBy != nil:
				return AccountantReview{}, problem(accountantreview.CodeAnswered, "The query is answered already.", "Ask the Accountant to raise another query")
			}
			if _, err := u.AccountantReviews().Answer(ctx, id, r.Version, query, p.UserID, a.deps.Clock.Now(), text); err != nil {
				return AccountantReview{}, err
			}
			return view(ctx, u, p, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w, http.StatusOK), nil
	})

	register(a, authz.LinkAccountantReviewJournal, huma.Operation{
		OperationID: "linkAccountantReviewJournal", Method: http.MethodPost, Path: "/v1/accountant-reviews/{review_id}/journals", Tags: []string{"Periods"},
		Summary: "Link an adjusting journal to a review",
		Description: "Records that a review raised an adjusting journal (FM-7): a draft, or one posted already. It posts as any journal does, " +
			"through approval above the self-approval threshold (FR-GL-01); dated in a closed month, that month reopens first (FR-GL-08), " +
			"which supersedes this review. REVIEW-05 for a document that is not a journal, REVIEW-03 once the review is signed off or " +
			"superseded. Needs a person signed in to Fyniq, and If-Match with the review's ETag. Side effects: the link, audited, and " +
			"the review's next version. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{accountantreview.CodeNotInReview, accountantreview.CodeNotJournal}, writes...), func(ctx context.Context, in *reviewLinkInput) (*reviewOutput, error) {
		id, err := parseID("review_id", in.ReviewID)
		if err != nil {
			return nil, err
		}
		journal, err := parseID("body.journal_id", in.Body.JournalID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := signedIn(ctx, "Linking a journal")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountantReview, error) {
			r, err := reviewIn(ctx, u, id, in.IfMatch)
			if err != nil {
				return AccountantReview{}, err
			}
			doc, err := u.Documents().Get(ctx, journal)
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return AccountantReview{}, err
			}
			if err != nil || doc.Type != storage.DocumentJournal {
				return AccountantReview{}, problem(accountantreview.CodeNotJournal, "That is not a journal of this organisation.", "Link a manual journal")
			}
			if slices.ContainsFunc(r.Journals, func(j storage.ReviewJournal) bool { return j.JournalID == journal }) {
				return view(ctx, u, p, id)
			}
			if _, err := u.AccountantReviews().Link(ctx, id, r.Version, journal, p.UserID, a.deps.Clock.Now()); err != nil {
				return AccountantReview{}, err
			}
			return view(ctx, u, p, id)
		})
		if err != nil {
			return nil, err
		}
		return answer(w, http.StatusOK), nil
	})

	register(a, authz.SignOffAccountantReview, huma.Operation{
		OperationID: "signOffAccountantReview", Method: http.MethodPost, Path: "/v1/accountant-reviews/{review_id}/sign-off", Tags: []string{"Periods"},
		Summary: "Sign off a review",
		Description: "The Accountant signs a review off with a statement, stepped up in the last 5 minutes, once every query has its answer " +
			"(FM-7; REVIEW-06 otherwise, and the database refuses it too). Signed off, it covers its quarter when it is of the quarter's " +
			"last month (C.2 #61), and changes only when its month reopens, which supersedes it. Needs If-Match with the review's ETag. " +
			"Side effects: the sign-off, audited. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStepUpRequired, accountantreview.CodeNotInReview, accountantreview.CodeOpenQueries}, writes...),
		func(ctx context.Context, in *reviewSignInput) (*reviewOutput, error) {
			id, err := parseID("review_id", in.ReviewID)
			if err != nil {
				return nil, err
			}
			statement := strings.TrimSpace(in.Body.Statement)
			if statement == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.statement", Message: "is empty", Fix: "Say what you sign off"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := a.steppedUpPerson(ctx, "Signing off a review")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountantReview, error) {
				r, err := reviewIn(ctx, u, id, in.IfMatch)
				if err != nil {
					return AccountantReview{}, err
				}
				if slices.ContainsFunc(r.Queries, func(q storage.ReviewQuery) bool { return q.AnsweredBy == nil }) {
					return AccountantReview{}, problem(accountantreview.CodeOpenQueries, "A query has no answer yet, so the review is not signed off.",
						"An Owner or the Controller answers each query first")
				}
				if _, err := u.AccountantReviews().SignOff(ctx, id, r.Version, p.UserID, a.deps.Clock.Now(), statement); err != nil {
					return AccountantReview{}, err
				}
				return view(ctx, u, p, id)
			})
			if err != nil {
				return nil, err
			}
			return answer(w, http.StatusOK), nil
		})
}
