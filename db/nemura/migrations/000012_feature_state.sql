-- The feature registry (PRD-002 NM-FEAT-01 to NM-FEAT-05; p1-nemura-records-packs slice 3).
--
-- Each optional feature of an organisation is on, off or external (another system holds that data, named). A feature with no row is off: People
-- records is always on and has no row. Switching a feature changes this row and nothing else: its records stay (NM-FEAT-04). The dependencies are
-- checked in the code (internal/nemura/features) and the states here are constrained.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.feature_state (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  feature         text        NOT NULL,
  state           text        NOT NULL,
  external_system text,
  changed_by      uuid        NOT NULL,
  changed_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT feature_state_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT feature_state_feature_key UNIQUE (organisation_id, feature),
  CONSTRAINT feature_state_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT feature_state_changed_by_fkey FOREIGN KEY (organisation_id, changed_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT feature_state_feature_known CHECK (feature IN ('leave', 'payroll', 'self_service', 'letters', 'hiring', 'performance', 'onboarding', 'time')),
  CONSTRAINT feature_state_state_known CHECK (state IN ('on', 'off', 'external')),
  CONSTRAINT feature_state_external_named CHECK ((state = 'external') = (external_system IS NOT NULL AND length(btrim(external_system)) BETWEEN 1 AND 100))
);

-- A row only changes state (and who and when); the feature never changes, and nothing is deleted.
-- +goose StatementBegin
CREATE FUNCTION nemura.feature_state_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.feature IS DISTINCT FROM OLD.feature THEN
    RAISE EXCEPTION 'A feature switch is changed, never removed or renamed' USING ERRCODE = 'NM125';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER feature_state_guard BEFORE UPDATE OR DELETE ON nemura.feature_state FOR EACH ROW EXECUTE FUNCTION nemura.feature_state_guard();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.feature_state
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('feature_state', 'id');

ALTER TABLE nemura.feature_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.feature_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.feature_state USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.feature_state TO nemura_api, nemura_worker, nemura_executor, nemura_readonly;
GRANT INSERT (id, feature, state, external_system, changed_by), UPDATE (state, external_system, changed_by, changed_at) ON nemura.feature_state TO nemura_api;
