package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// Only the local build guesses a database URL. Without one a role says which database role's URL to give.
func TestARoleWithNoDatabaseURLSaysWhichToGive(t *testing.T) {
	if defaultDatabaseURL("api") != "" {
		t.Skip("the local build defaults the URL")
	}
	_, err := databaseURL("api", func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "NEMURA_DATABASE_URL is not set") || !strings.Contains(err.Error(), "nemura_api") {
		t.Errorf("want a plain refusal naming nemura_api, got %v", err)
	}
}
