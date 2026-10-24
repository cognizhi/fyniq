package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ banners and announcements (FR-NTF-03, rev 0.112)

// The banner codes.
const (
	CodeBannerStays = "NTF-04" // a critical banner, or one published as not dismissible, stays until it ends
	CodeBannerNone  = "NTF-05" // no banner showing has that ID
	CodeBannerEnded = "NTF-06" // the announcement ended or was withdrawn already
)

// Banner is a banner the caller sees under the header now.
type Banner struct {
	ID          string    `json:"id" format:"uuid" doc:"The banner's ID: dismiss it with this." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
	Scope       string    `json:"scope" enum:"organisation,installation" doc:"organisation: published by its Owner or Administrator; installation: by a Super admin, to everyone." example:"organisation"`
	Message     string    `json:"message" doc:"What it says." example:"The office is closed on 3 November; bills due that day are paid on 4 November."`
	Link        string    `json:"link,omitempty" doc:"Where it leads: a path in the web app or an https address; absent for none." example:"/help/organisation-settings"`
	Severity    string    `json:"severity" enum:"info,warning,critical" doc:"info and warning are announced politely (role=status), critical at once (role=alert)." example:"info"`
	Dismissible bool      `json:"dismissible" doc:"false for a critical banner, or one published as not dismissible: it stays until it ends." example:"true"`
	EndsAt      time.Time `json:"ends_at" doc:"When it stops showing." example:"2026-11-03T16:00:00Z"`
}

// BannerList is every banner the caller sees now, the most severe first.
type BannerList struct {
	Items      []Banner `json:"items" doc:"The banners, critical first, then warning, then info; newest first within each."`
	NextCursor string   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// Announcement is a published banner, as its publishers see it.
type Announcement struct {
	ID          string     `json:"id" format:"uuid" doc:"The announcement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
	Message     string     `json:"message" doc:"What it says." example:"The office is closed on 3 November; bills due that day are paid on 4 November."`
	Link        string     `json:"link,omitempty" doc:"Where it leads; absent for none." example:"/help/organisation-settings"`
	Severity    string     `json:"severity" enum:"info,warning,critical" doc:"How it shows." example:"info"`
	Roles       []RoleName `json:"roles" doc:"The roles it shows to; empty for everyone."`
	StartsAt    time.Time  `json:"starts_at" doc:"When it starts showing." example:"2026-10-24T01:00:00Z"`
	EndsAt      time.Time  `json:"ends_at" doc:"When it stops showing." example:"2026-11-03T16:00:00Z"`
	Dismissible bool       `json:"dismissible" doc:"Whether a person may dismiss it; never a critical one." example:"true"`
	Status      string     `json:"status" enum:"scheduled,live,ended" doc:"scheduled before it starts, live while it shows, ended after it ends or is withdrawn." example:"live"`
	PublishedBy string     `json:"published_by" doc:"Who published it, as type:id." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	PublishedAt time.Time  `json:"published_at" doc:"When it was published." example:"2026-10-24T01:00:00Z"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty" doc:"When it was withdrawn early; absent unless it was." example:"2026-10-25T01:00:00Z"`
}

// AnnouncementList is every announcement, newest first.
type AnnouncementList struct {
	Items      []Announcement `json:"items" doc:"The announcements, newest first: scheduled, live and ended."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// AnnouncementRequest publishes a banner.
type AnnouncementRequest struct {
	Message     string     `json:"message" minLength:"1" maxLength:"300" doc:"What it says, in plain words." example:"The office is closed on 3 November; bills due that day are paid on 4 November."`
	Link        string     `json:"link,omitempty" maxLength:"300" doc:"Where it leads: a path in the web app, such as /help/…, or an https address." example:"/help/organisation-settings"`
	Severity    string     `json:"severity" enum:"info,warning,critical" doc:"info, warning, or critical for what everyone must see." example:"info"`
	Roles       []RoleName `json:"roles,omitempty" maxItems:"20" doc:"The roles it shows to; leave it out for everyone."`
	StartsAt    *time.Time `json:"starts_at,omitempty" doc:"When it starts showing, now or later; leave it out to start now." example:"2026-10-24T01:00:00Z"`
	EndsAt      time.Time  `json:"ends_at" doc:"When it stops showing, after it starts." example:"2026-11-03T16:00:00Z"`
	Dismissible *bool      `json:"dismissible,omitempty" doc:"Whether a person may dismiss it; true unless critical, which never is." example:"true"`
}

// BannerPath names a banner or an announcement.
type BannerPath struct {
	ID string `path:"announcement_id" format:"uuid" doc:"The banner's or announcement's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f60"`
}

type bannerListOutput struct{ Body BannerList }
type bannerListInput struct{ PageQuery }
type announcementListOutput struct{ Body AnnouncementList }
type announcementOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Announcement
}
type bannerActionInput struct {
	BannerPath
	KeyHeader
	PreferHeader
}
type publishAnnouncementInput struct {
	KeyHeader
	PreferHeader
	Body AnnouncementRequest
}

// appPath is a path in the web app a banner may link to.
var appPath = regexp.MustCompile(`^/[A-Za-z0-9/_?=&.#-]*$`)

// newAnnouncement checks a request and makes the banner to publish at now.
func newAnnouncement(in AnnouncementRequest, now time.Time) (storage.NewAnnouncement, error) {
	var bad []FieldProblem
	n := storage.NewAnnouncement{Message: strings.TrimSpace(in.Message), Link: strings.TrimSpace(in.Link), Severity: in.Severity, EndsAt: in.EndsAt,
		StartsAt: now, Dismissible: in.Severity != "critical", At: now}
	if n.Message == "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.message", Message: "is blank", Fix: "Say what the banner tells people"})
	}
	if n.Link != "" && !appPath.MatchString(n.Link) {
		if u, err := url.Parse(n.Link); err != nil || u.Scheme != "https" || u.Host == "" || strings.ContainsAny(n.Link, " \t\r\n") {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.link", Message: "is neither a path in Fyniq nor an https address",
				Fix: "Use a path such as /help/… or an address that starts with https://"})
		}
	}
	for _, rn := range in.Roles {
		r := string(rn)
		if _, ok := authz.Roles[authz.Role(r)]; !ok || r == string(authz.AI) || r == string(authz.SuperAdmin) {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.roles", Message: r + " is not a role of an organisation",
				Fix: "Name roles such as owner or controller, or leave roles out for everyone"})
		} else if !slices.Contains(n.Roles, r) {
			n.Roles = append(n.Roles, r)
		}
	}
	if in.StartsAt != nil {
		if in.StartsAt.Before(now.Add(-time.Minute)) {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.starts_at", Message: "is in the past", Fix: "Leave it out to start now"})
		} else if in.StartsAt.After(now) {
			n.StartsAt = *in.StartsAt
		}
	}
	if !n.EndsAt.After(n.StartsAt) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.ends_at", Message: "is not after the start", Fix: "End it after it starts"})
	}
	if in.Dismissible != nil {
		if *in.Dismissible && in.Severity == "critical" {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.dismissible", Message: "is true for a critical banner",
				Fix: "A critical banner stays until it ends: leave dismissible out, or choose warning"})
		}
		n.Dismissible = *in.Dismissible && in.Severity != "critical"
	}
	if len(bad) > 0 {
		return n, invalid(bad)
	}
	return n, nil
}

// status is where a banner is at now.
func status(a storage.Announcement, now time.Time) string {
	switch {
	case a.WithdrawnAt != nil || !a.EndsAt.After(now):
		return "ended"
	case a.StartsAt.After(now):
		return "scheduled"
	}
	return "live"
}

func announcementOf(a storage.Announcement, now time.Time) Announcement {
	out := Announcement{ID: a.ID.String(), Message: a.Message, Link: a.Link, Severity: a.Severity, Roles: []RoleName{}, StartsAt: a.StartsAt.UTC(),
		EndsAt: a.EndsAt.UTC(), Dismissible: a.Dismissible, Status: status(a, now), PublishedBy: a.PublishedBy, PublishedAt: a.PublishedAt.UTC()}
	if a.WithdrawnAt != nil {
		at := a.WithdrawnAt.UTC()
		out.WithdrawnAt = &at
	}
	for _, r := range a.Roles {
		out.Roles = append(out.Roles, RoleName(r))
	}
	return out
}

// announcementPage is a page of announcements as their publishers see them at now.
func announcementPage(all []storage.Announcement, q PageQuery, now time.Time) (AnnouncementList, error) {
	out := AnnouncementList{Items: []Announcement{}}
	page, next, err := pageOf(all, func(x storage.Announcement) string { return x.ID.String() }, q)
	for _, x := range page {
		out.Items = append(out.Items, announcementOf(x, now))
	}
	out.NextCursor = next
	return out, err
}

// showsTo reports whether a live banner shows to a person holding roles.
func showsTo(a storage.Announcement, roles []authz.Role) bool {
	return len(a.Roles) == 0 || slices.ContainsFunc(roles, func(r authz.Role) bool { return slices.Contains(a.Roles, string(r)) })
}

var severityRank = map[string]int{"critical": 0, "warning": 1, "info": 2}

// banners are the organisation's and the installation's banners that show to p now, but those p dismissed.
func banners(ctx context.Context, u storage.UnitOfWork, p authn.Principal, now time.Time) ([]storage.Announcement, error) {
	own, err := u.Announcements().List(ctx)
	if err != nil {
		return nil, err
	}
	everyone, err := u.Announcements().Installation(ctx)
	if err != nil {
		return nil, err
	}
	var dismissed []uuid.UUID
	if p.UserID != uuid.Nil {
		if dismissed, err = u.Announcements().Dismissed(ctx, p.UserID); err != nil {
			return nil, err
		}
	}
	var out []storage.Announcement
	for _, a := range append(own, everyone...) {
		if status(a, now) == "live" && showsTo(a, p.Roles) && (!a.Dismissible || !slices.Contains(dismissed, a.ID)) {
			out = append(out, a)
		}
	}
	slices.SortStableFunc(out, func(x, y storage.Announcement) int {
		if d := severityRank[x.Severity] - severityRank[y.Severity]; d != 0 {
			return d
		}
		return y.StartsAt.Compare(x.StartsAt)
	})
	return out, nil
}

func bannerOf(a storage.Announcement) Banner {
	scope := "organisation"
	if a.Installation {
		scope = "installation"
	}
	return Banner{ID: a.ID.String(), Scope: scope, Message: a.Message, Link: a.Link, Severity: a.Severity, Dismissible: a.Dismissible, EndsAt: a.EndsAt.UTC()}
}

// inInstallationWrite runs a write of the Super admin by in the installation organisation's unit of work, with the API's
// conventions (Idempotency-Key, Prefer), its events in the installation's trail.
func inInstallationWrite[B any](ctx context.Context, a *API, by uuid.UUID, key, prefer string, request any, status int,
	run func(ctx context.Context, u storage.UnitOfWork) (B, error)) (written[B], error) {
	org, err := a.deps.Installation.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return written[B]{}, problem(CodeUnavailable, "The installation is not set up yet.", "Complete first-run setup first.")
	}
	if err != nil {
		return written[B]{}, err
	}
	p, _ := authn.From(ctx)
	p.Organisation = org
	return write(authn.WithPrincipal(actingAs(ctx, by), p), a, key, prefer, request, status, run)
}

func registerBanners(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)

	register(a, authz.ListBanners, huma.Operation{
		OperationID: "listBanners", Method: http.MethodGet, Path: "/v1/banners", Tags: []string{"Announcements"},
		Summary: "List the banners you see now",
		Description: "The banners that show under the header for the caller now (FR-NTF-03): the organisation's, from its Owner or " +
			"Administrator, and the installation's, from a Super admin, while they are live and meant for everyone or for one of the " +
			"caller's roles, but those the caller dismissed. Critical first. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *bannerListInput) (*bannerListOutput, error) {
		p, _ := authn.From(ctx)
		out := BannerList{Items: []Banner{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			bs, err := banners(ctx, u, p, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			bs, out.NextCursor, err = pageOf(bs, func(b storage.Announcement) string { return b.ID.String() }, in.PageQuery)
			for _, b := range bs {
				out.Items = append(out.Items, bannerOf(b))
			}
			return err
		})
		return &bannerListOutput{Body: out}, err
	})

	register(a, authz.DismissBanner, huma.Operation{
		OperationID: "dismissBanner", Method: http.MethodPost, Path: "/v1/banners/{announcement_id}/dismiss", Tags: []string{"Announcements"},
		Summary: "Dismiss a banner",
		Description: "Hides a banner showing to the caller, for the caller alone, on every device: the dismissal is kept on the server " +
			"(FR-NTF-03). A critical banner, or one published as not dismissible, stays until it ends (NTF-04); NTF-05 for no banner " +
			"showing with that ID. Side effects: the dismissal. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeBannerStays, CodeBannerNone, CodeCaptureNoUser}, writes...), func(ctx context.Context, in *bannerActionInput) (*bannerListOutput, error) {
		user, err := person(ctx)
		if err != nil {
			return nil, err
		}
		id, err := parseID("announcement_id", in.ID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BannerList, error) {
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			showing, err := banners(ctx, u, p, now)
			if err != nil {
				return BannerList{}, err
			}
			i := slices.IndexFunc(showing, func(b storage.Announcement) bool { return b.ID == id })
			if i < 0 {
				return BannerList{}, problem(CodeBannerNone, "No banner showing to you has that ID.", "List your banners")
			}
			b := showing[i]
			if !b.Dismissible {
				return BannerList{}, problem(CodeBannerStays, "This banner cannot be dismissed: it stays until it ends.",
					"Nothing to do: it goes by itself at its end")
			}
			if err := u.Announcements().Dismiss(ctx, user, id, b.Installation, now); err != nil {
				return BannerList{}, err
			}
			out := BannerList{Items: []Banner{}}
			for _, x := range append(showing[:i:i], showing[i+1:]...) {
				out.Items = append(out.Items, bannerOf(x))
			}
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &bannerListOutput{Body: w.Body}, nil
	})

	register(a, authz.ListAnnouncements, huma.Operation{
		OperationID: "listAnnouncements", Method: http.MethodGet, Path: "/v1/announcements", Tags: []string{"Announcements"},
		Summary: "List the organisation's announcements",
		Description: "Every banner the organisation published, newest first, with its status: scheduled, live or ended (FR-NTF-03). " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *bannerListInput) (*announcementListOutput, error) {
		p, _ := authn.From(ctx)
		var out AnnouncementList
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Announcements().List(ctx)
			if err != nil {
				return err
			}
			out, err = announcementPage(all, in.PageQuery, a.deps.Clock.Now())
			return err
		})
		return &announcementListOutput{Body: out}, err
	})

	register(a, authz.PublishAnnouncement, huma.Operation{
		OperationID: "publishAnnouncement", Method: http.MethodPost, Path: "/v1/announcements", Tags: []string{"Announcements"},
		DefaultStatus: http.StatusCreated, Summary: "Publish a banner to the organisation",
		Description: "Publishes a banner under the header for the organisation's members, or those holding the roles named, from its " +
			"start (now unless later) until its end (FR-NTF-03): a message and a link (a path in Fyniq or an https address), a severity, " +
			"and whether it may be dismissed (never a critical one). It never changes; withdraw it to end it early. Side effects: the " +
			"announcement and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.",
	}, append([]string{CodeInvalid}, writes...), func(ctx context.Context, in *publishAnnouncementInput) (*announcementOutput, error) {
		n, err := newAnnouncement(in.Body, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Announcement, error) {
			x, err := u.Announcements().Publish(ctx, n)
			return announcementOf(x, a.deps.Clock.Now()), err
		})
		if err != nil {
			return nil, err
		}
		return &announcementOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.WithdrawAnnouncement, huma.Operation{
		OperationID: "withdrawAnnouncement", Method: http.MethodPost, Path: "/v1/announcements/{announcement_id}/withdraw", Tags: []string{"Announcements"},
		Summary: "Withdraw an announcement",
		Description: "Ends one of the organisation's banners now, before its end, or cancels a scheduled one (FR-NTF-03); NTF-06 when it " +
			"ended or was withdrawn already. Side effects: the withdrawal and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and changes nothing.",
	}, append([]string{CodeNotFound, CodeBannerEnded}, writes...), func(ctx context.Context, in *bannerActionInput) (*announcementOutput, error) {
		id, err := parseID("announcement_id", in.ID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Announcement, error) {
			now := a.deps.Clock.Now()
			x, err := u.Announcements().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				return Announcement{}, problem(CodeNotFound, "The organisation has no announcement with that ID.", "List its announcements")
			}
			if err != nil {
				return Announcement{}, err
			}
			if status(x, now) == "ended" {
				return Announcement{}, problem(CodeBannerEnded, "This announcement has ended already.", "Nothing to do; publish another to say more")
			}
			x, err = u.Announcements().Withdraw(ctx, id, now)
			return announcementOf(x, now), err
		})
		if err != nil {
			return nil, err
		}
		return &announcementOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, locks...)
	register(a, authz.ListInstallationAnnouncements, huma.Operation{
		OperationID: "listInstallationAnnouncements", Method: http.MethodGet, Path: "/v1/installation/announcements", Tags: []string{"Installation"},
		Summary: "List the announcements to everyone",
		Description: "Every banner a Super admin published to everyone in the installation, newest first, with its status (FR-NTF-03, " +
			"FR-ADM-07). " + authz.ListInstallationAnnouncements.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, admin...), func(ctx context.Context, in *bannerListInput) (*announcementListOutput, error) {
		by, err := a.superAdmin(ctx, "Listing the announcements to everyone")
		if err != nil {
			return nil, err
		}
		var out AnnouncementList
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := u.Announcements().Installation(ctx)
			if err != nil {
				return err
			}
			out, err = announcementPage(all, in.PageQuery, a.deps.Clock.Now())
			return err
		})
		return &announcementListOutput{Body: out}, err
	})

	register(a, authz.PublishInstallationAnnouncement, huma.Operation{
		OperationID: "publishInstallationAnnouncement", Method: http.MethodPost, Path: "/v1/installation/announcements", Tags: []string{"Installation"},
		DefaultStatus: http.StatusCreated, Summary: "Publish a banner to everyone",
		Description: "Publishes a banner to every organisation's members, or those holding the roles named, such as a maintenance window " +
			"or an incident (FR-NTF-03, FR-ADM-07), with the same fields and rules as an organisation's. " +
			authz.PublishInstallationAnnouncement.Note + " Side effects: the announcement and its event in the installation's trail. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.",
	}, append(append([]string{CodeInvalid}, admin...), writes...), func(ctx context.Context, in *publishAnnouncementInput) (*announcementOutput, error) {
		by, err := a.superAdmin(ctx, "Publishing a banner to everyone")
		if err != nil {
			return nil, err
		}
		n, err := newAnnouncement(in.Body, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (Announcement, error) {
			x, err := u.Announcements().PublishInstallation(ctx, by, n)
			return announcementOf(x, a.deps.Clock.Now()), err
		})
		if err != nil {
			return nil, err
		}
		return &announcementOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.WithdrawInstallationAnnouncement, huma.Operation{
		OperationID: "withdrawInstallationAnnouncement", Method: http.MethodPost, Path: "/v1/installation/announcements/{announcement_id}/withdraw",
		Tags: []string{"Installation"}, Summary: "Withdraw an announcement to everyone",
		Description: "Ends one of the installation's banners now, or cancels a scheduled one (FR-NTF-03); NTF-06 when it ended already. " +
			authz.WithdrawInstallationAnnouncement.Note + " Side effects: the withdrawal and its event in the installation's trail. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing.",
	}, append(append([]string{CodeNotFound, CodeBannerEnded}, admin...), writes...), func(ctx context.Context, in *bannerActionInput) (*announcementOutput, error) {
		by, err := a.superAdmin(ctx, "Withdrawing a banner to everyone")
		if err != nil {
			return nil, err
		}
		id, err := parseID("announcement_id", in.ID)
		if err != nil {
			return nil, err
		}
		w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Announcement, error) {
			now := a.deps.Clock.Now()
			all, err := u.Announcements().Installation(ctx)
			if err != nil {
				return Announcement{}, err
			}
			i := slices.IndexFunc(all, func(x storage.Announcement) bool { return x.ID == id })
			if i < 0 {
				return Announcement{}, problem(CodeNotFound, "The installation has no announcement with that ID.", "List its announcements")
			}
			if status(all[i], now) == "ended" {
				return Announcement{}, problem(CodeBannerEnded, "This announcement has ended already.", "Nothing to do; publish another to say more")
			}
			x, err := u.Announcements().WithdrawInstallation(ctx, by, id, now)
			return announcementOf(x, now), err
		})
		if err != nil {
			return nil, err
		}
		return &announcementOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
