//go:build local

package local

import (
	"bytes"
	"context"
	"io"

	"github.com/cognizhi/fyniq/internal/ports"
)

// EICAR is the industry's harmless antivirus test file: every scanner reports it, so tests can prove a
// flagged upload is refused without handling malware. It is split so this source file is not itself flagged.
const EICAR = `X5O!P%@AP[4\PZX54(P^)7CC)7}$` + `EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// EICARScanner is the Scanner port for tests and local mode (FR-DOC-01): it flags the EICAR test string
// anywhere in the file and passes everything else. ClamAV takes its place in the cloud (SEC-24).
type EICARScanner struct{}

var _ ports.Scanner = EICARScanner{}

// Scan implements ports.Scanner. It reads in chunks and keeps an overlap, so the string is found across
// chunk boundaries without holding the whole file.
func (EICARScanner) Scan(ctx context.Context, r io.Reader) (ports.ScanResult, error) {
	sig := []byte(EICAR)
	buf := make([]byte, 64<<10)
	var tail []byte
	for {
		if err := ctx.Err(); err != nil {
			return ports.ScanResult{}, err
		}
		n, err := r.Read(buf)
		if n > 0 {
			window := append(tail, buf[:n]...)
			if bytes.Contains(window, sig) {
				return ports.ScanResult{Signature: "Eicar-Test-Signature"}, nil
			}
			keep := min(len(window), len(sig)-1)
			tail = append([]byte(nil), window[len(window)-keep:]...)
		}
		if err == io.EOF {
			return ports.ScanResult{Clean: true}, nil
		}
		if err != nil {
			return ports.ScanResult{}, err
		}
	}
}
