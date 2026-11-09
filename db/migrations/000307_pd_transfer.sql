-- Overseas transfers of personal data, each assessed and recorded (PRD-001 CMP-PD-06; rev 0.244; p1-pdpa-retention
-- slice 8, design D7). An entry names the recipient, where it processes the data, the data, the purpose, the basis that
-- protects it abroad, when and by whom it was assessed, the evidence and the next review. It may cover one of Fyniq's
-- own flows (compliance/data-inventory.yaml's flows, such as sign-in and email) or one the company has outside Fyniq.
-- Entries are kept as versions, only ever added (FY316), each audited in the same transaction (AUD-01), under the
-- "Data protection (PDPA)" row of Appendix D.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.pd_transfer (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  transfer_id     uuid        NOT NULL,  -- the transfer, the same across its versions
  version         integer     NOT NULL,
  flow            text,                  -- the catalogue flow it covers, or none for one of the company's own
  recipient       text        NOT NULL,
  countries       text        NOT NULL,  -- where the recipient processes the data
  data            text        NOT NULL,
  purpose         text        NOT NULL,
  basis           text        NOT NULL,
  assessed_on     date        NOT NULL,
  assessed_by     text        NOT NULL,  -- who assessed it: a person's name, or the firm that did
  evidence        text        NOT NULL,  -- where the contract, rules or certificate are kept
  next_review_on  date        NOT NULL,
  state           text        NOT NULL,  -- open, or retired once the transfer stops
  saved_by        text        NOT NULL,
  saved_at        timestamptz NOT NULL,
  CONSTRAINT pd_transfer_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT pd_transfer_version_once UNIQUE (organisation_id, transfer_id, version),
  CONSTRAINT pd_transfer_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT pd_transfer_version_positive CHECK (version > 0),
  CONSTRAINT pd_transfer_flow_format CHECK (flow ~ '^[a-z][a-z0-9-]{0,39}$'),
  CONSTRAINT pd_transfer_texts CHECK (length(btrim(recipient)) BETWEEN 1 AND 300 AND length(btrim(countries)) BETWEEN 1 AND 300
    AND length(btrim(data)) BETWEEN 1 AND 500 AND length(btrim(purpose)) BETWEEN 1 AND 1000 AND length(btrim(assessed_by)) BETWEEN 1 AND 300
    AND length(btrim(evidence)) BETWEEN 1 AND 1000),
  -- The bases the transfer limitation obligation accepts: legally enforceable obligations (a contract, binding corporate
  -- rules, or another law or binding instrument), a specified certification such as APEC CBPR, or the person's consent.
  CONSTRAINT pd_transfer_basis_known CHECK (basis IN ('contract', 'binding_corporate_rules', 'other_instrument', 'certification', 'consent')),
  CONSTRAINT pd_transfer_review_after CHECK (next_review_on > assessed_on),
  CONSTRAINT pd_transfer_state_known CHECK (state IN ('open', 'retired')),
  CONSTRAINT pd_transfer_saved_by_format CHECK (saved_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- A transfer's version follows its last, keeps the flow it covers, and nothing of the register ever changes or goes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.pd_transfer_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  last fyniq.pd_transfer;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The transfers'' versions never change or go' USING ERRCODE = 'FY316',
      HINT = 'Save the transfer''s next version, or retire it (CMP-PD-06).';
  END IF;
  SELECT * INTO last FROM fyniq.pd_transfer t
    WHERE t.organisation_id = NEW.organisation_id AND t.transfer_id = NEW.transfer_id ORDER BY t.version DESC LIMIT 1;
  IF NEW.version <> coalesce(last.version, 0) + 1 THEN
    RAISE EXCEPTION 'Version % of a transfer follows its version %', NEW.version, coalesce(last.version, 0) USING ERRCODE = 'FY316';
  END IF;
  IF last.version IS NOT NULL AND NEW.flow IS DISTINCT FROM last.flow THEN
    RAISE EXCEPTION 'A transfer keeps the flow it covers' USING ERRCODE = 'FY316',
      HINT = 'Retire it and record a new transfer for the other flow.';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER pd_transfer_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.pd_transfer
  FOR EACH ROW EXECUTE FUNCTION fyniq.pd_transfer_guard();
CREATE TRIGGER pd_transfer_no_truncate BEFORE TRUNCATE ON fyniq.pd_transfer
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.pd_transfer
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('pd_transfer', 'transfer_id');

ALTER TABLE fyniq.pd_transfer ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.pd_transfer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.pd_transfer
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (transfer_id, version, flow, recipient, countries, data, purpose, basis, assessed_on, assessed_by, evidence, next_review_on, state,
  saved_by, saved_at) ON fyniq.pd_transfer TO fyniq_api;
GRANT SELECT ON fyniq.pd_transfer TO fyniq_api, fyniq_worker, fyniq_readonly;
