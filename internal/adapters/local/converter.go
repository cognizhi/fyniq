//go:build local

package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
)

// StandInConverter is the Converter port for tests and for a developer who asks for it (FR-DOC-08): it makes a small
// picture from the file's own bytes, so a test proves what happens around a conversion (the scan of both files, the
// type from the content, the stripped metadata, both hashes) without a decoder. It does NOT convert a photo: the picture
// is a flat colour taken from the file's SHA-256. Local mode leaves it off unless FYNIQ_CONVERTER_STANDIN is set, so a
// real HEIC photo is refused with its plain message rather than turned into a grey square; no real engine is in the
// pinned toolchain, and the cloud's converter service takes its place ([live-verify], cmd/converter).
//
// A file that is not what its kind needs (a HEIC conversion of anything but a HEIC picture, a preview of a file with no
// readable type) is ErrNotConvertible, as a damaged photo is.
type StandInConverter struct{}

var _ ports.Converter = StandInConverter{}

// Convert implements ports.Converter.
func (StandInConverter) Convert(ctx context.Context, kind ports.ConvertKind, data []byte) (ports.Converted, error) {
	if err := ctx.Err(); err != nil {
		return ports.Converted{}, err
	}
	sum := sha256.Sum256(data)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	c := color.RGBA{R: sum[0], G: sum[1], B: sum[2], A: 255}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	switch kind {
	case ports.ConvertHEICToJPEG:
		if !evidence.IsHEIC(data) {
			return ports.Converted{}, ports.ErrNotConvertible
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			return ports.Converted{}, err
		}
		return ports.Converted{MediaType: "image/jpeg", Data: buf.Bytes()}, nil
	case ports.ConvertPreviewPNG:
		if _, ok := evidence.Sniff(data, ""); !ok {
			return ports.Converted{}, ports.ErrNotConvertible
		}
		if err := png.Encode(&buf, img); err != nil {
			return ports.Converted{}, err
		}
		return ports.Converted{MediaType: "image/png", Data: buf.Bytes()}, nil
	}
	return ports.Converted{}, ports.ErrNotConvertible
}
