package salesdoc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// LinkDays is how long an online link lasts at most (FR-AR-06; p1-sales's design): it stops sooner when revoked, when
// its document is voided or cancelled or changes version, and when an invoice is paid in full.
const LinkDays = 180

// CodeLink is the one answer to a link that opens nothing: unknown, malformed, expired or revoked alike, so the answer
// tells nothing about which tokens existed (the threat model's T9).
const CodeLink = "LINK-01"

// tokenBytes is a token's randomness: 256 bits.
const tokenBytes = 32

// NewToken is a link's token, 32 bytes from crypto/rand in base64url without padding (43 characters), and its SHA-256
// in hex, which is all the database keeps (T1, T2).
func NewToken() (token, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	h, _ := TokenHash(token)
	return token, h, nil
}

// TokenHash is a token's SHA-256 in hex; false for a string NewToken cannot have made, refused before any lookup.
func TokenHash(token string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != tokenBytes || base64.RawURLEncoding.EncodeToString(b) != token {
		return "", false
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:]), true
}

// Live says whether a link opens its document at now: not revoked, not expired, its document still posted at the
// link's version, and, for an invoice, not paid in full (settled).
func Live(l storage.DocumentLink, doc storage.Document, settled bool, now time.Time) bool {
	return l.RevokedAt == nil && now.Before(l.ExpiresAt) && doc.ID == l.DocumentID && doc.Status == storage.DocumentPosted &&
		doc.Version == l.DocumentVersion && !settled
}
