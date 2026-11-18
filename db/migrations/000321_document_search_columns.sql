-- Each document version's date and search text as columns (PRD-001 NFR-PERF-01, FR-UX-02; p1-operations-dr tasks 13.9
-- and 13.10, design D11).
--
-- Lists of documents ordered by date, and the global search, read the date and the text out of each version's payload:
-- a list page sorted every document of its type, unpacking each payload, and a search rebuilt every document's text
-- from its lines on every call, so both grew with the ledger. The two values become generated columns, computed from the
-- payload when a version is written (a version never changes), so 000322 can index them:
--   - doc_date: the payload's date, '' for a draft without one, as the lists already compared it;
--   - search_text: the memo, reference, notes, title, supplier's reference and the lines' descriptions and merchants,
--     lower-cased, the payload's part of what the search matches (FR-UX-02, rev 0.190).
-- pg_trgm, which the PRD names for global search (§12, "PostgreSQL full-text search plus pg_trgm"), indexes the text.
-- Adding a stored generated column rewrites the table once, under its lock; nothing is live yet (rev 0.250).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '15min';

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA fyniq_ext;
CREATE EXTENSION IF NOT EXISTS btree_gin WITH SCHEMA fyniq_ext; -- organisation_id leads the trigram indexes too (§11.4)

-- +goose StatementBegin
CREATE FUNCTION fyniq.document_search_text(p jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
SET search_path = pg_catalog, pg_temp
AS $$
  SELECT lower(concat_ws(' ', p->>'memo', p->>'reference', p->>'notes', p->>'title', p->>'supplier_ref',
    (SELECT string_agg(concat_ws(' ', x->>'description', x->>'merchant'), ' ')
       FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p->'lines') = 'array' THEN p->'lines' ELSE '[]'::jsonb END) x)))
$$;
-- +goose StatementEnd

-- A generated column runs its function as the role that writes the row: the roles that write versions may run it
-- (000001 revokes EXECUTE from PUBLIC). It reads only the payload it is given.
GRANT EXECUTE ON FUNCTION fyniq.document_search_text(jsonb) TO fyniq_api, fyniq_worker, fyniq_executor;

-- squawk's way, a nullable column backfilled by an update, cannot work here: a version never changes, and its guard
-- (document_version_immutable) refuses the update. The rewrite runs once, before anything is live (rev 0.250).
ALTER TABLE fyniq.document_version
  -- squawk-ignore adding-field-with-default
  ADD COLUMN doc_date text GENERATED ALWAYS AS (coalesce(payload->>'date', '')) STORED,
  -- squawk-ignore adding-field-with-default
  ADD COLUMN search_text text GENERATED ALWAYS AS (fyniq.document_search_text(payload)) STORED;
