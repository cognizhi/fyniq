//go:build !local

package main

import (
	"context"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ensureDelegation does nothing in a release build: the MCP service's delegation client and its trust relationship are
// OpenTofu's, with Cloud KMS's key [live-verify].
func ensureDelegation(context.Context, *hydra.Admin, ports.Clock) error { return nil }
