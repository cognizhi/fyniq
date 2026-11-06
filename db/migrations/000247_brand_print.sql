-- The logo for documents and emails (PRD-001 FR-ORG-09; rev 0.201; p1-organisation-chart task 2.4): with its logo for
-- light backgrounds, a brand version keeps the PNG the render service drew from it, 192 pixels high on a transparent
-- background, which documents and emails show; kept in the blob store under its SHA-256, as the brand's other files.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.brand_version ADD COLUMN print_sha256 bytea, ADD COLUMN print_size integer;
ALTER TABLE fyniq.brand_version ADD CONSTRAINT brand_version_print CHECK ((print_sha256 IS NULL) = (print_size IS NULL)
  AND (print_sha256 IS NULL OR light_sha256 IS NOT NULL) AND coalesce(length(print_sha256), 32) = 32
  AND coalesce(print_size, 1) BETWEEN 1 AND 524288) NOT VALID;
