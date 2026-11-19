-- Standard letters without waiting (PRD-002 NM-LTR-04; p1-nemura-self-service-letters slice 4).
--
-- A signatory delegates automatic issue of one letter type: the approved template version, who may ask (active employees not serving notice), a monthly limit and an end date at
-- most twelve months away. A delegation never covers salary or pay terms. It is revoked once, by a signatory, or ends by itself: at its end date, or when the type's template
-- changes (the use checks that the template is still the one approved). Each letter issued under it is an ordinary issued letter whose authority names the delegation.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.letter_delegation (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  type_key        text        NOT NULL,
  template_id     uuid        NOT NULL,
  who             text        NOT NULL DEFAULT 'active_employees',
  fields          text[]      NOT NULL,
  monthly_limit   integer     NOT NULL,
  created_on      date        NOT NULL,
  ends_on         date        NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  revoked_by      uuid,
  revoked_reason  text,
  CONSTRAINT letter_delegation_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT letter_delegation_template_fkey FOREIGN KEY (organisation_id, template_id) REFERENCES nemura.letter_template_version (organisation_id, id),
  CONSTRAINT letter_delegation_created_by_fkey FOREIGN KEY (organisation_id, created_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT letter_delegation_revoked_by_fkey FOREIGN KEY (organisation_id, revoked_by) REFERENCES nemura.member (organisation_id, id),
  -- never a letter that shows pay: the salary certificate, the pay-change letter and the key employment terms
  CONSTRAINT letter_delegation_no_pay CHECK (type_key NOT IN ('salary_certificate', 'pay_change_letter', 'kets')),
  CONSTRAINT letter_delegation_who_known CHECK (who IN ('active_employees')),
  CONSTRAINT letter_delegation_limit CHECK (monthly_limit BETWEEN 1 AND 1000),
  CONSTRAINT letter_delegation_ends CHECK (ends_on > created_on AND ends_on <= (created_on + interval '12 months')::date),
  CONSTRAINT letter_delegation_revoked_whole CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND coalesce(length(revoked_reason), 0) <= 500)
);
CREATE INDEX letter_delegation_type_idx ON nemura.letter_delegation (organisation_id, type_key, ends_on) WHERE revoked_at IS NULL;

-- A delegation never changes except to be revoked, once.
-- +goose StatementBegin
CREATE FUNCTION nemura.letter_delegation_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, nemura, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.type_key IS DISTINCT FROM OLD.type_key OR NEW.template_id IS DISTINCT FROM OLD.template_id OR NEW.who IS DISTINCT FROM OLD.who
     OR NEW.fields IS DISTINCT FROM OLD.fields OR NEW.monthly_limit IS DISTINCT FROM OLD.monthly_limit OR NEW.created_on IS DISTINCT FROM OLD.created_on OR NEW.ends_on IS DISTINCT FROM OLD.ends_on
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL THEN
    RAISE EXCEPTION 'A delegation keeps its terms and is revoked once' USING ERRCODE = 'NM169';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER letter_delegation_guard BEFORE UPDATE OR DELETE ON nemura.letter_delegation FOR EACH ROW EXECUTE FUNCTION nemura.letter_delegation_guard();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON nemura.letter_delegation DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('letter_delegation', 'id');

ALTER TABLE nemura.letter_delegation ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.letter_delegation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.letter_delegation USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT ON nemura.letter_delegation TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, type_key, template_id, who, fields, monthly_limit, created_on, ends_on, created_by, created_at), UPDATE (revoked_at, revoked_by, revoked_reason) ON nemura.letter_delegation TO nemura_api;
