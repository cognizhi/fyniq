package main

import (
	"context"
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
)

// Opened at an IP address, which WebAuthn does not accept as a relying party, the service still starts and answers a passkey ceremony with
// NM_UNAVAILABLE, in plain words; at a domain name it has passkeys. (Found by the serve tests, which open the API at 127.0.0.1.)
// req: NM-ARC-06
func TestAnIPAddressOriginStartsWithoutPasskeys(t *testing.T) {
	pk, err := newPasskeys(nil, "http://127.0.0.1:8080", "", clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pk.BeginStepUp(context.Background(), api.Session{}); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("a ceremony at an IP address: %v", err)
	}
	pk, err = newPasskeys(nil, "https://people.example.com", "example.com", clock.System{})
	if _, off := pk.(noPasskeys); err != nil || off {
		t.Errorf("a domain name: %T %v", pk, err)
	}
}
