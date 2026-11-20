// Package sftp is a bank's SFTP folder as a ports.FileDrop (PRD-001 FR-BNK-17; p2-bank-feeds-payments task 2.1, design D4): the host-to-host half of a bank's statement delivery
// (IDEAL Connect leaves MT940 and MT942 files in a folder). It connects with a private key to a host whose key is pinned by its SHA-256 fingerprint, lists the folder's plain
// files and reads one whole; it never writes, renames or deletes, so a delivered statement stays where the bank put it. A connection is made for each call and closed after it, through
// the connector's egress-guarded dialer. Against a real bank it is [live-verify]: the host, the key exchange the bank accepts, the file naming and PGP are known only from onboarding.
package sftp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strings"
	"time"

	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Drop is a folder on a bank's SFTP server.
type Drop struct {
	Addr          string // host:port
	User          string
	Folder        string // the folder the bank delivers into, such as /outbox
	PrivateKey    []byte // an OpenSSH or PEM private key, without a passphrase
	HostKeySHA256 string // the server's host key as ssh-keygen -l prints it: SHA256:<base64>; a server with any other key is refused
	// Dial connects to Addr; nil dials directly. The connector runtime gives the egress guard's dialer.
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Timeout time.Duration // 30 seconds when zero
}

var _ ports.FileDrop = Drop{}

// ErrHostKey is a server whose host key is not the pinned one.
var ErrHostKey = errors.New("the server's host key is not the one pinned for this connector: check the fingerprint with the bank")

func (d Drop) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return 30 * time.Second
}

// connect opens an SFTP session; the caller closes it.
func (d Drop) connect(ctx context.Context) (*pkgsftp.Client, func(), error) {
	signer, err := ssh.ParsePrivateKey(d.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("the private key is not one Fyniq can read (it must have no passphrase): %w", err)
	}
	pinned := strings.TrimSpace(d.HostKeySHA256)
	if !strings.HasPrefix(pinned, "SHA256:") {
		return nil, nil, errors.New("the host key fingerprint is not set: enter it as SHA256:… from the bank")
	}
	cfg := &ssh.ClientConfig{
		User: d.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, Timeout: d.timeout(),
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(key)), []byte(pinned)) != 1 {
				return ErrHostKey
			}
			return nil
		},
	}
	dial := d.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: d.timeout()}).DialContext
	}
	// The whole call, from the dial to the last read, is bounded by the timeout: when it ends the connection is closed under whatever is waiting.
	ctx, cancel := context.WithTimeout(ctx, d.timeout())
	raw, err := dial(ctx, "tcp", d.Addr)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("the SFTP server cannot be reached: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	conn, chans, reqs, err := ssh.NewClientConn(raw, d.Addr, cfg)
	if err != nil {
		stop()
		cancel()
		_ = raw.Close()
		return nil, nil, fmt.Errorf("the SFTP server refused the connection: %w", err)
	}
	client, err := pkgsftp.NewClient(ssh.NewClient(conn, chans, reqs))
	if err != nil {
		stop()
		cancel()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("the SFTP session could not start: %w", err)
	}
	// The connection closes first so the session ends at once, whatever the server does with its side of the channel.
	return client, func() { stop(); cancel(); _ = conn.Close(); _ = client.Close() }, nil
}

// List implements ports.FileDrop: the folder's regular files that are not hidden, by name.
func (d Drop) List(ctx context.Context) ([]ports.DropFile, error) {
	c, closeAll, err := d.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeAll()
	infos, err := c.ReadDir(d.Folder)
	if err != nil {
		return nil, fmt.Errorf("the folder %s cannot be read: %w", d.Folder, err)
	}
	var out []ports.DropFile
	for _, i := range infos {
		if !i.Mode().IsRegular() || strings.HasPrefix(i.Name(), ".") {
			continue
		}
		out = append(out, ports.DropFile{Name: i.Name(), Size: i.Size(), ModTime: i.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Read implements ports.FileDrop.
func (d Drop) Read(ctx context.Context, name string, max int64) ([]byte, error) {
	if name == "" || name != path.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "\\\x00") {
		return nil, errors.New("not a file name in the drop folder")
	}
	c, closeAll, err := d.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeAll()
	full := path.Join(d.Folder, name)
	info, err := c.Lstat(full)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > max {
		return nil, fmt.Errorf("the file is %d bytes; the most is %d", info.Size(), max)
	}
	f, err := c.Open(full)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be opened: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read: %w", name, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the file is larger than the %d bytes allowed", max)
	}
	return data, nil
}
