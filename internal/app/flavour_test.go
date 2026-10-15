package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// build compiles cmd/jobs, a tool that exits as soon as the start-up checks pass, in one flavour.
func build(t *testing.T, tags string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	out := filepath.Join(t.TempDir(), "jobs-"+tags)
	args := []string{"build", "-o", out}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	cmd := exec.Command("go", append(args, "./cmd/jobs")...)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", tags, err, b)
	}
	return out
}

func run(t *testing.T, bin string, env ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), stderr.String()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, stderr.String()
}

// req: DEV-07
// req: SEC-16
// asvs: V13.4.2, V15.2.3
func TestBuildFlavoursRefuseTheWrongEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	release, local := build(t, ""), build(t, "local")
	cases := []struct {
		bin, name string
		env       []string
		code      int
		says      string
	}{
		{release, "release in production", []string{"FYNIQ_ENV=production"}, 0, ""},
		{release, "release in staging", []string{"FYNIQ_ENV=staging"}, 0, ""},
		{release, "release locally", []string{"FYNIQ_ENV=local"}, 2, "RUNTIME-FLAVOUR-01"},
		{release, "release without FYNIQ_ENV", nil, 2, "RUNTIME-ENV-01"},
		{release, "business date in production", []string{"FYNIQ_ENV=production", "FYNIQ_BUSINESS_DATE=2026-12-31"}, 2, "CLOCK-01"},
		{local, "local by default", nil, 0, ""},
		{local, "local in CI", []string{"FYNIQ_ENV=ci"}, 0, ""},
		{local, "local in staging", []string{"FYNIQ_ENV=staging"}, 2, "RUNTIME-FLAVOUR-03"},
		{local, "local in production", []string{"FYNIQ_ENV=production"}, 2, "RUNTIME-FLAVOUR-03"},
	}
	for _, c := range cases {
		code, stderr := run(t, c.bin, c.env...)
		if code != c.code || !strings.Contains(stderr, c.says) {
			t.Errorf("%s: exit %d %q; want exit %d mentioning %q", c.name, code, stderr, c.code, c.says)
		}
	}
	rb, _ := os.ReadFile(release)
	lb, _ := os.ReadFile(local)
	adapters := []byte("internal/adapters/local.")
	if bytes.Contains(rb, adapters) {
		t.Error("the release binary contains the local adapters package")
	}
	if !bytes.Contains(lb, adapters) {
		t.Error("the local binary lacks the local adapters package: is it linked?")
	}
}
