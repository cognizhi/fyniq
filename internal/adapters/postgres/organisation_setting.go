package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Settings() storage.Settings { return settings{u} }

type settings struct{ u *unitOfWork }

func toSettingChange(r sqlcdb.OrganisationSettingsRow) storage.SettingChange {
	return storage.SettingChange{ID: r.ID, Key: r.Key, Value: r.Value, EffectiveFrom: r.EffectiveFrom, Version: int(r.Version), RecordedAt: r.RecordedAt}
}

func (r settings) Changes(ctx context.Context, key string) ([]storage.SettingChange, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.OrganisationSettings(ctx, key)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SettingChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSettingChange(row))
	}
	return out, nil
}

func (r settings) Record(ctx context.Context, key, was, value string, from time.Time, version int) (storage.SettingChange, error) {
	if err := r.u.check(); err != nil {
		return storage.SettingChange{}, err
	}
	if version < 1 || version > 1_000_000 {
		return storage.SettingChange{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of setting %s", version, key)}
	}
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	row, err := r.u.q.InsertOrganisationSetting(ctx, sqlcdb.InsertOrganisationSettingParams{Key: key, Value: value, EffectiveFrom: day,
		Version: int32(version)}) // 1 to 1,000,000, checked above
	if err != nil {
		return storage.SettingChange{}, mapError(err)
	}
	c := toSettingChange(sqlcdb.OrganisationSettingsRow(row))
	if err := r.u.event(ctx, "organisation_setting.changed", "organisation_setting", c.ID, &c.Version, map[string][2]any{
		"key": {nil, c.Key}, "value": {nilIfEmpty(was), c.Value}, "effective_from": {nil, c.EffectiveFrom.Format(time.DateOnly)}}); err != nil {
		return storage.SettingChange{}, err
	}
	return c, nil
}
