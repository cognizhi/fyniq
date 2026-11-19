//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// taskKit is what every catalogue task type's test shares (p1-ai-proposals): the AI switched on and the task type on, the AI service's token,
// a person's turn that proposes, the approval by a named other person and the executor's run.
type taskKit struct {
	t       *testing.T
	e       *env
	f       founders
	service string
}

// newTaskKit switches the AI on and turns taskTypes on (the setting's value, such as "ap.draft_bill").
func newTaskKit(e *env, taskTypes string) taskKit {
	e.t.Helper()
	aiSwitchedOn(e)
	enableTaskTypes(e, taskTypes, 1)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		e.t.Fatal(err)
	}
	return taskKit{t: e.t, e: e, f: foundersIn(e), service: service}
}

// propose is the AI service's proposal for the turn's person.
func (k taskKit) propose(turn, taskType string, payload map[string]any) response {
	k.t.Helper()
	return k.e.call(http.MethodPost, "/internal/v1/proposals", turn, map[string]any{"task_type": taskType, "payload": payload, "rationale": "The document says so.",
		"model": "claude-opus-5-5", "prompt": "chat@4"}, ServiceTokenHeader, k.service)
}

// stored is the proposal as the store keeps it.
func (k taskKit) stored(id string) storage.AIProposal {
	k.t.Helper()
	var pr storage.AIProposal
	k.e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pr, err = u.AIProposals().Get(ctx, uuid.MustParse(id))
		return err
	})
	return pr
}

// approve is a person's approval of the proposal's request, signed with the hash the request shows.
func (k taskKit) approve(id, approverToken string) {
	k.t.Helper()
	pr := k.stored(id)
	shown := k.e.call(http.MethodGet, "/v1/approvals/"+pr.ApprovalRequest.String(), approverToken, nil)
	if d := k.f.decide(approverToken, pr.ApprovalRequest.String(), "approve", hashOf(shown)); d.Status != http.StatusOK {
		k.t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
}

// execute runs the executor over the proposal n times (it is idempotent by proposal ID) and returns the proposal after.
func (k taskKit) execute(id string, n int) storage.AIProposal {
	k.t.Helper()
	executor := proposal.Executor{Store: executorStore(k.t), Catalogue: proposal.Default(), Commands: proposal.Release, Clock: k.e.clock,
		Outbox: outbox.Dispatcher{Store: executorStore(k.t), Queue: k.e.queue, Clock: k.e.clock}}
	body, _ := json.Marshal(map[string]any{"proposal_id": id})
	for range n {
		if err := executor.Work(context.Background(), k.e.books.Org, storage.OutboxRow{ID: uuid.New(), Class: storage.ClassExecutor, Kind: proposal.ExecutorKind, Payload: body}); err != nil {
			k.t.Fatal(err)
		}
	}
	return k.stored(id)
}

// roleToken is a signed-in person of role, a new member each time.
func (k taskKit) roleToken(name string, roles ...authz.Role) (uuid.UUID, string) {
	k.t.Helper()
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	id := k.e.joinedIn(k.e.books.Org, name, names...)
	return id, k.f.as(id.String(), roles...)
}
