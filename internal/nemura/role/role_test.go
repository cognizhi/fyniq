package role

import (
	"errors"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/runtime"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Role
		says string // a fragment of the refusal; empty means accepted
	}{
		{in: "api", want: API},
		{in: " api ", want: API},
		{in: "", says: "NEMURA_ROLE is not set"},
		{in: "worker", says: "worker role is not built yet"},
		{in: "assistant", says: "assistant role is not built yet"},
		{in: "migrate", says: "migrate role is not built yet"},
		{in: "admin", says: `"admin" is not a role`},
	} {
		got, err := Parse(tc.in)
		if tc.says == "" {
			if err != nil || got != tc.want {
				t.Errorf("Parse(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
			continue
		}
		var re *runtime.Error
		if !errors.As(err, &re) || re.Code != Code || !strings.Contains(re.Message, tc.says) {
			t.Errorf("Parse(%q) = %v; want %s saying %q", tc.in, err, Code, tc.says)
		}
	}
}

func TestEveryBuiltRoleIsAKnownRole(t *testing.T) {
	for _, b := range Built {
		if !containsRole(All, b) {
			t.Errorf("%s is built but not a role", b)
		}
	}
}

func TestServiceName(t *testing.T) {
	if got := API.Service(); got != "nemura-api" {
		t.Fatalf("Service() = %q", got)
	}
}
