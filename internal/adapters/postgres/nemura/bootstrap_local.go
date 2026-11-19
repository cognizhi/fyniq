//go:build local

package nemura

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LocalPassword is the password of every Nemura role in a local or test database. It is valid nowhere else (SEC-12); in
// the cloud, roles sign in with IAM database authentication (infra/).
const LocalPassword = "nemura-local-only"

// Database and Migrator are the local database's name and its owner.
const (
	Database = "nemura"
	Migrator = "nemura_migrator"
)

// ServiceRoles are the group roles migration 000001 creates, one per service plus read-only.
var ServiceRoles = []string{"nemura_api", "nemura_worker", "nemura_executor", "nemura_assistant", "nemura_readonly"}

// BootstrapLocal prepares an empty local server with its superuser (adminURL, any database on it): it creates
// nemura_migrator, able to log in with LocalPassword and to create the service roles, and the database `nemura` it owns.
// It runs again harmlessly. Only the local build has it.
func BootstrapLocal(ctx context.Context, adminURL string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as the database superuser: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err = conn.Exec(ctx, `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = '`+Migrator+`') THEN
    CREATE ROLE `+Migrator+` LOGIN CREATEROLE NOSUPERUSER NOCREATEDB NOREPLICATION NOBYPASSRLS
      PASSWORD '`+LocalPassword+`';
  END IF;
END
$$`); err != nil {
		return fmt.Errorf("create %s: %w", Migrator, err)
	}
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)", Database).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE DATABASE "+Database+" OWNER "+Migrator); err != nil {
			return fmt.Errorf("create the %s database: %w", Database, err)
		}
	}
	// The OAuth server's own database on the same server (Nemura's own instance of the authorisation server, question 11), owned by a role that
	// holds nothing in the nemura database.
	if _, err := conn.Exec(ctx, `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'nemura_hydra') THEN
    CREATE ROLE nemura_hydra LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '`+LocalPassword+`';
  END IF;
END
$$`); err != nil {
		return fmt.Errorf("create the nemura_hydra role: %w", err)
	}
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = 'nemura_hydra')").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE DATABASE nemura_hydra OWNER nemura_hydra"); err != nil {
			return fmt.Errorf("create the nemura_hydra database: %w", err)
		}
	}
	return nil
}

// EnableLocalLogins lets each service role log in with LocalPassword, as nemura_migrator (which created the roles in
// migration 000001 and so administers them). Only the local build has it.
func EnableLocalLogins(ctx context.Context, migratorURL string) error {
	conn, err := pgx.Connect(ctx, migratorURL)
	if err != nil {
		return fmt.Errorf("connect as %s: %w", Migrator, err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for _, r := range ServiceRoles {
		if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s LOGIN PASSWORD '%s'", pgx.Identifier{r}.Sanitize(), LocalPassword)); err != nil {
			return fmt.Errorf("let %s log in: %w", r, err)
		}
	}
	return nil
}
