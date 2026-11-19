package passkey

import "github.com/google/uuid"

// Key is one passkey as an application other than Fyniq's keeps it, so that application builds a User without Fyniq's storage types
// (Nemura, PRD-002: its passkeys are its own rows).
type Key struct {
	CredentialID, PublicKey, AAGUID []byte
	SignCount                       uint32
	Transports                      []string
	BackupEligible, BackupState     bool
	Revoked                         bool
}

// UserOf is a person with the passkeys an application keeps for them; a revoked key is left out of every ceremony.
func UserOf(id uuid.UUID, name string, keys []Key) User {
	u := User{ID: id, Name: name}
	for _, k := range keys {
		p := storagePasskey(k)
		u.Passkeys = append(u.Passkeys, p)
	}
	return u
}

// CredentialID is the ID of the passkey that answered an assertion.
func (a Asserted) CredentialID() []byte { return a.Passkey.CredentialID }
