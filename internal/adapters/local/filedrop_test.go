//go:build local

package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A local drop folder lists its regular, visible files by name and reads one whole; a name that leaves the folder, a hidden file, a link and a file over the limit are refused (DEV-07).
// req: FR-BNK-17
func TestADirectoryDropListsAndReadsOnlyItsOwnPlainFiles(t *testing.T) {
	ctx := context.Background()
	root, outside := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"b.sta": "bb", "a.sta": "a", ".hidden": "x"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link.sta")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := Directory{Root: root}
	files, err := d.List(ctx)
	if err != nil || len(files) != 2 || files[0].Name != "a.sta" || files[1].Name != "b.sta" || files[1].Size != 2 {
		t.Fatalf("list: %+v, %v", files, err)
	}
	if b, err := d.Read(ctx, "b.sta", 10); err != nil || string(b) != "bb" {
		t.Errorf("read: %q, %v", b, err)
	}
	for _, name := range []string{"../secret", "sub/x", ".hidden", "link.sta", "", "sub", "missing"} {
		if _, err := d.Read(ctx, name, 10); err == nil {
			t.Errorf("%q was read", name)
		}
	}
	if _, err := d.Read(ctx, "b.sta", 1); err == nil {
		t.Error("a file over the limit was read")
	}
}
