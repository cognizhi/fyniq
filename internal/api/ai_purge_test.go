//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The daily purge of the assistant's conversations (§11.9; design D11): as the worker, in each organisation, the
// conversations whose last question is older than retention.ai_conversations_days go, with those their people deleted,
// and the purge is recorded in the audit trail with how many went; a conversation within its retention stays.
// req: data/backup-and-recovery Retention of AI conversations
// req: AI-P8
func TestTheAssistantsConversationsArePurgedAfterTheirRetention(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	user := uuid.MustParse(f.ahkow)
	conversation := func(asked time.Time) uuid.UUID {
		var id uuid.UUID
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: user, CreatedAt: asked})
			if err != nil {
				return err
			}
			id = c.ID
			_, err = u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: user, Question: "What was revenue?", AskedAt: asked})
			return err
		})
		return id
	}
	y, m, d := e.clock.Now().Date()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "retention.ai_conversations_days", "365", "30", time.Date(y, m, d, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	old, kept := conversation(e.clock.Now().AddDate(0, 0, -31)), conversation(e.clock.Now().AddDate(0, 0, -29))
	n, err := retention.PurgeAIConversations(context.Background(), workerStore(t), e.clock)
	if err != nil || n != 1 {
		t.Fatalf("the purge: %d %v", n, err)
	}
	ahkow := f.as(f.ahkow)
	e.problemIs(e.call(http.MethodGet, "/v1/assistant/conversations/"+old.String(), ahkow, nil), "getAIConversation", 404, CodeNotFound)
	if r := e.call(http.MethodGet, "/v1/assistant/conversations/"+kept.String(), ahkow, nil); r.Status != http.StatusOK {
		t.Errorf("a conversation within its retention: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/audit/events?action="+retention.ActionAIConversationsPurged, f.as(f.ahkow, authz.Owner), nil); r.Status != http.StatusOK ||
		!strings.Contains(r.Raw, `"action":"ai_conversation.purged"`) || !strings.Contains(r.Raw, `"fields":["purged"]`) {
		t.Errorf("the purge's audit event: %d %s", r.Status, r.Raw)
	}
	if n, err := retention.PurgeAIConversations(context.Background(), workerStore(t), e.clock); err != nil || n != 0 {
		t.Errorf("a second purge: %d %v", n, err)
	}
}
