package erasure

import (
	"bytes"
	"encoding/json"
	"errors"
)

type fakeVerifier struct{ msg []byte }

func (f fakeVerifier) Verify(keyID string, msg, sig []byte) error {
	if keyID != "k1" || !bytes.Equal(msg, f.msg) || string(sig) != "sig" {
		return errors.New("bad signature")
	}
	return nil
}

func signedJSON(e Entry, key string, sig []byte) []byte {
	b, _ := json.Marshal(Signed{Entry: e, KeyID: key, Sig: sig})
	return b
}
