//go:build local

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// TemplateClone copies the local database for the restore drill (DR-06; p1-operations-dr design D2) with CREATE DATABASE
// … TEMPLATE. PostgreSQL copies only a database nobody is connected to, so it refuses new connections to the source, ends
// the open ones (the local stack's pools connect again), copies it and lets connections in again: a second's pause.
// It cannot go back in time: the copy is of now, whatever time the drill asks for.
type TemplateClone struct {
	AdminURL string      // a superuser's connection to another database than Source (postgres)
	Source   string      // the database to copy
	Clock    ports.Clock // names the copy; nil reads the wall clock
}

var _ ports.DatabaseClone = TemplateClone{}

// drillPrefix is how a drill copy is named: <source>_drill_<when it was made, UTC>.
func (c TemplateClone) drillPrefix() string { return c.Source + "_drill_" }

// Clone implements ports.DatabaseClone.
func (c TemplateClone) Clone(ctx context.Context, _ time.Time) (ports.CloneTarget, error) {
	now := clock.System{}.Now()
	if c.Clock != nil {
		now = c.Clock.Now()
	}
	name := c.drillPrefix() + now.UTC().Format("20060102_150405")
	conn, err := pgx.Connect(ctx, c.AdminURL)
	if err != nil {
		return ports.CloneTarget{}, fmt.Errorf("the drill's copy: connect as the database's owner: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	source := pgx.Identifier{c.Source}.Sanitize()
	if _, err := conn.Exec(ctx, "ALTER DATABASE "+source+" ALLOW_CONNECTIONS false"); err != nil {
		return ports.CloneTarget{}, fmt.Errorf("the drill's copy: refuse connections to %s: %w", c.Source, err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "ALTER DATABASE "+source+" ALLOW_CONNECTIONS true")
	}()
	if _, err := conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", c.Source); err != nil {
		return ports.CloneTarget{}, fmt.Errorf("the drill's copy: end the connections to %s: %w", c.Source, err)
	}
	// PostgreSQL waits up to five seconds for the ended connections to go.
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE "+source); err != nil {
		return ports.CloneTarget{}, fmt.Errorf("the drill's copy of %s: %w", c.Source, err)
	}
	return ports.CloneTarget{Name: name, Database: name}, nil
}

// Remove implements ports.DatabaseClone.
func (c TemplateClone) Remove(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, c.drillPrefix()) {
		return fmt.Errorf("%q is not a drill copy of %s, so it is not removed", name, c.Source)
	}
	conn, err := pgx.Connect(ctx, c.AdminURL)
	if err != nil {
		return fmt.Errorf("remove the drill's copy: connect as the database's owner: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	_, err = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}
