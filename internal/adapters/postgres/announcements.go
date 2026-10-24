package postgres

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Announcements() storage.Announcements { return announcements{u} }

type announcements struct{ u *unitOfWork }

// announcementRow is every organisation banner query's row: they all select the same columns.
type announcementRow = sqlcdb.InsertAnnouncementRow

func toAnnouncement(r announcementRow) storage.Announcement {
	a := storage.Announcement{ID: r.ID, Message: r.Message, Severity: r.Severity, Roles: r.Roles, StartsAt: r.StartsAt, EndsAt: r.EndsAt,
		Dismissible: r.Dismissible, PublishedBy: r.PublishedBy, PublishedAt: r.PublishedAt, WithdrawnAt: r.WithdrawnAt}
	if r.Link != nil {
		a.Link = *r.Link
	}
	if r.WithdrawnBy != nil {
		a.WithdrawnBy = *r.WithdrawnBy
	}
	if a.Roles == nil {
		a.Roles = []string{}
	}
	return a
}

func announcementChanges(a storage.Announcement) map[string][2]any {
	return map[string][2]any{"message": {nil, a.Message}, "link": {nil, nilIfEmpty(a.Link)}, "severity": {nil, a.Severity}, "roles": {nil, a.Roles},
		"starts_at": {nil, a.StartsAt.UTC().Format(time.RFC3339)}, "ends_at": {nil, a.EndsAt.UTC().Format(time.RFC3339)},
		"dismissible": {nil, a.Dismissible}}
}

func (r announcements) Publish(ctx context.Context, n storage.NewAnnouncement) (storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return storage.Announcement{}, err
	}
	row, err := r.u.q.InsertAnnouncement(ctx, sqlcdb.InsertAnnouncementParams{Message: n.Message, Link: opt(n.Link), Severity: n.Severity,
		Roles: rolesOrNone(n.Roles), StartsAt: n.StartsAt, EndsAt: n.EndsAt, Dismissible: n.Dismissible, PublishedBy: r.u.actor(ctx), PublishedAt: n.At})
	if err != nil {
		return storage.Announcement{}, mapError(err)
	}
	out := toAnnouncement(row)
	return out, r.u.event(ctx, "announcement.published", "announcement", out.ID, nil, announcementChanges(out))
}

func (r announcements) Withdraw(ctx context.Context, id uuid.UUID, at time.Time) (storage.Announcement, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return storage.Announcement{}, err
	}
	if before.WithdrawnAt != nil {
		return storage.Announcement{}, &storage.Error{Code: storage.CodeVersion, Message: "the banner was withdrawn already"}
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.WithdrawAnnouncement(ctx, sqlcdb.WithdrawAnnouncementParams{At: &at, By: &by, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Announcement{}, &storage.Error{Code: storage.CodeVersion, Message: "the banner was withdrawn already"}
	}
	if err != nil {
		return storage.Announcement{}, mapError(err)
	}
	out := toAnnouncement(announcementRow(row))
	return out, r.u.event(ctx, "announcement.withdrawn", "announcement", id, nil, map[string][2]any{"withdrawn_at": {nil, at.UTC().Format(time.RFC3339)}})
}

func (r announcements) Get(ctx context.Context, id uuid.UUID) (storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return storage.Announcement{}, err
	}
	row, err := r.u.q.GetAnnouncement(ctx, id)
	if err != nil {
		return storage.Announcement{}, mapError(err)
	}
	return toAnnouncement(announcementRow(row)), nil
}

func (r announcements) List(ctx context.Context) ([]storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAnnouncements(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Announcement, 0, len(rows))
	for _, x := range rows {
		out = append(out, toAnnouncement(announcementRow(x)))
	}
	return out, nil
}

func (r announcements) PublishInstallation(ctx context.Context, by uuid.UUID, n storage.NewAnnouncement) (storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return storage.Announcement{}, err
	}
	id, err := r.u.q.PublishInstallationAnnouncement(ctx, sqlcdb.PublishInstallationAnnouncementParams{By: by, Message: n.Message, Link: opt(n.Link),
		Severity: n.Severity, Roles: rolesOrNone(n.Roles), StartsAt: n.StartsAt, EndsAt: n.EndsAt, Dismissible: n.Dismissible, At: n.At})
	if err != nil {
		return storage.Announcement{}, mapError(err)
	}
	out, err := r.installationOne(ctx, id)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "installation_announcement.published", "installation_announcement", id, nil, announcementChanges(out))
}

func (r announcements) WithdrawInstallation(ctx context.Context, by, id uuid.UUID, at time.Time) (storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return storage.Announcement{}, err
	}
	if err := r.u.q.WithdrawInstallationAnnouncement(ctx, sqlcdb.WithdrawInstallationAnnouncementParams{By: by, ID: id, At: at}); err != nil {
		return storage.Announcement{}, mapError(err)
	}
	out, err := r.installationOne(ctx, id)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "installation_announcement.withdrawn", "installation_announcement", id, nil,
		map[string][2]any{"withdrawn_at": {nil, at.UTC().Format(time.RFC3339)}})
}

func (r announcements) installationOne(ctx context.Context, id uuid.UUID) (storage.Announcement, error) {
	all, err := r.Installation(ctx)
	if err != nil {
		return storage.Announcement{}, err
	}
	i := slices.IndexFunc(all, func(a storage.Announcement) bool { return a.ID == id })
	if i < 0 {
		return storage.Announcement{}, ports.ErrNotFound
	}
	return all[i], nil
}

func (r announcements) Installation(ctx context.Context) ([]storage.Announcement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.InstallationAnnouncements(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Announcement, 0, len(rows))
	for _, x := range rows {
		a := storage.Announcement{ID: x.ID, Installation: true, Message: x.Message, Link: x.Link, Severity: x.Severity, Roles: x.Roles,
			StartsAt: x.StartsAt, EndsAt: x.EndsAt, Dismissible: x.Dismissible, PublishedBy: "user:" + x.PublishedBy.String(), PublishedAt: x.PublishedAt}
		if x.Withdrawn {
			at := x.WithdrawnAt
			a.WithdrawnAt, a.WithdrawnBy = &at, "user:"+x.WithdrawnBy
		}
		if a.Roles == nil {
			a.Roles = []string{}
		}
		out = append(out, a)
	}
	return out, nil
}

func (r announcements) Dismiss(ctx context.Context, user, id uuid.UUID, installation bool, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return mapError(r.u.q.InsertAnnouncementDismissal(ctx, sqlcdb.InsertAnnouncementDismissalParams{AnnouncementID: id, Installation: installation,
		UserID: user, DismissedAt: at}))
}

func (r announcements) Dismissed(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.AnnouncementDismissals(ctx, user)
	return ids, mapError(err)
}

func rolesOrNone(roles []string) []string {
	if roles == nil {
		return []string{}
	}
	return roles
}
