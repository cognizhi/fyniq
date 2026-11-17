package proposal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ExpireJob ends the proposals that waited past their expiry undecided (§9.5, C.2 #19; design D9), every 15 minutes, as
// the worker. Each one's approval request expires with it, at the same moment (its expires_at), so it can no longer be
// signed.
const ExpireJob = "ai.proposals.expire"

// ExpireAll moves every organisation's Pending proposals past their expiry to Expired, audited, and gives how many.
func ExpireAll(ctx context.Context, store storage.Store, clock ports.Clock) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	total := 0
	for _, org := range orgs {
		n, err := ExpireIn(ctx, store, org, clock)
		total += n
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}

// ExpireIn moves org's Pending proposals past their expiry to Expired, audited, in one unit of work, and gives how many.
func ExpireIn(ctx context.Context, store storage.Store, org uuid.UUID, clock ports.Clock) (int, error) {
	n := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		n = 0
		now := clock.Now()
		q := storage.AIProposalQuery{Statuses: []string{storage.ProposalPending}, Limit: 500}
		for {
			page, err := u.AIProposals().List(ctx, q)
			if err != nil {
				return err
			}
			for _, p := range page {
				if now.Before(p.ExpiresAt) {
					continue
				}
				if _, err := u.AIProposals().Expire(ctx, p.ID, p.Version, now); err != nil {
					return err
				}
				n++
			}
			if len(page) < q.Limit {
				return nil
			}
			q.After = page[len(page)-1].ID
		}
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
