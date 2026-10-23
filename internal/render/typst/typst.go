// Package typst compiles a layout (internal/render) to PDF with the pinned Typst binary (ADR-T1; Typst 0.15.1 in
// mise.toml and the render image), as PDF/A-2a and PDF/UA-1, the same bytes for the same layout and creation time
// (FR-TPL-04's identical reprints). Each run is sandboxed (FR-TPL-02): a fresh folder holding only the source, the
// layout and the logo, which is also Typst's root, so it reads nothing else; the vendored fonts and no others; no
// packages, so nothing is fetched; an empty environment; and a time limit.
package typst

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/render"
)

//go:embed document.typ
var source []byte

//go:embed fonts
var fonts embed.FS

// Standards are the PDF standards every document conforms to: archival and accessible (ADR-T1).
const Standards = "a-2a,ua-1"

// Timeout is how long one compile may take.
const Timeout = 30 * time.Second

// Compiler runs the Typst binary.
type Compiler struct {
	binary string
	fonts  string // the vendored fonts, written out once
}

// New finds the Typst binary (binary, or "typst" on the PATH when it is "") and writes the vendored fonts to a folder
// of their own under dir (os.TempDir when it is ""), which a read-only root file system leaves writable.
func New(binary, dir string) (*Compiler, error) {
	if binary == "" {
		binary = "typst"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("typst: the Typst binary %q: %w (install the toolchain with make setup; the render image ships it)", binary, err)
	}
	out, err := os.MkdirTemp(dir, "fyniq-fonts-")
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(fonts, "fonts", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".ttf") {
			return err
		}
		b, err := fonts.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(out, filepath.Base(p)), b, 0o400)
	})
	if err != nil {
		return nil, err
	}
	return &Compiler{binary: path, fonts: out}, nil
}

// Version is the Typst binary's version line, such as "typst 0.15.1 (9dfd3a08)".
func (c *Compiler) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, c.binary, "--version").Output()
	return strings.TrimSpace(string(out)), err
}

// Input is one document to compile.
type Input struct {
	Layout  render.Layout
	Logo    []byte    // a PNG, when the layout shows the logo
	Created time.Time // the PDF's creation date: the document's issue, so a reprint is the same bytes
}

// sandbox writes the input to a fresh folder and returns it and a function that removes it.
func sandbox(in Input) (string, func(), error) {
	dir, err := os.MkdirTemp("", "fyniq-render-")
	if err != nil {
		return "", nil, err
	}
	done := func() { _ = os.RemoveAll(dir) }
	layout, err := json.Marshal(in.Layout)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "layout.json"), layout, 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "document.typ"), source, 0o600)
	}
	if err == nil && len(in.Logo) > 0 {
		err = os.WriteFile(filepath.Join(dir, "logo.png"), in.Logo, 0o600)
	}
	if err == nil {
		err = os.Mkdir(filepath.Join(dir, "packages"), 0o700)
	}
	if err != nil {
		done()
		return "", nil, err
	}
	return dir, done, nil
}

// run runs Typst in dir with args, after the sandbox's own.
func (c *Compiler) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	base := []string{"--root", dir, "--font-path", c.fonts, "--ignore-system-fonts", "--ignore-embedded-fonts",
		"--package-path", filepath.Join(dir, "packages"), "--package-cache-path", filepath.Join(dir, "packages"),
		"--diagnostic-format", "short"}
	cmd := exec.CommandContext(ctx, c.binary, slices.Concat(args[:1], base, args[1:])...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "TZ=UTC", "NO_COLOR=1"} // nothing of the service's environment
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("typst: the document took longer than %s", Timeout)
		}
		return nil, fmt.Errorf("typst: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// PDF compiles in to PDF/A-2a and PDF/UA-1.
func (c *Compiler) PDF(ctx context.Context, in Input) ([]byte, error) {
	dir, done, err := sandbox(in)
	if err != nil {
		return nil, err
	}
	defer done()
	if _, err := c.run(ctx, dir, "compile", "--creation-timestamp", strconv.FormatInt(in.Created.Unix(), 10), "--pdf-standard", Standards,
		"document.typ", "document.pdf"); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, "document.pdf"))
}

// Particulars are the IRAS particulars the compiled document holds (render.Required lists those it must), each once,
// sorted: what the layout marked and Typst laid out.
func (c *Compiler) Particulars(ctx context.Context, in Input) ([]string, error) {
	dir, done, err := sandbox(in)
	if err != nil {
		return nil, err
	}
	defer done()
	out, err := c.run(ctx, dir, "query", "document.typ", "<particular>", "--field", "value", "--format", "json")
	if err != nil {
		return nil, err
	}
	var found []string
	if err := json.Unmarshal(out, &found); err != nil {
		return nil, fmt.Errorf("typst: the particulars: %w", err)
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

// Missing are the particulars of want that got lacks.
func Missing(want, got []string) []string {
	var out []string
	for _, p := range want {
		if !slices.Contains(got, p) {
			out = append(out, p)
		}
	}
	return out
}
