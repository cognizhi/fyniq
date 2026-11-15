package postgres

import (
	"context"
	"encoding/json"

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
		"billing_mode": c.Billing, "quota": c.Quota, "data_handling": handling(c.DataHandling), "state": c.State})
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
			"data_handling": string(handling(c.DataHandling)), "state": c.State}
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
		CredentialSetAt: c.CredentialSetAt, BillingMode: c.Billing, Quota: c.Quota, DataHandling: handling(c.DataHandling), CreatedBy: c.CreatedBy,
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
		DataHandling: handling(c.DataHandling), State: c.State,
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
