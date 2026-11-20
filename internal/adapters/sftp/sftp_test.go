package sftp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkgsftp "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// server is an in-process SFTP server over a temporary directory (the server is not chrooted: a folder is its absolute path), for one authorised key, on a local port: the fixture for the adapter (no network, no account).
type server struct {
	addr, fingerprint string
	clientKey         []byte
	root              string
}

func keyPair(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	_ = pub
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(block)
}

func startServer(t *testing.T) server {
	t.Helper()
	hostSigner, _ := keyPair(t)
	clientSigner, clientPEM := keyPair(t)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(clientSigner.PublicKey().Marshal()) {
			return &ssh.Permissions{}, nil
		}
		return nil, errors.New("unknown key")
	}}
	cfg.AddHostKey(hostSigner)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	root := t.TempDir()
	go func() {
		for {
			raw, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				conn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
				if err != nil {
					_ = raw.Close()
					return
				}
				defer func() { _ = conn.Close() }()
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					if nc.ChannelType() != "session" {
						_ = nc.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					ch, creqs, err := nc.Accept()
					if err != nil {
						return
					}
					go func() {
						for r := range creqs {
							ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
							_ = r.Reply(ok, nil)
						}
					}()
					srv, err := pkgsftp.NewServer(ch, pkgsftp.WithServerWorkingDirectory(root))
					if err != nil {
						return
					}
					_ = srv.Serve()
					_ = ch.Close()
				}
			}()
		}
	}()
	return server{addr: l.Addr().String(), fingerprint: ssh.FingerprintSHA256(hostSigner.PublicKey()), clientKey: clientPEM, root: root}
}

// A bank's SFTP folder lists its plain visible files by name and reads one whole, with the host key pinned by fingerprint and the client authorised by key: a server with another host key, a
// client with another key, a name that leaves the folder, a sub-folder, a hidden file and a file over the limit are all refused, and it only ever reads (FR-BNK-17, SEC-36).
// req: FR-BNK-17
// asvs: V9.2.1
func TestAnSFTPFolderListsAndReadsPlainFilesFromAPinnedHost(t *testing.T) {
	ctx := context.Background()
	s := startServer(t)
	inbox := filepath.Join(s.root, "outbox")
	if err := os.MkdirAll(filepath.Join(inbox, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"b.sta": "bb", "a.sta": "a", ".hidden": "x"} {
		if err := os.WriteFile(filepath.Join(inbox, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.root, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Drop{Addr: s.addr, User: "bank", Folder: inbox, PrivateKey: s.clientKey, HostKeySHA256: s.fingerprint}
	files, err := d.List(ctx)
	if err != nil || len(files) != 2 || files[0].Name != "a.sta" || files[1].Name != "b.sta" || files[1].Size != 2 {
		t.Fatalf("list: %+v, %v", files, err)
	}
	if b, err := d.Read(ctx, "b.sta", 10); err != nil || string(b) != "bb" {
		t.Errorf("read: %q, %v", b, err)
	}
	for _, name := range []string{"../secret", "sub/x", ".hidden", "sub", "", "missing"} {
		if _, err := d.Read(ctx, name, 10); err == nil {
			t.Errorf("%q was read", name)
		}
	}
	if _, err := d.Read(ctx, "b.sta", 1); err == nil {
		t.Error("a file over the limit was read")
	}
	// A server whose host key is not the pinned one is refused before anything is sent.
	wrong := d
	wrong.HostKeySHA256 = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := wrong.List(ctx); err == nil || !strings.Contains(err.Error(), "host key") {
		t.Errorf("a wrong host key: %v", err)
	}
	none := d
	none.HostKeySHA256 = ""
	if _, err := none.List(ctx); err == nil {
		t.Error("no pinned fingerprint was accepted")
	}
	// A client with another key is refused.
	_, other := keyPair(t)
	stranger := d
	stranger.PrivateKey = other
	if _, err := stranger.List(ctx); err == nil {
		t.Error("an unknown key was accepted")
	}
	if _, err := (Drop{Addr: s.addr, User: "bank", Folder: inbox, PrivateKey: []byte("not a key"), HostKeySHA256: s.fingerprint}).List(ctx); err == nil {
		t.Error("a private key that is not one was accepted")
	}
	// The dialer the runtime gives is the one used.
	dialed := false
	via := d
	via.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if _, err := via.List(ctx); err != nil || !dialed {
		t.Errorf("the egress dialer: used %v, %v", dialed, err)
	}
}
