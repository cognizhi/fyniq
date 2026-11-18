-- Each agent's route (PRD-001 AI-74, AI-26; p1-ai-providers task 5.1, design D6).
--
-- ai_agent lists the agents that call a model: a global table, the same for every organisation, written only by the
-- migrations (later changes add their agents) and read through the view ai_agents. Each agent names the data classes it
-- may ever handle and the model line its presets pin.
--
-- ai_route is each version of an agent's route in an organisation: the classes it handles and, in ai_route_step, its
-- ordered steps (a connection, a model on that connection's allow-list, the thinking effort and whether the stable
-- prompt and tools are cached), the first the primary and the rest its fallbacks. A version is the next one for its
-- agent, kept as it was made, with its steps written with it, never changed or removed (FY334). A version that sends
-- the agent's data to a connection or model the one before did not use, or a class it did not handle, names the approval
-- of its second approver; the database refuses a class the agent never handles and a step whose model is not on its
-- connection's allow-list.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- ------------------------------------------------------------------ the agents (global)
CREATE TABLE fyniq.ai_agent (
  id          text    NOT NULL,
  name        text    NOT NULL,
  description text    NOT NULL,
  classes     text[]  NOT NULL, -- the data classes it may ever handle (AI-75)
  line        text    NOT NULL, -- the model line its presets pin: main, or classification for high-volume work (AI-26)
  position    integer NOT NULL, -- where it is listed
  CONSTRAINT ai_agent_pkey PRIMARY KEY (id),
  CONSTRAINT ai_agent_position UNIQUE (position),
  CONSTRAINT ai_agent_id_format CHECK (id ~ '^[a-z][a-z0-9_.]{1,59}$'),
  CONSTRAINT ai_agent_text CHECK (length(name) BETWEEN 1 AND 80 AND length(description) BETWEEN 1 AND 300),
  CONSTRAINT ai_agent_classes CHECK (cardinality(classes) >= 1
    AND classes <@ ARRAY['know-how', 'business-figures', 'personal-data', 'payroll']::text[]),
  CONSTRAINT ai_agent_line CHECK (line IN ('main', 'classification'))
);
REVOKE ALL ON fyniq.ai_agent FROM PUBLIC;

-- The agents of AI-74. Each catalogue task type's drafting and each scheduled agent are added by the changes that add
-- them; task_drafting stands for the drafting until then.
INSERT INTO fyniq.ai_agent (id, name, description, classes, line, position) VALUES
  ('chat', 'Assistant', 'The assistant''s chat and tool loop: questions, answers and the tools it reads with.',
   ARRAY['know-how', 'business-figures', 'personal-data', 'payroll'], 'main', 10),
  ('document_capture', 'Document capture', 'Reads bills, receipts and statements into their fields.',
   ARRAY['business-figures', 'personal-data'], 'classification', 20),
  ('bank_categorisation', 'Bank categorisation', 'Suggests the category, contact and GST code of bank lines, and their matches.',
   ARRAY['business-figures', 'personal-data'], 'classification', 30),
  ('task_drafting', 'Proposal drafting', 'Drafts the proposals of the catalogue''s task types for a person to approve.',
   ARRAY['business-figures', 'personal-data'], 'main', 40),
  ('report_commentary', 'Report commentary', 'Writes the commentary of reports and board packs from their figures.',
   ARRAY['business-figures'], 'main', 50),
  ('kb_compiler', 'Knowledge-base compiler', 'Compiles the knowledge base from the user guide and its other sources.',
   ARRAY['know-how'], 'main', 60),
  ('eval_runner', 'Eval runner', 'Runs the evals that qualify a model for an agent, on synthetic data.',
   ARRAY['know-how', 'business-figures'], 'main', 70),
  ('probe', 'Probe', 'Asks a model a few tokens of synthetic text to prove a connection can call it.',
   ARRAY['know-how'], 'classification', 80);

CREATE VIEW fyniq.ai_agents AS SELECT id, name, description, classes, line, position FROM fyniq.ai_agent;
GRANT SELECT ON fyniq.ai_agents TO fyniq_api, fyniq_worker, fyniq_readonly;

-- ------------------------------------------------------------------ an organisation's routes
CREATE TABLE fyniq.ai_route (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  agent           text        NOT NULL,
  version         integer     NOT NULL,
  classes         text[]      NOT NULL, -- the data classes it handles (AI-74)
  preset          text,                 -- the ADR-T2 preset it was set from, if any (AI-26)
  approval_id     uuid,                 -- the second approver's approval, for a version that widens where data goes
  made_by         text        NOT NULL,
  made_at         timestamptz NOT NULL,
  created_xact    bigint      NOT NULL DEFAULT pg_current_xact_id()::text::bigint,
  CONSTRAINT ai_route_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_route_agent_fkey FOREIGN KEY (agent) REFERENCES fyniq.ai_agent (id),
  CONSTRAINT ai_route_approval_fkey FOREIGN KEY (organisation_id, approval_id) REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT ai_route_version UNIQUE (organisation_id, agent, version),
  CONSTRAINT ai_route_version_positive CHECK (version >= 1),
  CONSTRAINT ai_route_classes CHECK (cardinality(classes) >= 1
    AND classes <@ ARRAY['know-how', 'business-figures', 'personal-data', 'payroll']::text[]),
  CONSTRAINT ai_route_preset CHECK (preset IN ('A', 'B', 'C')),
  CONSTRAINT ai_route_made_by_format CHECK (made_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$')
);
CREATE INDEX ai_route_by_approval ON fyniq.ai_route (organisation_id, approval_id);

CREATE TABLE fyniq.ai_route_step (
  organisation_id uuid    NOT NULL DEFAULT fyniq.current_org(),
  id              uuid    NOT NULL DEFAULT uuidv7(),
  route_id        uuid    NOT NULL,
  position        integer NOT NULL, -- 1 the primary, then the fallbacks in order
  connection_id   uuid    NOT NULL,
  model           text    NOT NULL,
  effort          text    NOT NULL DEFAULT '',
  cache           boolean NOT NULL DEFAULT true,
  CONSTRAINT ai_route_step_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_route_step_route_fkey FOREIGN KEY (organisation_id, route_id) REFERENCES fyniq.ai_route (organisation_id, id),
  CONSTRAINT ai_route_step_model_fkey FOREIGN KEY (organisation_id, connection_id, model) REFERENCES fyniq.ai_model (organisation_id, connection_id, model),
  CONSTRAINT ai_route_step_position UNIQUE (organisation_id, route_id, position),
  CONSTRAINT ai_route_step_once UNIQUE (organisation_id, route_id, connection_id, model),
  CONSTRAINT ai_route_step_position_range CHECK (position BETWEEN 1 AND 5),
  CONSTRAINT ai_route_step_effort CHECK (effort IN ('', 'low', 'medium', 'high'))
);
CREATE INDEX ai_route_step_by_model ON fyniq.ai_route_step (organisation_id, connection_id, model);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_route_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  may text[];
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A route''s version and its steps are kept as they were made' USING ERRCODE = 'FY334';
  END IF;
  IF TG_TABLE_NAME = 'ai_route_step' THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.ai_route r WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.route_id
                     AND r.created_xact = pg_current_xact_id()::text::bigint) THEN
      RAISE EXCEPTION 'A route''s steps are written with its version, in its transaction' USING ERRCODE = 'FY334';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM fyniq.ai_model m WHERE m.organisation_id = NEW.organisation_id AND m.connection_id = NEW.connection_id
                     AND m.model = NEW.model AND m.allowed) THEN
      RAISE EXCEPTION 'A route names only a model on its connection''s allow-list, not %', NEW.model USING ERRCODE = 'FY334';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.version <> 1 + coalesce((SELECT max(r.version) FROM fyniq.ai_route r
                                   WHERE r.organisation_id = NEW.organisation_id AND r.agent = NEW.agent), 0)
     OR NEW.created_xact <> pg_current_xact_id()::text::bigint THEN
    RAISE EXCEPTION 'A route''s version is the next one for its agent' USING ERRCODE = 'FY334';
  END IF;
  SELECT a.classes INTO may FROM fyniq.ai_agents a WHERE a.id = NEW.agent;
  IF NOT NEW.classes <@ may THEN
    RAISE EXCEPTION 'The agent % never handles all of %', NEW.agent, NEW.classes USING ERRCODE = 'FY334';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_route_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_route
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_route_guard();
CREATE TRIGGER ai_route_step_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.ai_route_step
  FOR EACH ROW EXECUTE FUNCTION fyniq.ai_route_guard();
CREATE TRIGGER ai_route_no_truncate BEFORE TRUNCATE ON fyniq.ai_route
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER ai_route_step_no_truncate BEFORE TRUNCATE ON fyniq.ai_route_step
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_route
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_route', 'id');

ALTER TABLE fyniq.ai_route ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_route FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_route
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_route_step ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_route_step FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_route_step
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.ai_route, fyniq.ai_route_step FROM PUBLIC;
GRANT SELECT ON fyniq.ai_route, fyniq.ai_route_step TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The API sets a route, at once or as its approval executes.
GRANT INSERT (agent, version, classes, preset, approval_id, made_by, made_at) ON fyniq.ai_route TO fyniq_api;
GRANT INSERT (route_id, position, connection_id, model, effort, cache) ON fyniq.ai_route_step TO fyniq_api;

-- A route that widens where an agent's data goes is a request under "AI providers and model routes", which the Owner and
-- a second approver approve.
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
                               ('ai_connection', 'ai_connection.credential'), ('ai_connection', 'ai_connection.enable'),
                               ('ai_data_policy', 'ai_data_policy.change'),
                               ('ai_connection', 'ai_connection.allow_model'),
                               ('ai_connection', 'ai_connection.price'),
                               ('ai_route', 'ai_route.change'))) NOT VALID;
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
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy', 'ai_route')) NOT VALID;
