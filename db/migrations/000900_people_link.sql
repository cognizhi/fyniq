-- The link to Nemura, the people platform (PRD-001 §8.26, FR-PPL-01, 06, 15, 16; PRD-002 NM-INT-01, 10, 11; p1-payroll-link tasks 2.1, 3.1,
-- 3.2). Three things live here and no pay or bank detail does (FR-PPL-15; design D5):
--
--  * people_link: the one link an organisation has to a Nemura organisation, its state and its epoch. Every event carries the epoch and
--    a link of an earlier one is refused. A link is never deleted: unlinking ends it and a later link is a new row, one epoch higher.
--  * people_link_inbox: every event received, once, by its ID: applied, ignored as older than what was applied, or refused with a code.
--    Append-only; it keeps identities and digests, never a payload.
--  * person_projection: what accounting needs of a person (FR-PPL-06): the person's ID, display name, work email, department and cost
--    centre values, start and leaving dates, status, line manager, the tax-clearance hold's state and version, and the bank details'
--    record and version. Never the account number, an NRIC or a salary. Each part only moves to a newer version.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_link (
  organisation_id        uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                     uuid        NOT NULL DEFAULT uuidv7(),
  state                  text        NOT NULL,
  epoch                  integer     NOT NULL,
  nemura_organisation_id uuid        NOT NULL,
  link_key               bytea       NOT NULL,   -- Nemura's Ed25519 public key for the link's envelopes: given when the link was approved
  client_id              uuid,                   -- the API client (kind link) Nemura calls with; none once unlinked
  approval_id            uuid        NOT NULL,   -- the approval that made the link: the Owner and a second approver
  version                integer     NOT NULL DEFAULT 1,
  created_by             text        NOT NULL,
  created_at             timestamptz NOT NULL,
  changed_at             timestamptz NOT NULL,
  CONSTRAINT people_link_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT people_link_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT people_link_state_known CHECK (state IN ('active', 'suspended', 'unlinking', 'unlinked')),
  CONSTRAINT people_link_epoch_positive CHECK (epoch >= 1),
  CONSTRAINT people_link_key_size CHECK (octet_length(link_key) = 32),
  CONSTRAINT people_link_version_positive CHECK (version >= 1),
  CONSTRAINT people_link_unlinked_no_client CHECK (state <> 'unlinked' OR client_id IS NULL)
);
-- One live link per organisation, and an epoch used once.
CREATE UNIQUE INDEX people_link_one_live ON fyniq.people_link (organisation_id) WHERE state <> 'unlinked';
CREATE UNIQUE INDEX people_link_epoch ON fyniq.people_link (organisation_id, epoch);

-- +goose StatementBegin
CREATE FUNCTION fyniq.people_link_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  top integer;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A link is kept: unlink it instead' USING ERRCODE = 'FY900',
      HINT = 'Unlinking ends the link; its epoch is never used again (FR-PPL-16).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT coalesce(max(epoch), 0) INTO top FROM fyniq.people_link WHERE organisation_id = NEW.organisation_id;
    IF NEW.state <> 'active' OR NEW.version <> 1 OR NEW.epoch <> top + 1 THEN
      RAISE EXCEPTION 'A link starts active, at its first version, one epoch above the last' USING ERRCODE = 'FY900',
        HINT = 'Epochs only go up, so that an event of an earlier epoch is refused (FR-PPL-16).';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.id, NEW.epoch, NEW.nemura_organisation_id, NEW.link_key, NEW.approval_id, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM (OLD.id, OLD.epoch, OLD.nemura_organisation_id, OLD.link_key, OLD.approval_id, OLD.created_by, OLD.created_at)
     OR NEW.version <> OLD.version + 1
     OR NOT ((OLD.state, NEW.state) IN (('active', 'suspended'), ('suspended', 'active'), ('active', 'unlinking'),
                                       ('suspended', 'unlinking'), ('unlinking', 'unlinked'), ('unlinking', 'active'))) THEN
    RAISE EXCEPTION 'A link only moves between its states, at its next version, and keeps its epoch and key' USING ERRCODE = 'FY900',
      HINT = 'active, suspended, unlinking, unlinked: unlinked is final (NM-INT-11).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER people_link_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.people_link
  FOR EACH ROW EXECUTE FUNCTION fyniq.people_link_guard();
CREATE TRIGGER people_link_no_truncate BEFORE TRUNCATE ON fyniq.people_link
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.people_link
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_link', 'id');

-- Where each feature lives (FR-PPL-01): the latest row of a feature is in force; each change is a new row, audited, so the history stays.
CREATE TABLE fyniq.people_source (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  feature         text        NOT NULL,
  source          text        NOT NULL,
  effective_from  date        NOT NULL,   -- the start date of the change, which brings year-to-date state across (FR-PPL-16)
  approval_id     uuid        NOT NULL,
  set_at          timestamptz NOT NULL,
  CONSTRAINT people_source_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT people_source_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT people_source_feature_known CHECK (feature IN ('records', 'payroll', 'leave', 'letters')),
  CONSTRAINT people_source_source_known CHECK (source IN ('nemura', 'external', 'none'))
);
CREATE INDEX people_source_latest ON fyniq.people_source (organisation_id, feature, set_at);
CREATE TRIGGER people_source_guard BEFORE UPDATE OR DELETE ON fyniq.people_source
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER people_source_no_truncate BEFORE TRUNCATE ON fyniq.people_source
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.people_source
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_source', 'id');

-- Every event received, once (FR-PPL-15): a repeat does nothing. Identities and digests only, never a payload.
CREATE TABLE fyniq.people_link_inbox (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  event_id        uuid        NOT NULL,
  link_id         uuid        NOT NULL,
  epoch           integer     NOT NULL,
  sequence        bigint      NOT NULL,
  type            text        NOT NULL,
  object_type     text        NOT NULL,
  object_id       text        NOT NULL,
  object_version  integer     NOT NULL,
  payload_sha256  text        NOT NULL,
  outcome         text        NOT NULL,
  code            text,
  received_at     timestamptz NOT NULL,
  CONSTRAINT people_link_inbox_pkey PRIMARY KEY (organisation_id, event_id),
  CONSTRAINT people_link_inbox_id_key UNIQUE (organisation_id, id),
  CONSTRAINT people_link_inbox_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT people_link_inbox_outcome_known CHECK (outcome IN ('applied', 'ignored_older', 'refused')),
  CONSTRAINT people_link_inbox_code_with_refusal CHECK ((outcome = 'refused') = (code IS NOT NULL)),
  CONSTRAINT people_link_inbox_digest CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT people_link_inbox_type_shape CHECK (type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$' AND char_length(object_id) BETWEEN 1 AND 100)
);
CREATE INDEX people_link_inbox_object ON fyniq.people_link_inbox (organisation_id, object_type, object_id, object_version);
CREATE TRIGGER people_link_inbox_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_inbox
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER people_link_inbox_no_truncate BEFORE TRUNCATE ON fyniq.people_link_inbox
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

-- The employee projection (FR-PPL-06). Not an HR record: the person ID is Nemura's, the department and cost centre are Fyniq's own
-- dimension values (departments come from Fyniq, NM-INT-01), and the three versions move apart: the person's record, the tax-clearance
-- hold and the bank details, so a late older version of one never undoes a newer one (FR-PPL-15).
CREATE TABLE fyniq.person_projection (
  organisation_id        uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                     uuid        NOT NULL DEFAULT uuidv7(),
  person_id              uuid        NOT NULL,
  version                integer     NOT NULL DEFAULT 0,
  display_name           text,
  work_email             text,
  department_value_id    uuid,
  cost_centre_value_id   uuid,
  started_on             date,
  left_on                date,
  status                 text,
  line_manager_person_id uuid,                                  -- a person ID only: the manager's own record may arrive after this one
  removed                boolean     NOT NULL DEFAULT false,   -- a tombstone: the record was removed in Nemura
  hold_version           integer     NOT NULL DEFAULT 0,
  hold_id                uuid,
  held                   boolean     NOT NULL DEFAULT false,
  hold_from              date,
  bank_version           integer     NOT NULL DEFAULT 0,
  bank_record_id         uuid,
  user_id                uuid,                                  -- the Fyniq user this person was mapped to, explicitly (FR-PPL-18)
  updated_at             timestamptz NOT NULL,
  CONSTRAINT person_projection_pkey PRIMARY KEY (organisation_id, person_id),
  CONSTRAINT person_projection_id_key UNIQUE (organisation_id, id),
  CONSTRAINT person_projection_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT person_projection_status_known CHECK (status IS NULL OR status IN ('candidate', 'active', 'on_leave', 'resigned', 'terminated', 'retired', 'ended')),
  CONSTRAINT person_projection_versions CHECK (version >= 0 AND hold_version >= 0 AND bank_version >= 0),
  CONSTRAINT person_projection_name_length CHECK (display_name IS NULL OR char_length(display_name) BETWEEN 1 AND 200),
  CONSTRAINT person_projection_email_shape CHECK (work_email IS NULL OR work_email ~ '^[^@\s]{1,100}@[^@\s]{1,200}$'),
  CONSTRAINT person_projection_dates CHECK (left_on IS NULL OR started_on IS NULL OR left_on >= started_on),
  CONSTRAINT person_projection_no_self_manager CHECK (line_manager_person_id IS DISTINCT FROM person_id),
  CONSTRAINT person_projection_hold_with_id CHECK (NOT held OR hold_id IS NOT NULL)
);
CREATE UNIQUE INDEX person_projection_user ON fyniq.person_projection (organisation_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX person_projection_manager ON fyniq.person_projection (organisation_id, line_manager_person_id);

-- +goose StatementBegin
CREATE FUNCTION fyniq.person_projection_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A person of the projection is kept: a removal is a tombstone' USING ERRCODE = 'FY900',
      HINT = 'Set removed instead; the history of who approved what names the person (FR-PPL-15).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.person_id <> OLD.person_id OR NEW.version < OLD.version OR NEW.hold_version < OLD.hold_version OR NEW.bank_version < OLD.bank_version THEN
      RAISE EXCEPTION 'A person of the projection only moves to a newer version of each part' USING ERRCODE = 'FY900',
        HINT = 'An older version is ignored and recorded in the inbox, never applied (FR-PPL-15).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER person_projection_guard BEFORE UPDATE OR DELETE ON fyniq.person_projection
  FOR EACH ROW EXECUTE FUNCTION fyniq.person_projection_guard();
CREATE TRIGGER person_projection_no_truncate BEFORE TRUNCATE ON fyniq.person_projection
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.person_projection
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('person_projection', 'person_id');

-- ------------------------------------------------------------------ row-level security and grants
ALTER TABLE fyniq.people_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.people_source ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_source FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_source
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.people_link_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_inbox FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_inbox
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.person_projection ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.person_projection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.person_projection
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_link, fyniq.people_source, fyniq.people_link_inbox, fyniq.person_projection FROM PUBLIC;
GRANT SELECT ON fyniq.people_link, fyniq.people_source, fyniq.people_link_inbox, fyniq.person_projection TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, state, epoch, nemura_organisation_id, link_key, client_id, approval_id, created_by, created_at, changed_at)
  ON fyniq.people_link TO fyniq_api;
GRANT UPDATE (state, client_id, version, changed_at) ON fyniq.people_link TO fyniq_api;
GRANT INSERT (feature, source, effective_from, approval_id, set_at) ON fyniq.people_source TO fyniq_api;
GRANT INSERT (event_id, link_id, epoch, sequence, type, object_type, object_id, object_version, payload_sha256, outcome, code, received_at)
  ON fyniq.people_link_inbox TO fyniq_api;
GRANT INSERT (person_id, version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status,
              line_manager_person_id, removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at)
  ON fyniq.person_projection TO fyniq_api;
GRANT UPDATE (version, display_name, work_email, department_value_id, cost_centre_value_id, started_on, left_on, status,
              line_manager_person_id, removed, hold_version, hold_id, held, hold_from, bank_version, bank_record_id, user_id, updated_at)
  ON fyniq.person_projection TO fyniq_api;
