-- An organisation's activation of a statutory pack version (PRD-001 ENG-32, SOD-10, FR-ORG-05;
-- p1-statutory-packs tasks 1.2 and 3.1).
--
-- Pack contents are global (000033); choosing when to adopt a version is the organisation's own, so it is a
-- tenant row. Activating needs the version's pinned statutory tests to pass and a second person's approval
-- through the approval engine (internal/domain/pack CheckActivation): the row names who proposed it, who
-- approved it (never the same person, SOD-10) and the approval engine's record. Insert-only and audited.
--
-- Until p1-approval-engine lands no role may insert: every role reads, and the engine's executor gets the
-- INSERT grant with the engine (provisional, PRD-001 rev 0.37).

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.statutory_pack_activation (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version_id      text        NOT NULL,
  proposed_by     uuid        NOT NULL,
  approved_by     uuid        NOT NULL,
  approval_id     uuid        NOT NULL,
  activated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT statutory_pack_activation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT statutory_pack_activation_version_key UNIQUE (organisation_id, version_id),
  CONSTRAINT statutory_pack_activation_organisation_fkey FOREIGN KEY (organisation_id)
    REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT statutory_pack_activation_version_fkey FOREIGN KEY (version_id)
    REFERENCES fyniq.statutory_pack_version (id),
  CONSTRAINT statutory_pack_activation_two_people CHECK (approved_by <> proposed_by)
);

ALTER TABLE fyniq.statutory_pack_activation ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.statutory_pack_activation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.statutory_pack_activation
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.statutory_pack_activation
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('statutory_pack_activation', 'id');

GRANT SELECT ON fyniq.statutory_pack_activation TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
