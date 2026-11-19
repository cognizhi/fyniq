//go:build local

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura/nemuratest"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

// The migrate role prepares an empty local server (the migrator, the database, its roles' logins) and applies Nemura's
// migrations, idempotently; the api role then connects as its own database role, answers the three endpoints every image has,
// names itself nemura-api, reports the database in /readyz, and its own healthcheck passes (the probe of the image and of the
// compose service). Without the database /readyz fails.
// req: NFR-IMG-06
func TestTheMigrateRolePreparesTheDatabaseAndTheAPIServesOverIt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary against a database")
	}
	ctx := context.Background()
	bin := build(t)
	d, err := nemuratest.StartEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	migrate := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env("NEMURA_ROLE=migrate", "NEMURA_DATABASE_URL="+d.URL("nemura_migrator"), "NEMURA_DATABASE_ADMIN_URL="+d.AdminURL())
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err
	}
	if out, err := migrate("up"); err != nil || !strings.Contains(out, "000001_roles_schema_tenancy.sql") {
		t.Fatalf("migrate up: %v\n%s", err, out)
	}
	if out, err := migrate("up"); err != nil || !strings.Contains(out, "up to date") {
		t.Fatalf("migrate up again: %v\n%s", err, out)
	}
	if out, err := migrate("status"); err != nil || !strings.Contains(out, "applied") {
		t.Fatalf("migrate status: %v\n%s", err, out)
	}
	if out, err := migrate("sideways"); err == nil || !strings.Contains(out, "usage:") {
		t.Fatalf("a bad argument: %v\n%s", err, out)
	}

	port := freePort(t)
	api := exec.Command(bin)
	api.Env = env("NEMURA_ROLE=api", "PORT="+port, "NEMURA_DATABASE_URL="+d.URL("nemura_api"))
	if err := api.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = api.Process.Kill(); _ = api.Wait() }()
	base := "http://127.0.0.1:" + port
	var lastErr error
	for range 200 { // up to about 20 seconds for the binary to listen
		var resp *http.Response
		if resp, lastErr = http.Get(base + "/healthz"); lastErr == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("the api role never answered: %v", lastErr)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(base + path)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %v %v", path, resp, err)
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
	}
	resp, err := http.Get(base + "/version")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v struct{ Service, Version, Commit, Flavour string }
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.Service != "nemura-api" || v.Version == "" || v.Commit == "" || v.Flavour != "local" {
		t.Errorf("/version: %+v, %v", v, err)
	}
	probe := exec.Command(bin, "healthcheck")
	probe.Env = env("PORT=" + port) // no role: the probe answers for any container of the image
	if out, err := probe.CombinedOutput(); err != nil {
		t.Errorf("healthcheck: %v\n%s", err, out)
	}

	// An API that cannot reach its database is not ready.
	port2 := freePort(t)
	down := exec.Command(bin)
	down.Env = env("NEMURA_ROLE=api", "PORT="+port2, "NEMURA_DATABASE_URL=postgres://nemura_api:wrong@127.0.0.1:1/nemura?sslmode=disable&connect_timeout=2")
	if err := down.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = down.Process.Kill(); _ = down.Wait() }()
	for range 200 {
		var r *http.Response
		if r, lastErr = http.Get("http://127.0.0.1:" + port2 + "/readyz"); lastErr == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusOK {
				t.Errorf("/readyz answered 200 with no database")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Errorf("the api with no database never answered: %v", lastErr)
	}
}
