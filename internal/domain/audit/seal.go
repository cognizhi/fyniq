package audit

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/period"
)

// Personal data in immutable records is crypto-shredded at the end of retention (PRD-001 §11.8, rev 0.244;
// p1-pdpa-retention design D8). In an event about a person, each value of a field named as personal (a name, an email
// address, a phone number, an address) is sealed under that person's key for the event's financial year, with the
// organisation, the event and the field as associated data. The chain hashes the sealed text, so shredding the key at
// the end of retention leaves the event, the chain and the anchors whole: only the value no longer opens. Format
// version 3.

// personalKeys are field names whose values are a person's own data in an event about them.
var personalKeys = []string{"name", "names", "email", "emails", "phone", "phones", "mobile", "address", "addresses"}

// Personal reports whether a field's value is a person's own data, sealed in an event about them: display_name,
// legal_name, billing_email and mailing_address match; user_id does not.
func Personal(key string) bool { return keyIs(key, personalKeys) }

// A sealed value's fields.
const (
	SealedKey  = "pk" // the person's key that sealed it
	SealedText = "ct" // the nonce, the ciphertext and its tag, in unpadded standard base64
)

// SealedOf reports whether v is a sealed value, and its key and ciphertext.
func SealedOf(v any) (key uuid.UUID, text []byte, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 2 {
		return uuid.Nil, nil, false
	}
	k, _ := m[SealedKey].(string)
	c, _ := m[SealedText].(string)
	id, err := uuid.Parse(k)
	if err != nil || c == "" {
		return uuid.Nil, nil, false
	}
	text, err = base64.RawStdEncoding.DecodeString(c)
	if err != nil {
		return uuid.Nil, nil, false
	}
	return id, text, true
}

// Sealed is the value SealedOf reads.
func Sealed(key uuid.UUID, text []byte) map[string]any {
	return map[string]any{SealedKey: key.String(), SealedText: base64.RawStdEncoding.EncodeToString(text)}
}

// SealAAD is the associated data a personal value is sealed with: the organisation, the event and the field, so a
// sealed value copied to another event or field does not open there.
func SealAAD(org, event uuid.UUID, field string) []byte {
	return []byte("fyniq-audit-personal@1:" + org.String() + ":" + event.String() + ":" + field)
}

// SealFunc seals a personal value of field.
type SealFunc func(field string, plaintext []byte) (map[string]any, error)

// SealPersonal replaces every personal value in a change set, at any depth, with its sealed form. A text value is
// sealed; an absent or empty one, a proof (AUD-02) and one sealed already stay as they are.
func SealPersonal(changes map[string]Change, seal SealFunc) (map[string]Change, error) {
	out := make(map[string]Change, len(changes))
	for k, c := range changes {
		before, err := sealValue(k, k, c[0], seal)
		if err != nil {
			return nil, err
		}
		after, err := sealValue(k, k, c[1], seal)
		if err != nil {
			return nil, err
		}
		out[k] = Change{before, after}
	}
	return out, nil
}

func sealValue(field, key string, v any, seal SealFunc) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if _, _, done := SealedOf(x); done {
			return x, nil
		}
		if f, ok := x["fingerprint"].(string); ok && len(f) > 0 {
			return x, nil
		}
		out := make(map[string]any, len(x))
		for k, val := range x {
			s, err := sealValue(field+"."+k, k, val, seal)
			if err != nil {
				return nil, err
			}
			out[k] = s
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			s, err := sealValue(fmt.Sprintf("%s[%d]", field, i), key, val, seal)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	case string:
		if x == "" || !Personal(key) {
			return x, nil
		}
		return seal(field, []byte(x))
	}
	return v, nil
}

// The people an event may be about.
const (
	PersonMember     = "member"       // member:<user id>
	PersonContact    = "contact"      // contact:<contact id>, when the contact is an individual or a director
	PersonInvitation = "invitation"   // invitation:<invitation id>: an invited person, before they are a member
	PersonJoin       = "join_request" // join_request:<request id>: a person who asked to join, before they are a member
)

// Subject is the person an event about an object of objectType is about, from the object or its changes, and whether
// it is one; a contact is a person only when it is an individual or a director, which the caller checks.
func Subject(objectType string, objectID uuid.UUID, changes map[string]Change) (string, bool) {
	switch objectType {
	case "super_admin", "app_user":
		return PersonMember + ":" + objectID.String(), true
	case "user_membership":
		for _, v := range changes["user_id"] {
			if v != nil {
				if id, err := uuid.Parse(fmt.Sprint(v)); err == nil {
					return PersonMember + ":" + id.String(), true
				}
			}
		}
		return "", false
	case "member_invitation":
		return PersonInvitation + ":" + objectID.String(), true
	case "join_request":
		return PersonJoin + ":" + objectID.String(), true
	case "contact":
		return PersonContact + ":" + objectID.String(), true
	}
	return "", false
}

// Cohort is the financial year a personal value is sealed for: the last day of the organisation's financial year that
// holds day (the first year for a day before the books start), or of day's calendar year while the financial years
// are not set. Its retention ends retention.records_years after it.
func Cohort(cal period.Calendar, set bool, day time.Time) time.Time {
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	if !set {
		return time.Date(d.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	}
	return cal.Year(max(cal.Number(d), 1)).EndsOn
}

// Unseal returns v, the value of field as an event keeps it, with each sealed value in it replaced by what open returns
// for it: open is given the value's path (as SealPersonal named it in the associated data), its key and its ciphertext.
func Unseal(field string, v any, open func(path string, key uuid.UUID, text []byte) any) any {
	switch x := v.(type) {
	case map[string]any:
		if k, ct, ok := SealedOf(x); ok {
			return open(field, k, ct)
		}
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = Unseal(field+"."+k, val, open)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = Unseal(fmt.Sprintf("%s[%d]", field, i), val, open)
		}
		return out
	}
	return v
}
