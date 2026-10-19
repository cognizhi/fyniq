package evidence

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// errImage is a JPEG, PNG or WebP whose structure cannot be walked: refused, never stored as it came.
var errImage = errors.New("the image's structure cannot be read")

// StripMetadata removes an image's metadata, such as its location, camera and comments (FR-DOC-08), and
// keeps what the picture needs (JFIF, the colour profile, Adobe's colour transform). Other types come back
// as they are. It walks the file's own structure, without decoding the picture, on bounded memory.
func StripMetadata(mediaType string, data []byte) ([]byte, error) {
	switch mediaType {
	case JPEG:
		return stripJPEG(data)
	case PNG:
		return stripPNG(data)
	case WebP:
		return stripWebP(data)
	}
	return data, nil
}

// stripJPEG drops APP1 (EXIF, XMP), APP3 to APP13, APP15 and comments, up to the start of the scan.
func stripJPEG(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, errImage
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)))
	out.Write(data[:2])
	i := 2
	for i < len(data) {
		if data[i] != 0xff {
			return nil, errImage
		}
		for i < len(data) && data[i] == 0xff { // fill bytes
			i++
		}
		if i >= len(data) {
			return nil, errImage
		}
		m := data[i]
		i++
		switch {
		case m == 0xd8 || m == 0x01 || (m >= 0xd0 && m <= 0xd7):
			out.Write([]byte{0xff, m})
			continue
		case m == 0xd9: // end of image
			out.Write([]byte{0xff, m})
			return out.Bytes(), nil
		}
		if i+2 > len(data) {
			return nil, errImage
		}
		n := int(binary.BigEndian.Uint16(data[i:]))
		if n < 2 || i+n > len(data) {
			return nil, errImage
		}
		if m == 0xda { // start of scan: the rest is picture data
			out.Write([]byte{0xff, m})
			out.Write(data[i:])
			return out.Bytes(), nil
		}
		drop := m == 0xe1 || (m >= 0xe3 && m <= 0xed) || m == 0xef || m == 0xfe
		if !drop {
			out.Write([]byte{0xff, m})
			out.Write(data[i : i+n])
		}
		i += n
	}
	return nil, errImage
}

// stripPNG drops text, EXIF and time chunks.
func stripPNG(data []byte) ([]byte, error) {
	const sig = 8
	if len(data) < sig || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, errImage
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)))
	out.Write(data[:sig])
	for i := sig; i < len(data); {
		if i+8 > len(data) {
			return nil, errImage
		}
		n := uint64(binary.BigEndian.Uint32(data[i:]))
		end := uint64(i) + 12 + n
		if end > uint64(len(data)) {
			return nil, errImage
		}
		kind := string(data[i+4 : i+8])
		switch kind {
		case "tEXt", "zTXt", "iTXt", "eXIf", "tIME":
		default:
			out.Write(data[i:end])
		}
		i = int(end) // #nosec G115 -- end is at most len(data)
		if kind == "IEND" {
			return out.Bytes(), nil
		}
	}
	return nil, errImage
}

// stripWebP drops the EXIF and XMP chunks and clears their flags in the extended header.
func stripWebP(data []byte) ([]byte, error) {
	if len(data) < 12 {
		return nil, errImage
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)))
	out.Write(data[:12])
	for i := 12; i < len(data); {
		if i+8 > len(data) {
			return nil, errImage
		}
		n := uint64(binary.LittleEndian.Uint32(data[i+4:]))
		end := uint64(i) + 8 + n + n%2
		if end > uint64(len(data)) {
			return nil, errImage
		}
		kind := string(data[i : i+4])
		switch kind {
		case "EXIF", "XMP ":
		case "VP8X":
			chunk := bytes.Clone(data[i:end])
			if len(chunk) > 8 {
				chunk[8] &^= 0x08 | 0x04 // the EXIF and XMP flags
			}
			out.Write(chunk)
		default:
			out.Write(data[i:end])
		}
		i = int(end) // #nosec G115 -- end is at most len(data)
	}
	b := out.Bytes()
	size := len(b) - 8
	if size < 0 || uint64(size) > 0xffffffff {
		return nil, errImage
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(size)) // #nosec G115 -- bounded above
	return b, nil
}
