-- Workflow rules without code (PRD-001 FR-AUT-03; p2-mobile-approvals-automation design D5, rev 0.619).
--
-- A rule is data, never code: a trigger from a fixed list (a document of a kind is posted), typed conditions (the kind, an amount at least in the base currency) and
-- one action from a fixed list: notify named members. The database holds both lists, so no rule can name an action that posts, approves, pays or files. A rule is
-- evaluated where the event is emitted and tells people through the same outbox fan-out as every notification; the API holds no write on anything else for it, and the
-- worker holds none on this table. A rule is paused or archived, never deleted.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.automation_rule (
  organisation_id  uuid          NOT NULL DEFAULT fyniq.current_org(),
  id               uuid          NOT NULL DEFAULT uuidv7(),
  name             text          NOT NULL,
  trigger          text          NOT NULL,
  document_type    text,                      -- the kind of document, such as bill; none for any kind
  amount_at_least  numeric(20,4),             -- the document's debits in the base currency; none for any amount
  action           text          NOT NULL,
  recipients       uuid[]        NOT NULL,    -- the members told
  state            text          NOT NULL DEFAULT 'active',
  version          integer       NOT NULL DEFAULT 1,
  created_by       uuid          NOT NULL,
  created_at       timestamptz   NOT NULL,
  updated_at       timestamptz   NOT NULL,
  CONSTRAINT automation_rule_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT automation_rule_creator_fkey FOREIGN KEY (created_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT automation_rule_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
  CONSTRAINT automation_rule_trigger_known CHECK (trigger IN ('document.posted')),
  CONSTRAINT automation_rule_action_known CHECK (action IN ('notify')),
  CONSTRAINT automation_rule_document_type CHECK (document_type IS NULL OR document_type ~ '^[a-z][a-z_]{1,40}$'),
  CONSTRAINT automation_rule_amount CHECK (amount_at_least IS NULL OR (amount_at_least <> 'NaN' AND amount_at_least >= 0)),
  CONSTRAINT automation_rule_recipients CHECK (cardinality(recipients) BETWEEN 1 AND 20 AND array_position(recipients, NULL) IS NULL),
  CONSTRAINT automation_rule_state_known CHECK (state IN ('active', 'paused', 'archived')),
  CONSTRAINT automation_rule_version_positive CHECK (version >= 1)
);
CREATE INDEX automation_rule_active ON fyniq.automation_rule (organisation_id, trigger) WHERE state = 'active';

-- Never deleted; an archived rule stays archived; the rule's identity and who made it never change (FYA01).
-- +goose StatementBegin
CREATE FUNCTION fyniq.automation_rule_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP IN ('DELETE', 'TRUNCATE') THEN
    RAISE EXCEPTION 'A workflow rule is kept: it is paused or archived, never deleted' USING ERRCODE = 'FYA01', HINT = 'Archive it instead (FR-AUT-03).';
  END IF;
  IF OLD.state = 'archived' OR NEW.version <> OLD.version + 1
     OR (NEW.organisation_id, NEW.id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.created_by, OLD.created_at) THEN
    RAISE EXCEPTION 'A workflow rule is changed as its next version, and an archived one not at all' USING ERRCODE = 'FYA01',
      HINT = 'Read the rule again and make a new one if it was archived (FR-AUT-03).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER automation_rule_guard BEFORE UPDATE OR DELETE ON fyniq.automation_rule FOR EACH ROW EXECUTE FUNCTION fyniq.automation_rule_guard();
CREATE TRIGGER automation_rule_no_truncate BEFORE TRUNCATE ON fyniq.automation_rule FOR EACH STATEMENT EXECUTE FUNCTION fyniq.automation_rule_guard();

ALTER TABLE fyniq.automation_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.automation_rule FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.automation_rule USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.automation_rule
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('automation_rule', 'id');

REVOKE ALL ON fyniq.automation_rule FROM PUBLIC;
GRANT SELECT ON fyniq.automation_rule TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (name, trigger, document_type, amount_at_least, action, recipients, created_by, created_at, updated_at) ON fyniq.automation_rule TO fyniq_api;
GRANT UPDATE (name, document_type, amount_at_least, recipients, state, version, updated_at) ON fyniq.automation_rule TO fyniq_api;
