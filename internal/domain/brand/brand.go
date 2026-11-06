// Package brand checks an organisation's logo and accent colour (FR-ORG-09, PRD-001 rev 0.200): an SVG is reduced to a
// safe subset (no scripts, event handlers, external references, embedded images or fonts, no document type or
// entities), a PNG is decoded and written again without its metadata, and the accent is one of the contrast-checked
// options. It reads bytes and returns bytes: storing and serving them is the API's.
package brand

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"maps"
	"slices"
	"strings"
)

// The stable codes of a refused file (ENG-23).
const (
	CodeFormat = "BRAND-01" // not an SVG or a PNG, or not one Fyniq reads
	CodeSize   = "BRAND-02" // too large, too small, too many elements or nested too deep
	CodeAccent = "BRAND-03" // not one of the accent options
)

// Error is a refused file or colour, with its code.
type Error struct {
	Code, Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func refuse(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Limits.
const (
	MaxSVG      = 512 << 10 // bytes
	MaxPNG      = 2 << 20   // bytes
	MaxElements = 20000
	MaxDepth    = 64
	MinLogoPNG  = 512  // a logo PNG's least width, in pixels, so emails and PDFs print it sharp
	MinIconPNG  = 192  // an icon PNG's least side
	MaxPNGSide  = 4096 // pixels, against decompression bombs
)

// Kind is what a file is for: a logo for light or dark backgrounds, or the square icon.
type Kind string

const (
	Light Kind = "light"
	Dark  Kind = "dark"
	Icon  Kind = "icon"
)

// Clean is a file made safe: its media type and bytes, and what was removed from an SVG.
type Clean struct {
	MediaType string // image/svg+xml or image/png
	Data      []byte
	Removed   Removed
}

// Removed counts what the SVG sanitiser took out.
type Removed struct {
	Elements   map[string]int // element name → how many (script, style, foreignObject, image, …)
	Attributes int            // event handlers and attributes outside the safe set
	External   int            // references to anything outside the file
}

// Any reports whether anything was removed.
func (r Removed) Any() bool { return len(r.Elements) > 0 || r.Attributes > 0 || r.External > 0 }

// Check makes a file of kind safe: an SVG (by its content, not its name) through the sanitiser, a PNG decoded and
// written again. Anything else is refused.
func Check(kind Kind, data []byte) (Clean, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n\xef\xbb\xbf")
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return checkPNG(kind, data)
	case bytes.HasPrefix(trimmed, []byte("<")):
		return SanitiseSVG(data)
	}
	return Clean{}, refuse(CodeFormat, "the file is neither an SVG nor a PNG")
}

func checkPNG(kind Kind, data []byte) (Clean, error) {
	if len(data) > MaxPNG {
		return Clean{}, refuse(CodeSize, "the PNG is larger than %d MB", MaxPNG>>20)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Clean{}, refuse(CodeFormat, "the PNG cannot be read")
	}
	if cfg.Width > MaxPNGSide || cfg.Height > MaxPNGSide {
		return Clean{}, refuse(CodeSize, "the PNG is larger than %d pixels on a side", MaxPNGSide)
	}
	switch {
	case kind == Icon && (cfg.Width != cfg.Height || cfg.Width < MinIconPNG):
		return Clean{}, refuse(CodeSize, "the icon is a square PNG at least %d pixels wide", MinIconPNG)
	case kind != Icon && cfg.Width < MinLogoPNG:
		return Clean{}, refuse(CodeSize, "the logo PNG is at least %d pixels wide, so it prints sharp", MinLogoPNG)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Clean{}, refuse(CodeFormat, "the PNG cannot be read")
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img); err != nil {
		return Clean{}, err
	}
	return Clean{MediaType: "image/png", Data: out.Bytes()}, nil
}

// Size returns the pixel size of a PNG, for the page that shows it.
func Size(data []byte) (image.Point, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	return image.Pt(cfg.Width, cfg.Height), err
}

// The SVG elements kept, with their children; everything else goes with its children, but <a>, which is unwrapped.
var keepElements = setOf("svg", "g", "defs", "title", "desc", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon",
	"text", "tspan", "linearGradient", "radialGradient", "stop", "clipPath", "mask", "pattern", "symbol", "use")

// The attributes kept on any kept element; anything else, and every on* handler, goes.
var keepAttributes = setOf("id", "class", "transform", "fill", "fill-opacity", "fill-rule", "stroke", "stroke-width", "stroke-opacity",
	"stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "stroke-dasharray", "stroke-dashoffset", "opacity", "clip-path", "clip-rule",
	"mask", "d", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "width", "height", "points", "viewBox",
	"preserveAspectRatio", "version", "offset", "stop-color", "stop-opacity", "gradientUnits", "gradientTransform", "spreadMethod", "fx",
	"fy", "fr", "patternUnits", "patternContentUnits", "patternTransform", "clipPathUnits", "maskUnits", "maskContentUnits", "font-size",
	"font-weight", "font-style", "font-family", "text-anchor", "dominant-baseline", "letter-spacing", "href", "style", "color", "display",
	"visibility")

// The style properties kept in a style attribute.
var keepStyles = setOf("fill", "fill-opacity", "fill-rule", "stroke", "stroke-width", "stroke-opacity", "stroke-linecap", "stroke-linejoin",
	"stroke-miterlimit", "stroke-dasharray", "stroke-dashoffset", "opacity", "stop-color", "stop-opacity", "font-size", "font-weight",
	"font-style", "font-family", "text-anchor", "letter-spacing", "color", "display", "visibility", "clip-rule", "mix-blend-mode")

const svgNS, xlinkNS = "http://www.w3.org/2000/svg", "http://www.w3.org/1999/xlink"

// SanitiseSVG keeps only the safe subset of an SVG: the shapes, gradients, clips, masks and text, their geometry and
// paint. It removes scripts, styles sheets, foreign objects, embedded images, fonts, animations and links' targets,
// every event handler, and every reference outside the file (a href or url() not to an #id); it refuses a document type
// or an entity, which could expand or reach out. The result is written again from scratch, so nothing passes through
// unread.
func SanitiseSVG(data []byte) (Clean, error) {
	if len(data) > MaxSVG {
		return Clean{}, refuse(CodeSize, "the SVG is larger than %d KB", MaxSVG>>10)
	}
	if bytes.Contains(data, []byte("<!DOCTYPE")) || bytes.Contains(data, []byte("<!ENTITY")) {
		return Clean{}, refuse(CodeFormat, "the SVG declares a document type or entities, which Fyniq does not read")
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	dec.Entity = map[string]string{} // only XML's own five
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	removed := Removed{Elements: map[string]int{}}
	var stack []bool // per open element: written (true) or skipped / unwrapped (false)
	skip, elements, root := 0, 0, false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Clean{}, refuse(CodeFormat, "the SVG is not well-formed XML")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elements++
			if elements > MaxElements || len(stack) >= MaxDepth {
				return Clean{}, refuse(CodeSize, "the SVG has more than %d elements, or nests deeper than %d", MaxElements, MaxDepth)
			}
			name := t.Name.Local
			if !root {
				if name != "svg" || (t.Name.Space != "" && t.Name.Space != svgNS) {
					return Clean{}, refuse(CodeFormat, "the file is not an SVG")
				}
				root = true
			}
			if skip > 0 {
				skip++
				stack = append(stack, false)
				continue
			}
			if name == "a" { // a link's shapes stay; the link goes
				removed.Elements["a"]++
				stack = append(stack, false)
				continue
			}
			if !keepElements[name] || (t.Name.Space != "" && t.Name.Space != svgNS) {
				removed.Elements[name]++
				skip = 1
				stack = append(stack, false)
				continue
			}
			attrs, gone, external := keepAttrs(t.Attr)
			removed.Attributes += gone
			removed.External += external
			if name == "svg" && len(stack) == 0 {
				attrs = append([]xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: svgNS}}, attrs...)
			}
			if err := enc.EncodeToken(xml.StartElement{Name: xml.Name{Local: name}, Attr: attrs}); err != nil {
				return Clean{}, err
			}
			stack = append(stack, true)
		case xml.EndElement:
			if len(stack) == 0 {
				return Clean{}, refuse(CodeFormat, "the SVG is not well-formed XML")
			}
			written := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if skip > 0 {
				skip--
				continue
			}
			if written {
				if err := enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: t.Name.Local}}); err != nil {
					return Clean{}, err
				}
			}
		case xml.CharData:
			if skip == 0 && len(stack) > 0 {
				if err := enc.EncodeToken(t.Copy()); err != nil {
					return Clean{}, err
				}
			}
		case xml.ProcInst, xml.Comment, xml.Directive:
			// dropped: processing instructions, comments and declarations carry nothing a logo needs
		}
	}
	if !root {
		return Clean{}, refuse(CodeFormat, "the file is not an SVG")
	}
	if err := enc.Flush(); err != nil {
		return Clean{}, err
	}
	if len(removed.Elements) == 0 {
		removed.Elements = nil
	}
	return Clean{MediaType: "image/svg+xml", Data: out.Bytes(), Removed: removed}, nil
}

// keepAttrs keeps the safe attributes, and in them only references inside the file; it says how many it removed and
// how many of those reached outside.
func keepAttrs(in []xml.Attr) (out []xml.Attr, gone, external int) {
	for _, a := range in {
		local := a.Name.Local
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && local == "xmlns") {
			continue // namespaces are written again
		}
		if a.Name.Space == xlinkNS && local == "href" {
			local = "href" // xlink:href becomes href
		} else if a.Name.Space != "" && a.Name.Space != svgNS {
			gone++
			continue
		}
		v := a.Value
		low := strings.ToLower(strings.Join(strings.Fields(v), ""))
		switch {
		case strings.HasPrefix(strings.ToLower(local), "on"):
			gone++
			continue
		case !keepAttributes[local]:
			gone++
			continue
		case local == "href":
			if !strings.HasPrefix(v, "#") {
				gone, external = gone+1, external+1
				continue
			}
		case local == "style":
			kept, dropped, ext := keepStyle(v)
			gone, external = gone+dropped, external+ext
			if kept == "" {
				continue
			}
			v = kept
		case strings.Contains(low, "url(") && !onlyLocalURLs(low), strings.Contains(low, "javascript:"), strings.Contains(low, "data:"):
			gone, external = gone+1, external+1
			continue
		}
		out = append(out, xml.Attr{Name: xml.Name{Local: local}, Value: v})
	}
	slices.SortStableFunc(out, func(a, b xml.Attr) int { return strings.Compare(a.Name.Local, b.Name.Local) })
	return out, gone, external
}

// keepStyle keeps the safe declarations of a style attribute.
func keepStyle(v string) (kept string, gone, external int) {
	var parts []string
	for decl := range strings.SplitSeq(v, ";") {
		prop, val, ok := strings.Cut(decl, ":")
		prop, val = strings.ToLower(strings.TrimSpace(prop)), strings.TrimSpace(val)
		if !ok || prop == "" {
			continue
		}
		low := strings.ToLower(strings.Join(strings.Fields(val), ""))
		switch {
		case strings.Contains(low, "url(") && !onlyLocalURLs(low), strings.Contains(low, "javascript:"), strings.Contains(low, "expression("),
			strings.Contains(low, "@import"), strings.Contains(low, "data:"):
			gone, external = gone+1, external+1
		case !keepStyles[prop]:
			gone++
		default:
			parts = append(parts, prop+":"+val)
		}
	}
	return strings.Join(parts, ";"), gone, external
}

// onlyLocalURLs reports whether every url(…) in a value points inside the file (url(#id)).
func onlyLocalURLs(low string) bool {
	for rest := low; ; {
		i := strings.Index(rest, "url(")
		if i < 0 {
			return true
		}
		rest = strings.TrimLeft(rest[i+4:], `'"`)
		if !strings.HasPrefix(rest, "#") {
			return false
		}
	}
}

// RemovedNames are the names of the elements removed, sorted, for the page that says what was cleaned.
func (r Removed) RemovedNames() []string { return slices.Sorted(maps.Keys(r.Elements)) }

// Accents are the accent colours a document or email may use: each has at least 4.5:1 contrast with white, so white
// text on it and it as text on white both read (WCAG 2.2 AA). Violet is left out: it marks AI content.
var Accents = []string{"#0F766E", "#1E3A8A", "#374151", "#7C2D12", "#166534", "#9F1239", "#1F2937", "#0E7490"}

// AccentNames are the accent options' names, for the page that offers them.
var AccentNames = map[string]string{"#0F766E": "Deep teal", "#1E3A8A": "Navy", "#374151": "Slate", "#7C2D12": "Rust",
	"#166534": "Forest", "#9F1239": "Crimson", "#1F2937": "Charcoal", "#0E7490": "Ocean"}

// CheckAccent refuses a colour that is not one of Accents; empty keeps the design's default.
func CheckAccent(c string) (string, error) {
	if c == "" {
		return "", nil
	}
	up := strings.ToUpper(c)
	if !slices.Contains(Accents, up) {
		return "", refuse(CodeAccent, "the accent colour is not one of the contrast-checked options")
	}
	return up, nil
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
