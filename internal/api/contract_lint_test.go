//go:build local

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The contract lint refuses an operation that breaks a convention of §8.18.1, so a later change gets the
// conventions without a reviewer having to spot a missing header: `make api-check` runs the same
// ruleset over the real contract. Spectral runs here on contracts/openapi/testdata/planted.yaml, where
// every `bad` operation must be refused and every `good` one must pass. It runs in the local build,
// like the oasdiff gate, because it needs the web toolchain (pnpm).
// req: NFR-API-01
// req: FR-INT-06
// req: FR-INT-08
// req: FR-INT-09
// req: FR-INT-10
func TestTheContractLintRefusesPlantedOperations(t *testing.T) {
	cmd := exec.Command("pnpm", "exec", "spectral", "lint", "contracts/openapi/testdata/planted.yaml",
		"--ruleset", "contracts/openapi/spectral.yaml", "--format", "json", "--quiet")
	cmd.Dir = "../.."
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		t.Fatal("pnpm is not installed: install the toolchain with `make setup`")
	case err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1):
		t.Fatalf("spectral: %v\n%s%s", err, out.String(), stderr.String())
	}
	var results []struct {
		Code    string   `json:"code"`
		Message string   `json:"message"`
		Path    []string `json:"path"`
	}
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("spectral's report: %v\n%s", err, out.String())
	}
	var got []string
	for _, r := range results {
		if (r.Code == "fyniq-api-conventions" || r.Code == "fyniq-exemption-reasons") && len(r.Path) >= 3 {
			got = append(got, r.Code+" "+r.Path[2]+" "+r.Path[1]+": "+r.Message)
		}
	}
	want := map[string]string{ // what each refusal must say, by rule, method and path
		"fyniq-api-conventions post /v1/widgets":                     "takes no Idempotency-Key",
		"fyniq-api-conventions get /v1/widgets/{widget_id}":          "returns no ETag",
		"fyniq-api-conventions patch /v1/widgets/{widget_id}":        "does not document 412 and 428",
		"fyniq-api-conventions post /v1/widgets/{widget_id}/archive": "takes no If-Match",
		"fyniq-api-conventions get /v1/gadgets":                      "not cursor-paginated",
		"fyniq-api-conventions post /v1/gizmos":                      "takes no Idempotency-Key",
		"fyniq-exemption-reasons post /v1/gizmos":                    "x-fyniq-exempt",
		"fyniq-exemption-reasons post /v1/gizmos/{gizmo_id}/codes":   "x-fyniq-exempt",
		"fyniq-api-conventions get /v1/old":                          "less than six months",
		"fyniq-api-conventions get /v1/older":                        "without x-fyniq-deprecated-on and x-sunset",
		"fyniq-api-conventions get /v1/sunset-only":                  "is not deprecated",
	}
	for key, says := range want {
		if !slices.ContainsFunc(got, func(g string) bool { return strings.HasPrefix(g, key+": ") && strings.Contains(g, says) }) {
			t.Errorf("not refused: %s (%s)", key, says)
		}
	}
	for _, g := range got {
		key, _, _ := strings.Cut(g, ": ")
		if _, ok := want[key]; !ok {
			t.Errorf("a good operation was refused: %s", g)
		}
	}
}
