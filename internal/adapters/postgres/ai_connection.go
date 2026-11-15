package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ connections to AI model providers (AI-72)

func (u *unitOfWork) AIConnections() storage.AIConnections { return aiConnections{u} }

type aiConnections struct{ u *unitOfWork }

func toAIConnection(r sqlcdb.FyniqAiConnection) storage.AIConnection {
	c := storage.AIConnection{ID: r.ID, Name: r.Name, Connector: r.Connector, Auth: r.Auth, Configuration: json.RawMessage(r.Configuration),
		Host: r.Host, SelfHosted: r.SelfHosted, CredentialSetAt: r.CredentialSetAt, Billing: r.BillingMode, State: r.State, Version: int(r.Version),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ChangedBy: r.ChangedBy, ChangedAt: r.ChangedAt}
	if r.Preset != nil {
		c.Preset = *r.Preset
	}
	if r.CredentialRef != nil {
		c.CredentialRef = *r.CredentialRef
	}
	if len(r.Quota) > 0 {
		c.Quota = json.RawMessage(r.Quota)
	}
	c.DataHandling = json.RawMessage(r.DataHandling)
	c.AutoQualify = r.AutoQualify
	return c
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// snapshot is what a version keeps of a connection: everything but who changed it, which the version records.
func snapshot(c storage.AIConnection) ([]byte, error) {
	return json.Marshal(map[string]any{"name": c.Name, "connector": c.Connector, "preset": c.Preset, "auth": c.Auth,
		"configuration": c.Configuration, "host": c.Host, "self_hosted": c.SelfHosted, "credential_ref": c.CredentialRef,
		"billing_mode": c.Billing, "quota": c.Quota, "data_handling": handling(c.DataHandling), "auto_qualify": c.AutoQualify,
		"state": c.State})
}

// changes are an audit event's before and after of a connection; the audit trail masks the credential's reference
// (AUD-02), and never sees a value.
func changes(before *storage.AIConnection, after storage.AIConnection) map[string][2]any {
	view := func(c *storage.AIConnection) any {
		if c == nil {
			return nil
		}
		return map[string]any{"name": c.Name, "connector": c.Connector, "preset": c.Preset, "auth": c.Auth, "configuration": string(c.Configuration),
			"host": c.Host, "self_hosted": c.SelfHosted, "credential_ref": c.CredentialRef, "billing_mode": c.Billing, "quota": string(c.Quota),
			"data_handling": string(handling(c.DataHandling)), "auto_qualify": c.AutoQualify, "state": c.State}
	}
	return map[string][2]any{"connection": {view(before), view(&after)}}
}

func (r aiConnections) List(ctx context.Context) ([]storage.AIConnection, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIConnections(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIConnection, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIConnection(row))
	}
	return out, nil
}

func (r aiConnections) Get(ctx context.Context, id uuid.UUID) (storage.AIConnection, error) {
	if err := r.u.check(); err != nil {
		return storage.AIConnection{}, err
	}
	row, err := r.u.q.GetAIConnection(ctx, id)
	if err != nil {
		return storage.AIConnection{}, mapError(err)
	}
	return toAIConnection(row), nil
}

func (r aiConnections) version(ctx context.Context, c storage.AIConnection, step string, approval *uuid.UUID) error {
	snap, err := snapshot(c)
	if err != nil {
		return err
	}
	return mapError(r.u.q.InsertAIConnectionVersion(ctx, sqlcdb.InsertAIConnectionVersionParams{ConnectionID: c.ID, Version: int32(c.Version), // #nosec G115 -- a version counts up from 1
		Step: step, Snapshot: snap, ApprovalID: approval, MadeBy: c.ChangedBy, MadeAt: c.ChangedAt}))
}

func (r aiConnections) Create(ctx context.Context, c storage.AIConnection, approval uuid.UUID) (storage.AIConnection, error) {
	if err := r.u.check(); err != nil {
		return storage.AIConnection{}, err
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.Must(uuid.NewV7())
	}
	c.Version, c.State, c.ChangedBy, c.ChangedAt = 1, "disabled", c.CreatedBy, c.CreatedAt
	if err := r.version(ctx, c, storage.AIConnectionCreated, &approval); err != nil {
		return storage.AIConnection{}, err
	}
	row, err := r.u.q.InsertAIConnection(ctx, sqlcdb.InsertAIConnectionParams{ID: c.ID, Name: c.Name, Connector: c.Connector, Preset: optional(c.Preset),
		Auth: c.Auth, Configuration: c.Configuration, Host: c.Host, SelfHosted: c.SelfHosted, CredentialRef: optional(c.CredentialRef),
		CredentialSetAt: c.CredentialSetAt, BillingMode: c.Billing, Quota: c.Quota, DataHandling: handling(c.DataHandling), AutoQualify: c.AutoQualify,
		CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt})
	if err != nil {
		return storage.AIConnection{}, mapError(err)
	}
	out := toAIConnection(row)
	return out, r.u.event(ctx, "ai_connection.created", "ai_connection", out.ID, &out.Version, changes(nil, out))
}

// aiConnectionEvents are the audit events of each step.
var aiConnectionEvents = map[string]string{storage.AIConnectionChanged: "ai_connection.changed", storage.AIConnectionCredential: "ai_connection.credential_stored",
	storage.AIConnectionEnabled: "ai_connection.enabled", storage.AIConnectionDisabled: "ai_connection.disabled"}

func (r aiConnections) Step(ctx context.Context, c storage.AIConnection, step string, approval *uuid.UUID) (storage.AIConnection, error) {
	if err := r.u.check(); err != nil {
		return storage.AIConnection{}, err
	}
	event, ok := aiConnectionEvents[step]
	if !ok {
		return storage.AIConnection{}, &storage.Error{Code: storage.CodeAIConnection, Message: "not a step of an AI connection: " + step}
	}
	before, err := r.u.q.GetAIConnectionForUpdate(ctx, c.ID)
	if err != nil {
		return storage.AIConnection{}, mapError(err)
	}
	if int(before.Version) != c.Version {
		return storage.AIConnection{}, &storage.Error{Code: storage.CodeVersion, Message: "the AI connection changed since it was read",
			Hint: "Read it again and ask again"}
	}
	next := c
	next.Version = c.Version + 1
	if err := r.version(ctx, next, step, approval); err != nil {
		return storage.AIConnection{}, err
	}
	row, err := r.u.q.UpdateAIConnection(ctx, sqlcdb.UpdateAIConnectionParams{ID: c.ID, Version: int32(c.Version), Name: c.Name, // #nosec G115 -- a version counts up from 1
		Preset: optional(c.Preset), Auth: c.Auth, Configuration: c.Configuration, Host: c.Host, SelfHosted: c.SelfHosted,
		CredentialRef: optional(c.CredentialRef), CredentialSetAt: c.CredentialSetAt, BillingMode: c.Billing, Quota: c.Quota,
		DataHandling: handling(c.DataHandling), AutoQualify: c.AutoQualify, State: c.State,
		ChangedBy: c.ChangedBy, ChangedAt: c.ChangedAt})
	if err != nil {
		return storage.AIConnection{}, mapError(err)
	}
	was, out := toAIConnection(before), toAIConnection(row)
	return out, r.u.event(ctx, event, "ai_connection", out.ID, &out.Version, changes(&was, out))
}

func (r aiConnections) Versions(ctx context.Context, id uuid.UUID) ([]storage.AIConnectionVersion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if _, err := r.u.q.GetAIConnection(ctx, id); err != nil {
		return nil, mapError(err)
	}
	rows, err := r.u.q.AIConnectionVersions(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIConnectionVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AIConnectionVersion{Version: int(row.Version), Step: row.Step, Snapshot: json.RawMessage(row.Snapshot),
			Approval: row.ApprovalID, MadeBy: row.MadeBy, MadeAt: row.MadeAt})
	}
	return out, nil
}

// handling is a declaration as the database keeps it: {} until one is made.
func handling(d json.RawMessage) []byte {
	if len(d) == 0 {
		return []byte(`{}`)
	}
	return d
}

func (u *unitOfWork) AIDataPolicies() storage.AIDataPolicies { return aiDataPolicies{u} }

type aiDataPolicies struct{ u *unitOfWork }

func toAIDataPolicy(r sqlcdb.FyniqAiDataPolicy) storage.AIDataPolicy {
	return storage.AIDataPolicy{ID: r.ID, Version: int(r.Version), Policy: json.RawMessage(r.Policy), Approval: r.ApprovalID, MadeBy: r.MadeBy,
		MadeAt: r.MadeAt}
}

func (r aiDataPolicies) Latest(ctx context.Context) (storage.AIDataPolicy, error) {
	if err := r.u.check(); err != nil {
		return storage.AIDataPolicy{}, err
	}
	row, err := r.u.q.LatestAIDataPolicy(ctx)
	if notFound(err) {
		return storage.AIDataPolicy{Policy: json.RawMessage(`{}`)}, nil
	}
	if err != nil {
		return storage.AIDataPolicy{}, mapError(err)
	}
	return toAIDataPolicy(row), nil
}

func (r aiDataPolicies) Save(ctx context.Context, p storage.AIDataPolicy) (storage.AIDataPolicy, error) {
	if err := r.u.check(); err != nil {
		return storage.AIDataPolicy{}, err
	}
	latest, err := r.Latest(ctx)
	if err != nil {
		return storage.AIDataPolicy{}, err
	}
	if latest.Version != p.Version-1 {
		return storage.AIDataPolicy{}, &storage.Error{Code: storage.CodeVersion, Message: "the data-class policy changed since it was read",
			Hint: "Read it again and ask again"}
	}
	row, err := r.u.q.InsertAIDataPolicy(ctx, sqlcdb.InsertAIDataPolicyParams{Version: int32(p.Version), Policy: p.Policy, ApprovalID: p.Approval, // #nosec G115 -- a version counts up from 1
		MadeBy: p.MadeBy, MadeAt: p.MadeAt})
	if err != nil {
		return storage.AIDataPolicy{}, mapError(err)
	}
	out := toAIDataPolicy(row)
	return out, r.u.event(ctx, "ai_data_policy.changed", "ai_data_policy", out.ID, &out.Version, map[string][2]any{
		"policy": {string(latest.Policy), string(out.Policy)}})
}

func (u *unitOfWork) AIModels() storage.AIModels { return aiModels{u} }

type aiModels struct{ u *unitOfWork }

func toAIModel(r sqlcdb.FyniqAiModel) storage.AIModel {
	m := storage.AIModel{ConnectionID: r.ConnectionID, Model: r.Model, Created: r.ProviderCreated, Shutdown: r.ShutdownAt, Reports: r.Reports,
		FirstListed: r.FirstListedAt, LastListed: r.LastListedAt, Listed: r.Listed, NotCallableAt: r.NotCallableAt, Allowed: r.Allowed,
		ProbedAt: r.ProbedAt, PriceCheckedAt: r.PriceCheckedAt}
	if r.Unpriced != nil {
		m.Unpriced = *r.Unpriced
	}
	if r.ProbeOk != nil {
		m.ProbeOK = *r.ProbeOk
	}
	if r.ProbeDetail != nil {
		m.ProbeDetail = *r.ProbeDetail
	}
	if r.Name != nil {
		m.Name = *r.Name
	}
	if r.AliasOf != nil {
		m.AliasOf = *r.AliasOf
	}
	if r.ContextWindow != nil {
		m.ContextWindow = int(*r.ContextWindow)
	}
	if r.MaxOutput != nil {
		m.MaxOutput = int(*r.MaxOutput)
	}
	return m
}

func toAIModelListRead(r sqlcdb.FyniqAiModelListRead) storage.AIModelListRead {
	out := storage.AIModelListRead{ID: r.ID, ConnectionID: r.ConnectionID, Trigger: r.Trigger, Outcome: r.Outcome, Pages: int(r.Pages),
		Added: int(r.Added), Removed: int(r.Removed), ReadBy: r.ReadBy, ReadAt: r.ReadAt}
	if r.Failure != nil {
		out.Failure = *r.Failure
	}
	return out
}

func positive(n int) *int32 {
	if n <= 0 {
		return nil
	}
	v := int32(min(n, 1<<30)) // #nosec G115 -- capped
	return &v
}

func (r aiModels) Of(ctx context.Context, connection uuid.UUID) ([]storage.AIModel, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIModelsOf(ctx, connection)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIModel(row))
	}
	return out, nil
}

func (r aiModels) Record(ctx context.Context, read storage.AIModelListRead, models []storage.AIModel) (storage.AIModelListRead, error) {
	if err := r.u.check(); err != nil {
		return storage.AIModelListRead{}, err
	}
	for _, m := range models {
		reports := m.Reports
		if reports == nil {
			reports = []string{}
		}
		if err := r.u.q.UpsertAIModel(ctx, sqlcdb.UpsertAIModelParams{ConnectionID: read.ConnectionID, Model: m.Model, Name: optional(m.Name),
			AliasOf: optional(m.AliasOf), ProviderCreated: m.Created, ShutdownAt: m.Shutdown, ContextWindow: positive(m.ContextWindow),
			MaxOutput: positive(m.MaxOutput), Reports: reports, FirstListedAt: m.FirstListed, LastListedAt: m.LastListed, Listed: m.Listed,
			NotCallableAt: m.NotCallableAt, Allowed: m.Allowed}); err != nil {
			return storage.AIModelListRead{}, mapError(err)
		}
	}
	row, err := r.u.q.InsertAIModelListRead(ctx, sqlcdb.InsertAIModelListReadParams{ConnectionID: read.ConnectionID, Trigger: read.Trigger,
		Outcome: read.Outcome, Pages: int32(min(read.Pages, 1<<30)), Added: int32(min(read.Added, 1<<30)), Removed: int32(min(read.Removed, 1<<30)), // #nosec G115 -- capped
		Failure: optional(read.Failure), ReadBy: read.ReadBy, ReadAt: read.ReadAt})
	if err != nil {
		return storage.AIModelListRead{}, mapError(err)
	}
	out := toAIModelListRead(row)
	return out, r.u.event(ctx, "ai_model.listed", "ai_connection", read.ConnectionID, nil, map[string][2]any{"read": {nil, map[string]any{
		"trigger": out.Trigger, "outcome": out.Outcome, "pages": out.Pages, "added": out.Added, "removed": out.Removed, "failure": out.Failure}}})
}

func (r aiModels) Reads(ctx context.Context, connection uuid.UUID, limit int) ([]storage.AIModelListRead, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIModelListReads(ctx, sqlcdb.AIModelListReadsParams{ConnectionID: connection, PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIModelListRead, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAIModelListRead(row))
	}
	return out, nil
}

func (r aiModels) LastComplete(ctx context.Context, connection uuid.UUID) (storage.AIModelListRead, error) {
	if err := r.u.check(); err != nil {
		return storage.AIModelListRead{}, err
	}
	row, err := r.u.q.LastCompleteAIModelListRead(ctx, connection)
	if err != nil {
		return storage.AIModelListRead{}, mapError(err)
	}
	return toAIModelListRead(row), nil
}

func (r aiModels) Probe(ctx context.Context, connection uuid.UUID, model string, ok, refused bool, detail string, at time.Time) (storage.AIModel, error) {
	if err := r.u.check(); err != nil {
		return storage.AIModel{}, err
	}
	var notCallable *time.Time
	if refused {
		notCallable = &at
	}
	if len(detail) > 500 {
		detail = detail[:497] + "…"
	}
	row, err := r.u.q.ProbeAIModel(ctx, sqlcdb.ProbeAIModelParams{ConnectionID: connection, Model: model, ProbedAt: &at, ProbeOk: &ok,
		ProbeDetail: optional(detail), NotCallableAt: notCallable})
	if err != nil {
		return storage.AIModel{}, mapError(err)
	}
	return toAIModel(row), r.u.event(ctx, "ai_model.probed", "ai_connection", connection, nil, map[string][2]any{"probe": {nil, map[string]any{
		"model": model, "ok": ok, "not_callable": refused, "detail": detail}}})
}

func (r aiModels) Allow(ctx context.Context, connection uuid.UUID, model string, allowed bool, approval *uuid.UUID) (storage.AIModel, error) {
	if err := r.u.check(); err != nil {
		return storage.AIModel{}, err
	}
	row, err := r.u.q.AllowAIModel(ctx, sqlcdb.AllowAIModelParams{ConnectionID: connection, Model: model, Allowed: allowed})
	if err != nil {
		return storage.AIModel{}, mapError(err)
	}
	action, change := "ai_model.removed", map[string]any{"model": model}
	if allowed {
		action = "ai_model.allowed"
		if approval != nil {
			change["approval"] = approval.String()
		}
	}
	return toAIModel(row), r.u.event(ctx, action, "ai_connection", connection, nil, map[string][2]any{"allow_list": {nil, change}})
}
