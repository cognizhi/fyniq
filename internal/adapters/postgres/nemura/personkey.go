package nemura

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
)

// ErrErased is returned for sealed detail whose person's key was destroyed by an erasure: it can never be read again (PRD-002 NM-SEC-08).
var ErrErased = errors.New("this person's data was erased: it cannot be read")

func keyAAD(org, person uuid.UUID) []byte { return aad(org, person, "person-key") }

// personDEK returns the person's data key, made on first use and wrapped by the key service. A person whose data was erased gets none (the database refuses it).
func personDEK(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, person uuid.UUID, create bool) ([]byte, error) {
	var wrapped []byte
	err := tx.QueryRow(ctx, `SELECT wrapped FROM nemura.person_key WHERE person_id = $1`, person).Scan(&wrapped)
	if errors.Is(err, pgx.ErrNoRows) {
		if !create {
			return nil, ErrErased
		}
		dek := make([]byte, 32)
		if _, err := rand.Read(dek); err != nil {
			return nil, err
		}
		w, err := c.Seal(ctx, dek, keyAAD(org, person))
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_key (person_id, wrapped) VALUES ($1, $2)`, person, w); err != nil {
			return nil, err
		}
		return dek, nil
	}
	if err != nil {
		return nil, err
	}
	return c.Open(ctx, wrapped, keyAAD(org, person))
}

// sealForPerson seals detail under the person's own key, bound to the organisation, the person and the purpose, so it opens nowhere else.
func sealForPerson(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, person uuid.UUID, purpose string, plaintext []byte) ([]byte, error) {
	dek, err := personDEK(ctx, tx, c, org, person, true)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad(org, person, purpose)), nil
}

// openForPerson reads detail sealed by sealForPerson; ErrErased when the person's key is gone.
func openForPerson(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, person uuid.UUID, purpose string, sealed []byte) ([]byte, error) {
	dek, err := personDEK(ctx, tx, c, org, person, false)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ports.ErrNotSealed
	}
	out, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], aad(org, person, purpose))
	if err != nil {
		return nil, fmt.Errorf("%w", ports.ErrNotSealed)
	}
	return out, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// ReadPayslipDetail returns a payslip's itemised figures: from the sealed copy under the person's key, or for a payslip made before keys, its plain data. After an erasure it is ErrErased.
func ReadPayslipDetail(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, payslip uuid.UUID) ([]byte, error) {
	var person uuid.UUID
	var sealed []byte
	var plain []byte
	if err := tx.QueryRow(ctx, `SELECT person_id, data_sealed, data::text::bytea FROM nemura.payslip WHERE id = $1`, payslip).Scan(&person, &sealed, &plain); err != nil {
		return nil, err
	}
	if sealed == nil {
		return plain, nil
	}
	return openForPerson(ctx, tx, c, org, person, "payslip", sealed)
}

// SealPersonDetail seals new payroll detail under the person's own key for a purpose; an erasure destroys the key and with it every sealed value of the person (PRD-002 NM-SEC-08).
func SealPersonDetail(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, person uuid.UUID, purpose string, plaintext []byte) ([]byte, error) {
	return sealForPerson(ctx, tx, c, org, person, purpose, plaintext)
}

// OpenPersonDetail reads what SealPersonDetail sealed; ErrErased once the person's key is gone.
func OpenPersonDetail(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, person uuid.UUID, purpose string, sealed []byte) ([]byte, error) {
	return openForPerson(ctx, tx, c, org, person, purpose, sealed)
}
