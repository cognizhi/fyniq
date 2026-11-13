-- Connections to AI model providers (PRD-001 AI-72, AI-73, AI-79; p1-ai-providers task 3.1, design D2).
--
-- A connection is one of the release's model-provider connectors, from a preset or its own endpoint: how it
-- authenticates, its configuration, the one host its calls go to, its credential by reference (the value lives in the
-- secret store, never here), and how it is billed. It starts off. Creating it, changing it, replacing its credential and
-- enabling it are approval requests of the object ai_connection under Appendix D's "AI providers and model routes" (one
-- person asks, the Owner approves: golden rule 6); switching it off is never held back.
--
-- The database keeps it so (golden rule 2): a person's own plan (AI-73 (c)) cannot be stored, no authentication only
-- for a self-hosted server, a subscription always with its quota, an API-key connection enabled only with its
-- credential; every version is kept, as written, beside it, and the connection is never removed (FY330). The approval
-- objects are widened NOT VALID and 000344 validates them (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_connection (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  name              text        NOT NULL,
  connector         text        NOT NULL,  -- the model-provider connector: anthropic, openai, gemini, openai-compatible
  preset            text,                  -- the preset it starts from, if any
  auth              text        NOT NULL,
  configuration     jsonb       NOT NULL DEFAULT '{}',
  host              text        NOT NULL,  -- the one host its calls go to (SEC-36)
  self_hosted       boolean     NOT NULL DEFAULT false, -- reached only as a registered private endpoint (SEC-37)
  credential_ref    text,                  -- the secret version that holds the credential; never the value (AI-79)
  credential_set_at timestamptz,
  billing_mode      text        NOT NULL,
  quota             jsonb,                 -- a subscription's quota, window and fee (AI-73)
  state             text        NOT NULL DEFAULT 'disabled',
  version           integer     NOT NULL DEFAULT 1,
  created_by        text        NOT NULL,
  created_at        timestamptz NOT NULL,
  changed_by        text        NOT NULL,
  changed_at        timestamptz NOT NULL,
  CONSTRAINT ai_connection_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_connection_name UNIQUE (organisation_id, name),
  CONSTRAINT ai_connection_name_format CHECK (length(name) BETWEEN 1 AND 80 AND name = btrim(name)),
  CONSTRAINT ai_connection_connector_format CHECK (connector ~ '^[a-z][a-z0-9-]{1,39}$'),
  CONSTRAINT ai_connection_preset_format CHECK (preset ~ '^[a-z][a-z0-9-]{1,39}$'),
  -- A person's own plan (AI-73 (c)) is P2: not an authentication a connection may have yet.
  CONSTRAINT ai_connection_auth_known CHECK (auth IN ('api-key', 'workload-identity', 'none')),
  CONSTRAINT ai_connection_none_self_hosted CHECK (auth <> 'none' OR self_hosted),
  CONSTRAINT ai_connection_configuration_object CHECK (jsonb_typeof(configuration) = 'object'),
  CONSTRAINT ai_connection_host_format CHECK (host ~ '^[a-z0-9][a-z0-9.:-]{0,252}$'),
  CONSTRAINT ai_connection_credential_format CHECK (credential_ref ~ '^[!-~]+$' AND length(credential_ref) <= 512),
  CONSTRAINT ai_connection_credential_with_time CHECK ((credential_ref IS NULL) = (credential_set_at IS NULL)),
  CONSTRAINT ai_connection_billing_known CHECK (billing_mode IN ('usage', 'subscription')),
  CONSTRAINT ai_connection_quota_with_subscription CHECK ((billing_mode = 'subscription') = (quota IS NOT NULL)),
  CONSTRAINT ai_connection_quota_object CHECK (jsonb_typeof(quota) = 'object'),
  CONSTRAINT ai_connection_state_known CHECK (state IN ('disabled', 'enabled')),
  CONSTRAINT ai_connection_enabled_with_key CHECK (state <> 'enabled' OR auth <> 'api-key' OR credential_ref IS NOT NULL),
  CONSTRAINT ai_connection_version_positive CHECK (version >= 1),
  CONSTRAINT ai_connection_created_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT ai_connection_changed_by_format CHECK (changed_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- Every version of a connection, as it was: what it became, who and when, and the approval that made it (none for a
-- switch off, which is never held back).
CREATE TABLE fyniq.ai_connection_version (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  connection_id   uuid        NOT NULL,
  version         integer     NOT NULL,
  step            text        NOT NULL,
  snapshot        jsonb       NOT NULL,
  approval_id     uuid,
  made_by         text        NOT NULL,
  made_at         timestamptz NOT NULL,
  CONSTRAINT ai_connection_version_pkey PRIMARY KEY (organisation_id, connection_id, version),
  CONSTRAINT ai_connection_version_id UNIQUE (organisation_id, id),
  CONSTRAINT ai_connection_version_connection_fkey FOREIGN KEY (organisation_id, connection_id) REFERENCES fyniq.ai_connection (organisation_id, id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT ai_connection_version_step_known CHECK (step IN ('created', 'changed', 'credential', 'enabled', 'disabled')),
  CONSTRAINT ai_connection_version_approved CHECK (step = 'disabled' OR approval_id IS NOT NULL),
  CONSTRAINT ai_connection_version_snapshot_object CHECK (jsonb_typeof(snapshot) = 'object'),
  CONSTRAINT ai_connection_version_made_by_format CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_connection_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An AI connection is kept: switch it off instead' USING ERRCODE = 'FY330';
  END IF;
  IF TG_TABLE_NAME = 'ai_connection_version' THEN
    IF TG_OP <> 'INSERT' THEN
      RAISE EXCEPTION 'An AI connection''s version is kept as it was made' USING ERRCODE = 'FY330';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 OR NEW.state <> 'disabled' THEN
      RAISE EXCEPTION 'An AI connection starts off, at its first version' USING ERRCODE = 'FY330';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id <> OLD.id OR NEW.connector <> OLD.connector OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at
      OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'An AI connection changes one version at a time, its provider and creation as they were' USING ERRCODE = 'FY330';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_connection_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_connection
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_connection_guard();
CREATE TRIGGER ai_connection_version_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_connection_version
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_connection_guard();
CREATE TRIGGER ai_connection_no_truncate BEFORE TRUNCATE ON fyniq.ai_connection
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_connection_version_no_truncate BEFORE TRUNCATE ON fyniq.ai_connection_version
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- Each version of a connection is kept beside it in the same transaction, as its state says.
-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_connection_versioned() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.ai_connection_version v WHERE v.organisation_id = NEW.organisation_id AND v.connection_id = NEW.id
      AND v.version = NEW.version AND v.snapshot ->> 'state' = NEW.state AND v.made_by = NEW.changed_by) THEN
    RAISE EXCEPTION 'AI connection % has no record of its version %', NEW.id, NEW.version USING ERRCODE = 'FY330';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER ai_connection_versioned AFTER INSERT OR UPDATE ON fyniq.ai_connection
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.ai_connection_versioned();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.ai_connection
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_connection', 'id');

ALTER TABLE fyniq.ai_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_connection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_connection
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_connection_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_connection_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_connection_version
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_connection, fyniq.ai_connection_version FROM PUBLIC;
GRANT SELECT ON fyniq.ai_connection, fyniq.ai_connection_version TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (id, name, connector, preset, auth, configuration, host, self_hosted, credential_ref, credential_set_at, billing_mode, quota,
              created_by, created_at, changed_by, changed_at) ON fyniq.ai_connection TO fyniq_api;
GRANT UPDATE (name, preset, auth, configuration, host, self_hosted, credential_ref, credential_set_at, billing_mode, quota, state, version,
              changed_by, changed_at) ON fyniq.ai_connection TO fyniq_api;
GRANT INSERT (connection_id, version, step, snapshot, approval_id, made_by, made_at) ON fyniq.ai_connection_version TO fyniq_api;

-- The approval requests of a connection, under Appendix D's "AI providers and model routes".
ALTER TABLE fyniq.approval_request DROP CONSTRAINT approval_request_object_known;
ALTER TABLE fyniq.approval_request ADD CONSTRAINT approval_request_object_known CHECK (
  (object_type, operation) IN (('organisation_setting', 'organisation_setting.change'), ('membership', 'role.assign'),
                               ('membership', 'role.revoke'), ('membership', 'member.remove'),
                               ('member_invitation', 'member_invitation.create'),
                               ('approval_policy', 'approval_policy.change'), ('statutory_pack', 'statutory_pack.activate'),
                               ('team_mode', 'team_mode.change'), ('period', 'period.reopen'),
                               ('journal', 'journal.post'), ('journal', 'journal.reverse'), ('journal', 'journal.edit'),
                               ('opening_balance', 'opening_balance.edit'),
                               ('bill', 'bill.post'), ('bill', 'bill.void'), ('bill', 'bill.edit'),
                               ('bill_payment', 'bill_payment.void'),
                               ('credit_application', 'credit_application.void'),
                               ('supplier_refund', 'supplier_refund.void'),
                               ('purchase_order', 'purchase_order.approve'),
                               ('document_template', 'document_template.publish'),
                               ('invoice', 'invoice.post'), ('invoice', 'invoice.void'), ('invoice', 'invoice.edit'),
                               ('customer_receipt', 'customer_receipt.void'),
                               ('write_off', 'write_off.post'), ('write_off', 'write_off.void'),
                               ('customer_credit_application', 'customer_credit_application.void'),
                               ('customer_refund', 'customer_refund.void'),
                               ('customer_deposit', 'customer_deposit.void'),
                               ('recurring_invoice', 'recurring_invoice.approve'),
                               ('connector', 'connector.change'), ('bank_rule', 'bank_rule.auto_apply'),
                               ('expense_claim', 'expense_claim.post'), ('claim_reimbursement', 'claim_reimbursement.void'),
                               ('cash_advance', 'cash_advance.void'), ('asset_disposal', 'asset_disposal.void'),
                               ('fixed_asset', 'fixed_asset.void'),
                               ('payee_bank_change', 'payee_bank_change.approve'),
                               ('claimant_bank_change', 'claimant_bank_change.approve'),
                               ('payment_batch', 'payment_batch.approve'), ('payment_batch', 'payment_batch.reapprove'),
                               ('contact_member_link', 'contact_member_link.change'),
                               ('director_fee', 'director_fee.post'),
                               ('director_settlement', 'director_settlement.approve'),
                               ('director_repayment', 'director_repayment.void'),
                               ('account', 'account.merge'),
                               ('bank_reconciliation', 'bank_reconciliation.sign'),
                               ('holder_record', 'holder_record.record'), ('share_class', 'share_class.record'),
                               ('equity_transaction', 'equity_transaction.approve'),
                               ('instrument', 'instrument.record'),
                               ('instrument_template', 'instrument_template.enable'),
                               ('option_pool', 'option_pool.record'), ('option_grant', 'option_grant.record'),
                               ('option_event', 'option_event.record'), ('round_scenario', 'round_scenario.approve'),
                               ('investor_right', 'investor_right.record'),
                               ('controller_entry', 'controller_entry.record'),
                               ('budget', 'budget.record'),
                               ('migration_commit', 'migration.commit'),
                               ('tenant_export', 'tenant_export.prepare'),
                               ('ai_connection', 'ai_connection.create'), ('ai_connection', 'ai_connection.change'),
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'))) NOT VALID;

ALTER TABLE fyniq.approval_policy DROP CONSTRAINT approval_policy_object_known;
ALTER TABLE fyniq.approval_policy ADD CONSTRAINT approval_policy_object_known CHECK (
  object_type IN ('organisation_setting', 'membership', 'member_invitation', 'statutory_pack', 'team_mode', 'period', 'journal',
                  'opening_balance', 'bill', 'bill_payment', 'credit_application', 'supplier_refund', 'purchase_order',
                  'document_template', 'invoice', 'customer_receipt', 'write_off', 'customer_credit_application',
                  'customer_refund', 'customer_deposit', 'recurring_invoice', 'connector', 'bank_rule', 'expense_claim',
                  'claim_reimbursement', 'cash_advance', 'asset_disposal', 'fixed_asset',
                  'payee_bank_change', 'claimant_bank_change', 'payment_batch', 'contact_member_link',
                  'director_fee', 'director_settlement', 'director_repayment', 'account', 'bank_reconciliation',
                  'holder_record', 'share_class', 'equity_transaction', 'instrument', 'instrument_template',
                  'option_pool', 'option_grant', 'option_event', 'round_scenario', 'investor_right', 'controller_entry', 'budget',
                  'migration_commit', 'tenant_export', 'ai_connection')) NOT VALID;
