package letters

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// Document is what the renderer draws: the filled blocks and a footer line.
type Document struct {
	Blocks []Block
	Footer string
}

// Renderer draws a letter as PDF bytes (design D3). The production adapter calls the platform's private render service, with its sandbox and self-hosted fonts
// (`[live-verify]`); the local adapter below writes a plain Latin text PDF. The hash of an issued letter is of the bytes issued, whichever adapter made them.
type Renderer interface {
	Render(ctx context.Context, d Document) ([]byte, error)
}

// ErrUnsupported is a character the local renderer cannot draw: it refuses it, so a local letter is never silently wrong.
type ErrUnsupported struct{ R rune }

func (e ErrUnsupported) Error() string {
	return fmt.Sprintf("the local renderer cannot draw %q: Latin letters, digits and ordinary punctuation only; use the platform's render service for other scripts", e.R)
}

// LocalFooter marks a letter rendered by the local adapter.
const LocalFooter = "Local rendering: for the local build only."

// Local is the local renderer: A4, Helvetica, a plain layout with no dates or random values in the file, so the same letter gives the same bytes.
type Local struct{}

var _ Renderer = Local{}

const (
	pageW, pageH = 595, 842
	margin       = 56
	lineH        = 15
	maxChars     = 88 // about 480 points at 11 point Helvetica: a deterministic wrap
)

type line struct {
	text string
	bold bool
	size int
	gap  int // extra space before the line
}

func (Local) Render(_ context.Context, d Document) ([]byte, error) {
	var lines []line
	for _, b := range d.Blocks {
		switch b.Kind {
		case Letterhead:
			for _, l := range strings.Split(b.Text, "\n") {
				lines = append(lines, line{text: l, bold: len(lines) == 0, size: 12})
			}
			lines = append(lines, line{gap: 6})
		case Heading:
			lines = append(lines, line{text: b.Text, bold: true, size: 14, gap: 10})
		case Signature:
			lines = append(lines, line{gap: 28})
			for _, l := range strings.Split(b.Text, "\n") {
				lines = append(lines, line{text: l, size: 11})
			}
		default:
			for i, l := range strings.Split(b.Text, "\n") {
				for j, w := range wrap(l) {
					g := 0
					if i == 0 && j == 0 {
						g = 8
					}
					lines = append(lines, line{text: w, size: 11, gap: g})
				}
			}
		}
	}
	// Check every character first: a letter the renderer cannot draw is refused whole.
	all := d.Footer + LocalFooter
	for _, l := range lines {
		all += l.text
	}
	for _, r := range all {
		if r > 255 || (r < 32 && r != '\n') || (r >= 127 && r < 160) {
			return nil, ErrUnsupported{r}
		}
	}
	var pages [][]string // each page's content stream operations
	var ops strings.Builder
	y := pageH - margin
	flush := func() {
		pages = append(pages, []string{ops.String()})
		ops.Reset()
		y = pageH - margin
	}
	for _, l := range lines {
		y -= l.gap
		if y < margin+30 {
			flush()
		}
		if l.text != "" {
			font := "F1"
			if l.bold {
				font = "F2"
			}
			fmt.Fprintf(&ops, "BT /%s %d Tf %d %d Td (%s) Tj ET\n", font, l.size, margin, y, escape(l.text))
		}
		y -= lineH
	}
	flush()
	foot := strings.TrimSpace(d.Footer + " " + LocalFooter)
	var out bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n")
	n := len(pages)
	kids := make([]string, n)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 5+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, p := range pages {
		stream := p[0] + fmt.Sprintf("BT /F1 8 Tf %d 30 Td (%s  Page %d of %d) Tj ET\n", margin, escape(foot), i+1, n)
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>", pageW, pageH, 6+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return latin1(out.String()), nil
}

func wrap(s string) []string {
	var out []string
	for len([]rune(s)) > maxChars {
		r := []rune(s)
		cut := maxChars
		for cut > 0 && r[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = maxChars
		}
		out = append(out, strings.TrimRight(string(r[:cut]), " "))
		s = strings.TrimLeft(string(r[cut:]), " ")
	}
	return append(out, s)
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}

// latin1 writes each rune below 256 as one byte, as WinAnsiEncoding reads it.
func latin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return out
}
