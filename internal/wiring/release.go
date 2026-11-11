//go:build !local

package wiring

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/adapters/clamav"
	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/backup"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/scanservice"
)

// Config is the Google Cloud wiring the deployment writes (p0-infra-opentofu) as JSON in FYNIQ_GCP; the
// database comes from FYNIQ_DATABASE_URL, as the service's own role.
type Config struct {
	Tasks    map[string]gcp.TaskClass `json:"tasks"`    // worker class → its queue, handler and invoker
	Invokers map[string]gcp.Invoker   `json:"invokers"` // who may call each class's handler, and the tick ("scheduler")
	Anchors  struct {
		Bucket     string `json:"bucket"`      // the retention-locked bucket of the audit anchors
		KeyVersion string `json:"key_version"` // the KMS key version that signs them
	} `json:"anchors"`
	Job string `json:"job"` // the Cloud Run job that runs the jobs image: projects/…/locations/…/jobs/…
	// Fields names the KMS keys of the sealed fields (SEC-11): the key that wraps their data keys, and the fingerprint
	// key's pinned version; optional until the deployment adds them: without them no bank details can be kept.
	Fields struct {
		Key           string `json:"key"`             // projects/…/cryptoKeys/…
		MACKeyVersion string `json:"mac_key_version"` // projects/…/cryptoKeys/…/cryptoKeyVersions/N
	} `json:"fields"`
	// Evidence names the unlocked staging bucket and the retention-locked evidence bucket (FR-DOC-13, DR-07);
	// without them, and a scanner, no file can be attached.
	Evidence struct {
		Staging string `json:"staging"`
		Bucket  string `json:"bucket"`
	} `json:"evidence"`
	// Render is the private render service (PRD-001 §12.3, rev 0.96): its URL, its audience and its callers' accounts;
	// optional until the deployment adds it: without it no preview renders and no template with IRAS particulars publishes.
	Render gcp.Callers `json:"render"`
	// Scanner is the private scanner service, clamd behind an HTTP front (FR-DOC-08, SEC-24; PRD-001 rev 0.247): its URL,
	// its audience and its callers' accounts; without it, or FYNIQ_CLAMD_ADDR, no file can be attached.
	Scanner gcp.Callers `json:"scanner"`
	// Exports is the weekly export's instance and buckets (DR-02, DR-04; p1-operations-dr design D1): the instance's
	// resource name (projects/<project>/instances/<name>), its database, and the weekly and monthly buckets in the audit
	// project. Without it the export job fails, which its heartbeat's absence alert shows.
	Exports struct {
		Instance string `json:"instance"`
		Database string `json:"database"`
		backup.Buckets
	} `json:"exports"`
	// PlatformRanges are the platform's own networks (its subnet and the range its database is peered on), which no
	// registered private endpoint reaches (SEC-37).
	PlatformRanges []string `json:"platform_ranges"`
}

// Build wires a service's background work on Google Cloud (ADR-T7, ADR-T8): Cloud Tasks, the task
// handlers' OIDC checks, Cloud Run jobs, the anchors' bucket and key, all as the service's own account.
// Any gap in the configuration is an error, and the service serves without background work.
func Build(_ context.Context, service string, clock ports.Clock) (*Kit, error) {
	url := os.Getenv("FYNIQ_DATABASE_URL")
	if url == "" {
		return nil, errors.New("FYNIQ_DATABASE_URL is not set: the database, as this service's role")
	}
	var c Config
	if err := json.Unmarshal([]byte(os.Getenv("FYNIQ_GCP")), &c); err != nil {
		return nil, fmt.Errorf("FYNIQ_GCP is not the Google Cloud wiring as JSON: %w", err)
	}
	if err := c.check(); err != nil {
		return nil, err
	}
	tokens := gcp.MetadataTokens()
	// Every outbound request through the egress guard (SEC-36): Google's APIs and the render service through the
	// platform's own guard, each to its hosts only; the email providers and the connectors through one that also refuses
	// the platform's own services. Objects and documents up to 64 MiB.
	own, err := c.ownRanges()
	if err != nil {
		return nil, err
	}
	platform, outbound := PlatformEgress(), &egress.Guard{Internal: c.internalHosts(), Own: own}
	google := platform.Bounded(64<<20, 0).For(".googleapis.com")
	var fields ports.Cipher
	if c.Fields.Key != "" { // as the api's own account, the only one with a role on the keys
		fields = gcp.NewKMSFields(gcp.KMSAPI, c.Fields.Key, c.Fields.MACKeyVersion, tokens, google)
	}
	// The fields' fingerprint key also proves sensitive values in the trail (AUD-02). A service without the keys proves
	// them under its store's own key, so its proofs do not compare with the api's [live-verify: grant the worker and
	// jobs accounts macSign on the fingerprint key alone].
	store, err := postgres.Connect(url, postgres.WithClock(clock), postgres.WithFingerprints(fields))
	if err != nil {
		return nil, err
	}
	// The deployment's email providers, in order (FR-INT-21, PRD-001 rev 0.102); an SMTP relay only with STARTTLS.
	mail, err := ReadMail(os.Getenv, outbound)
	if err != nil {
		return nil, err
	}
	kit := &Kit{
		Mail:   mail,
		Origin: os.Getenv("FYNIQ_PUBLIC_ORIGIN"),
		Store:  store,
		Tokens: &gcp.TaskTokens{Invokers: c.Invokers, Keys: GoogleKeys(), Clock: clock},
		Queue:  gcp.NewTasks(gcp.TasksAPI, c.Tasks, tokens, google),
		Jobs:   gcp.NewRunJobs(gcp.RunAPI, c.Job, tokens, google),
		Blobs:  gcp.NewStorage(gcp.StorageAPI, c.Anchors.Bucket, tokens, google),
		Signer: gcp.NewKMS(gcp.KMSAPI, c.Anchors.KeyVersion, tokens, google),
	}
	kit.Fields = fields
	if c.Evidence.Staging != "" && c.Evidence.Bucket != "" {
		// Their files are saved through signed URLs, signed as the service's own account (SEC-24; the api may).
		sign := gcp.IAMSigner(tokens, google)
		staging, locked := gcp.NewStorage(gcp.StorageAPI, c.Evidence.Staging, tokens, google), gcp.NewStorage(gcp.StorageAPI, c.Evidence.Bucket, tokens, google)
		staging.Sign, locked.Sign = sign, sign
		kit.Staging, kit.Evidence = staging, locked
	}
	if strings.HasPrefix(c.Render.URL, "https://") && c.Render.Audience != "" { // as the service's own account
		kit.Render = &render.Client{URL: c.Render.URL, Token: gcp.IdentityTokens(c.Render.Audience, nil),
			HTTP: platform.Bounded(64<<20, 2*time.Minute).For(egress.HostOf(c.Render.URL))}
	}
	switch {
	case strings.HasPrefix(c.Scanner.URL, "https://") && c.Scanner.Audience != "": // the scanner service, as the service's own account
		kit.Scanner = &scanservice.Client{URL: c.Scanner.URL, Token: gcp.IdentityTokens(c.Scanner.Audience, nil),
			HTTP: platform.Bounded(64<<10, 2*time.Minute).For(egress.HostOf(c.Scanner.URL))}
	case os.Getenv("FYNIQ_CLAMD_ADDR") != "": // clamd beside the service (SEC-24)
		kit.Scanner = clamav.Scanner{Addr: os.Getenv("FYNIQ_CLAMD_ADDR")}
	}
	// The connectors (FR-INT-20, rev 0.139): their keys in Secret Manager, only the secrets FYNIQ_CONNECTOR_SECRETS names
	// and the organisations' connector secrets; their traffic through the egress guard, never to the platform's own
	// services.
	if kit.Connectors, err = connectors.Catalogue(); err != nil {
		return nil, err
	}
	if kit.ConnectorNames, err = ReadConnectorNames(os.Getenv); err != nil {
		return nil, err
	}
	installation := slices.Collect(maps.Values(kit.ConnectorNames))
	kit.ConnectorSecrets = gcp.NewSecretManager(gcp.SecretManagerAPI, projectOf(c.Job), installation, []string{gcp.OrgSecretPrefix}, tokens, google)
	// The organisations' connector secrets in their own project (design D12), each made under the secrets key the first
	// time it is typed (FYNIQ_ORG_SECRETS_PROJECT, FYNIQ_ORG_SECRETS_KEY; infra/gcp/modules/environment/org_secrets.tf).
	if project := os.Getenv("FYNIQ_ORG_SECRETS_PROJECT"); project != "" {
		key := os.Getenv("FYNIQ_ORG_SECRETS_KEY")
		if key == "" {
			return nil, errors.New("FYNIQ_ORG_SECRETS_PROJECT is set without FYNIQ_ORG_SECRETS_KEY, the key its secrets are made under")
		}
		orgs := gcp.NewSecretManager(gcp.SecretManagerAPI, project, nil, []string{gcp.OrgSecretPrefix}, tokens, google)
		orgs.Create = &gcp.SecretCreation{Location: secretsRegion, KMSKey: key}
		kit.ConnectorSecrets = gcp.SplitSecrets{
			Installation:  gcp.NewSecretManager(gcp.SecretManagerAPI, projectOf(c.Job), installation, nil, tokens, google),
			Organisations: orgs,
		}
	}
	kit.Egress = outbound
	// The weekly export (DR-02, DR-04) and the restore drill's copy (DR-06), as the jobs' account [live-verify].
	if parts := strings.Split(c.Exports.Instance, "/"); len(parts) == 4 && parts[0] == "projects" && parts[2] == "instances" {
		sql := gcp.NewCloudSQL(gcp.SQLAdminAPI, parts[1], parts[3], cmp.Or(c.Exports.Database, "fyniq"), tokens, google)
		sql.Clock = clock
		// The monthly restore drill's copy, cloned in this project and opened as this service's role (DR-06) [live-verify].
		kit.Backup, kit.Clone = sql, sql
		kit.OpenClone = copyOpener(url, postgres.WithClock(clock), postgres.WithFingerprints(fields))
		kit.ExportBuckets = c.Exports.Buckets
	}
	// The platform's posture and logs, read as the jobs' account (SEC-33, CTL-DP-03) [live-verify].
	kit.Posture = gcp.NewSecurityCenter(gcp.SecurityCenterAPI, "projects/"+projectOf(c.Job), tokens, google)
	kit.Logs = gcp.NewLogging(gcp.LoggingAPI, projectOf(c.Job), tokens, google)
	kit.watchChanges(clock)
	return kit, nil
}

// secretsRegion is where the organisations' connector secrets are replicated: Singapore only (CTL-DP-07).
const secretsRegion = "asia-southeast1"

// projectOf is the project of a resource name such as projects/<project>/locations/…/jobs/….
func projectOf(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) > 1 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

// internalHosts are the platform's own services, which no connector may reach: the worker classes' handlers, the
// audiences they check and the render service.
func (c Config) internalHosts() []string {
	var out []string
	add := func(raw string) {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" && !slices.Contains(out, u.Hostname()) {
			out = append(out, u.Hostname())
		}
	}
	for _, t := range c.Tasks {
		add(t.URL)
		add(t.Audience)
	}
	for _, i := range c.Invokers {
		add(i.Audience)
	}
	add(c.Render.URL)
	return out
}

// ownRanges are the platform's own networks, read from PlatformRanges.
func (c Config) ownRanges() ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(c.PlatformRanges))
	for _, r := range c.PlatformRanges {
		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("FYNIQ_GCP platform_ranges: %q is not a range such as 10.10.0.0/24", r)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// classes are the worker classes the outbox sends to (§12.11), and the callers the handlers check.
var classes = []string{"worker", "executor", "jobs"}

func (c Config) check() error {
	var gaps []string
	for _, class := range classes {
		t, ok := c.Tasks[class]
		if !ok || !strings.Contains(t.Queue, "/queues/") || !strings.HasPrefix(t.URL, "https://") || t.ServiceAccount == "" || t.Audience == "" {
			gaps = append(gaps, "tasks."+class)
		}
	}
	for _, caller := range append(classes, "scheduler") {
		if i, ok := c.Invokers[caller]; !ok || i.ServiceAccount == "" || i.Audience == "" {
			gaps = append(gaps, "invokers."+caller)
		}
	}
	// The executor accepts only its own invoker: never the worker's (Proposal executor queue isolation).
	if c.Invokers["executor"].ServiceAccount != "" && c.Invokers["executor"].ServiceAccount == c.Invokers["worker"].ServiceAccount {
		gaps = append(gaps, "invokers.executor (the same account as the worker's)")
	}
	if c.Anchors.Bucket == "" || !strings.Contains(c.Anchors.KeyVersion, "/cryptoKeyVersions/") {
		gaps = append(gaps, "anchors")
	}
	if !strings.Contains(c.Job, "/jobs/") {
		gaps = append(gaps, "job")
	}
	// The field keys come together or not at all: a wrapping key with no pinned fingerprint version (or a key version
	// where the key belongs) would seal values whose duplicates could never be found.
	if (c.Fields.Key != "" || c.Fields.MACKeyVersion != "") &&
		(!strings.Contains(c.Fields.Key, "/cryptoKeys/") || strings.Contains(c.Fields.Key, "/cryptoKeyVersions/") || !strings.Contains(c.Fields.MACKeyVersion, "/cryptoKeyVersions/")) {
		gaps = append(gaps, "fields")
	}
	if len(gaps) > 0 {
		return fmt.Errorf("FYNIQ_GCP lacks %s", strings.Join(gaps, ", "))
	}
	return nil
}

// mailEnv is the deployment's email settings the bootstrap reads, as they are (rev 0.139).
func mailEnv(env func(string) string) func(string) string { return env }

// PlatformEgress is the guard of the platform's own outbound connections: public addresses only.
func PlatformEgress() *egress.Guard { return &egress.Guard{} }

// GoogleKeys are Google's signing keys, which the OIDC checks of Cloud Tasks' and the render service's callers verify
// against, fetched through the platform's guard from Google's certificate host only.
func GoogleKeys() oidc.KeySet {
	return oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), PlatformEgress().For(egress.HostOf(gcp.GoogleCerts))), gcp.GoogleCerts)
}
