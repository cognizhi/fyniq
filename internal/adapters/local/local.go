//go:build local

// Package local holds the local adapters of every port (ADR-T4). It compiles only with the
// `local` build tag, so a release binary cannot contain it (DEV-07); each adapter also
// registers itself so that runtime.Check can refuse one that slipped in.
package local

import "github.com/cognizhi/fyniq/internal/runtime"

func init() {
	for _, a := range []runtime.Adapter{
		{Port: "secrets", Name: "env", Local: true},
		{Port: "blobstore", Name: "dir", Local: true},
		{Port: "queue", Name: "memory", Local: true},
	} {
		runtime.RegisterAdapter(a)
	}
}
