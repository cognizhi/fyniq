-- Packs, company policy and the decisions on unverified values (PRD-002 NM-PACK-02, 04, 07, 08; p1-nemura-records-packs slice 4).
--
-- The pack files are code (internal/nemura/packs); the database holds what an organisation did with them: which version it accepted (accepting verifies
-- nothing), the decisions it recorded to use a value that is only researched, and its own policy on top, effective-dated and checked against the pack.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.pack_acceptance (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  pack            text        NOT NULL,
  version         text        NOT NULL,
  accepted_by     uuid        NOT NULL,
  accepted_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pack_acceptance_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pack_acceptance_once UNIQUE (organisation_id, version),
  CONSTRAINT pack_acceptance_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT pack_acceptance_by_fkey FOREIGN KEY (organisation_id, accepted_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pack_acceptance_version_shape CHECK (version ~ '^[a-z][a-z0-9-]{1,30}@20[0-9]{2}\.[1-9][0-9]{0,2}$')
);

-- The company's recorded decision to use a value that is only researched (NM-PACK-07): who decided, why, and for which value of which version.
CREATE TABLE nemura.pack_decision (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  version         text        NOT NULL,
  table_name      text        NOT NULL,
  row_key         text        NOT NULL,
  decided_by      uuid        NOT NULL,
  reason          text        NOT NULL,
  decided_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT pack_decision_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pack_decision_once UNIQUE (organisation_id, version, table_name, row_key),
  CONSTRAINT pack_decision_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT pack_decision_by_fkey FOREIGN KEY (organisation_id, decided_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT pack_decision_reason_length CHECK (length(btrim(reason)) BETWEEN 10 AND 2000)
);

CREATE TABLE nemura.company_policy (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  effective_from  date        NOT NULL,
  effective_to    date,
  rules           jsonb       NOT NULL,
  pack_version    text        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT company_policy_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT company_policy_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES nemura.organisation (organisation_id),
  CONSTRAINT company_policy_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT company_policy_dates CHECK (effective_to IS NULL OR effective_to > effective_from),
  CONSTRAINT company_policy_rules_object CHECK (jsonb_typeof(rules) = 'object' AND octet_length(rules::text) <= 16384)
);

-- Acceptances and decisions are never changed or deleted; a policy row is history that can only be ended, once, and never overlaps another.
-- +goose StatementBegin
CREATE FUNCTION nemura.pack_record_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% is never changed or deleted', TG_ARGV[0] USING ERRCODE = 'NM129';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pack_acceptance_guard BEFORE UPDATE OR DELETE ON nemura.pack_acceptance FOR EACH ROW EXECUTE FUNCTION nemura.pack_record_guard('An acceptance');
CREATE TRIGGER pack_decision_guard BEFORE UPDATE OR DELETE ON nemura.pack_decision FOR EACH ROW EXECUTE FUNCTION nemura.pack_record_guard('A decision');
-- +goose StatementBegin
CREATE FUNCTION nemura.company_policy_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND (NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.rules IS DISTINCT FROM OLD.rules OR NEW.pack_version IS DISTINCT FROM OLD.pack_version
     OR (OLD.effective_to IS NOT NULL AND NEW.effective_to IS DISTINCT FROM OLD.effective_to))) THEN
    RAISE EXCEPTION 'A company policy is history: it can only be ended, once' USING ERRCODE = 'NM130';
  END IF;
  IF EXISTS (SELECT 1 FROM nemura.company_policy p WHERE p.organisation_id = NEW.organisation_id AND p.id <> NEW.id
               AND daterange(p.effective_from, coalesce(p.effective_to, 'infinity'), '[)') && daterange(NEW.effective_from, coalesce(NEW.effective_to, 'infinity'), '[)')) THEN
    RAISE EXCEPTION 'A company has one policy at a time' USING ERRCODE = 'NM131';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER company_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON nemura.company_policy FOR EACH ROW EXECUTE FUNCTION nemura.company_policy_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pack_acceptance DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pack_acceptance', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.pack_decision DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('pack_decision', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.company_policy DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('company_policy', 'id');

ALTER TABLE nemura.pack_acceptance ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pack_acceptance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pack_acceptance USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.pack_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.pack_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.pack_decision USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
ALTER TABLE nemura.company_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.company_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.company_policy USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.pack_acceptance, nemura.pack_decision, nemura.company_policy TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, pack, version, accepted_by) ON nemura.pack_acceptance TO nemura_api;
GRANT INSERT (id, version, table_name, row_key, decided_by, reason) ON nemura.pack_decision TO nemura_api;
GRANT INSERT (id, effective_from, rules, pack_version, created_by), UPDATE (effective_to) ON nemura.company_policy TO nemura_api;
