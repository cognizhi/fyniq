// Command worker runs the background work dispatched from the outbox (ADR-T7): POST /tasks for the
// worker class's tasks and, locally, POST /jobs for the platform's long jobs (in production those run
// as Cloud Run jobs from the jobs image).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/tokens"
	"github.com/cognizhi/fyniq/internal/wiring"
)

func main() {
	os.Exit(app.Serve("worker", func(s *httpserver.Server, deps app.Deps) error {
		kit, err := wiring.Build(context.Background(), "worker", deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "worker: serving without background work:", err)
			return nil
		}
		s.AddReadiness("database", kit.Store.Ping)
		kinds := map[string]outbox.Work{}
		if admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL"); admin != "" {
			kinds[tokens.Kind] = tokens.Work(hydra.New(admin)) // AI-38: revoke a user's tokens after a change
		}
		if c, ok := kit.Committer(deps.Clock); ok { // FR-DOC-13: commit a final record's files to the locked store
			kinds[evidence.CommitKind] = func(ctx context.Context, org uuid.UUID, _ storage.OutboxRow) error {
				_, err := c.Commit(ctx, org)
				return err
			}
		}
		var files email.Files // FR-AR-06: the evidence files a message carries, such as an invoice's PDF
		if kit.Staging != nil && kit.Evidence != nil {
			files = evidence.Files{Store: kit.Store, Staging: kit.Staging, Locked: kit.Evidence}.Read
		}
		// FR-INT-21: send queued mail once, through the email connectors enabled when each send starts, or the deployment's
		// providers until the first-start bootstrap has installed them (rev 0.139); none enabled: the message waits.
		kinds[email.Kind] = email.WorkFrom(kit.EmailProviders(kit.Runtime(deps.Clock)), kit.Store, kit.Mail.From, deps.Clock, files)
		if kit.Staging != nil && kit.Scanner != nil { // FR-INT-22: fetch, decide and keep received mail
			fetcher := kit.Fetcher(deps.Clock)
			fetcher.MailFrom = kit.InboundMail(kit.Runtime(deps.Clock)) // the enabled connector that receives email, read per fetch
			kinds[email.FetchKind] = fetcher.Work()
		}
		kinds[notify.FanoutKind] = kit.Fanout(deps.Clock).Work() // FR-NTF-05: one event, every person it concerns
		s.Handle("POST /tasks", &outbox.Handler{Class: storage.ClassWorker, Store: kit.Store, Tokens: kit.Tokens,
			Clock: deps.Clock, Holder: app.Holder("worker"), Kinds: kinds, Dead: kit.JobDead(deps.Clock)})
		s.Handle("POST /jobs", &outbox.JobsHandler{Tokens: kit.Tokens, Platform: kit.Store, Clock: deps.Clock,
			Jobs: kit.PlatformJobs(deps.Clock)})
		return nil
	}))
}
