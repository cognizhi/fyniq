package nemura

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The careers page (PRD-002 NM-REC-02): the organisation's address, switch and words, and the public list of its published posts, read through one definer function (migration 000068).

// CareersService is the careers page behind the API.
type CareersService struct {
	Store *Store
	Clock ports.Clock
}

// Public lists what a visitor reads at an address: nothing found for an unknown or switched-off page.
func (c CareersService) Public(ctx context.Context, slug string) (api.CareersView, error) {
	rows, err := c.Store.pool.Query(ctx, `SELECT company, intro, post_id, title, department, body, published_on FROM nemura.careers_public($1)`, slug)
	if err != nil {
		return api.CareersView{}, err
	}
	defer rows.Close()
	v := api.CareersView{Posts: []api.CareersPost{}}
	for rows.Next() {
		var company, intro, title, dept, body *string
		var post *uuid.UUID
		var on *time.Time
		if err := rows.Scan(&company, &intro, &post, &title, &dept, &body, &on); err != nil {
			return api.CareersView{}, err
		}
		if post == nil {
			v.Found = true
			v.Company, v.Intro = deref(company), deref(intro)
			continue
		}
		v.Posts = append(v.Posts, api.CareersPost{ID: post.String(), Title: deref(title), Department: deref(dept), Body: deref(body), PublishedOn: dayOrEmpty(on)})
	}
	if err := rows.Err(); err != nil {
		return api.CareersView{}, err
	}
	if !v.Found {
		return api.CareersView{Posts: []api.CareersPost{}}, nil
	}
	return v, nil
}

// Allow counts a public read against the limit every instance shares.
func (c CareersService) Allow(ctx context.Context, scope, key string, window time.Duration, limit int) (bool, error) {
	return c.Store.PublicRate(ctx, scope, key, window, limit, c.Clock.Now())
}

// Settings reads the organisation's page.
func (c CareersService) Settings(ctx context.Context, s api.Session) (out api.CareersSettings, err error) {
	err = c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var intro *string
		e := tx.QueryRow(ctx, `SELECT slug, enabled, company, intro FROM nemura.careers_page`).Scan(&out.Slug, &out.Enabled, &out.Company, &intro)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		out.Configured, out.Intro = e == nil, deref(intro)
		return e
	})
	return out, err
}

// Save sets the address, the name, the words and the switch, once for the organisation; an address another company uses is refused.
func (c CareersService) Save(ctx context.Context, s api.Session, in api.CareersSettings) (out api.CareersSettings, err error) {
	if err := hiring.CheckSlug(in.Slug); err != nil {
		return out, err
	}
	if n := len([]rune(strings.TrimSpace(in.Company))); n < 1 || n > 120 {
		return out, &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The company's name is 1 to 120 characters."}
	}
	if len([]rune(in.Intro)) > 1000 {
		return out, &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The words above the list are at most 1000 characters."}
	}
	err = c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var oldSlug *string
		var oldOn *bool
		_ = tx.QueryRow(ctx, `SELECT slug, enabled FROM nemura.careers_page`).Scan(&oldSlug, &oldOn)
		at := c.Clock.Now()
		var free bool
		if e := tx.QueryRow(ctx, `SELECT nemura.careers_register($1, $2, $3)`, s.Organisation, in.Slug, in.Enabled).Scan(&free); e != nil {
			return hiringError(e)
		}
		if !free {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Another company uses that address: choose a different one."}
		}
		if _, e := tx.Exec(ctx, `INSERT INTO nemura.careers_page (slug, enabled, company, intro, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (organisation_id) DO UPDATE SET slug = EXCLUDED.slug, enabled = EXCLUDED.enabled, company = EXCLUDED.company, intro = EXCLUDED.intro, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			in.Slug, in.Enabled, strings.TrimSpace(in.Company), orNil(strings.TrimSpace(in.Intro)), s.Member, at); e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "23505" {
				return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Another company uses that address: choose a different one."}
			}
			return hiringError(e)
		}
		var before, beforeOn any
		if oldSlug != nil {
			before, beforeOn = *oldSlug, *oldOn
		}
		return Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.careers_page.saved", audit.Object{Type: "careers_page", ID: s.Organisation}, map[string]audit.Change{"slug": {before, in.Slug}, "enabled": {beforeOn, in.Enabled}}, "")
	})
	if err != nil {
		return out, err
	}
	in.Configured = true
	return in, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
