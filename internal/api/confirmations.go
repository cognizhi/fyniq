package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ year-end confirmations (FR-EQ-12; p1-founders-money)
//
// Each director or shareholder confirms their balances on the director's accounts at each financial year's end, as the
// member who signs in as them, stepped up with a passkey (design D13). Confirmed means the latest confirmation shows the
// balances the ledger holds at that end now; anything else is ⚠ on the director accounts and in the year-end close. A
// director with no member linked cannot confirm: link the member first (D1).

// CodePasskeyStepUp is a confirmation stepped up otherwise than with a passkey.
const CodePasskeyStepUp = "FOUNDER-26"

// YearEndConfirmation is where a person's confirmation of a year's end stands.
type YearEndConfirmation struct {
	YearEnd     string     `json:"year_end" format:"date" doc:"The latest financial year end on or before the balances' day." example:"2025-12-31"`
	Status      string     `json:"status" enum:"confirmed,changed_since,not_confirmed" doc:"confirmed: the latest confirmation shows the balances held at the year's end now; changed_since: it shows others, as an entry dated in the year posted after it; not_confirmed: none (⚠)." example:"confirmed"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty" doc:"When the latest confirmation was made." example:"2026-01-09T10:15:00Z"`
	ConfirmedBy *Person    `json:"confirmed_by,omitempty" doc:"Who made it: the member who signs in as the person."`
}

// ConfirmationRequest is the year end a person confirms.
type ConfirmationRequest struct {
	YearEnd string `json:"year_end" format:"date" doc:"The financial year's last day: the year must have ended." example:"2025-12-31"`
}

// ConfirmedBalances is a person's confirmation as made: the balances confirmed at the year's end.
type ConfirmedBalances struct {
	ID       string            `json:"id" format:"uuid" doc:"The confirmation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4ff1"`
	YearEnd  string            `json:"year_end" format:"date" doc:"The year's end confirmed." example:"2025-12-31"`
	Balances []DirectorBalance `json:"balances" nullable:"false" doc:"The balances confirmed, one per director's account, as the ledger holds them: negative is owed to the person."`
}

type confirmInput struct {
	KeyHeader
	PreferHeader
	Body ConfirmationRequest
}

type confirmOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ConfirmedBalances
}

// yearEndView is what showing confirmations needs once: the latest year end on or before a day, each person's balances
// at it, and every confirmation.
type yearEndView struct {
	end           time.Time
	ok            bool
	balances      map[uuid.UUID]map[uuid.UUID]money.Money
	confirmations map[uuid.UUID][]director.RecordedConfirmation
}

func yearEndViewOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, asAt time.Time) (yearEndView, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return yearEndView{}, err
	}
	v := yearEndView{balances: map[uuid.UUID]map[uuid.UUID]money.Money{}}
	if v.end, v.ok = director.LastYearEnd(o.Profile, asAt); !v.ok {
		return v, nil
	}
	people, _, _, err := director.Accounts(ctx, u, base, v.end)
	if err != nil {
		return v, err
	}
	for _, p := range people {
		v.balances[p.Contact.ID] = p.Balances
	}
	v.confirmations, err = director.ConfirmationsOf(ctx, u)
	return v, err
}

// of is a person's confirmation of the year end, or nil when no year has ended.
func (v yearEndView) of(contact uuid.UUID) *YearEndConfirmation {
	if !v.ok {
		return nil
	}
	latest := director.LatestFor(v.confirmations[contact], v.end)
	out := &YearEndConfirmation{YearEnd: dateOf(v.end), Status: director.StateOf(latest, v.balances[contact])}
	if latest != nil {
		at := latest.ConfirmedAt
		out.ConfirmedAt, out.ConfirmedBy = &at, personOf(latest.ConfirmedBy)
	}
	return out
}

func registerConfirmations(a *API) {
	register(a, authz.ConfirmMyDirectorAccount, huma.Operation{
		OperationID: "confirmMyDirectorAccount", Method: http.MethodPost, Path: "/v1/me/director-account/confirmations", Tags: []string{"Founders"},
		DefaultStatus: http.StatusCreated, Summary: "Confirm your balances at a year's end",
		Description: "Confirms, as the director or shareholder you sign in as, your balances on the director's accounts at a financial year's " +
			"end (FR-EQ-12): the balances the ledger holds at that end now are kept with the confirmation, which is append-only. Only a " +
			"member linked to a director or shareholder confirms (FOUNDER-03), stepped up with a passkey in the last 5 minutes " +
			"(FOUNDER-26), for a year that has ended (FOUNDER-24). An entry dated in the year posted after it shows the confirmation as " +
			"changed since. Side effects: the confirmation, its audit event. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and saves nothing.",
	}, []string{CodeNotLinked, CodePasskeyStepUp, CodeSessionRequired, CodeStepUpRequired, director.CodeConfirm, director.CodeNotPerson,
		CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *confirmInput) (*confirmOutput, error) {
			yearEnd, err := time.Parse(time.DateOnly, in.Body.YearEnd)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.year_end", Message: "is not a date", Fix: "Send the year's last day as YYYY-MM-DD"}})
			}
			p, err := a.steppedUpPerson(ctx, "Confirming your balances")
			if err != nil {
				return nil, err
			}
			if p.StepUpMethod != "passkey" {
				return nil, problem(CodePasskeyStepUp, "Confirming your balances needs a step-up with your passkey (FR-EQ-12).",
					"Confirm it's you with your passkey, then try again")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ConfirmedBalances, error) {
				c, err := u.Contacts().OfMember(ctx, p.UserID)
				if errors.Is(err, ports.ErrNotFound) {
					return ConfirmedBalances{}, problem(CodeNotLinked, "No director or shareholder is linked to you, so there is nothing for you to confirm.",
						"Ask an Owner to link your contact to you in Accounting › Director accounts")
				}
				if err != nil {
					return ConfirmedBalances{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return ConfirmedBalances{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return ConfirmedBalances{}, err
				}
				doc, conf, err := director.ConfirmIn(ctx, u, base, c.ID, yearEnd, today)
				if err != nil {
					return ConfirmedBalances{}, directorProblem(err)
				}
				_, accounts, err := director.Get(ctx, u, base, c.ID, yearEnd)
				if err != nil {
					return ConfirmedBalances{}, directorProblem(err)
				}
				out := ConfirmedBalances{ID: doc.ID.String(), YearEnd: dateOf(yearEnd), Balances: []DirectorBalance{}}
				for _, acc := range accounts {
					out.Balances = append(out.Balances, DirectorBalance{AccountID: acc.ID.String(), Code: acc.Code, Name: acc.Name, Role: string(acc.Role),
						Balance: moneyAmount(conf.Balances[acc.ID])})
				}
				return out, nil
			})
			if err != nil {
				return nil, err
			}
			return &confirmOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
