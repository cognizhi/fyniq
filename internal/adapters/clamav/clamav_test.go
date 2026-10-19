package clamav

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// fakeClamd speaks clamd's INSTREAM protocol: it reads the chunks and answers as clamd would, flagging the
// EICAR test string, or with answer when it is set.
func fakeClamd(t *testing.T, answer string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				cmd := make([]byte, len("zINSTREAM\x00"))
				if _, err := io.ReadFull(c, cmd); err != nil || string(cmd) != "zINSTREAM\x00" {
					_, _ = c.Write([]byte("UNKNOWN COMMAND\x00"))
					return
				}
				var file bytes.Buffer
				for {
					var n uint32
					if err := binary.Read(c, binary.BigEndian, &n); err != nil {
						return
					}
					if n == 0 {
						break
					}
					if _, err := io.CopyN(&file, c, int64(n)); err != nil {
						return
					}
				}
				reply := "stream: OK"
				switch {
				case answer != "":
					reply = answer
				case strings.Contains(file.String(), "EICAR-STANDARD-ANTIVIRUS-TEST-FILE"):
					reply = "stream: Eicar-Test-Signature FOUND"
				}
				_, _ = c.Write([]byte(reply + "\x00"))
			}(c)
		}
	}()
	return l.Addr().String()
}

// The ClamAV adapter passes the Scanner conformance suite against a clamd that speaks the protocol.
// req: FR-DOC-01
// req: SEC-24
// req: NFR-PORT-04
// asvs: V5.2.1
func TestClamdConformance(t *testing.T) {
	porttest.Scanner(t, Scanner{Addr: fakeClamd(t, "")})
}

// A scanner that cannot decide never passes a file: an error, a size limit or no clamd at all is an error.
// req: FR-DOC-01
// asvs: V5.2.1
func TestClamdFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, answer := range []string{"INSTREAM size limit exceeded. ERROR", "stream: lstat() failed ERROR", "garbage"} {
		if r, err := (Scanner{Addr: fakeClamd(t, answer)}).Scan(ctx, strings.NewReader("%PDF-1.7")); err == nil || r.Clean {
			t.Errorf("clamd answered %q: %+v, %v; want an error", answer, r, err)
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	if r, err := (Scanner{Addr: addr}).Scan(ctx, strings.NewReader("%PDF-1.7")); err == nil || r.Clean {
		t.Errorf("no clamd: %+v, %v; want an error", r, err)
	}
}
