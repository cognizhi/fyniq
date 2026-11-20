package postgres

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the link to Nemura, its inbox and the projection (FR-PPL-06, 15, 16)

func (u *unitOfWork) PeopleLinks() storage.PeopleLinks { return peopleLinks{u} }

type peopleLinks struct{ u *unitOfWork }

type linkRow struct {
	id, nemura, approval uuid.UUID
	client               *uuid.UUID
	state, createdBy     string
	origin               string
	epoch, version       int32
	key                  []byte
	created, changed     time.Time
}

func toLink(r linkRow) storage.PeopleLinkRow {
	out := storage.PeopleLinkRow{ID: r.id, State: r.state, Epoch: int(r.epoch), NemuraOrganisationID: r.nemura, NemuraOrigin: r.origin, LinkKey: slices.Clone(r.key), ApprovalID: r.approval,
		Version: int(r.version), CreatedBy: r.createdBy, CreatedAt: r.created, ChangedAt: r.changed}
	if r.client != nil {
		out.ClientID = *r.client
	}
	return out
}

// linkView is what the audit trail keeps of a link: never the key's bytes, only that one was given.
func linkView(l storage.PeopleLinkRow) any {
	return map[string]any{"state": l.State, "epoch": l.Epoch, "nemura_organisation_id": l.NemuraOrganisationID.String(), "client_id": l.ClientID.String(),
		"approval_id": l.ApprovalID.String()}
}

func optionalID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (r peopleLinks) Live(ctx context.Context) (storage.PeopleLinkRow, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkRow{}, err
	}
	x, err := r.u.q.LivePeopleLink(ctx)
	if err != nil {
		return storage.PeopleLinkRow{}, mapError(err)
	}
	return toLink(linkRow{x.ID, x.NemuraOrganisationID, x.ApprovalID, x.ClientID, x.State, x.CreatedBy, x.NemuraOrigin, x.Epoch, x.Version, x.LinkKey, x.CreatedAt, x.ChangedAt}), nil
}

func (r peopleLinks) Link(ctx context.Context, l storage.PeopleLinkRow) (storage.PeopleLinkRow, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkRow{}, err
	}
	last, err := r.u.q.LastPeopleLinkEpoch(ctx)
	if err != nil {
		return storage.PeopleLinkRow{}, mapError(err)
	}
	x, err := r.u.q.InsertPeopleLink(ctx, sqlcdb.InsertPeopleLinkParams{Epoch: last + 1, NemuraOrganisationID: l.NemuraOrganisationID, NemuraOrigin: l.NemuraOrigin, LinkKey: l.LinkKey,
		ClientID: optionalID(l.ClientID), ApprovalID: l.ApprovalID, CreatedBy: l.CreatedBy, At: l.CreatedAt})
	if err != nil {
		return storage.PeopleLinkRow{}, mapError(err)
	}
	out := toLink(linkRow{x.ID, x.NemuraOrganisationID, x.ApprovalID, x.ClientID, x.State, x.CreatedBy, x.NemuraOrigin, x.Epoch, x.Version, x.LinkKey, x.CreatedAt, x.ChangedAt})
	return out, r.u.event(ctx, "people_link.linked", "people_link", out.ID, &out.Version, map[string][2]any{"link": {nil, linkView(out)}})
}

func (r peopleLinks) SetState(ctx context.Context, id uuid.UUID, version int, state string, client uuid.UUID, at time.Time) (storage.PeopleLinkRow, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkRow{}, err
	}
	b, err := r.u.q.PeopleLinkForUpdate(ctx, id)
	if err != nil {
		return storage.PeopleLinkRow{}, mapError(err)
	}
	if int(b.Version) != version {
		return storage.PeopleLinkRow{}, &storage.Error{Code: storage.CodeVersion, Message: "the link changed since it was read", Hint: "Read it again and try again"}
	}
	before := toLink(linkRow{b.ID, b.NemuraOrganisationID, b.ApprovalID, b.ClientID, b.State, b.CreatedBy, b.NemuraOrigin, b.Epoch, b.Version, b.LinkKey, b.CreatedAt, b.ChangedAt})
	x, err := r.u.q.SetPeopleLinkState(ctx, sqlcdb.SetPeopleLinkStateParams{State: state, ClientID: optionalID(client), At: at, ID: id, Version: int32(version)}) // #nosec G115 -- a version counts up from 1
	if err != nil {
		return storage.PeopleLinkRow{}, mapError(err)
	}
	out := toLink(linkRow{x.ID, x.NemuraOrganisationID, x.ApprovalID, x.ClientID, x.State, x.CreatedBy, x.NemuraOrigin, x.Epoch, x.Version, x.LinkKey, x.CreatedAt, x.ChangedAt})
	return out, r.u.event(ctx, "people_link."+state, "people_link", out.ID, &out.Version, map[string][2]any{"link": {linkView(before), linkView(out)}})
}

func (r peopleLinks) SaveKeys(ctx context.Context, k storage.PeopleLinkKeys, sec storage.PeopleLinkSecrets) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.InsertPeopleLinkKey(ctx, sqlcdb.InsertPeopleLinkKeyParams{LinkID: k.LinkID, NemuraClientID: k.NemuraClientID, LinkPublic: k.LinkPublic, ClientPublic: k.ClientPublic}); err != nil {
		return mapError(err)
	}
	if err := r.u.q.InsertPeopleLinkSecret(ctx, sqlcdb.InsertPeopleLinkSecretParams{LinkID: sec.LinkID, LinkPrivate: sec.LinkPrivate, ClientPrivate: sec.ClientPrivate}); err != nil {
		return mapError(err)
	}
	// The audit event says that the link has keys and which client they are for; no key, public or private.
	return r.u.event(ctx, "people_link.keys_made", "people_link_key", k.LinkID, nil, map[string][2]any{"nemura_client_id": {nil, k.NemuraClientID}})
}

func (r peopleLinks) Keys(ctx context.Context, link uuid.UUID) (storage.PeopleLinkKeys, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkKeys{}, err
	}
	x, err := r.u.q.PeopleLinkKeys(ctx, link)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PeopleLinkKeys{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PeopleLinkKeys{}, mapError(err)
	}
	return storage.PeopleLinkKeys{LinkID: x.LinkID, NemuraClientID: x.NemuraClientID, LinkPublic: slices.Clone(x.LinkPublic), ClientPublic: slices.Clone(x.ClientPublic), Sequence: x.Sequence}, nil
}

func (r peopleLinks) NextSequence(ctx context.Context, link uuid.UUID) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.NextPeopleLinkSequence(ctx, link)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ports.ErrNotFound
	}
	return n, mapError(err)
}

func (r peopleLinks) Secrets(ctx context.Context, link uuid.UUID) (storage.PeopleLinkSecrets, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkSecrets{}, err
	}
	x, err := r.u.q.PeopleLinkSecret(ctx, link)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PeopleLinkSecrets{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PeopleLinkSecrets{}, mapError(err)
	}
	return storage.PeopleLinkSecrets{LinkID: x.LinkID, LinkPrivate: slices.Clone(x.LinkPrivate), ClientPrivate: slices.Clone(x.ClientPrivate)}, nil
}

func (r peopleLinks) SaveAskKey(ctx context.Context, k storage.PeopleLinkAskKey) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.InsertPeopleLinkAskKey(ctx, sqlcdb.InsertPeopleLinkAskKeyParams{LinkID: k.LinkID, NemuraAskClientID: k.ClientID, AskPublic: k.Public, AskPrivate: k.PrivateDER}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "people_link.ask_key_made", "people_link_ask_key", k.LinkID, nil, map[string][2]any{"nemura_ask_client_id": {nil, k.ClientID}})
}

func (r peopleLinks) AskKey(ctx context.Context, link uuid.UUID) (storage.PeopleLinkAskKey, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleLinkAskKey{}, err
	}
	x, err := r.u.q.PeopleLinkAskKey(ctx, link)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PeopleLinkAskKey{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PeopleLinkAskKey{}, mapError(err)
	}
	return storage.PeopleLinkAskKey{LinkID: x.LinkID, ClientID: x.NemuraAskClientID, Public: slices.Clone(x.AskPublic), PrivateDER: slices.Clone(x.AskPrivate)}, nil
}

func (r peopleLinks) SetSource(ctx context.Context, s storage.PeopleSource) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.InsertPeopleSource(ctx, sqlcdb.InsertPeopleSourceParams{Feature: s.Feature, Source: s.Source, EffectiveFrom: s.EffectiveFrom, ApprovalID: s.ApprovalID, SetAt: s.SetAt}); err != nil {
		return mapError(err)
	}
	// The row's own ID is its audit object: read back the newest of the feature.
	rows, err := r.u.q.PeopleSourcesInForce(ctx)
	if err != nil {
		return mapError(err)
	}
	for _, x := range rows {
		if x.Feature == s.Feature {
			return r.u.event(ctx, "people_source.set", "people_source", x.ID, nil, map[string][2]any{"feature": {nil, s.Feature}, "source": {nil, s.Source},
				"effective_from": {nil, s.EffectiveFrom.Format(time.DateOnly)}})
		}
	}
	return errors.New("people source: the row just written is not there")
}

func (r peopleLinks) Sources(ctx context.Context) ([]storage.PeopleSource, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PeopleSourcesInForce(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PeopleSource, len(rows))
	for i, x := range rows {
		out[i] = storage.PeopleSource{Feature: x.Feature, Source: x.Source, EffectiveFrom: x.EffectiveFrom, ApprovalID: x.ApprovalID, SetAt: x.SetAt}
	}
	return out, nil
}

func (r peopleLinks) Receive(ctx context.Context, e storage.InboxEntry) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	n, err := r.u.q.InsertPeopleInbox(ctx, sqlcdb.InsertPeopleInboxParams{EventID: e.EventID, LinkID: e.LinkID, Epoch: int32(e.Epoch), Sequence: e.Sequence, // #nosec G115 -- bounded by the CHECK
		Type: e.Type, ObjectType: e.ObjectType, ObjectID: e.ObjectID, ObjectVersion: int32(e.ObjectVersion), PayloadSha256: e.PayloadSHA256, Outcome: e.Outcome, // #nosec G115 -- a version
		Code: optional(e.Code), ReceivedAt: e.ReceivedAt})
	if err != nil {
		return false, mapError(err)
	}
	return n == 1, nil
}

func (r peopleLinks) InboxCounts(ctx context.Context, link uuid.UUID) ([]storage.InboxCount, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PeopleInboxCounts(ctx, link)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InboxCount, len(rows))
	for i, x := range rows {
		out[i] = storage.InboxCount{Outcome: x.Outcome, N: int(x.N)}
		if t, ok := x.LastAt.(time.Time); ok {
			out[i].LastAt = t
		}
	}
	return out, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func idOf(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

func toPerson(id uuid.UUID, version int32, name, email *string, dept, cc *uuid.UUID, started, left pgtype.Date, status *string, manager *uuid.UUID, removed bool,
	holdVersion int32, holdID *uuid.UUID, held bool, holdFrom pgtype.Date, bankVersion int32, bankRecord, user *uuid.UUID, at time.Time) peoplelink.Person {
	return peoplelink.Existing(peoplelink.Person{ID: id, Version: int(version), DisplayName: strOf(name), WorkEmail: strOf(email), DepartmentValue: idOf(dept), CostCentreValue: idOf(cc),
		StartedOn: nullDate(started), LeftOn: nullDate(left), Status: strOf(status), LineManager: idOf(manager), Removed: removed, HoldVersion: int(holdVersion), HoldID: idOf(holdID), Held: held,
		HoldFrom: nullDate(holdFrom), BankVersion: int(bankVersion), BankRecordID: idOf(bankRecord), UserID: idOf(user), UpdatedAt: at})
}

func (r peopleLinks) Person(ctx context.Context, id uuid.UUID) (peoplelink.Person, error) {
	if err := r.u.check(); err != nil {
		return peoplelink.Person{}, err
	}
	x, err := r.u.q.PersonProjectionForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return peoplelink.Person{ID: id}, nil
	}
	if err != nil {
		return peoplelink.Person{}, mapError(err)
	}
	return toPerson(x.PersonID, x.Version, x.DisplayName, x.WorkEmail, x.DepartmentValueID, x.CostCentreValueID, x.StartedOn, x.LeftOn, x.Status, x.LineManagerPersonID,
		x.Removed, x.HoldVersion, x.HoldID, x.Held, x.HoldFrom, x.BankVersion, x.BankRecordID, x.UserID, x.UpdatedAt), nil
}

func (r peopleLinks) People(ctx context.Context) ([]peoplelink.Person, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PersonProjections(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]peoplelink.Person, len(rows))
	for i, x := range rows {
		out[i] = toPerson(x.PersonID, x.Version, x.DisplayName, x.WorkEmail, x.DepartmentValueID, x.CostCentreValueID, x.StartedOn, x.LeftOn, x.Status, x.LineManagerPersonID,
			x.Removed, x.HoldVersion, x.HoldID, x.Held, x.HoldFrom, x.BankVersion, x.BankRecordID, x.UserID, x.UpdatedAt)
	}
	return out, nil
}

func (r peopleLinks) PersonOfUser(ctx context.Context, user uuid.UUID) (peoplelink.Person, error) {
	if err := r.u.check(); err != nil {
		return peoplelink.Person{}, err
	}
	x, err := r.u.q.PersonProjectionByUser(ctx, &user)
	if errors.Is(err, pgx.ErrNoRows) {
		return peoplelink.Person{}, ports.ErrNotFound
	}
	if err != nil {
		return peoplelink.Person{}, mapError(err)
	}
	return toPerson(x.PersonID, x.Version, x.DisplayName, x.WorkEmail, x.DepartmentValueID, x.CostCentreValueID, x.StartedOn, x.LeftOn, x.Status, x.LineManagerPersonID,
		x.Removed, x.HoldVersion, x.HoldID, x.Held, x.HoldFrom, x.BankVersion, x.BankRecordID, x.UserID, x.UpdatedAt), nil
}

// changedFields names the fields that differ: the audit trail keeps these names, never a person's values (golden rule 7, FR-PPL-15).
func changedFields(a, b peoplelink.Person) []string {
	var out []string
	add := func(name string, differs bool) {
		if differs {
			out = append(out, name)
		}
	}
	add("display_name", a.DisplayName != b.DisplayName)
	add("work_email", a.WorkEmail != b.WorkEmail)
	add("department", a.DepartmentValue != b.DepartmentValue)
	add("cost_centre", a.CostCentreValue != b.CostCentreValue)
	add("dates", (a.StartedOn == nil) != (b.StartedOn == nil) || (a.StartedOn != nil && !a.StartedOn.Equal(*b.StartedOn)) ||
		(a.LeftOn == nil) != (b.LeftOn == nil) || (a.LeftOn != nil && !a.LeftOn.Equal(*b.LeftOn)))
	add("status", a.Status != b.Status)
	add("line_manager", a.LineManager != b.LineManager)
	add("removed", a.Removed != b.Removed)
	add("hold", a.HoldVersion != b.HoldVersion)
	add("bank_details", a.BankVersion != b.BankVersion)
	add("user", a.UserID != b.UserID)
	return out
}

func (r peopleLinks) SavePerson(ctx context.Context, p peoplelink.Person) error {
	if err := r.u.check(); err != nil {
		return err
	}
	before, err := r.Person(ctx, p.ID)
	if err != nil {
		return err
	}
	if err := r.u.q.UpsertPersonProjection(ctx, sqlcdb.UpsertPersonProjectionParams{PersonID: p.ID, Version: int32(p.Version), DisplayName: strPtr(p.DisplayName), // #nosec G115 -- versions count up from 0
		WorkEmail: strPtr(p.WorkEmail), DepartmentValueID: optionalID(p.DepartmentValue), CostCentreValueID: optionalID(p.CostCentreValue), StartedOn: pgDate(p.StartedOn),
		LeftOn: pgDate(p.LeftOn), Status: strPtr(p.Status), LineManagerPersonID: optionalID(p.LineManager), Removed: p.Removed, HoldVersion: int32(p.HoldVersion), // #nosec G115 -- as above
		HoldID: optionalID(p.HoldID), Held: p.Held, HoldFrom: pgDate(p.HoldFrom), BankVersion: int32(p.BankVersion), BankRecordID: optionalID(p.BankRecordID), // #nosec G115 -- as above
		UserID: optionalID(p.UserID), UpdatedAt: p.UpdatedAt}); err != nil {
		return mapError(err)
	}
	fields := changedFields(before, p)
	v := p.Version
	return r.u.event(ctx, "person_projection.changed", "person_projection", p.ID, &v, map[string][2]any{"fields": {nil, fields}})
}

func peopleAccessOf(x sqlcdb.PeopleLinkAccessRow) storage.PeopleAccess {
	return storage.PeopleAccess{ID: x.ID, Issuer: x.Issuer, Subject: x.Subject, PeopleAccess: x.PeopleAccess, PayrollApprover: x.PayrollApprover, Version: x.Version, Epoch: int(x.Epoch), ReceivedAt: x.ReceivedAt}
}

func (r peopleLinks) Access(ctx context.Context, issuer, subject string) (storage.PeopleAccess, error) {
	if err := r.u.check(); err != nil {
		return storage.PeopleAccess{}, err
	}
	x, err := r.u.q.PeopleLinkAccess(ctx, sqlcdb.PeopleLinkAccessParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PeopleAccess{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PeopleAccess{}, mapError(err)
	}
	return peopleAccessOf(sqlcdb.PeopleLinkAccessRow(x)), nil
}

func (r peopleLinks) AccessList(ctx context.Context) ([]storage.PeopleAccess, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PeopleLinkAccessList(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PeopleAccess, len(rows))
	for i, x := range rows {
		out[i] = peopleAccessOf(sqlcdb.PeopleLinkAccessRow(x))
	}
	return out, nil
}

func (r peopleLinks) SaveAccess(ctx context.Context, a storage.PeopleAccess) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	id, err := r.u.q.SavePeopleLinkAccess(ctx, sqlcdb.SavePeopleLinkAccessParams{Issuer: a.Issuer, Subject: a.Subject, PeopleAccess: a.PeopleAccess, PayrollApprover: a.PayrollApprover, Version: a.Version, Epoch: int32(a.Epoch), ReceivedAt: a.ReceivedAt}) // #nosec G115 -- an epoch is small
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // not newer than what is held
	}
	if err != nil {
		return false, mapError(err)
	}
	return true, r.u.event(ctx, "people_link_access.changed", "people_link_access", id, nil, map[string][2]any{"people_access": {nil, a.PeopleAccess}, "payroll_approver": {nil, a.PayrollApprover}})
}

func (r peopleLinks) InboxSequences(ctx context.Context, link uuid.UUID, from, to int64) ([]int64, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	out, err := r.u.q.PeopleInboxSequences(ctx, sqlcdb.PeopleInboxSequencesParams{LinkID: link, FromSequence: from, ToSequence: to})
	return out, mapError(err)
}

func (r peopleLinks) RecordReplay(ctx context.Context, link uuid.UUID, from, to int64, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	id, err := r.u.q.InsertPeopleLinkReplay(ctx, sqlcdb.InsertPeopleLinkReplayParams{LinkID: link, FromSequence: from, ToSequence: to, AskedAt: at})
	if err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "people_link_replay.asked", "people_link_replay", id, nil, map[string][2]any{"from_sequence": {nil, from}, "to_sequence": {nil, to}})
}

func (r peopleLinks) ReplaysSince(ctx context.Context, link uuid.UUID, since time.Time) ([][2]int64, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PeopleLinkReplaysSince(ctx, sqlcdb.PeopleLinkReplaysSinceParams{LinkID: link, Since: since})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([][2]int64, len(rows))
	for i, x := range rows {
		out[i] = [2]int64{x.FromSequence, x.ToSequence}
	}
	return out, nil
}
