-- Tasks made by integrations (PRD-001 FR-INT-24, rev 0.609; p2-api-third-parties task 2, design D5): what a ticketing system or another API client with the
-- scope fyniq.write.tasks asks a person to do. They are kept in a table of their own, not among the compliance tasks (AI-65): the statutory and agent tasks
-- cannot be reached by a client by construction, and a client's role here is only the API's, limited by column. A client makes a task with a reference of its own
-- (unique per client, so a retried create finds its task), updates only the tasks it made, and may finish one of them once: done or cancelled, for good. The
-- client is the OAuth client behind the call, recorded at creation and never changed. Nothing is deleted. Every change is audited in its transaction (AUD-01).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.integration_task (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  client_id       text        NOT NULL,              -- the OAuth client that made it: the only client that may update it
  external_ref    text        NOT NULL,              -- the client's own reference, such as the ticket key
  title           text        NOT NULL,
  detail          text        NOT NULL DEFAULT '',
  due_on          date,
  link_url        text,                              -- where the ticket is, for the person who reads the task
  status          text        NOT NULL DEFAULT 'open',
  created_by      uuid        NOT NULL,              -- the member the client acts for
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  closed_at       timestamptz,
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT integration_task_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT integration_task_created_by_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT integration_task_ref_once UNIQUE (organisation_id, client_id, external_ref),
  CONSTRAINT integration_task_client_form CHECK (client_id ~ '^[!-~]{1,200}$'),
  CONSTRAINT integration_task_ref_form CHECK (external_ref ~ '^[!-~]{1,200}$'),
  CONSTRAINT integration_task_words CHECK (length(btrim(title)) BETWEEN 1 AND 200 AND length(detail) <= 2000),
  CONSTRAINT integration_task_link CHECK (link_url IS NULL OR (link_url ~ '^https://[^[:space:]]+$' AND length(link_url) <= 2048)),
  CONSTRAINT integration_task_status CHECK (status IN ('open', 'done', 'cancelled')),
  CONSTRAINT integration_task_closed CHECK ((closed_at IS NOT NULL) = (status <> 'open')),
  CONSTRAINT integration_task_version CHECK (version >= 1)
);
CREATE INDEX integration_task_by_status ON fyniq.integration_task (organisation_id, status, id DESC);

-- What a task is, and who made it, never change; once finished it stays finished; each change is the next version, and only the API changes one (FYT01).
-- +goose StatementBegin
CREATE FUNCTION fyniq.integration_task_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A task made by an integration is kept: it is never deleted' USING ERRCODE = 'FYT01';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'open' OR NEW.version <> 1 THEN
      RAISE EXCEPTION 'A task made by an integration starts open, at version 1' USING ERRCODE = 'FYT01';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.client_id, NEW.external_ref, NEW.created_by, NEW.created_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.client_id, OLD.external_ref, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'What a task made by an integration is, and who made it, never changes' USING ERRCODE = 'FYT01';
  END IF;
  IF OLD.status <> 'open' THEN
    RAISE EXCEPTION 'A finished task made by an integration stays finished' USING ERRCODE = 'FYT01';
  END IF;
  IF NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Each change of a task made by an integration is its next version' USING ERRCODE = 'FYT01';
  END IF;
  IF NOT pg_has_role(current_user, 'fyniq_api', 'MEMBER') THEN
    RAISE EXCEPTION 'Only the API changes a task made by an integration' USING ERRCODE = 'FYT01';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER integration_task_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.integration_task
  FOR EACH ROW EXECUTE FUNCTION fyniq.integration_task_guard();
CREATE TRIGGER integration_task_no_truncate BEFORE TRUNCATE ON fyniq.integration_task
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.integration_task
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('integration_task', 'id');

ALTER TABLE fyniq.integration_task ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.integration_task FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.integration_task
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.integration_task FROM PUBLIC;
GRANT SELECT ON fyniq.integration_task TO fyniq_api, fyniq_worker, fyniq_readonly; -- the worker reads it for the full tenant export (FR-MIG-04)
GRANT INSERT (client_id, external_ref, title, detail, due_on, link_url, created_by, created_at, updated_at) ON fyniq.integration_task TO fyniq_api;
GRANT UPDATE (title, detail, due_on, link_url, status, closed_at, version, updated_at) ON fyniq.integration_task TO fyniq_api;
