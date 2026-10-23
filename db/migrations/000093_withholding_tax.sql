-- Withholding tax on payments to non-resident suppliers (PRD-001 FR-AP-19; the owner's decisions of 23 Oct 2026, built
-- in rev 0.90; p1-purchases task 9.1).
--
-- A contact keeps its tax residence when it differs from its country. For each non-resident supplier and nature of
-- payment, the tax agent's decision is kept and reused: not applicable, withhold at the rate they advised, or a reduced
-- treaty rate with a certificate of residence. A decision is never changed or removed (FY174): a later one for the same
-- supplier and nature replaces it from when it is recorded, and a bill posts with the decision in force when its entry
-- is recorded, so a replay finds the same one. The decision names the tax agent who gave it, and the bill it was
-- recorded from, whose files hold their advice. The tax withheld posts to the account with the new system role
-- withholding-tax-payable (2330 in the standard chart).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE fyniq.contact ADD COLUMN tax_residence text;
ALTER TABLE fyniq.contact ADD CONSTRAINT contact_tax_residence_format CHECK (tax_residence ~ '^[A-Z]{2}$') NOT VALID;
GRANT INSERT (tax_residence), UPDATE (tax_residence) ON fyniq.contact TO fyniq_api;

ALTER TABLE fyniq.account DROP CONSTRAINT account_role_known;
ALTER TABLE fyniq.account ADD CONSTRAINT account_role_known CHECK (role IN ('accounts-receivable', 'accounts-payable', 'gst-control',
  'fx-rounding', 'unrealised-fx', 'deferred-revenue', 'prepayments', 'salaries-payable', 'cpf-sdl-payable', 'employer-cpf', 'sdl',
  'depreciation', 'accumulated-depreciation', 'suspense', 'retained-earnings', 'expense-claims-payable', 'income-tax-payable',
  'income-tax-expense', 'director-loan', 'deferred-remuneration', 'share-capital', 'capital-contribution',
  'withholding-tax-payable')) NOT VALID;

CREATE TABLE fyniq.withholding_decision (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  supplier_id     uuid           NOT NULL,
  nature          text           NOT NULL,
  outcome         text           NOT NULL,
  rate_percent    numeric(9, 6),
  tax_agent       text           NOT NULL,
  note            text,
  bill_id         uuid,
  recorded_by     text           NOT NULL,
  recorded_at     timestamptz    NOT NULL DEFAULT now(),
  CONSTRAINT withholding_decision_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT withholding_decision_supplier_fkey FOREIGN KEY (organisation_id, supplier_id)
    REFERENCES fyniq.contact (organisation_id, id),
  CONSTRAINT withholding_decision_bill_fkey FOREIGN KEY (organisation_id, bill_id)
    REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT withholding_decision_nature_known CHECK (nature IN ('goods', 'software_use', 'software_rights', 'services_in_singapore',
    'services_outside_singapore', 'interest', 'movable_rent', 'other')),
  CONSTRAINT withholding_decision_outcome_known CHECK (outcome IN ('not_applicable', 'withhold', 'treaty')),
  CONSTRAINT withholding_decision_rate_with_outcome CHECK ((outcome = 'not_applicable') = (rate_percent IS NULL)),
  CONSTRAINT withholding_decision_rate_range CHECK (rate_percent <> 'NaN' AND rate_percent > 0 AND rate_percent <= 100
    AND rate_percent = trunc(rate_percent, 2)),
  CONSTRAINT withholding_decision_tax_agent_length CHECK (length(btrim(tax_agent)) BETWEEN 1 AND 200),
  CONSTRAINT withholding_decision_note_length CHECK (length(note) BETWEEN 1 AND 2000),
  CONSTRAINT withholding_decision_recorded_by_format CHECK (recorded_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX withholding_decision_supplier ON fyniq.withholding_decision (organisation_id, supplier_id, nature, recorded_at);

ALTER TABLE fyniq.withholding_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.withholding_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.withholding_decision
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.withholding_decision
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('withholding_decision', 'id');

-- A decision never changes or goes, whoever tries (FY174): a later decision replaces it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.withholding_decision_immutable() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION 'Withholding decision % never changes', OLD.id
    USING ERRCODE = 'FY174', HINT = 'Record a new decision for the supplier and nature: it replaces this one from when it is recorded (FR-AP-19).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER withholding_decision_immutable BEFORE UPDATE OR DELETE ON fyniq.withholding_decision
  FOR EACH ROW EXECUTE FUNCTION fyniq.withholding_decision_immutable();

GRANT SELECT ON fyniq.withholding_decision TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (supplier_id, nature, outcome, rate_percent, tax_agent, note, bill_id, recorded_by) ON fyniq.withholding_decision TO fyniq_api;
