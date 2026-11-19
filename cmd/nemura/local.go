//go:build local

package main

import (
	"cmp"
	"context"
	"crypto/sha512"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/role"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The local server is the one of `make deps-up`, on its port (FYNIQ_PG_PORT, 55432): a database `nemura` beside Fyniq's,
// with roles of its own. NEMURA_DATABASE_ADMIN_URL is its superuser (deploy/local/compose.yaml), used only to create the
// migrator and the database the first time.
func localURL(user, password, database string) string {
	return fmt.Sprintf("postgres://%s:%s@localhost:%s/%s?sslmode=disable",
		user, password, cmp.Or(os.Getenv("FYNIQ_PG_PORT"), "55432"), database)
}

func init() {
	defaultDatabaseURL = func(r role.Role) string { return localURL(r.DatabaseRole(), nemura.LocalPassword, nemura.Database) }
	before = func(ctx context.Context) error {
		return nemura.BootstrapLocal(ctx, cmp.Or(os.Getenv("NEMURA_DATABASE_ADMIN_URL"), localURL("fyniq", "fyniq-local-only", "postgres")))
	}
	after = nemura.EnableLocalLogins
	taskWiring = localTasks
	signInWiring = localSignIn
	restrictedWiring = localRestricted
	letterRenderer = letters.Local{} // a plain Latin text PDF that says it is local (design D3)
	hydraAdmin = func(admin string) ports.OAuthClients {
		return hydra.New(admin, (&egress.Guard{Local: true, Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin)))
	}
	linkHTTP = func(hosts ...string) *http.Client {
		return (&egress.Guard{Local: true, Propagate: true}).Bounded(0, 15*time.Second).For(hosts...)
	}
}

// localSignIn is the local build's sign-in: an OpenID Connect issuer inside the api whose page asks who you are, as a provider would after
// checking you, and which the flow checks like any provider's (state, PKCE, issuer, audience, expiry, nonce). Whoever runs the local
// stack can sign in as anyone, so it exists only in the local build (DEV-07).
func localSignIn(deps app.Deps, getenv func(string) string, s *httpserver.Server) (ports.SignIn, error) {
	origin := cmp.Or(getenv("NEMURA_PUBLIC_ORIGIN"), "http://localhost:"+cmp.Or(getenv("PORT"), "8080"))
	base := getenv("NEMURA_API_BASE")
	d, err := local.NewLocalSignIn(base, origin+base+"/auth/callback", deps.Clock)
	if err != nil {
		return nil, err
	}
	d.Register(s)
	return d, nil
}

// localTasks is the local substitute for Cloud Tasks: HMAC credentials with a key the local services share
// (NEMURA_TASK_KEY, local only), and a push dispatcher that delivers each task to the worker's own /tasks
// (NEMURA_WORKER_URL, by default the worker's own port).
func localTasks(deps app.Deps, getenv func(string) string) (ports.TaskTokens, ports.Queue, error) {
	tokens := local.NewHMACTokens([]byte(cmp.Or(getenv("NEMURA_TASK_KEY"), "nemura-local-only-task-key")), deps.Clock, time.Hour)
	self := cmp.Or(getenv("NEMURA_WORKER_URL"), "http://127.0.0.1:"+cmp.Or(getenv("PORT"), "8080"))
	return tokens, local.NewPush(map[string]string{"worker": self}, tokens, deps.Clock, 5, time.Second), nil
}

// localRestricted keeps the local field keys and the files of restricted people data under NEMURA_DATA_DIR (.data/nemura on a host, a volume in the
// container). When the directory cannot be written the service runs without them and says so in plain words at the API (NM_UNAVAILABLE).
func localRestricted(getenv func(string) string) (ports.Cipher, ports.BlobStore) {
	dir := cmp.Or(getenv("NEMURA_DATA_DIR"), ".data/nemura")
	var cipher ports.Cipher
	var err error
	if phrase := getenv("NEMURA_LOCAL_FIELD_KEY"); phrase != "" {
		// The local stack's own key (deploy/local/compose.yaml), for a container whose disk is read-only: not a secret, and never in a release image.
		seed := sha512.Sum512([]byte(phrase))
		cipher, err = local.NewFieldKeys(seed[:])
	} else {
		cipher, err = local.LoadOrCreateFieldKeys(dir + "/field-keys")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "nemura: restricted people data is off: %v\n", err)
		return nil, nil
	}
	blobs, err := local.NewDir(dir + "/blobs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "nemura: documents are off: %v\n", err)
		return cipher, nil
	}
	return cipher, blobs
}
