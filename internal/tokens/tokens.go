// Package tokens revokes a user's OAuth tokens when what they were granted on changes (AI-38): a change
// of roles, a deactivation, a client removed or a kill switch. The revocation is an outbox row written
// in the same unit of work as the change, so it is never lost and never happens for a change that
// rolled back; the worker then asks the OAuth server to revoke every token of the user. With
// introspection cached for at most 30 seconds (authn.MaxCache), the user's next call is denied within
// 60 seconds.
package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of a revocation, run by the worker class.
const Kind = "oauth.subject.revoke"

// ActionRevokeRequested is the audit action recorded with the change that asked for the revocation.
const ActionRevokeRequested = "oauth.tokens.revoke_requested"

type payload struct {
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
}

// RevokeAll asks, in the unit of work u, for every token of subject to be revoked once u commits, and
// records why in the organisation's audit trail.
func RevokeAll(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, subject, reason string) error {
	if subject == "" || reason == "" {
		return errors.New("tokens: a revocation needs a subject and a reason")
	}
	b, err := json.Marshal(payload{Subject: subject, Reason: reason})
	if err != nil {
		return err
	}
	if _, err := d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b}); err != nil {
		return err
	}
	return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionRevokeRequested, ObjectType: "organisation", ObjectID: org,
		Changes: map[string][2]any{"subject": {nil, subject}, "reason": {nil, reason}}})
}

// Work is the worker's handler of Kind.
func Work(o ports.OAuthServer) outbox.Work {
	return func(ctx context.Context, _ uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.Subject == "" {
			return fmt.Errorf("tokens: outbox row %s is not a revocation", row.ID)
		}
		return o.RevokeSubject(ctx, p.Subject)
	}
}

// RemoveClientKind is the outbox kind that deletes an API client and its tokens from the OAuth server after Fyniq revoked
// it (FR-INT-02), run by the worker class. The API refuses the client's tokens from the revocation on; this tidies the
// OAuth server.
const RemoveClientKind = "oauth.client.remove"

type removal struct {
	ClientID string `json:"client_id"`
}

// RemoveClientItem is the outbox item that deletes clientID from the OAuth server.
func RemoveClientItem(clientID string) (storage.NewOutboxItem, error) {
	if clientID == "" {
		return storage.NewOutboxItem{}, errors.New("tokens: a removal needs a client ID")
	}
	b, err := json.Marshal(removal{ClientID: clientID})
	if err != nil {
		return storage.NewOutboxItem{}, err
	}
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: RemoveClientKind, Payload: b, BusinessKey: "oauth.client.remove:" + clientID}, nil
}

// RemoveClientWork is the worker's handler of RemoveClientKind.
func RemoveClientWork(o ports.OAuthClients) outbox.Work {
	return func(ctx context.Context, _ uuid.UUID, row storage.OutboxRow) error {
		var r removal
		if err := json.Unmarshal(row.Payload, &r); err != nil || r.ClientID == "" {
			return fmt.Errorf("tokens: outbox row %s is not a client removal", row.ID)
		}
		return o.RemoveClient(ctx, r.ClientID)
	}
}

// AgentClientKind is the outbox kind that revokes what a person granted one AI app, run by the worker class: an organisation taking an
// app off its allowlist (AI-35, AI-38).
const AgentClientKind = "oauth.agent_client.revoke"

// ActionAgentClientRevokeRequested is the audit action recorded with the change that asked for it.
const ActionAgentClientRevokeRequested = "oauth.agent_client_tokens.revoke_requested"

type clientPayload struct {
	Subject  string `json:"subject"`
	ClientID string `json:"client_id"`
	Reason   string `json:"reason"`
}

// RevokeAgentClient asks, in the unit of work u, for the tokens subject holds for the one AI app clientID to be revoked once u commits,
// and records why in the organisation's audit trail: their other apps, web session and integrations are left alone.
func RevokeAgentClient(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, subject, clientID, reason string) error {
	if subject == "" || clientID == "" || reason == "" {
		return errors.New("tokens: a revocation needs a subject, a client and a reason")
	}
	b, err := json.Marshal(clientPayload{Subject: subject, ClientID: clientID, Reason: reason})
	if err != nil {
		return err
	}
	if _, err := d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: AgentClientKind, Payload: b}); err != nil {
		return err
	}
	return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionAgentClientRevokeRequested, ObjectType: "organisation", ObjectID: org,
		Changes: map[string][2]any{"subject": {nil, subject}, "client": {nil, clientID}, "reason": {nil, reason}}})
}

// AgentClientWork is the worker's handler of AgentClientKind.
func AgentClientWork(o ports.OAuthConsents) outbox.Work {
	return func(ctx context.Context, _ uuid.UUID, row storage.OutboxRow) error {
		var p clientPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.Subject == "" || p.ClientID == "" {
			return fmt.Errorf("tokens: outbox row %s is not an AI app revocation", row.ID)
		}
		return o.RevokeClientConsent(ctx, p.Subject, p.ClientID)
	}
}

// AgentKind is the outbox kind that revokes what a person granted AI apps, run by the worker class (AI-35, AI-38).
const AgentKind = "oauth.agent.revoke"

// ActionAgentRevokeRequested is the audit action recorded with the change that asked for it.
const ActionAgentRevokeRequested = "oauth.agent_tokens.revoke_requested"

// RevokeAgents asks, in the unit of work u, for every token of subject held by an AI app to be revoked once u commits, and records
// why in the organisation's audit trail: their web session and integrations are left alone.
func RevokeAgents(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, subject, reason string) error {
	if subject == "" || reason == "" {
		return errors.New("tokens: a revocation needs a subject and a reason")
	}
	b, err := json.Marshal(payload{Subject: subject, Reason: reason})
	if err != nil {
		return err
	}
	if _, err := d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: AgentKind, Payload: b}); err != nil {
		return err
	}
	return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionAgentRevokeRequested, ObjectType: "organisation", ObjectID: org,
		Changes: map[string][2]any{"subject": {nil, subject}, "reason": {nil, reason}}})
}

// AgentWork is the worker's handler of AgentKind.
func AgentWork(o ports.OAuthConsents) outbox.Work {
	return func(ctx context.Context, _ uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.Subject == "" {
			return fmt.Errorf("tokens: outbox row %s is not an AI app revocation", row.ID)
		}
		return o.RevokeAgentConsents(ctx, p.Subject)
	}
}
