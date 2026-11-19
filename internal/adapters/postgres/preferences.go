package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/preference"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Preferences() storage.Preferences { return preferences{u} }

type preferences struct{ u *unitOfWork }

func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func valueOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func savedOf(userID uuid.UUID, mode, view, theme, contrast, density, motion *string, shortcutsOn *bool, keys []byte, number, date *string, at storage.SavedPreferences) (storage.SavedPreferences, error) {
	out := at
	out.UserID = userID
	out.Mode, out.LedgerView, out.Theme, out.Contrast, out.Density, out.Motion = valueOf(mode), valueOf(view), valueOf(theme), valueOf(contrast), valueOf(density), valueOf(motion)
	out.ShortcutsOn = shortcutsOn
	out.NumberFormat, out.DateFormat = valueOf(number), valueOf(date)
	out.ShortcutKeys = map[string]string{}
	if err := json.Unmarshal(keys, &out.ShortcutKeys); err != nil {
		return storage.SavedPreferences{}, err
	}
	return out, nil
}

func (r preferences) Save(ctx context.Context, user uuid.UUID, p preference.Preferences) (storage.SavedPreferences, error) {
	if err := r.u.check(); err != nil {
		return storage.SavedPreferences{}, err
	}
	before, _, err := r.Latest(ctx, user)
	if err != nil {
		return storage.SavedPreferences{}, err
	}
	keys := p.ShortcutKeys
	if keys == nil {
		keys = map[string]string{}
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return storage.SavedPreferences{}, err
	}
	row, err := r.u.q.InsertUserPreference(ctx, sqlcdb.InsertUserPreferenceParams{
		UserID: user, Mode: textOrNil(p.Mode), LedgerView: textOrNil(p.LedgerView), Theme: textOrNil(p.Theme), Contrast: textOrNil(p.Contrast),
		Density: textOrNil(p.Density), Motion: textOrNil(p.Motion), ShortcutsOn: p.ShortcutsOn, ShortcutKeys: raw,
		NumberFormat: textOrNil(p.NumberFormat), DateFormat: textOrNil(p.DateFormat), SetAt: r.u.store.clock.Now(),
	})
	if err != nil {
		return storage.SavedPreferences{}, mapError(err)
	}
	out, err := savedOf(row.UserID, row.Mode, row.LedgerView, row.Theme, row.Contrast, row.Density, row.Motion, row.ShortcutsOn, row.ShortcutKeys, row.NumberFormat, row.DateFormat, storage.SavedPreferences{SetAt: row.SetAt})
	if err != nil {
		return storage.SavedPreferences{}, err
	}
	// The audit event lists what changed, never the keys' text beyond the count (no secret is kept here, but the event stays small).
	changed := map[string][2]any{}
	note := func(field string, was, now any) {
		if was != now {
			changed[field] = [2]any{was, now}
		}
	}
	note("mode", before.Mode, out.Mode)
	note("ledger_view", before.LedgerView, out.LedgerView)
	note("theme", before.Theme, out.Theme)
	note("contrast", before.Contrast, out.Contrast)
	note("density", before.Density, out.Density)
	note("motion", before.Motion, out.Motion)
	note("number_format", before.NumberFormat, out.NumberFormat)
	note("date_format", before.DateFormat, out.DateFormat)
	on := func(b *bool) any {
		if b == nil {
			return nil
		}
		return *b
	}
	note("shortcuts_on", on(before.ShortcutsOn), on(out.ShortcutsOn))
	if !sameKeys(before.ShortcutKeys, out.ShortcutKeys) {
		changed["shortcut_keys"] = [2]any{len(before.ShortcutKeys), len(out.ShortcutKeys)}
	}
	if err := r.u.event(ctx, "user_preference.set", "user_preference", row.ID, nil, changed); err != nil {
		return storage.SavedPreferences{}, err
	}
	return out, nil
}

func sameKeys(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (r preferences) Latest(ctx context.Context, user uuid.UUID) (storage.SavedPreferences, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.SavedPreferences{}, false, err
	}
	rows, err := r.u.q.LatestUserPreference(ctx, user)
	if err != nil {
		return storage.SavedPreferences{}, false, mapError(err)
	}
	if len(rows) == 0 {
		return storage.SavedPreferences{}, false, nil
	}
	x := rows[0]
	out, err := savedOf(x.UserID, x.Mode, x.LedgerView, x.Theme, x.Contrast, x.Density, x.Motion, x.ShortcutsOn, x.ShortcutKeys, x.NumberFormat, x.DateFormat, storage.SavedPreferences{SetAt: x.SetAt})
	return out, true, err
}
