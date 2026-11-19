package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// build compiles cmd/nemura as the local flavour, the one `make up` runs.
func build(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "nemura")
	cmd := exec.Command("go", "build", "-tags", "local", "-o", out, ".")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

func env(extra ...string) []string {
	return append([]string{"PATH=" + os.Getenv("PATH"), "FYNIQ_ENV=local"}, extra...)
}

// A role that is unset, unknown or not built yet refuses to start with its code, and says how to fix it.
func TestARoleThatCannotRunRefusesWithItsCode(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := build(t)
	for _, tc := range []struct{ role, says string }{
		{"", "NEMURA_ROLE is not set"},
		{"worker", "worker role is not built yet"},
		{"nonsense", "is not a role"},
	} {
		cmd := exec.Command(bin)
		cmd.Env = env("NEMURA_ROLE=" + tc.role)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 || !strings.Contains(stderr.String(), "NEMURA-ROLE-01") || !strings.Contains(stderr.String(), tc.says) {
			t.Errorf("role %q: want exit 2 with NEMURA-ROLE-01 saying %q, got %v\n%s", tc.role, tc.says, err, stderr.String())
		}
	}
	// The version needs no role.
	out, err := exec.Command(bin, "--version").Output()
	if err != nil || !strings.HasPrefix(string(out), "nemura ") {
		t.Errorf("--version: %q, %v", out, err)
	}
}

// The api role answers the three endpoints every image has, names itself nemura-api, and its own healthcheck passes
// (the probe of the image and of the compose service).
// req: NFR-IMG-06
func TestTheAPIRoleServesTheProbesAndTheVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary")
	}
	bin := build(t)
	port := freePort(t)
	cmd := exec.Command(bin)
	cmd.Env = env("NEMURA_ROLE=api", "PORT="+port)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	base := "http://127.0.0.1:" + port
	var err error
	for range 200 { // up to about 20 seconds for the binary to listen
		var resp *http.Response
		if resp, err = http.Get(base + "/healthz"); err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the api role never answered: %v", err)
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
}
