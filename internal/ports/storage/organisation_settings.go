package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SettingChange is one recorded change to an organisation setting (FR-ORG-08): its value from a date. Changes
// are never edited or deleted; a later change, or a reset to the recommended value, is a new one.
type SettingChange struct {
	ID            uuid.UUID
	Key           string
	Value         string
	EffectiveFrom time.Time // a date
	Version       int       // the key's versions count from 1
	RecordedAt    time.Time
}

// Settings are the organisation's recorded setting changes. Every change is audited.
type Settings interface {
	// Changes returns the recorded changes of key, or of every key when key is "", by key then version.
	Changes(ctx context.Context, key string) ([]SettingChange, error)
	// Record adds a change as the key's version: CodeVersion when another change took that version first. was
	// is the value in effect on from before the change, for its audit event.
	Record(ctx context.Context, key, was, value string, from time.Time, version int) (SettingChange, error)
}
