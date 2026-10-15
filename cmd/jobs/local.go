//go:build local

package main

import (
	"cmp"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/ports"
)

func init() {
	auditAdapters = func() (ports.BlobStore, ports.Signer, error) {
		data := cmp.Or(os.Getenv("FYNIQ_DATA_DIR"), ".data")
		blobs, err := local.NewDir(filepath.Join(data, "blobs"))
		if err != nil {
			return nil, nil, err
		}
		signer, err := local.LoadOrCreateEd25519(filepath.Join(data, "keys", "audit-anchor.ed25519"))
		return blobs, signer, err
	}
}
