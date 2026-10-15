//go:build !local

package main

import (
	"fmt"

	"github.com/cognizhi/fyniq/internal/ports"
)

func init() {
	auditAdapters = func() (ports.BlobStore, ports.Signer, error) {
		return nil, nil, fmt.Errorf("the Cloud Storage and Cloud KMS adapters arrive with p0-infra-opentofu; this release build cannot anchor yet")
	}
}
