// Package members changes who holds which role in an organisation. Every change also asks for the
// user's OAuth tokens to be revoked, in the same unit of work (AI-38): the token claims consent stamped
// are a snapshot, so a role change must end them, and the next call is denied within 60 seconds.
// Inviting and managing users through the API is FR-ADM-01 (P1); P0 has the commands and their tests.
package members

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/tokens"
)

// Assign gives a member a role until expires (nil: no expiry), and revokes their tokens.
func Assign(ctx context.Context, store storage.Store, d outbox.Dispatcher, org, userID uuid.UUID, role string,
	expires *time.Time, reason string) (storage.RoleAssignment, error) {
	var out storage.RoleAssignment
	err := store.Do(storage.WithReason(ctx, reason), org, func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Members().Membership(ctx, userID)
		if err != nil {
			return err
		}
		if out, err = u.Members().Assign(ctx, m.ID, role, expires); err != nil {
			return err
		}
		return tokens.RevokeAll(ctx, d, u, org, userID.String(), "role "+role+" assigned: "+reason)
	})
	return out, err
}

// Revoke ends a role assignment, and revokes the member's tokens.
func Revoke(ctx context.Context, store storage.Store, d outbox.Dispatcher, org, userID, assignmentID uuid.UUID, reason string) error {
	return store.Do(storage.WithReason(ctx, reason), org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := u.Members().Revoke(ctx, assignmentID); err != nil {
			return err
		}
		return tokens.RevokeAll(ctx, d, u, org, userID.String(), "role revoked: "+reason)
	})
}
