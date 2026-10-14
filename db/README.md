# db

Database migrations for PostgreSQL 18 (ADR-D1): goose SQL files in `migrations/NNNNNN_name.sql`,
linted by squawk, applied by the `migrate` job under its own role (PRD-001 §12.4, SEC-03). A merged
migration is never edited: correct it with a new forward migration (expand and contract). The
first migrations arrive with p0-database-tenancy-migrations.
