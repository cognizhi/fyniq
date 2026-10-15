//go:build !local

package wiring

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/clock"
)

func config() Config {
	var c Config
	c.Tasks = map[string]gcp.TaskClass{}
	c.Invokers = map[string]gcp.Invoker{"scheduler": {ServiceAccount: "scheduler@p.iam.gserviceaccount.com", Audience: "https://api.run.app/internal/tick"}}
	for _, class := range []string{"worker", "executor", "jobs"} {
		c.Tasks[class] = gcp.TaskClass{Queue: "projects/p/locations/asia-southeast1/queues/" + class, URL: "https://" + class + ".run.app",
			ServiceAccount: "tasks-" + class + "@p.iam.gserviceaccount.com", Audience: "https://" + class + ".run.app"}
		c.Invokers[class] = gcp.Invoker{ServiceAccount: "tasks-" + class + "@p.iam.gserviceaccount.com", Audience: "https://" + class + ".run.app"}
	}
	c.Anchors.Bucket = "fyniq-staging-anchors"
	c.Anchors.KeyVersion = "projects/p/locations/asia-southeast1/keyRings/audit/cryptoKeys/anchor/cryptoKeyVersions/1"
	c.Job = "projects/p/locations/asia-southeast1/jobs/fyniq-jobs"
	return c
}

// The release build wires its background work from the deployment's configuration, and refuses a
// configuration with a gap, such as an executor that would accept the worker's account (Proposal
// executor queue isolation). It connects to nothing until the service runs.
// req: NFR-PORT-04
// req: Proposal executor queue isolation
// req: SEC-03
// asvs: V13.2.1, V13.2.2
func TestTheReleaseBuildWiresGoogleCloud(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	t.Setenv("FYNIQ_DATABASE_URL", "postgres://fyniq_worker@127.0.0.1:1/fyniq?sslmode=disable")
	b, _ := json.Marshal(config())
	t.Setenv("FYNIQ_GCP", string(b))
	kit, err := Build(context.Background(), "worker", c)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer kit.Store.Close()
	if _, ok := kit.Queue.(*gcp.Tasks); !ok {
		t.Errorf("the queue is %T", kit.Queue)
	}
	if _, ok := kit.Signer.(*gcp.KMS); !ok {
		t.Errorf("the signer is %T", kit.Signer)
	}
	for name, change := range map[string]func(*Config){
		"no executor queue":                func(c *Config) { delete(c.Tasks, "executor") },
		"an http handler":                  func(c *Config) { t := c.Tasks["worker"]; t.URL = "http://worker"; c.Tasks["worker"] = t },
		"no tick caller":                   func(c *Config) { delete(c.Invokers, "scheduler") },
		"the executor trusting the worker": func(c *Config) { c.Invokers["executor"] = c.Invokers["worker"] },
		"no anchor key version":            func(c *Config) { c.Anchors.KeyVersion = "projects/p/keyRings/audit/cryptoKeys/anchor" },
		"no job":                           func(c *Config) { c.Job = "" },
	} {
		cfg := config()
		change(&cfg)
		b, _ := json.Marshal(cfg)
		t.Setenv("FYNIQ_GCP", string(b))
		if _, err := Build(context.Background(), "worker", c); err == nil || !strings.Contains(err.Error(), "FYNIQ_GCP lacks") {
			t.Errorf("%s: %v", name, err)
		}
	}
	t.Setenv("FYNIQ_DATABASE_URL", "")
	if _, err := Build(context.Background(), "worker", c); err == nil {
		t.Error("no database URL was accepted")
	}
}
