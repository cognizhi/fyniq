// Command worker runs the background work dispatched from the outbox (ADR-T7): POST /tasks for the
// worker class's tasks and, locally, POST /jobs for the platform's long jobs (in production those run
// as Cloud Run jobs from the jobs image).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/bankingest"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/security"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
	"github.com/cognizhi/fyniq/internal/reportexport"
	"github.com/cognizhi/fyniq/internal/sheet"
	"github.com/cognizhi/fyniq/internal/tokens"
	"github.com/cognizhi/fyniq/internal/version"
	"github.com/cognizhi/fyniq/internal/wiring"
)

func main() {
	os.Exit(app.Serve("worker", func(s *httpserver.Server, deps app.Deps) error {
		kit, err := wiring.Build(context.Background(), "worker", deps.Clock)
		if err != nil {
			fmt.Fprintln(os.Stderr, "worker: serving without background work:", err)
			return nil
		}
		kit.Release = controls.Release{Tag: version.Release, Repository: version.Repository} // CMP-01: the CI checks' release
		// REL-05: every exported report names the version and the commit.
		kit.Stamp = sheet.Stamp(version.Version, version.Commit)
		s.AddReadiness("database", kit.Store.Ping)
		kinds := map[string]outbox.Work{}
		if admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL"); admin != "" {
			kinds[tokens.Kind] = tokens.Work(wiring.Hydra(admin)) // AI-38: revoke a user's tokens after a change
		}
		kinds[security.SignInKind] = security.SignInWork(kit.Store, kit.Alerts(deps.Clock)) // SEC-31: an admin's sign-in, checked
		if c, ok := kit.Committer(deps.Clock); ok {                                         // FR-DOC-13: commit a final record's files to the locked store
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
		kinds[remitmail.Kind] = kit.Remittances(deps.Clock).Work()        // FR-BNK-12: a batch's supplier payment matched, its remittance advice sent
		kinds[reportexport.Kind] = kit.Exports(deps.Clock).Work()         // FR-RPT-09: an accountant export prepared in the background
		kinds[migration.ValidateKind] = kit.Migrations(deps.Clock).Work() // FR-MIG-01: an uploaded workbook validated in the background
		kinds[ai.ReadKind] = kit.AIModels(deps.Clock).Work()              // AI-80: a connection's model list, read after its key is approved
		kinds[email.Kind] = email.WorkFrom(kit.EmailProviders(kit.Runtime(deps.Clock)), kit.Store, kit.Mail.From, deps.Clock, files)
		if kit.Staging != nil && kit.Scanner != nil { // FR-INT-22: fetch, decide and keep received mail
			fetcher := kit.Fetcher(deps.Clock)
			fetcher.MailFrom = kit.InboundMail(kit.Runtime(deps.Clock)) // the enabled connector that receives email, read per fetch
			kinds[email.FetchKind] = fetcher.Work()
			kinds[bankingest.Kind] = kit.BankReports(deps.Clock).Work() // FR-BNK-17: an accepted bank report, imported
		}
		kinds[bankfeed.Kind] = kit.BankFeeds(deps.Clock).Work()  // FR-BNK-15: an early sync a bank's verified webhook asked for
		kinds[notify.FanoutKind] = kit.Fanout(deps.Clock).Work() // FR-NTF-05: one event, every person it concerns
		kinds[notify.RelayKind] = kit.Relays(deps.Clock).Work()  // FR-ADM-07: the installation's notices, in each Super admin's organisation
		for kind, work := range kit.CheckKinds(deps.Clock) {     // FR-CTL-01: the checks after a change, and Run now
			kinds[kind] = work
		}
		s.Handle("POST /tasks", &outbox.Handler{Class: storage.ClassWorker, Store: kit.Store, Tokens: kit.Tokens,
			Clock: deps.Clock, Holder: app.Holder("worker"), Kinds: kinds, Dead: kit.JobDead(deps.Clock)})
		s.Handle("POST /jobs", &outbox.JobsHandler{Tokens: kit.Tokens, Platform: kit.Store, Clock: deps.Clock,
			Jobs: kit.PlatformJobs(deps.Clock)})
		return nil
	}))
}
