//go:build local

package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The platform version answers without a credential with the version and the build's commit, time and flavour, as the
// service's /version does, for the About dialog (REL-05).
// req: REL-05
func TestThePlatformVersionNeedsNoCredential(t *testing.T) {
	e := newEnv(t)
	r := e.call(http.MethodGet, "/v1/platform/version", "", nil)
	var v PlatformVersion
	if r.Status != http.StatusOK || json.Unmarshal([]byte(r.Raw), &v) != nil {
		t.Fatalf("%d %s", r.Status, r.Raw)
	}
	if v != (PlatformVersion{Service: "api", Version: "0.0.0-test", Commit: "3f2c1a9d", BuildTime: "2026-10-15T08:00:00Z", Flavour: "local"}) {
		t.Errorf("the version: %+v", v)
	}
}
