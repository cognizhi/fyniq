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
