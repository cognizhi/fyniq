package main

import (
	"fmt"
	"strings"

	"github.com/cognizhi/fyniq/internal/runtime"
)

// Region is where Nemura's data lives: Singapore, like Fyniq's (PRD-002 NM-NFR-01, CTL-DP-07).
const Region = "asia-southeast1"

// checkRegion refuses a region setting outside Singapore before any role starts. The release flavour takes NEMURA_REGION unset (the deployment
// is Singapore's) or exactly Region; the local flavour has no region and also takes "local".
func checkRegion(getenv func(string) string, flavour string) error {
	got := strings.ToLower(strings.TrimSpace(getenv("NEMURA_REGION")))
	switch {
	case got == "" || got == Region:
		return nil
	case got == "local" && flavour == "local":
		return nil
	}
	return fmt.Errorf("NEMURA-REGION-01: NEMURA_REGION=%q is outside Singapore: Nemura's data stays in %s (NM-NFR-01)", got, Region)
}

var flavour = runtime.Flavour
