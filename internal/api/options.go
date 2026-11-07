package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// OptionCodes are the codes the option pool and grants answer besides the common ones.
var OptionCodes = []string{equity.CodePool, equity.CodeGrant, equity.CodeEvent, equity.CodeRecorded, storage.CodeOption}

// PoolResolutionDraft is a resolution setting the option pool's size, as drafted (FR-EQ-19).
type PoolResolutionDraft struct {
	Size           string `json:"size" pattern:"^[0-9]{1,14}$" doc:"The shares the resolution sets aside for options, a whole number above zero." example:"1000000"`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The share-capital account of the class the options are over." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ResolutionRef  string `json:"resolution_ref" maxLength:"1000" doc:"The resolution that sets it." example:"Ordinary resolution 1 Oct 2026"`
	ResolvedOn     string `json:"resolved_on" format:"date" doc:"The day the resolution was passed: the pool has this size from then until a later resolution." example:"2026-10-01"`
	Note           string `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"ESOP 2026"`
}

// PoolResolutionView is a pool resolution as the API shows it.
type PoolResolutionView struct {
	PoolResolutionDraft
	ID         string      `json:"id" format:"uuid" doc:"The resolution." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6c01"`
	Status     string      `json:"status" enum:"draft,recorded" doc:"draft: waiting for the resolution and approval; recorded: in the register." example:"recorded"`
	Version    int         `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	ClassCode  string      `json:"class_code" doc:"The class's account code." example:"3010"`
	InForce    bool        `json:"in_force" doc:"It sets the pool's size today." example:"true"`
	Evidence   int         `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, the resolution." example:"1"`
	ApprovalID string      `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6c11"`
	RecordedBy *Person     `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt *time.Time  `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-02T09:30:00Z"`
	Checks     []BillCheck `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// OptionPoolView is the option pool on a day (FR-EQ-19, design D9): its size by the resolution in force, the options
// granted by recorded grants, what is left, and every resolution.
type OptionPoolView struct {
	At             string               `json:"at" format:"date" doc:"The day it is read at, its end." example:"2026-10-15"`
	Size           string               `json:"size" doc:"The shares set aside by the latest recorded resolution passed on or before the day; 0 while none is." example:"1000000"`
	ClassAccountID string               `json:"class_account_id,omitempty" format:"uuid" doc:"The class the options are over, by the resolution in force." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ClassCode      string               `json:"class_code,omitempty" doc:"Its account code." example:"3010"`
	Granted        string               `json:"granted" doc:"The options the recorded grants granted on or before the day." example:"68000"`
	Returned       string               `json:"returned" doc:"The options recorded forfeitures and expiries gave back on or before the day." example:"0"`
	Forfeited      string               `json:"forfeited" doc:"The options the recorded grants' forfeitures took by the day." example:"45000"`
	Expired        string               `json:"expired" doc:"The options their expiries let lapse by the day." example:"0"`
	Exercised      string               `json:"exercised" doc:"The options effective exercises turned into shares by the day, never back in the pool." example:"0"`
	Outstanding    string               `json:"outstanding" doc:"The recorded grants' options not exercised, forfeited or expired at the end of the day." example:"425000"`
	Exercisable    string               `json:"exercisable" doc:"Of those, the options vested: what exercises may take." example:"40000"`
	Unallocated    string               `json:"unallocated" doc:"What the pool leaves to grant: its size less what is granted, with what is given back." example:"932000"`
	Resolutions    []PoolResolutionView `json:"resolutions" nullable:"false" doc:"Every resolution, drafts and recorded, the latest passed first."`
}

// OptionGrantDraft is a grant of options, as drafted (FR-EQ-19).
type OptionGrantDraft struct {
	HolderID       string `json:"holder_id" format:"uuid" doc:"The person the options are granted to: a contact marked investor or shareholder. It stays once drafted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	Number         string `json:"number" pattern:"^[0-9]{1,14}$" doc:"The options granted, a whole number above zero, at most what the pool leaves on the grant date." example:"48000"`
	ExercisePrice  string `json:"exercise_price" pattern:"^[0-9]{1,10}(\\.[0-9]{1,6})?$" doc:"The price per share paid on exercise, in the base currency, from zero." example:"0.10"`
	GrantedOn      string `json:"granted_on" format:"date" doc:"The grant date, from which it vests." example:"2026-10-15"`
	CliffMonths    int    `json:"cliff_months" minimum:"0" maximum:"120" doc:"Months before anything vests." example:"12"`
	VestingMonths  int    `json:"vesting_months" minimum:"0" maximum:"120" doc:"Months it vests over, evenly by month from the grant date; 0 vests it all at once." example:"48"`
	Acceleration   string `json:"acceleration" enum:"none,single,double" doc:"Its acceleration on a change of control, as recorded: none, single or double trigger." example:"double"`
	ExpiresOn      string `json:"expires_on" format:"date" doc:"The day it lapses, after the grant date." example:"2036-10-15"`
	ClassAccountID string `json:"class_account_id" format:"uuid" doc:"The class's share-capital account: the pool's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	Note           string `json:"note,omitempty" maxLength:"1000" doc:"A note." example:"New hire grant"`
}

// OptionGrantView is a grant as the API shows it.
type OptionGrantView struct {
	OptionGrantDraft
	ID          string            `json:"id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6d01"`
	Status      string            `json:"status" enum:"draft,recorded" doc:"draft: waiting for the grant letter and approval; recorded: in the register." example:"recorded"`
	Version     int               `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	HolderName  string            `json:"holder_name" doc:"Its holder's name." example:"Priya Nair"`
	Currency    string            `json:"currency" doc:"The exercise price's currency, the base currency." example:"SGD"`
	ClassCode   string            `json:"class_code" doc:"The class's account code." example:"3010"`
	Vested      string            `json:"vested" doc:"What has vested at the end of today (computed, never stored): none before the cliff, then evenly by whole months, rounded down [accountant to confirm]." example:"12000"`
	Outstanding string            `json:"outstanding" doc:"The options not exercised, forfeited or expired, at the end of today." example:"48000"`
	Forfeited   string            `json:"forfeited" doc:"The options its recorded forfeitures took, back in the pool." example:"0"`
	Expired     string            `json:"expired" doc:"The options its recorded expiries let lapse, back in the pool." example:"0"`
	Exercised   string            `json:"exercised" doc:"The options effective exercises turned into shares, less those a reversal undid." example:"0"`
	NextVestsOn string            `json:"next_vests_on,omitempty" format:"date" doc:"The next day more of it vests, a month's day after today; left out when nothing more will." example:"2026-11-15"`
	NextVests   string            `json:"next_vests,omitempty" doc:"How many options vest that day." example:"1000"`
	Exercisable string            `json:"exercisable" doc:"What is vested and not exercised at the end of today: what an exercise may take (an exercise transaction, createEquityTransaction)." example:"12000"`
	Events      []OptionEventView `json:"events" nullable:"false" doc:"Its forfeitures and expiries, drafts and recorded, oldest first (createOptionEvent)."`
	Evidence    int               `json:"evidence" doc:"How many files are attached and not removed: asking for approval needs one, the grant letter." example:"1"`
	ApprovalID  string            `json:"approval_id,omitempty" format:"uuid" doc:"The approval request waiting, if one is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6d11"`
	RecordedBy  *Person           `json:"recorded_by,omitempty" doc:"Who approved it, as it was recorded."`
	RecordedAt  *time.Time        `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-10-16T09:30:00Z"`
	Checks      []BillCheck       `json:"checks" nullable:"false" doc:"A draft's status: ✖ while no evidence is attached."`
}

// OptionGrantPage is a page of grants, the latest granted first.
type OptionGrantPage struct {
	Items      []OptionGrantView `json:"items" nullable:"false"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// MyOptionGrants are the caller's own grants (Appendix D, "own grants"): those whose holder is the contact they sign in
// as (linkContactMember).
type MyOptionGrants struct {
	Linked bool              `json:"linked" doc:"The caller signs in as a holder: a contact whose member is them." example:"true"`
	Items  []OptionGrantView `json:"grants" nullable:"false" doc:"Their recorded grants, the latest granted first."`
}

type (
	PoolResolutionPath struct {
		ResolutionID string `path:"resolution_id" format:"uuid" doc:"The pool resolution." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6c01"`
	}
	OptionGrantPath struct {
		GrantID string `path:"grant_id" format:"uuid" doc:"The grant." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6d01"`
	}
	optionPoolInput struct {
		At string `query:"at" format:"date" doc:"The day to read the pool at, its end; left out, the organisation's today." example:"2026-10-15"`
	}
	optionPoolOutput         struct{ Body OptionPoolView }
	poolResolutionDraftInput struct {
		KeyHeader
		PreferHeader
		Body PoolResolutionDraft
	}
	poolResolutionReviseInput struct {
		PoolResolutionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body PoolResolutionDraft
	}
	poolResolutionAskInput struct {
		PoolResolutionPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	poolResolutionReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body PoolResolutionView
	}
	poolResolutionWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PoolResolutionView
	}
	grantListInput  struct{ PageQuery }
	grantListOutput struct{ Body OptionGrantPage }
	grantDraftInput struct {
		KeyHeader
		PreferHeader
		Body OptionGrantDraft
	}
	grantReviseInput struct {
		OptionGrantPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body OptionGrantDraft
	}
	grantAskInput struct {
		OptionGrantPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	grantReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body OptionGrantView
	}
	grantWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     OptionGrantView
	}
	myGrantsOutput struct{ Body MyOptionGrants }
)

// poolIn reads a pool resolution's fields, or what of them is wrong.
func poolIn(d PoolResolutionDraft) (equity.PoolResolution, error) {
	p := equity.PoolResolution{Size: strings.TrimSpace(d.Size), ResolutionRef: strings.TrimSpace(d.ResolutionRef), Note: strings.TrimSpace(d.Note)}
	var fields []FieldProblem
	var err error
	if p.ClassAccountID, err = uuid.Parse(d.ClassAccountID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.class_account_id", Message: "is not an ID", Fix: "Send the share-capital account's ID"})
	}
	if p.ResolvedOn, err = time.Parse(time.DateOnly, d.ResolvedOn); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.resolved_on", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
	}
	if len(fields) > 0 {
		return p, invalid(fields)
	}
	return p, nil
}

// grantIn reads a grant's fields, or what of them is wrong.
func grantIn(d OptionGrantDraft) (equity.Grant, error) {
	g := equity.Grant{Number: strings.TrimSpace(d.Number), ExercisePrice: strings.TrimSpace(d.ExercisePrice), Note: strings.TrimSpace(d.Note),
		Vesting: equity.Vesting{CliffMonths: d.CliffMonths, Months: d.VestingMonths, Acceleration: d.Acceleration}}
	var fields []FieldProblem
	var err error
	if d.HolderID != "" {
		if g.HolderID, err = uuid.Parse(d.HolderID); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.holder_id", Message: "is not an ID", Fix: "Send the holder's contact ID"})
		}
	}
	if g.ClassAccountID, err = uuid.Parse(d.ClassAccountID); err != nil {
		fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.class_account_id", Message: "is not an ID", Fix: "Send the share-capital account's ID"})
	}
	for _, x := range []struct {
		to          *time.Time
		text, field string
	}{{&g.GrantedOn, d.GrantedOn, "granted_on"}, {&g.ExpiresOn, d.ExpiresOn, "expires_on"}} {
		if *x.to, err = time.Parse(time.DateOnly, x.text); err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body." + x.field, Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
		}
	}
	if len(fields) > 0 {
		return g, invalid(fields)
	}
	return g, nil
}

// optionView is what showing the pool and grants needs once: the account codes, the names, today and the requests waiting.
type optionView struct {
	codes      map[uuid.UUID]string
	names      map[uuid.UUID]string
	today      time.Time
	poolAsked  map[string]string
	grantAsked map[string]string
	eventAsked map[string]string
	events     map[uuid.UUID][]storage.Document // every event document, drafts and recorded, by its grant
	exercises  map[uuid.UUID][]equity.Exercise  // every grant's effective exercises
}

func (a *API) optionViewOf(ctx context.Context, u storage.UnitOfWork) (optionView, error) {
	v := optionView{codes: map[uuid.UUID]string{}, names: map[uuid.UUID]string{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return v, err
	}
	for _, x := range accounts {
		v.codes[x.ID] = x.Code
	}
	if v.today, err = orgToday(ctx, a, u); err != nil {
		return v, err
	}
	if v.poolAsked, err = a.waiting(ctx, u, approval.ObjectOptionPool); err != nil {
		return v, err
	}
	if v.grantAsked, err = a.waiting(ctx, u, approval.ObjectOptionGrant); err != nil {
		return v, err
	}
	if v.eventAsked, err = a.waiting(ctx, u, approval.ObjectOptionEvent); err != nil {
		return v, err
	}
	if v.exercises, err = equity.ExercisesIn(ctx, u); err != nil {
		return v, err
	}
	docs, err := equity.OptionEvents(ctx, u)
	if err != nil {
		return v, err
	}
	v.events = map[uuid.UUID][]storage.Document{}
	for _, d := range docs {
		e, err := equity.ReadEvent(d)
		if err != nil {
			return v, err
		}
		v.events[e.GrantID] = append(v.events[e.GrantID], d)
	}
	return v, nil
}

func (a *API) poolResolutionOf(ctx context.Context, u storage.UnitOfWork, v optionView, doc storage.Document, inForce *storage.Document) (PoolResolutionView, error) {
	p, err := equity.ReadPool(doc)
	if err != nil {
		return PoolResolutionView{}, err
	}
	out := PoolResolutionView{PoolResolutionDraft: PoolResolutionDraft{Size: p.Size, ClassAccountID: p.ClassAccountID.String(), ResolutionRef: p.ResolutionRef,
		ResolvedOn: p.ResolvedOn.Format(time.DateOnly), Note: p.Note}, ID: doc.ID.String(), Status: "draft", Version: doc.Version, ClassCode: v.codes[p.ClassAccountID],
		InForce: inForce != nil && inForce.ID == doc.ID}
	if doc.Status == storage.DocumentPosted {
		out.Status = "recorded"
	}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, v.poolAsked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the resolution"
		}
	}
	return out, err
}

func (a *API) grantOf(ctx context.Context, u storage.UnitOfWork, v optionView, doc storage.Document) (OptionGrantView, error) {
	g, err := equity.ReadGrant(doc)
	if err != nil {
		return OptionGrantView{}, err
	}
	name, ok := v.names[g.HolderID]
	if !ok {
		c, err := u.Contacts().Get(ctx, g.HolderID)
		if err != nil {
			return OptionGrantView{}, err
		}
		name, v.names[g.HolderID] = c.LegalName, c.LegalName
	}
	base, err := baseOf(ctx, u)
	if err != nil {
		return OptionGrantView{}, err
	}
	var recorded []equity.GrantEvent
	events := []OptionEventView{}
	for _, d := range v.events[doc.ID] {
		x, err := a.optionEventOf(ctx, u, v, d, name)
		if err != nil {
			return OptionGrantView{}, err
		}
		events = append(events, x)
		if d.Status == storage.DocumentPosted {
			e, err := equity.ReadEvent(d)
			if err != nil {
				return OptionGrantView{}, err
			}
			recorded = append(recorded, e)
		}
	}
	standing, err := equity.StandingOf(g, recorded, v.exercises[doc.ID], v.today)
	if err != nil {
		return OptionGrantView{}, err
	}
	nextOn, next, more, err := equity.NextVesting(g, recorded, v.exercises[doc.ID], v.today)
	if err != nil {
		return OptionGrantView{}, err
	}
	out := OptionGrantView{OptionGrantDraft: OptionGrantDraft{HolderID: g.HolderID.String(), Number: g.Number, ExercisePrice: g.ExercisePrice,
		GrantedOn: g.GrantedOn.Format(time.DateOnly), CliffMonths: g.Vesting.CliffMonths, VestingMonths: g.Vesting.Months, Acceleration: g.Vesting.Acceleration,
		ExpiresOn: g.ExpiresOn.Format(time.DateOnly), ClassAccountID: g.ClassAccountID.String(), Note: g.Note},
		ID: doc.ID.String(), Status: "draft", Version: doc.Version, HolderName: name, Currency: base.Code, ClassCode: v.codes[g.ClassAccountID],
		Vested: standing.Vested.Text('f'), Outstanding: standing.Outstanding.Text('f'), Forfeited: standing.Forfeited.Text('f'),
		Expired: standing.Expired.Text('f'), Exercised: standing.Exercised.Text('f'), Exercisable: standing.Exercisable.Text('f'), Events: events}
	if more {
		out.NextVestsOn, out.NextVests = nextOn.Format(time.DateOnly), next.Text('f')
	}
	if doc.Status == storage.DocumentPosted {
		out.Status = "recorded"
	}
	out.Evidence, out.ApprovalID, out.RecordedBy, out.RecordedAt, out.Checks, err = a.recordCommon(ctx, u, doc, v.grantAsked)
	for k := range out.Checks {
		if out.Checks[k].Code == equity.CodeNoEvidence {
			out.Checks[k].Fix = "Attach the grant letter"
		}
	}
	return out, err
}

// poolDependencies are a pool resolution's envelope values: its status.
func poolDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status)}, nil
}

// grantDependencies are a grant's envelope values: its status and its holder.
func grantDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	g, err := equity.ReadGrant(doc)
	if err != nil {
		return 0, nil, err
	}
	return doc.Version, map[string]string{"status": string(doc.Status), "holder": g.HolderID.String()}, nil
}

// grantExecutable refuses the grant's own holder (FM-1): another person approves a grant.
func grantExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	g, err := equity.ReadGrant(doc)
	if err != nil {
		return err
	}
	c, err := u.Contacts().Get(ctx, g.HolderID)
	if err != nil {
		return err
	}
	if c.MemberUserID != nil && *c.MemberUserID == signer {
		return problem(approval.CodeNotApprover, "This is your own grant of options, so another person approves it (FM-1).", "Ask another person who holds the approval right")
	}
	return nil
}

func poolAnswer(w written[PoolResolutionView]) *poolResolutionWriteOutput {
	if w.Previewed {
		return &poolResolutionWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &poolResolutionWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/option-pool/resolutions/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func grantAnswer(w written[OptionGrantView]) *grantWriteOutput {
	if w.Previewed {
		return &grantWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &grantWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/option-grants/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

func (a *API) shownPoolResolution(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (PoolResolutionView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionPool, id)
	if err != nil {
		return PoolResolutionView{}, err
	}
	v, err := a.optionViewOf(ctx, u)
	if err != nil {
		return PoolResolutionView{}, err
	}
	pool, err := equity.PoolOn(ctx, u, v.today)
	if err != nil {
		return PoolResolutionView{}, err
	}
	return a.poolResolutionOf(ctx, u, v, doc, pool.Resolution)
}

func (a *API) shownGrant(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (OptionGrantView, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionGrant, id)
	if err != nil {
		return OptionGrantView{}, err
	}
	v, err := a.optionViewOf(ctx, u)
	if err != nil {
		return OptionGrantView{}, err
	}
	return a.grantOf(ctx, u, v, doc)
}

// grantsSorted are grant views, the latest granted first.
func grantsSorted(all []OptionGrantView) {
	slices.SortStableFunc(all, func(x, y OptionGrantView) int {
		if c := strings.Compare(y.GrantedOn, x.GrantedOn); c != 0 {
			return c
		}
		return strings.Compare(x.ID, y.ID)
	})
}

func registerOptions(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.GetOptionPool, huma.Operation{
		OperationID: "getOptionPool", Method: http.MethodGet, Path: "/v1/option-pool", Tags: []string{"Equity"},
		Summary: "Get the option pool",
		Description: "The option pool at the end of a day (FR-EQ-19, design D9): its size by the latest recorded resolution passed on or before " +
			"it, the class its options are over, the options the recorded grants granted on or before it, what it leaves to grant, and " +
			"every resolution with its status. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *optionPoolInput) (*optionPoolOutput, error) {
		p, _ := authn.From(ctx)
		var out optionPoolOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			day, err := a.capDay(ctx, u, "at", in.At)
			if err != nil {
				return err
			}
			v, err := a.optionViewOf(ctx, u)
			if err != nil {
				return err
			}
			pool, err := equity.PoolOn(ctx, u, day)
			if err != nil {
				return err
			}
			inForce, err := equity.PoolOn(ctx, u, v.today)
			if err != nil {
				return err
			}
			standing, err := equity.GrantsStanding(ctx, u, day)
			if err != nil {
				return err
			}
			out.Body = OptionPoolView{At: day.Format(time.DateOnly), Size: pool.Size.Text('f'), Granted: pool.Granted.Text('f'),
				Returned: pool.Returned.Text('f'), Unallocated: pool.Unallocated.Text('f'), Forfeited: standing.Forfeited.Text('f'),
				Expired: standing.Expired.Text('f'), Exercised: standing.Exercised.Text('f'), Outstanding: standing.Outstanding.Text('f'),
				Exercisable: standing.Exercisable.Text('f'), Resolutions: []PoolResolutionView{}}
			if pool.ClassID != uuid.Nil {
				out.Body.ClassAccountID, out.Body.ClassCode = pool.ClassID.String(), v.codes[pool.ClassID]
			}
			docs, err := equity.PoolResolutions(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				x, err := a.poolResolutionOf(ctx, u, v, d, inForce.Resolution)
				if err != nil {
					return err
				}
				out.Body.Resolutions = append(out.Body.Resolutions, x)
			}
			slices.SortStableFunc(out.Body.Resolutions, func(x, y PoolResolutionView) int {
				if c := strings.Compare(y.ResolvedOn, x.ResolvedOn); c != 0 {
					return c
				}
				return strings.Compare(y.ID, x.ID)
			})
			return nil
		})
		return &out, err
	})

	register(a, authz.CreatePoolResolution, huma.Operation{
		OperationID: "createPoolResolution", Method: http.MethodPost, Path: "/v1/option-pool/resolutions", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a resolution setting the option pool",
		Description: "Drafts a resolution that sets the option pool's size (FR-EQ-19): the shares set aside, the class the options are " +
			"over, the resolution and the day it was passed. Attach the resolution with addPoolResolutionEvidence, then ask another " +
			"person to approve it with requestPoolResolutionApproval. Side effects: the draft, audit events. Every problem comes back " +
			"together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), OptionCodes...), func(ctx context.Context, in *poolResolutionDraftInput) (*poolResolutionWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PoolResolutionView, error) {
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return PoolResolutionView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return PoolResolutionView{}, err
			}
			p, err := poolIn(in.Body)
			if err != nil {
				return PoolResolutionView{}, err
			}
			doc, err := equity.DraftPoolIn(ctx, u, p, s, today)
			if err != nil {
				return PoolResolutionView{}, equityProblems(err)
			}
			return a.shownPoolResolution(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return poolAnswer(w), nil
	})

	register(a, authz.GetPoolResolution, huma.Operation{
		OperationID: "getPoolResolution", Method: http.MethodGet, Path: "/v1/option-pool/resolutions/{resolution_id}", Tags: []string{"Equity"},
		Summary:     "Get a resolution setting the option pool",
		Description: "Returns a pool resolution with its ETag: its size, class, resolution and day, whether it is in force, its status, its files and the approval waiting (FR-EQ-19). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *PoolResolutionPath) (*poolResolutionReadOutput, error) {
		id, err := parseID("resolution_id", in.ResolutionID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out poolResolutionReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionPool, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownPoolResolution(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplacePoolResolution, huma.Operation{
		OperationID: "replacePoolResolution", Method: http.MethodPut, Path: "/v1/option-pool/resolutions/{resolution_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted resolution setting the option pool",
		Description: "Saves a new version of a pool resolution not recorded yet (FR-EQ-19). A recorded one changes no more (EQUITY-05): a " +
			"later resolution sets the pool again. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), OptionCodes...), func(ctx context.Context, in *poolResolutionReviseInput) (*poolResolutionWriteOutput, error) {
		id, err := parseID("resolution_id", in.ResolutionID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PoolResolutionView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentOptionPool, id)
			if err != nil {
				return PoolResolutionView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return PoolResolutionView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return PoolResolutionView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return PoolResolutionView{}, err
			}
			p, err := poolIn(in.Body)
			if err != nil {
				return PoolResolutionView{}, err
			}
			if _, err := equity.RevisePoolIn(ctx, u, id, p, s, today); err != nil {
				return PoolResolutionView{}, equityProblems(err)
			}
			return a.shownPoolResolution(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return poolAnswer(w), nil
	})

	register(a, authz.RequestPoolResolutionApproval, huma.Operation{
		OperationID: "requestPoolResolutionApproval", Method: http.MethodPost, Path: "/v1/option-pool/resolutions/{resolution_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a resolution setting the option pool",
		Description: "Asks another person to approve a drafted pool resolution at the version read (FR-EQ-19, FM-1): the pool is set " +
			"only this way. It needs the resolution attached (EQUITY-06). The answer is 202 with an approval request, which a person " +
			"who holds A on \"Cap table, holders, investors, equity transactions\" approves, stepped up, never the one who asked. Needs a " +
			"person signed in who passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), OptionCodes...),
		func(ctx context.Context, in *poolResolutionAskInput) (*equityAskOutput, error) {
			id, err := parseID("resolution_id", in.ResolutionID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentOptionPool, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the resolution has no evidence attached",
						Fix: "Attach the resolution, then ask again"}
				}
				p, err := equity.ReadPool(doc)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "size", After: p.Size},
					{Field: "resolved_on", After: p.ResolvedOn.Format(time.DateOnly)}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectOptionPool, ObjectKey: doc.ID.String(),
					Operation: approval.OpOptionPoolRecord, Summary: fmt.Sprintf("Set the option pool at %s shares", p.Size),
					Execute: map[string]any{"resolution_id": doc.ID.String(), "version": strconv.Itoa(doc.Version)}, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListOptionGrants, huma.Operation{
		OperationID: "listOptionGrants", Method: http.MethodGet, Path: "/v1/option-grants", Tags: []string{"Equity"},
		Summary: "List grants of options",
		Description: "Every grant of options, drafts and recorded, the latest granted first, a page at a time (FR-EQ-19): its holder, " +
			"number, exercise price, grant date, vesting, expiry, what has vested today and what is outstanding. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *grantListInput) (*grantListOutput, error) {
		p, _ := authn.From(ctx)
		var out grantListOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.optionViewOf(ctx, u)
			if err != nil {
				return err
			}
			docs, err := equity.Grants(ctx, u)
			if err != nil {
				return err
			}
			all := make([]OptionGrantView, 0, len(docs))
			for _, d := range docs {
				x, err := a.grantOf(ctx, u, v, d)
				if err != nil {
					return err
				}
				all = append(all, x)
			}
			grantsSorted(all)
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x OptionGrantView) string { return x.GrantedOn + "|" + x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []OptionGrantView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateOptionGrant, huma.Operation{
		OperationID: "createOptionGrant", Method: http.MethodPost, Path: "/v1/option-grants", Tags: []string{"Equity"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a grant of options",
		Description: "Drafts a grant of options from the pool (FR-EQ-19): its holder (a contact marked investor or shareholder), the " +
			"number, at most what the pool leaves on the grant date, the exercise price per share, the grant date, the vesting (cliff, " +
			"months, acceleration) and the expiry, over the pool's class. Attach the grant letter with addOptionGrantEvidence, then ask " +
			"another person to approve it with requestOptionGrantApproval. Side effects: the draft, audit events. Every problem comes " +
			"back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound}, writes...), OptionCodes...), func(ctx context.Context, in *grantDraftInput) (*grantWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (OptionGrantView, error) {
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return OptionGrantView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return OptionGrantView{}, err
			}
			g, err := grantIn(in.Body)
			if err != nil {
				return OptionGrantView{}, err
			}
			doc, err := equity.DraftGrantIn(ctx, u, g, s, today)
			if err != nil {
				return OptionGrantView{}, equityProblems(err)
			}
			return a.shownGrant(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return grantAnswer(w), nil
	})

	register(a, authz.GetOptionGrant, huma.Operation{
		OperationID: "getOptionGrant", Method: http.MethodGet, Path: "/v1/option-grants/{grant_id}", Tags: []string{"Equity"},
		Summary:     "Get a grant of options",
		Description: "Returns a grant with its ETag: its terms, what has vested today, what is outstanding, its status, its files and the approval waiting (FR-EQ-19). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *OptionGrantPath) (*grantReadOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out grantReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentOptionGrant, id)
			if err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			out.Body, err = a.shownGrant(ctx, u, id)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceOptionGrant, huma.Operation{
		OperationID: "replaceOptionGrant", Method: http.MethodPut, Path: "/v1/option-grants/{grant_id}", Tags: []string{"Equity"},
		Summary: "Change a drafted grant of options",
		Description: "Saves a new version of a grant not recorded yet (FR-EQ-19); its holder stays. A recorded grant changes no more " +
			"(EQUITY-05): its exercises, forfeitures and expiry are its events. Side effects: the new version, audit events. Needs " +
			"If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), OptionCodes...), func(ctx context.Context, in *grantReviseInput) (*grantWriteOutput, error) {
		id, err := parseID("grant_id", in.GrantID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (OptionGrantView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentOptionGrant, id)
			if err != nil {
				return OptionGrantView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return OptionGrantView{}, err
			}
			s, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return OptionGrantView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return OptionGrantView{}, err
			}
			body := in.Body
			body.HolderID = ""
			g, err := grantIn(body)
			if err != nil {
				return OptionGrantView{}, err
			}
			if _, err := equity.ReviseGrantIn(ctx, u, id, g, s, today); err != nil {
				return OptionGrantView{}, equityProblems(err)
			}
			return a.shownGrant(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		return grantAnswer(w), nil
	})

	register(a, authz.RequestOptionGrantApproval, huma.Operation{
		OperationID: "requestOptionGrantApproval", Method: http.MethodPost, Path: "/v1/option-grants/{grant_id}/approval", Tags: []string{"Equity"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for approval to record a grant of options",
		Description: "Asks another person to approve a drafted grant at the version read (FR-EQ-19, FM-1): a grant is recorded only this " +
			"way, within what the pool leaves on its grant date (FY293). It needs the grant letter attached (EQUITY-06). The answer is " +
			"202 with an approval request, which a person who holds A on \"Cap table, holders, investors, equity transactions\" " +
			"approves, stepped up: never the one who asked, nor the holder. The request holds the grant's version, status and holder. " +
			"Needs a person signed in who passed step-up, and If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and asks nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, CodeStepUpRequired, equity.CodeNoEvidence}, writes...), OptionCodes...),
		func(ctx context.Context, in *grantAskInput) (*equityAskOutput, error) {
			id, err := parseID("grant_id", in.GrantID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := equity.Drafted(ctx, u, storage.DocumentOptionGrant, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return Approval{}, err
				}
				if n, err := attachedFiles(ctx, u, id); err != nil {
					return Approval{}, err
				} else if n == 0 {
					return Approval{}, &equity.Error{Code: equity.CodeNoEvidence, Message: "the grant has no evidence attached",
						Fix: "Attach the grant letter, then ask again"}
				}
				g, err := equity.ReadGrant(doc)
				if err != nil {
					return Approval{}, err
				}
				c, err := u.Contacts().Get(ctx, g.HolderID)
				if err != nil {
					return Approval{}, err
				}
				base, err := baseOf(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				diff := []ApprovalDiff{{Field: "status", Before: "draft", After: "recorded"}, {Field: "number", After: g.Number},
					{Field: "exercise_price", After: g.ExercisePrice + " " + base.Code}, {Field: "granted_on", After: g.GrantedOn.Format(time.DateOnly)},
					{Field: "vesting", After: fmt.Sprintf("%d months, %d-month cliff, %s acceleration", g.Vesting.Months, g.Vesting.CliffMonths, g.Vesting.Acceleration)}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectOptionGrant, ObjectKey: doc.ID.String(),
					Operation: approval.OpOptionGrantRecord, Summary: fmt.Sprintf("Grant %s options to %s", g.Number, c.LegalName),
					Execute: map[string]any{"grant_id": doc.ID.String(), "version": strconv.Itoa(doc.Version), "holder_id": g.HolderID.String()},
					Diff:    diff})
			})
			if err != nil {
				return nil, err
			}
			return &equityAskOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.GetMyOptionGrants, huma.Operation{
		OperationID: "getMyOptionGrants", Method: http.MethodGet, Path: "/v1/me/option-grants", Tags: []string{"Equity"},
		Summary: "Get my grants of options",
		Description: "The caller's own recorded grants (FR-EQ-19, Appendix D \"own grants\"): those whose holder is the contact they sign " +
			"in as (linkContactMember), with what has vested today. An Employee reads their own grants this way, and nobody else's. A " +
			"caller who signs in as no holder gets linked false and no grant. Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*myGrantsOutput, error) {
		p, _ := authn.From(ctx)
		var out myGrantsOutput
		out.Body.Items = []OptionGrantView{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if p.UserID == uuid.Nil {
				return nil
			}
			mine, err := u.Contacts().OfMember(ctx, p.UserID)
			if errors.Is(err, ports.ErrNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			out.Body.Linked = true
			ov, err := a.optionViewOf(ctx, u)
			if err != nil {
				return err
			}
			docs, err := equity.Grants(ctx, u)
			if err != nil {
				return err
			}
			for _, d := range docs {
				if d.Status != storage.DocumentPosted {
					continue
				}
				g, err := equity.ReadGrant(d)
				if err != nil {
					return err
				}
				if g.HolderID != mine.ID {
					continue
				}
				x, err := a.grantOf(ctx, u, ov, d)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			grantsSorted(out.Body.Items)
			return nil
		})
		return &out, err
	})
}
