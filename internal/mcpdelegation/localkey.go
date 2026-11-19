//go:build local

package mcpdelegation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
)

// LocalKey is the MCP server's delegation key in local mode: derived from a seed both the MCP service and the API read from the
// local stack's configuration, so the API can register its public half with the OAuth server without the two sharing a database
// or a secret store. Local only (SEC-12, DEV-07): it is not compiled into a release build, where the key is Cloud KMS's
// (EC_SIGN_P256_SHA256) and never leaves it [live-verify].
func LocalKey(seed string) (*ecdsa.PrivateKey, error) {
	if seed == "" {
		return nil, errors.New("mcpdelegation: the local key needs a seed")
	}
	sum := sha256.Sum256([]byte("fyniq-local-mcp-delegation-key:" + seed))
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), sum[:])
}
