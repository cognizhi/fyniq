package local

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Services that share a data directory and start together hold one key (the local stack's shared volume): each that
// finds none makes one, and only the first is kept; the others read it, the file is the owner's alone (0600), and no
// file half written or left over is ever seen.
// req: DEV-02
func TestServicesStartingTogetherHoldOneKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "fields.key")
	seeds := make([][]byte, 16)
	var wg sync.WaitGroup
	for i := range seeds {
		wg.Go(func() {
			s, err := loadOrCreateSeed(path, 64)
			if err != nil {
				t.Error(err)
			}
			seeds[i] = s
		})
	}
	wg.Wait()
	kept, err := os.ReadFile(path)
	if err != nil || len(kept) != 64 {
		t.Fatalf("the kept seed: %d bytes, %v", len(kept), err)
	}
	for i, s := range seeds {
		if !bytes.Equal(s, kept) {
			t.Errorf("service %d holds another key", i)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the key's mode: %v %v", fi.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".seed-*")); len(left) != 0 {
		t.Errorf("left over: %v", left)
	}
	if again, err := loadOrCreateSeed(path, 64); err != nil || !bytes.Equal(again, kept) {
		t.Errorf("read again: %v", err)
	}
}
