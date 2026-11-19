package retention

import (
	"context"
	"fmt"
	"strconv"
	"time"

	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The purge of the assistant's conversations (PRD-001 §11.9, AI-P8; p1-ai-assistant task 6.3, design D11): daily, in
// every organisation, the conversations whose last question is older than retention.ai_conversations_days (365 unless
// changed), and those their people deleted, go with their turns, reads and feedback, and with them the trail of the AI apps'
// calls of that age (AI-44); each organisation's purge is
// recorded in its audit trail with how many went.
const (
	AIConversationsJob          = "ai.conversations.purge"
	ActionAIConversationsPurged = "ai_conversation.purged"
	aiRetentionSetting          = "retention.ai_conversations_days"
)

// PurgeAIConversations purges every organisation's conversations past their retention, and gives how many went.
func PurgeAIConversations(ctx context.Context, store storage.Store, clock ports.Clock) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	def, ok := settings.Lookup(aiRetentionSetting)
	if !ok {
		return 0, fmt.Errorf("no setting %s", aiRetentionSetting)
	}
	total := 0
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			changes, err := u.Settings().Changes(ctx, def.Key)
			if err != nil {
				return err
			}
			now := clock.Now()
			y, m, d := now.In(appclock.Singapore).Date()
			days, err := strconv.Atoi(settings.On(def, changes, time.Date(y, m, d, 0, 0, 0, 0, time.UTC)).Value)
			if err != nil {
				return fmt.Errorf("%s: %w", def.Key, err)
			}
			before := now.Add(-time.Duration(days) * 24 * time.Hour)
			conversations, turns, err := u.AIConversations().Purge(ctx, before)
			if err != nil {
				return err
			}
			// The AI apps' trail of calls goes with them (AI-44): its evidence stays in the audit trail.
			calls, err := u.MCPCalls().Purge(ctx, before)
			if err != nil || conversations == 0 && calls == 0 {
				return err
			}
			total += conversations
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionAIConversationsPurged, ObjectType: "ai_conversation", ObjectID: org,
				Changes: map[string][2]any{"purged": {nil, map[string]any{"conversations": conversations, "turns": turns, "mcp_calls": calls, "days": days,
					"before": before.UTC().Format(time.RFC3339)}}}})
		})
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}
