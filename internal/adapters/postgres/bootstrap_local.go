//go:build local

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LocalPassword is the password of every role in a local or test database. It is valid nowhere
// else (SEC-12); in the cloud, roles sign in with IAM database authentication (p0-infra-opentofu).
const LocalPassword = "fyniq-local-only"

// ServiceRoles are the group roles migration 000001 creates, one per service plus read-only.
var ServiceRoles = []string{"fyniq_api", "fyniq_worker", "fyniq_executor", "fyniq_ai", "fyniq_readonly"}

// BootstrapLocal prepares an empty local database with its superuser (adminURL): it creates
// fyniq_migrator, the only role that owns objects, able to log in with LocalPassword, to create the
// service roles and to create schemas. It runs again harmlessly. Only the local build has it.
func BootstrapLocal(ctx context.Context, adminURL string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as the database superuser: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	_, err = conn.Exec(ctx, `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'fyniq_migrator') THEN
    CREATE ROLE fyniq_migrator LOGIN CREATEROLE NOSUPERUSER NOCREATEDB NOREPLICATION NOBYPASSRLS
      PASSWORD '`+LocalPassword+`';
  END IF;
  EXECUTE format('GRANT CREATE ON DATABASE %I TO fyniq_migrator', current_database());
END
$$`)
	if err != nil {
		return fmt.Errorf("create fyniq_migrator: %w", err)
	}
	return nil
}

// EnableLocalLogins lets each service role log in with LocalPassword, as fyniq_migrator (which
// created the roles in migration 000001 and so administers them). Only the local build has it.
func EnableLocalLogins(ctx context.Context, migratorURL string) error {
	conn, err := pgx.Connect(ctx, migratorURL)
	if err != nil {
		return fmt.Errorf("connect as fyniq_migrator: %w", err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for _, r := range ServiceRoles {
		if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s LOGIN PASSWORD '%s'", pgx.Identifier{r}.Sanitize(), LocalPassword)); err != nil {
			return fmt.Errorf("let %s log in: %w", r, err)
		}
	}
	return nil
}
