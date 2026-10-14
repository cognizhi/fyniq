//go:build local

package local

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/runtime"
)

// req: NFR-PORT-04
func TestSecretsConformance(t *testing.T) {
	porttest.Secrets(t, func(t *testing.T, seed map[string]string) ports.Secrets {
		return NewSecrets(func(k string) (string, bool) {
			v, ok := seed[k[len("FYNIQ_SECRET_"):]]
			return v, ok
		})
	})
}

// req: NFR-PORT-04
func TestBlobStoreConformance(t *testing.T) {
	porttest.BlobStore(t, func(t *testing.T) ports.BlobStore {
		d, err := NewDir(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return d
	})
}

// req: NFR-PORT-04
func TestQueueConformance(t *testing.T) {
	porttest.Queue(t, func(t *testing.T) ports.Queue { return NewMemory() })
}

// req: DEV-07
func TestLocalAdaptersRegisterAsLocal(t *testing.T) {
	var n int
	for _, a := range runtime.Adapters() {
		if !a.Local {
			t.Errorf("%s/%s registered from the local package without Local", a.Port, a.Name)
		}
		n++
	}
	if n < 3 {
		t.Fatalf("want the three local adapters registered, got %d", n)
	}
}
