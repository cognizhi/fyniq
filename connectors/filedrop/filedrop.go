// Package filedrop is the SFTP statement-files connector (FR-BNK-15, FR-BNK-17, FR-INT-25; p2-bank-feeds-payments task 2.1, design D4): a bank's statement files left in an
// SFTP folder, read-only, with its manifest beside it. The adapters are internal/adapters/sftp (the folder) and internal/adapters/filedrop (the files as transactions). Against a real
// bank it is [live-verify].
package filedrop

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	adapter "github.com/cognizhi/fyniq/internal/adapters/filedrop"
	"github.com/cognizhi/fyniq/internal/adapters/sftp"
	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

//go:embed manifest.json
var manifest []byte

// Binding is the SFTP statement-files connector's manifest and its adapter.
func Binding() (connector.Binding, error) {
	m, err := connector.Parse(manifest)
	if err != nil {
		return connector.Binding{}, err
	}
	return connector.Binding{Manifest: m, BankData: func(c connector.Config) (ports.BankData, error) {
		key, err := c.Secret("private_key")
		if err != nil {
			return nil, err
		}
		addr := c.String("address", "")
		if _, port, err := net.SplitHostPort(addr); err != nil {
			return nil, fmt.Errorf("the SFTP server is not set: enter its host and port, such as sftp.bank.example:22")
		} else if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return nil, errors.New("the SFTP port is not from 1 to 65535")
		}
		format := c.String("format", bankfile.FormatMT940)
		if format != bankfile.FormatMT940 && format != bankfile.FormatCAMT053 && format != bankfile.FormatOFX {
			return nil, fmt.Errorf("%q is not a file format this connector reads: use mt940, camt053 or ofx", format)
		}
		return adapter.Statements{Format: format, Drop: sftp.Drop{Addr: addr, User: c.String("user", ""), Folder: c.String("folder", ""), PrivateKey: []byte(key),
			HostKeySHA256: c.String("host_key_sha256", ""), Dial: c.Dial, Timeout: 30 * time.Second}}, nil
	}}, nil
}
