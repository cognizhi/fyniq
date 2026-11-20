package nemura

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/packs"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/policy"
	"github.com/cognizhi/fyniq/internal/ports"
)

// AcceptPack records that the organisation accepts a version of a pack: it verifies nothing, the publisher does (NM-PACK-02, NM-PACK-07).
func AcceptPack(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, version string) error {
	id := uuid.Must(uuid.NewV7())
	pk, _, _ := strings.Cut(version, "@")
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pack_acceptance (id, pack, version, accepted_by) VALUES ($1, $2, $3, $4)", id, pk, version, by); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return &people.Refusal{Code: people.CodeExists, Message: "The organisation has accepted that version already."}
		}
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.pack.accepted", audit.Object{Type: "pack_acceptance", ID: id}, map[string]audit.Change{"version": {nil, version}}, "")
}

// DecidePack records the company's decision to use a value that is only researched (NM-PACK-07).
func DecidePack(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, version, table, key, reason string) error {
	if len(strings.TrimSpace(reason)) < 10 {
		return &people.Refusal{Code: people.CodeInvalid, Message: "A decision to use a researched value says why, in at least a sentence."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pack_decision (id, version, table_name, row_key, decided_by, reason) VALUES ($1, $2, $3, $4, $5, $6)", id, version, table, key, by, reason); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return &people.Refusal{Code: people.CodeExists, Message: "The company has recorded a decision on that value already."}
		}
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.pack.value_decided", audit.Object{Type: "pack_decision", ID: id},
		map[string]audit.Change{"version": {nil, version}, "table": {nil, table}, "key": {nil, key}}, reason)
}

// PackDecisions are the organisation's recorded decisions.
func PackDecisions(ctx context.Context, tx pgx.Tx) ([]checks.Decision, error) {
	rows, err := tx.Query(ctx, "SELECT version, table_name, row_key FROM nemura.pack_decision ORDER BY decided_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.Decision
	for rows.Next() {
		var d checks.Decision
		if err := rows.Scan(&d.Version, &d.Table, &d.Key); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetPolicy starts the company's policy on a day against the statute of the pack version in force: a policy below the statute for any rule is refused with
// every rule it breaks, a policy above it says so (NM-PACK-04, NM-PACK-08). The policy in force before is ended the day before.
func SetPolicy(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, from time.Time, r policy.Rules) (policy.Result, error) {
	res := policy.Check(v, r, from)
	if len(res.Breaches) > 0 {
		var msgs []string
		for _, b := range res.Breaches {
			msgs = append(msgs, b.Message)
		}
		return res, &people.Refusal{Code: policy.CodeBelowStatute, Message: "The policy gives less than the statute: " + strings.Join(msgs, " ")}
	}
	var open uuid.UUID
	err := tx.QueryRow(ctx, "UPDATE nemura.company_policy SET effective_to = $1 WHERE effective_to IS NULL AND effective_from < $1 RETURNING id", day(from)).Scan(&open)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.company_policy").Scan(&n); err != nil {
			return res, err
		}
		if n > 0 {
			return res, &people.Refusal{Code: people.CodeHistory, Message: "The company has a policy that began on or after that day: choose a later day."}
		}
	case err != nil:
		return res, historyError(err)
	default:
		if err := Audit(ctx, tx, org, at, user(by), "nemura.policy.ended", audit.Object{Type: "company_policy", ID: open}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
			return res, err
		}
	}
	body, err := json.Marshal(r)
	if err != nil {
		return res, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.company_policy (id, effective_from, rules, pack_version, created_by) VALUES ($1, $2, $3, $4, $5)", id, day(from), body, v.ID, by); err != nil {
		return res, historyError(err)
	}
	return res, Audit(ctx, tx, org, at, user(by), "nemura.policy.set", audit.Object{Type: "company_policy", ID: id},
		map[string]audit.Change{"effective_from": {nil, day(from)}, "pack_version": {nil, v.ID}}, "")
}

// PolicyOn is the company's policy in force on a day, with the pack version it was checked against, or false.
func PolicyOn(ctx context.Context, tx pgx.Tx, on time.Time) (policy.Rules, string, time.Time, bool, error) {
	var body []byte
	var version string
	var from time.Time
	err := tx.QueryRow(ctx, `SELECT rules, pack_version, effective_from FROM nemura.company_policy WHERE effective_from <= $1 AND (effective_to IS NULL OR effective_to > $1)`, day(on)).Scan(&body, &version, &from)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy.Rules{}, "", time.Time{}, false, nil
	}
	if err != nil {
		return policy.Rules{}, "", time.Time{}, false, err
	}
	var r policy.Rules
	return r, version, from, true, json.Unmarshal(body, &r)
}

// PassFacts are the work passes with what the check needs about their people.
func PassFacts(ctx context.Context, tx pgx.Tx) ([]checks.Pass, error) {
	rows, err := tx.Query(ctx, `SELECT p.person_id::text, p.pass_type, p.expires_on, per.leaving_date,
 EXISTS (SELECT 1 FROM nemura.work_pass n WHERE n.person_id = p.person_id AND n.pass_type = p.pass_type AND n.expires_on > p.expires_on)
FROM nemura.work_pass p JOIN nemura.person per ON per.organisation_id = p.organisation_id AND per.id = p.person_id ORDER BY p.person_id, p.expires_on`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.Pass
	for rows.Next() {
		var p checks.Pass
		if err := rows.Scan(&p.Person, &p.Type, &p.Expires, &p.LeavingDate, &p.RenewedLater); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PacksService is the packs, policy and checks behind the API.
type PacksService struct {
	Store    *Store
	Clock    ports.Clock
	versions []pack.Version
}

var _ api.Packs = (*PacksService)(nil)

// NewPacksService loads Nemura's packs; a pack that does not validate stops the service from starting.
func NewPacksService(store *Store, clock ports.Clock) (*PacksService, error) {
	v, err := pack.LoadAll(packs.Files())
	if err != nil {
		return nil, fmt.Errorf("nemura packs: %w", err)
	}
	return &PacksService{Store: store, Clock: clock, versions: v}, nil
}

// inForce is the newest version of a pack in force on a day.
func (p *PacksService) inForce(pk string, on time.Time) (pack.Version, bool) {
	var best pack.Version
	found := false
	for _, v := range p.versions {
		if v.Pack == pk && !v.EffectiveFrom.After(on) && (v.EffectiveTo == nil || !v.EffectiveTo.Before(on)) && (!found || v.ID > best.ID) {
			best, found = v, true
		}
	}
	return best, found
}

func (p *PacksService) find(id string) (pack.Version, bool) {
	for _, v := range p.versions {
		if v.ID == id {
			return v, true
		}
	}
	return pack.Version{}, false
}

func (p *PacksService) Versions(ctx context.Context, s api.Session) ([]api.PackVersion, error) {
	var out []api.PackVersion
	err := p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		decided, err := PackDecisions(ctx, tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT version FROM nemura.pack_acceptance")
		if err != nil {
			return err
		}
		accepted := map[string]bool{}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			accepted[v] = true
		}
		rows.Close()
		for _, v := range p.versions {
			pv := api.PackVersion{ID: v.ID, Title: v.Title, EffectiveFrom: v.EffectiveFrom.Format(time.DateOnly), ReviewDate: v.ReviewDate.Format(time.DateOnly), Verifier: v.Verifier, Accepted: accepted[v.ID], Values: []api.PackValue{}, Sources: []api.PackSource{}}
			for _, s := range v.Sources {
				pv.Sources = append(pv.Sources, api.PackSource{ID: s.ID, Title: s.Title, URL: s.URL, Section: s.Section, Retrieved: s.Retrieved, Quote: s.Quote})
			}
			have := map[checks.Decision]bool{}
			for _, d := range decided {
				have[d] = true
			}
			for _, u := range pack.Unverified(v) {
				pv.Values = append(pv.Values, api.PackValue{Table: u.Table, Key: u.Key, Status: u.Status, Decided: have[checks.Decision{Version: v.ID, Table: u.Table, Key: u.Key}]})
			}
			pv.Researched = len(pv.Values)
			out = append(out, pv)
		}
		return nil
	})
	return out, err
}

func (p *PacksService) Accept(ctx context.Context, s api.Session, version string) error {
	if _, ok := p.find(version); !ok {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such pack version."}
	}
	return p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return AcceptPack(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, version)
	})
}

func (p *PacksService) Decide(ctx context.Context, s api.Session, version, table, key, reason string) error {
	v, ok := p.find(version)
	if !ok {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such pack version."}
	}
	known := false
	for _, u := range pack.Unverified(v) {
		known = known || (u.Table == table && u.Key == key)
	}
	if !known {
		return &people.Refusal{Code: people.CodeInvalid, Message: "That value is not a researched value of the version: a verified value needs no decision."}
	}
	return p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return DecidePack(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, version, table, key, reason)
	})
}

func (p *PacksService) SetPolicy(ctx context.Context, s api.Session, from time.Time, r policy.Rules) ([]string, error) {
	v, ok := p.inForce("sg-people", from)
	if !ok {
		return nil, &people.Refusal{Code: people.CodeInvalid, Message: "No pack is in force on that day: choose a day on or after the pack's first day."}
	}
	var notes []string
	err := p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		res, err := SetPolicy(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, v, from, r)
		for _, n := range res.Notes {
			notes = append(notes, n.Message)
		}
		return err
	})
	return notes, err
}

func (p *PacksService) Policy(ctx context.Context, s api.Session, on time.Time) (policy.Rules, string, bool, error) {
	var r policy.Rules
	var version string
	var found bool
	err := p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		r, version, _, found, err = PolicyOn(ctx, tx, on)
		return err
	})
	return r, version, found, err
}

func (p *PacksService) Checks(ctx context.Context, s api.Session, on time.Time) ([]checks.Finding, error) {
	var out []checks.Finding
	err := p.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		passes, err := PassFacts(ctx, tx)
		if err != nil {
			return err
		}
		out = append(out, checks.WorkPasses(passes, on)...)
		if v, ok := p.inForce("sg-people", on); ok {
			out = append(out, checks.Calendar(v, on)...)
			waiting, negative, err := LeaveFacts(ctx, tx)
			if err != nil {
				return err
			}
			out = append(out, checks.LeaveWaiting(v, waiting, negative, on)...)
			if days, ok := pack.TimingDays(v, "kets_days_after_start", on); ok {
				due, err := KETFacts(ctx, tx)
				if err != nil {
					return err
				}
				out = append(out, checks.KETs(due, days, on)...)
			}
			if limit, ok := pack.PartIV(v, "overtime_hours_per_month", on); ok {
				months, err := OvertimeMonths(ctx, tx)
				if err != nil {
					return err
				}
				out = append(out, checks.OvertimeOverLimit(months, limit)...)
			}
		}
		payments, err := PaymentFacts(ctx, tx)
		if err != nil {
			return err
		}
		out = append(out, checks.Payments(payments)...)
		linkFindings, err := LinkFindings(ctx, tx, on)
		if err != nil {
			return err
		}
		out = append(out, linkFindings...)
		calendar, err := CalendarFacts(ctx, tx, on)
		if err != nil {
			return err
		}
		finals, err := FinalPayFacts(ctx, tx, func(day time.Time) (pack.Version, bool) { return p.inForce("sg-people", day) })
		if err != nil {
			return err
		}
		out = append(out, checks.FinalPay(finals, on)...)
		if v, ok := p.inForce("sg-people", on); ok {
			if days, ok := pack.TimingDays(v, "salary_days_after_period", on); ok {
				out = append(out, checks.PayrollCalendar(calendar, days, on)...)
			}
			if days, ok := pack.TimingDays(v, "payslip_working_days_after_payment", on); ok {
				out = append(out, checks.Payslips(v, payments, days, on)...)
			}
		}
		claims, err := GovClaimFacts(ctx, tx)
		if err != nil {
			return err
		}
		out = append(out, checks.GovClaims(claims, on)...)
		flex, err := FlexFacts(ctx, tx)
		if err != nil {
			return err
		}
		out = append(out, checks.FlexReplies(flex, on)...)
		rules, version, from, found, err := PolicyOn(ctx, tx, on)
		if err != nil || !found {
			return err
		}
		v, ok := p.find(version)
		if !ok {
			return nil
		}
		decided, err := PackDecisions(ctx, tx)
		if err != nil {
			return err
		}
		out = append(out, checks.UnverifiedInUse(v.ID, policy.Check(v, rules, from).Used, decided)...)
		return nil
	})
	return out, err
}
