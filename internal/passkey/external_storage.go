package passkey

import (
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// storagePasskey is Fyniq's storage type for a Key: the package's own, never an application's.
func storagePasskey(k Key) storage.Passkey {
	p := storage.Passkey{CredentialID: k.CredentialID, PublicKey: k.PublicKey, SignCount: k.SignCount, AAGUID: k.AAGUID, Transports: k.Transports,
		BackupEligible: k.BackupEligible, BackupState: k.BackupState}
	if k.Revoked {
		now := time.Time{}
		p.RevokedAt = &now
	}
	return p
}
