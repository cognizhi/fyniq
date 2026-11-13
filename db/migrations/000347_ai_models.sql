-- The models each AI connection's provider lists (PRD-001 AI-80, AI-81, AI-84; p1-ai-providers task 4.1, design D3).
--
-- Fyniq ships no list of models: each connection's list is read from its provider's own list endpoint, with the
-- connection's credential, page by page to a cap. Each read is kept with its outcome (complete, partial or failed), why
-- it was read, its pages and the models it added and removed: never changed or removed (FY332). Each model keeps what
-- the provider reports of it, when it was first and last listed, and whether the last complete read listed it: only a
-- complete read marks a model no longer listed (AI-84); a model is kept, never removed, so its history stays (AI-81).
-- The reads come from the API (Refresh) and the worker (daily at 06:00 SGT, and after a key is approved).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_model_list_read (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connection_id   uuid        NOT NULL,
  trigger         text        NOT NULL,
  outcome         text        NOT NULL,
  pages           integer     NOT NULL,
  added           integer     NOT NULL,
  removed         integer     NOT NULL,
  failure         text,
  read_by         text        NOT NULL,
  read_at         timestamptz NOT NULL,
  CONSTRAINT ai_model_list_read_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_model_list_read_connection_fkey FOREIGN KEY (organisation_id, connection_id) REFERENCES fyniq.ai_connection (organisation_id, id),
  CONSTRAINT ai_model_list_read_trigger_known CHECK (trigger IN ('credential', 'test', 'enable', 'daily', 'not_found', 'refresh')),
  CONSTRAINT ai_model_list_read_outcome_known CHECK (outcome IN ('complete', 'partial', 'failed')),
  CONSTRAINT ai_model_list_read_counts CHECK (pages >= 0 AND added >= 0 AND removed >= 0),
  -- Only a complete read removes a model (AI-84); a failed one names why.
  CONSTRAINT ai_model_list_read_removes_complete CHECK (removed = 0 OR outcome = 'complete'),
  CONSTRAINT ai_model_list_read_failure CHECK ((outcome = 'complete') = (failure IS NULL) AND length(failure) <= 500),
  CONSTRAINT ai_model_list_read_by_format CHECK (read_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX ai_model_list_read_connection ON fyniq.ai_model_list_read (organisation_id, connection_id, read_at DESC);

CREATE TABLE fyniq.ai_model (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connection_id   uuid        NOT NULL,
  model           text        NOT NULL,  -- the provider's ID
  name            text,
  alias_of        text,                  -- the model an alias points at, when the list says (AI-78)
  provider_created timestamptz,
  shutdown_at     timestamptz,           -- the provider's retirement or shutdown date (AI-84)
  context_window  integer,
  max_output      integer,
  reports         text[]      NOT NULL DEFAULT '{}', -- what the list reports it supports: hints the probe still proves (AI-71)
  first_listed_at timestamptz NOT NULL,
  last_listed_at  timestamptz NOT NULL,
  listed          boolean     NOT NULL DEFAULT true, -- listed by the last complete read
  not_callable_at timestamptz,           -- a confirmed access or not-found error, until a read or probe succeeds (AI-81)
  CONSTRAINT ai_model_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_model_connection_fkey FOREIGN KEY (organisation_id, connection_id) REFERENCES fyniq.ai_connection (organisation_id, id),
  CONSTRAINT ai_model_one UNIQUE (organisation_id, connection_id, model),
  CONSTRAINT ai_model_format CHECK (model ~ '^[!-~]{1,200}$'),
  CONSTRAINT ai_model_alias_format CHECK (alias_of ~ '^[!-~]{1,200}$'),
  CONSTRAINT ai_model_name_length CHECK (length(name) <= 300),
  CONSTRAINT ai_model_limits CHECK (context_window > 0 AND max_output > 0),
  CONSTRAINT ai_model_listed_order CHECK (last_listed_at >= first_listed_at)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_model_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A listed model and its reads are kept, so their history stays' USING ERRCODE = 'FY332';
  END IF;
  IF TG_TABLE_NAME = 'ai_model_list_read' THEN
    IF TG_OP <> 'INSERT' THEN
      RAISE EXCEPTION 'A model list''s read is kept as it was made' USING ERRCODE = 'FY332';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.connection_id <> OLD.connection_id OR NEW.model <> OLD.model OR NEW.first_listed_at <> OLD.first_listed_at) THEN
    RAISE EXCEPTION 'A listed model stays the same model of the same connection' USING ERRCODE = 'FY332';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_model_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_model
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_model_guard();
CREATE TRIGGER ai_model_list_read_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_model_list_read
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_model_guard();
CREATE TRIGGER ai_model_no_truncate BEFORE TRUNCATE ON fyniq.ai_model
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_model_list_read_no_truncate BEFORE TRUNCATE ON fyniq.ai_model_list_read
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_model_list_read
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_connection', 'connection_id');

ALTER TABLE fyniq.ai_model_list_read ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_model_list_read FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_model_list_read
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_model ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_model FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_model
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_model_list_read, fyniq.ai_model FROM PUBLIC;
GRANT SELECT ON fyniq.ai_model_list_read, fyniq.ai_model TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The API reads a list on Refresh; the worker daily and after a key is approved.
GRANT INSERT (connection_id, trigger, outcome, pages, added, removed, failure, read_by, read_at) ON fyniq.ai_model_list_read TO fyniq_api, fyniq_worker;
GRANT INSERT (connection_id, model, name, alias_of, provider_created, shutdown_at, context_window, max_output, reports, first_listed_at, last_listed_at,
              listed, not_callable_at) ON fyniq.ai_model TO fyniq_api, fyniq_worker;
GRANT UPDATE (name, alias_of, provider_created, shutdown_at, context_window, max_output, reports, last_listed_at, listed, not_callable_at)
  ON fyniq.ai_model TO fyniq_api, fyniq_worker;
