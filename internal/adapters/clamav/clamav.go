// Package clamav is the Scanner port on ClamAV's clamd (FR-DOC-01, SEC-24): the file goes to clamd over its
// INSTREAM protocol in chunks, and clamd answers OK or the signature it found. clamd runs as its own service
// (a sidecar of the API in the cloud, an optional profile of the local stack), outside the Go images' budgets.
package clamav

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// chunk is how much of the file goes in one INSTREAM chunk.
const chunk = 64 << 10

// Scanner talks to clamd at Addr (host:port).
type Scanner struct {
	Addr    string
	Timeout time.Duration // for one scan; 0 means a minute
}

var _ ports.Scanner = Scanner{}

// Scan implements ports.Scanner. Anything but clamd's OK or FOUND is an error, so a file is never passed
// because the scanner could not decide.
func (s Scanner) Scan(ctx context.Context, r io.Reader) (ports.ScanResult, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return ports.ScanResult{}, fmt.Errorf("clamd did not answer: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = clock.System{}.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return ports.ScanResult{}, err
	}
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return ports.ScanResult{}, fmt.Errorf("clamd refused the stream: %w", err)
	}
	buf := make([]byte, 4+chunk)
	for {
		n, rerr := io.ReadFull(r, buf[4:])
		if n > 0 {
			binary.BigEndian.PutUint32(buf, uint32(n)) // #nosec G115 -- n is at most chunk
			if _, err := conn.Write(buf[:4+n]); err != nil {
				return ports.ScanResult{}, fmt.Errorf("clamd stopped reading the file: %w", err)
			}
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			break
		}
		if rerr != nil {
			return ports.ScanResult{}, rerr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return ports.ScanResult{}, fmt.Errorf("clamd stopped reading the file: %w", err)
	}
	reply, err := bufio.NewReader(io.LimitReader(conn, 4096)).ReadString(0)
	if err != nil && !errors.Is(err, io.EOF) {
		return ports.ScanResult{}, fmt.Errorf("clamd's answer cannot be read: %w", err)
	}
	return verdict(strings.TrimRight(reply, "\x00\n"))
}

// verdict reads clamd's answer to INSTREAM: "stream: OK" or "stream: <signature> FOUND".
func verdict(reply string) (ports.ScanResult, error) {
	body, ok := strings.CutPrefix(reply, "stream: ")
	switch {
	case ok && body == "OK":
		return ports.ScanResult{Clean: true}, nil
	case ok && strings.HasSuffix(body, " FOUND"):
		return ports.ScanResult{Signature: strings.TrimSuffix(body, " FOUND")}, nil
	}
	return ports.ScanResult{}, fmt.Errorf("clamd could not scan the file: %q", reply)
}
