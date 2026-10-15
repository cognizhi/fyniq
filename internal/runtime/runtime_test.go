package runtime

import (
	"errors"
	"testing"
)

func code(err error) string {
	var re *Error
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// req: DEV-07
func TestReleaseBuildRunsOnlyInStagingAndProduction(t *testing.T) {
	for env, want := range map[Env]string{
		EnvStaging: "", EnvProduction: "",
		EnvLocal: "RUNTIME-FLAVOUR-01", EnvCI: "RUNTIME-FLAVOUR-01", EnvDev: "RUNTIME-FLAVOUR-01",
	} {
		if got := code(Check(FlavourRelease, env, nil)); got != want {
			t.Errorf("release in %s: got %q, want %q", env, got, want)
		}
	}
}

// req: DEV-07
func TestReleaseBuildRefusesALinkedLocalAdapter(t *testing.T) {
	linked := []Adapter{{Port: "secrets", Name: "env", Local: true}}
	if got := code(Check(FlavourRelease, EnvProduction, linked)); got != "RUNTIME-FLAVOUR-02" {
		t.Fatalf("got %q, want RUNTIME-FLAVOUR-02", got)
	}
}

// req: SEC-16
// asvs: V13.4.2, V15.2.3
func TestLocalBuildNeverRunsInStagingOrProduction(t *testing.T) {
	for env, want := range map[Env]string{
		EnvLocal: "", EnvCI: "", EnvDev: "",
		EnvStaging: "RUNTIME-FLAVOUR-03", EnvProduction: "RUNTIME-FLAVOUR-03",
	} {
		if got := code(Check(FlavourLocal, env, nil)); got != want {
			t.Errorf("local in %s: got %q, want %q", env, got, want)
		}
	}
}

func TestParseEnv(t *testing.T) {
	if _, err := ParseEnv(""); code(err) != "RUNTIME-ENV-01" {
		t.Errorf("empty: %v", err)
	}
	if _, err := ParseEnv("prod"); code(err) != "RUNTIME-ENV-02" {
		t.Errorf("prod: %v", err)
	}
	if e, err := ParseEnv("staging"); err != nil || e != EnvStaging {
		t.Errorf("staging: %v %v", e, err)
	}
}
