//go:build local

package local

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Directory is a drop folder that is a directory on the local disk (local mode and tests; DEV-07: never in a release). It confines itself to Root: a name with a separator, a
// hidden file and a link are refused, so a configured folder cannot be used to read what it was not given.
type Directory struct{ Root string }

var _ ports.FileDrop = Directory{}

// List implements ports.FileDrop.
func (d Directory) List(ctx context.Context) ([]ports.DropFile, error) {
	entries, err := os.ReadDir(d.Root)
	if err != nil {
		return nil, fmt.Errorf("the drop folder cannot be read: %w", err)
	}
	var out []ports.DropFile
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, ports.DropFile{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Read implements ports.FileDrop.
func (d Directory) Read(ctx context.Context, name string, max int64) ([]byte, error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return nil, errors.New("not a file name in the drop folder")
	}
	path := filepath.Join(d.Root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > max {
		return nil, fmt.Errorf("the file is %d bytes; the most is %d", info.Size(), max)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the file is larger than the %d bytes allowed", max)
	}
	return data, nil
}

// FileDropManifest is the local drop folder's connector manifest, which local mode's catalogue adds (internal/wiring); never a release's.
//
//go:embed filedrop-manifest.json
var FileDropManifest []byte
