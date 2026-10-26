package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ mileage (FR-AP-16; p1-expense-claims design D5)
//
// A member keeps the places they often drive between, their own, and picks them on a trip; each month's trips make
// their mileage log. A place's address is never sent anywhere: the maps service stays off.

// ClaimPlace is one of the caller's saved places.
type ClaimPlace struct {
	ID      string `json:"id" format:"uuid" doc:"The place's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa1"`
	Name    string `json:"name" doc:"What the member calls it." example:"Office"`
	Address string `json:"address" doc:"Its address, as the member typed it." example:"1 Raffles Place, Singapore 048616"`
	Active  bool   `json:"active" doc:"false once put out of use: kept, as trips name its address." example:"true"`
	Version int    `json:"version" doc:"Its version, for If-Match." example:"1"`
}

// ClaimPlaceDraft is a saved place as entered.
type ClaimPlaceDraft struct {
	Name    string `json:"name" minLength:"1" maxLength:"80" doc:"What to call it, unique among the member's places in use." example:"Office"`
	Address string `json:"address" minLength:"1" maxLength:"300" doc:"Its address." example:"1 Raffles Place, Singapore 048616"`
	Active  *bool  `json:"active,omitempty" doc:"false puts it out of use; left out, in use." example:"true"`
}

// ClaimPlacePath names a saved place.
type ClaimPlacePath struct {
	PlaceID string `path:"place_id" format:"uuid" doc:"The place's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fa1"`
}

// MileageTrip is one trip of a mileage log.
type MileageTrip struct {
	Date        string `json:"date" format:"date" doc:"The trip's date." example:"2026-10-24"`
	ClaimID     string `json:"claim_id" format:"uuid" doc:"The claim it is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fd1"`
	ClaimNumber string `json:"claim_number,omitempty" doc:"The claim's number." example:"CLM-0007"`
	Stage       string `json:"stage" doc:"Where the claim stands." example:"approved"`
	Purpose     string `json:"purpose,omitempty" doc:"What it was for." example:"Client visit"`
	From        string `json:"from" doc:"Where it started." example:"Office"`
	To          string `json:"to" doc:"Where it went." example:"Acme HQ"`
	Return      bool   `json:"return" doc:"true for a return trip." example:"true"`
	Vehicle     string `json:"vehicle" doc:"The vehicle." example:"car"`
	ClaimedKm   string `json:"claimed_km" doc:"The distance claimed, to 0.1 km; 0 while it has no rate." example:"18.4"`
	Amount      *Money `json:"amount,omitempty" doc:"Its amount; absent while its vehicle has no rate per km on its date."`
}

// MileageLog is a member's trips of a month, oldest first (FR-AP-16's monthly mileage log).
type MileageLog struct {
	Month    string        `json:"month" doc:"The month, YYYY-MM." example:"2026-10"`
	Claimant Person        `json:"claimant" doc:"Whose trips."`
	Trips    []MileageTrip `json:"trips" nullable:"false" doc:"Their trips in the month, on drafts and claims alike, oldest first."`
	Total    Money         `json:"total" doc:"What the trips with a rate come to."`
}

// ClaimPlacePage is a page of the caller's saved places.
type ClaimPlacePage struct {
	Items      []ClaimPlace `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMCJ9"`
}

type placeListInput struct {
	PageQuery
}

type placeListOutput struct{ Body ClaimPlacePage }

type createPlaceInput struct {
	KeyHeader
	PreferHeader
	Body ClaimPlaceDraft
}

type replacePlaceInput struct {
	ClaimPlacePath
	IfMatchHeader
	PreferHeader
	Body ClaimPlaceDraft
}

type placeOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The place's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ClaimPlace
}

type mileageLogInput struct {
	Month      string `query:"month" required:"true" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month, YYYY-MM." example:"2026-10"`
	ClaimantID string `query:"claimant_id" format:"uuid" doc:"Another member's log, for a person who sees everyone's claims; left out, the caller's own." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type mileageLogOutput struct{ Body MileageLog }

func placeOf(p storage.ClaimPlace) ClaimPlace {
	return ClaimPlace{ID: p.ID.String(), Name: p.Name, Address: p.Address, Active: p.Active, Version: p.Version}
}

// placeNameFree refuses a name another of p's places in use has (but self), in plain words.
func placeNameFree(ctx context.Context, u storage.UnitOfWork, p authn.Principal, self uuid.UUID, name string) error {
	places, err := u.ClaimPlaces().List(ctx, p.UserID)
	if err != nil {
		return err
	}
	for _, x := range places {
		if x.Active && x.ID != self && strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(name)) {
			return invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.name", Message: "another of your places is called " + x.Name,
				Fix: "Choose another name, or change that place"}})
		}
	}
	return nil
}

// ownPlace is one of p's places, locked for a change: anyone else's is not found.
func ownPlace(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.ClaimPlace, error) {
	place, err := u.ClaimPlaces().Get(ctx, id)
	if err == nil && place.UserID != p.UserID {
		return storage.ClaimPlace{}, ports.ErrNotFound
	}
	return place, err
}

func registerMileage(a *API) {
	placeCodes := []string{CodeClaimNotYours, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, storage.CodeClaim, storage.CodeRetry,
		storage.CodeLockTimeout}

	register(a, authz.ListClaimPlaces, huma.Operation{
		OperationID: "listClaimPlaces", Method: http.MethodGet, Path: "/v1/claim-places", Tags: []string{"Expense claims"},
		Summary: "List your saved places",
		Description: "The caller's own places for trips (FR-AP-16), those in use first, by name, a page at a time. Nobody sees another " +
			"member's. Read-only; no side effects.",
	}, []string{CodeClaimNotYours, CodeCursor}, func(ctx context.Context, in *placeListInput) (*placeListOutput, error) {
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		skip, _ := strconv.Atoi(after) // a cursor is the number of places on the pages before
		p, err := claimant(ctx)
		if err != nil {
			return nil, err
		}
		out := placeListOutput{Body: ClaimPlacePage{Items: []ClaimPlace{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			places, err := u.ClaimPlaces().List(ctx, p.UserID)
			for i, x := range places {
				switch {
				case i < skip:
					continue
				case len(out.Body.Items) == in.PageSize:
					out.Body.NextCursor = encodeCursor(strconv.Itoa(i))
					return err
				}
				out.Body.Items = append(out.Body.Items, placeOf(x))
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateClaimPlace, huma.Operation{
		OperationID: "createClaimPlace", Method: http.MethodPost, Path: "/v1/claim-places", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Save a place for your trips",
		Description: "Saves a place the caller drives between, by name and address, their own (FR-AP-16). Its address is never sent " +
			"anywhere. Side effects: the place and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and " +
			"saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, placeCodes...),
		func(ctx context.Context, in *createPlaceInput) (*placeOutput, error) {
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ClaimPlace, error) {
				if err := placeNameFree(ctx, u, p, uuid.Nil, in.Body.Name); err != nil {
					return ClaimPlace{}, err
				}
				x, err := u.ClaimPlaces().Create(ctx, storage.NewClaimPlace{UserID: p.UserID, Name: strings.TrimSpace(in.Body.Name),
					Address: strings.TrimSpace(in.Body.Address), Active: true, At: a.deps.Clock.Now()})
				return placeOf(x), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &placeOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &placeOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ReplaceClaimPlace, huma.Operation{
		OperationID: "replaceClaimPlace", Method: http.MethodPut, Path: "/v1/claim-places/{place_id}", Tags: []string{"Expense claims"},
		Summary: "Change one of your saved places",
		Description: "Changes the name or address of one of the caller's places, or puts it out of use; it is never deleted, as trips " +
			"name its address. Side effects: its next version and its audit event. Needs If-Match with its ETag. Prefer: return=preview " +
			"checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, placeCodes...),
		func(ctx context.Context, in *replacePlaceInput) (*placeOutput, error) {
			id, err := parseID("place_id", in.PlaceID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := claimant(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimPlace, error) {
				place, err := ownPlace(ctx, u, p, id)
				if err != nil {
					return ClaimPlace{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(place.Version)); err != nil {
					return ClaimPlace{}, err
				}
				active := in.Body.Active == nil || *in.Body.Active
				if active {
					if err := placeNameFree(ctx, u, p, id, in.Body.Name); err != nil {
						return ClaimPlace{}, err
					}
				}
				x, err := u.ClaimPlaces().Change(ctx, id, place.Version, storage.NewClaimPlace{Name: strings.TrimSpace(in.Body.Name),
					Address: strings.TrimSpace(in.Body.Address), Active: active, At: a.deps.Clock.Now()})
				return placeOf(x), err
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &placeOutput{Status: http.StatusOK, Applied: applied(w), Body: w.Body}, nil
			}
			return &placeOutput{Status: http.StatusOK, ETag: etag(w.Body.Version), Body: w.Body}, nil
		})

	register(a, authz.GetMileageLog, huma.Operation{
		OperationID: "getMileageLog", Method: http.MethodGet, Path: "/v1/expense-claims/mileage-log", Tags: []string{"Expense claims"},
		Summary: "A member's mileage log for a month",
		Description: "The trips of a member's claims in a month, oldest first, each with its places, vehicle, the distance claimed to 0.1 " +
			"km and its amount at the rate in force on its date (FR-AP-16's monthly mileage log): the caller's own, or another member's " +
			"for a person who sees everyone's claims. Read-only; no side effects.",
	}, []string{CodeInvalid, CodeNotFound, claim.CodeNotClaim, claim.CodeBadDraft}, func(ctx context.Context, in *mileageLogInput) (*mileageLogOutput, error) {
		start, err := time.Parse("2006-01", in.Month)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "month", Message: "is not a month", Fix: "Send it as YYYY-MM"}})
		}
		p, _ := authn.From(ctx)
		whose := p.UserID
		if in.ClaimantID != "" {
			if whose, err = uuid.Parse(in.ClaimantID); err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "claimant_id", Message: "is not an ID", Fix: "Send the member's user ID"}})
			}
		}
		if whose != p.UserID && ownClaimsOnly(p) || whose == uuid.Nil {
			return nil, ports.ErrNotFound
		}
		out := mileageLogOutput{Body: MileageLog{Month: in.Month, Claimant: Person{Kind: "user", ID: whose.String()}, Trips: []MileageTrip{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.claimView(ctx, u)
			if err != nil {
				return err
			}
			total := money.Zero(v.base)
			end := start.AddDate(0, 1, 0)
			var afterDate string
			var afterID *uuid.UUID
			for {
				docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExpenseClaim, AfterDate: afterDate, AfterID: afterID, Limit: 500})
				if err != nil {
					return err
				}
				for _, doc := range docs {
					afterDate, afterID = doc.Date, &doc.ID
					if doc.Status == storage.DocumentReversed {
						continue
					}
					d, err := claim.Read(doc, v.base)
					if err != nil {
						return err
					}
					if d.ClaimantID != whose || !hasTripIn(d, start, end) {
						continue
					}
					c, err := a.claimOf(ctx, u, v, doc, false)
					if err != nil {
						return err
					}
					for i, l := range d.Lines {
						if l.Kind != claim.LineMileage || l.Date.Before(start) || !l.Date.Before(end) {
							continue
						}
						shown := c.Lines[i]
						t := MileageTrip{Date: dateOf(l.Date), ClaimID: doc.ID.String(), ClaimNumber: doc.Number, Stage: c.Stage, Purpose: l.Description,
							From: l.From, To: l.To, Return: l.Return, Vehicle: l.Vehicle, ClaimedKm: cmpOr(shown.ClaimedKm, "0"), Amount: shown.Amount}
						if shown.Amount != nil {
							x, err := money.ParseStored(shown.Amount.Amount, v.base)
							if err == nil {
								total, _ = total.Add(x)
							}
						}
						out.Body.Trips = append(out.Body.Trips, t)
					}
				}
				if len(docs) < 500 {
					break
				}
			}
			sortTrips(out.Body.Trips)
			out.Body.Total = moneyOut(total)
			return a.name(ctx, u, &out.Body.Claimant)
		})
		return &out, err
	})
}

// hasTripIn says whether d has a trip dated from start, before end.
func hasTripIn(d claim.Draft, start, end time.Time) bool {
	for _, l := range d.Lines {
		if l.Kind == claim.LineMileage && !l.Date.Before(start) && l.Date.Before(end) {
			return true
		}
	}
	return false
}

// sortTrips puts trips oldest first, then by claim number.
func sortTrips(ts []MileageTrip) {
	slices.SortStableFunc(ts, func(x, y MileageTrip) int {
		return cmp.Or(strings.Compare(x.Date, y.Date), strings.Compare(x.ClaimNumber, y.ClaimNumber))
	})
}
