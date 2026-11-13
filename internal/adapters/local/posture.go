//go:build local

package local

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Posture is local mode's stand-in for Security Command Center (SEC-33): the findings it was given, none by default.
type Posture struct{ Open []ports.PostureFinding }

var _ ports.Posture = Posture{}

// Findings implements ports.Posture.
func (p Posture) Findings(context.Context) ([]ports.PostureFinding, error) { return p.Open, nil }

// LogDir is local mode's stand-in for Cloud Logging (CTL-DP-03): the lines of the *.log files in Dir changed since a
// time, such as the stack's services' logs a developer saves there.
type LogDir struct{ Dir string }

var _ ports.LogReader = LogDir{}

// Read implements ports.LogReader.
func (l LogDir) Read(_ context.Context, since time.Time, limit int) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(l.Dir, "*.log"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	var out []string
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		fh, err := os.Open(f) // #nosec G304 -- a file under the configured log directory
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() && len(out) < limit {
			out = append(out, sc.Text())
		}
		_ = fh.Close()
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
