package postgres

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Brand() storage.Brand { return brand{u} }

type brand struct{ u *unitOfWork }

func brandFile(t *string, sum []byte, size *int32) *storage.BrandFile {
	if t == nil {
		return nil
	}
	return &storage.BrandFile{MediaType: *t, SHA256: sum, Size: int(*size)}
}

func toBrand(r sqlcdb.FyniqBrandVersion) storage.BrandVersion {
	b := storage.BrandVersion{ID: r.ID, Version: int(r.Version), Light: brandFile(r.LightType, r.LightSha256, r.LightSize),
		Dark: brandFile(r.DarkType, r.DarkSha256, r.DarkSize), Icon: brandFile(r.IconType, r.IconSha256, r.IconSize), Cleaned: r.Cleaned,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
	if r.PrintSha256 != nil {
		png := "image/png"
		b.Print = brandFile(&png, r.PrintSha256, r.PrintSize)
	}
	if r.Accent != nil {
		b.Accent = *r.Accent
	}
	return b
}

func (r brand) Versions(ctx context.Context) ([]storage.BrandVersion, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BrandVersions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BrandVersion, 0, len(rows))
	for _, x := range rows {
		out = append(out, toBrand(x))
	}
	return out, nil
}

func (r brand) Save(ctx context.Context, by uuid.UUID, b storage.BrandVersion, at time.Time) (storage.BrandVersion, error) {
	if err := r.u.check(); err != nil {
		return storage.BrandVersion{}, err
	}
	p := sqlcdb.InsertBrandVersionParams{Version: int32(b.Version), Cleaned: b.Cleaned, CreatedBy: by, CreatedAt: at} // #nosec G115 -- a version number, counted by the database
	if len(p.Cleaned) == 0 {
		p.Cleaned = []byte("{}")
	}
	set := func(f *storage.BrandFile, t **string, sum *[]byte, size **int32) {
		if f == nil {
			return
		}
		mt, n := f.MediaType, int32(min(f.Size, 1<<30)) // #nosec G115 -- bounded
		*t, *sum, *size = &mt, f.SHA256, &n
	}
	set(b.Light, &p.LightType, &p.LightSha256, &p.LightSize)
	set(b.Dark, &p.DarkType, &p.DarkSha256, &p.DarkSize)
	set(b.Icon, &p.IconType, &p.IconSha256, &p.IconSize)
	if b.Print != nil {
		n := int32(min(b.Print.Size, 1<<30)) // #nosec G115 -- bounded
		p.PrintSha256, p.PrintSize = b.Print.SHA256, &n
	}
	if b.Accent != "" {
		p.Accent = &b.Accent
	}
	row, err := r.u.q.InsertBrandVersion(ctx, p)
	if err != nil {
		return storage.BrandVersion{}, mapError(err)
	}
	out := toBrand(row)
	hash := func(f *storage.BrandFile) any {
		if f == nil {
			return nil
		}
		return f.MediaType + " " + hex.EncodeToString(f.SHA256)
	}
	version := out.Version
	return out, r.u.event(ctx, "brand_version.saved", "brand_version", out.ID, &version, map[string][2]any{
		"light": {nil, hash(out.Light)}, "dark": {nil, hash(out.Dark)}, "icon": {nil, hash(out.Icon)}, "print": {nil, hash(out.Print)},
		"accent": {nil, out.Accent}})
}
