-- The organisation's API clients (PRD-001 FR-INT-02, SEC-07, AI-34; p1-public-api task 3.1): integration clients, and
-- later developers' personal access tokens, issued by the OAuth server (Ory Hydra, ADR-T3).
--
-- Hydra holds the OAuth client and its tokens; Fyniq holds what the client is to the organisation (ADR-T3: client
-- classes and Connected apps are Fyniq data): its class (AI-34), the member behind it, whose rights it never exceeds
-- (Appendix D, "Outside the matrix"), its narrow scopes, the networks it may call from, its own rate limits, its key's
-- thumbprint and its expiry (SEC-07). The API accepts a client's token only while this record says the client is live:
-- an OAuth client that Fyniq does not know, or one revoked or expired here, gets nothing.
--
-- An integration client is registered by an approval request of the object api_client under Appendix D's "Connected apps"
-- (an Administrator asks, the Owner approves: FR-INT-02's dual control); revoking one is never held back. A client is
-- never removed (FY420): it is revoked, once, and kept for the access review. Only its class (a personal token reclassed
-- by the Owner, FR-INT-02) and its revocation change. The global route lets the API find a client's organisation from the
-- token before any tenant is set, as capture_route does for mail. The approval objects are widened NOT VALID and 000433
-- validates them (squawk: constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.api_client (
  organisation_id   uuid        NOT NULL DEFAULT fyniq.current_org(),
  id                uuid        NOT NULL DEFAULT uuidv7(),
  kind              text        NOT NULL,  -- integration, or personal (a personal access token)
  client_id         text        NOT NULL,  -- the OAuth server's client ID
  name              text        NOT NULL,
  class             text        NOT NULL,  -- AI-34: integration or agent
  owner_id          uuid        NOT NULL,  -- the member behind it: it never does more than they may
  scopes            text[]      NOT NULL,
  ip_allow          text[]      NOT NULL DEFAULT '{}', -- the networks it may call from (CIDR); none: anywhere
  reads_per_minute  integer,               -- its own limits (FR-INT-16); none: the platform's
  writes_per_minute integer,
  key_thumbprint    text,                  -- an integration's: the RFC 7638 thumbprint of the key its assertions are signed with
  expires_at        timestamptz NOT NULL,
  approval_id       uuid,                  -- the approval that registered it
  version           integer     NOT NULL DEFAULT 1,
  created_by        text        NOT NULL,
  created_at        timestamptz NOT NULL,
  revoked_at        timestamptz,
  revoked_by        text,
  revoke_reason     text,
  CONSTRAINT api_client_pkey PRIMARY KEY (organisation_id, id),
  -- A client ID is the organisation's here; api_client_route's key keeps it unique across organisations.
  CONSTRAINT api_client_client_id_key UNIQUE (organisation_id, client_id),
  CONSTRAINT api_client_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT api_client_owner_fkey FOREIGN KEY (organisation_id, owner_id) REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT api_client_kind_known CHECK (kind IN ('integration', 'personal')),
  -- AI-34: an integration client is always of class integration; a personal token is an agent until the Owner reclasses it.
  CONSTRAINT api_client_class_known CHECK (class IN ('integration', 'agent') AND (kind = 'personal' OR class = 'integration')),
  CONSTRAINT api_client_client_id_format CHECK (client_id ~ '^[!-~]{1,255}$'),
  CONSTRAINT api_client_name_format CHECK (length(name) BETWEEN 1 AND 80 AND name = btrim(name)),
  -- Narrow scopes only: fyniq.read.<area> and fyniq.write.<area>; never a wildcard, never approve or release.
  CONSTRAINT api_client_scopes_format CHECK (cardinality(scopes) BETWEEN 1 AND 16
    AND array_to_string(scopes, ' ') ~ '^fyniq\.(read|write)\.[a-z]+( fyniq\.(read|write)\.[a-z]+)*$'),
  CONSTRAINT api_client_ip_allow_format CHECK (cardinality(ip_allow) <= 32 AND ip_allow::cidr[] IS NOT NULL),
  CONSTRAINT api_client_limits_positive CHECK (reads_per_minute BETWEEN 1 AND 600 AND writes_per_minute BETWEEN 1 AND 120),
  CONSTRAINT api_client_key_format CHECK (key_thumbprint ~ '^[A-Za-z0-9_-]{43}$'),
  CONSTRAINT api_client_integration_key CHECK (kind <> 'integration' OR (key_thumbprint IS NOT NULL AND approval_id IS NOT NULL)),
  -- SEC-07: every client expires; a personal token within 90 days (FR-INT-02), an integration client within a year.
  CONSTRAINT api_client_expiry CHECK (expires_at > created_at
    AND expires_at <= created_at + CASE kind WHEN 'personal' THEN interval '90 days' ELSE interval '366 days' END),
  CONSTRAINT api_client_version_positive CHECK (version >= 1),
  CONSTRAINT api_client_by_format CHECK (created_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'
    AND revoked_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT api_client_revoked_together CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revoke_reason IS NULL)),
  CONSTRAINT api_client_reason_format CHECK (length(revoke_reason) BETWEEN 1 AND 500)
);
CREATE INDEX api_client_owner ON fyniq.api_client (organisation_id, owner_id);

-- The global route from a client ID to its organisation, read only by api_client_access before any tenant is set (a
-- global table, as connector_route is: it names the organisation as its owner). Its key makes a client ID unique
-- across organisations.
CREATE TABLE fyniq.api_client_route (
  client_id text NOT NULL,
  owner     uuid NOT NULL, -- the organisation the client is registered in
  id        uuid NOT NULL,
  CONSTRAINT api_client_route_pkey PRIMARY KEY (client_id)
);
REVOKE ALL ON fyniq.api_client_route FROM PUBLIC;

-- A client is registered at its first version, then only revoked, once, or (a personal token) reclassed while live; it is
-- never removed or rewritten (FY420). Its route is written with it.
-- +goose StatementBegin
CREATE FUNCTION fyniq.api_client_guard() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'An API client is kept: revoke it instead' USING ERRCODE = 'FY420',
      HINT = 'Revoke the client; the access review keeps it (SEC-07).';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.version <> 1 OR NEW.revoked_at IS NOT NULL THEN
      RAISE EXCEPTION 'An API client is registered live, at its first version' USING ERRCODE = 'FY420';
    END IF;
    INSERT INTO fyniq.api_client_route (client_id, owner, id) VALUES (NEW.client_id, NEW.organisation_id, NEW.id);
    RETURN NEW;
  END IF;
  IF (NEW.id, NEW.kind, NEW.client_id, NEW.name, NEW.owner_id, NEW.scopes, NEW.ip_allow, NEW.reads_per_minute, NEW.writes_per_minute,
      NEW.key_thumbprint, NEW.expires_at, NEW.approval_id, NEW.created_by, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.kind, OLD.client_id, OLD.name, OLD.owner_id, OLD.scopes, OLD.ip_allow, OLD.reads_per_minute,
      OLD.writes_per_minute, OLD.key_thumbprint, OLD.expires_at, OLD.approval_id, OLD.created_by, OLD.created_at)
     OR NEW.version <> OLD.version + 1 OR OLD.revoked_at IS NOT NULL
     OR (NEW.revoked_at IS NULL AND NEW.class = OLD.class) OR (NEW.revoked_at IS NOT NULL AND NEW.class <> OLD.class) THEN
    RAISE EXCEPTION 'An API client only changes class while live, or is revoked once' USING ERRCODE = 'FY420',
      HINT = 'Revoke the client and register another for any other change (SEC-07).';
  END IF;
  RETURN NEW;
END
$$;

-- What the API needs to accept a client's token: the client, live or not, and the roles its member holds now in its
-- organisation (none when they are no longer an active member). Nothing for a client ID Fyniq does not know.
CREATE FUNCTION fyniq.api_client_access(p_client text, p_now timestamptz,
  OUT organisation_id uuid, OUT id uuid, OUT kind text, OUT class text, OUT owner_id uuid, OUT owner_active boolean, OUT roles text[],
  OUT scopes text[], OUT ip_allow text[], OUT reads_per_minute integer, OUT writes_per_minute integer, OUT expires_at timestamptz,
  OUT revoked boolean)
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r        fyniq.api_client_route;
  c        fyniq.api_client;
  previous text := current_setting('app.org_id', true);
BEGIN
  SELECT * INTO r FROM fyniq.api_client_route x WHERE x.client_id = p_client;
  IF NOT FOUND THEN
    RETURN;
  END IF;
  PERFORM set_config('app.org_id', r.owner::text, true);
  SELECT * INTO c FROM fyniq.api_client a WHERE a.organisation_id = r.owner AND a.id = r.id;
  api_client_access.owner_active := EXISTS (SELECT 1 FROM fyniq.user_membership m
    WHERE m.organisation_id = c.organisation_id AND m.user_id = c.owner_id AND m.status = 'active');
  SELECT coalesce(array_agg(DISTINCT ra.role ORDER BY ra.role), '{}') INTO api_client_access.roles
    FROM fyniq.role_assignment ra
    JOIN fyniq.user_membership m ON m.organisation_id = ra.organisation_id AND m.id = ra.membership_id
   WHERE ra.organisation_id = c.organisation_id AND m.user_id = c.owner_id AND m.status = 'active'
     AND ra.revoked_at IS NULL AND (ra.expires_at IS NULL OR ra.expires_at > p_now);
  PERFORM set_config('app.org_id', coalesce(previous, ''), true);
  api_client_access.organisation_id := c.organisation_id;
  api_client_access.id := c.id;
  api_client_access.kind := c.kind;
  api_client_access.class := c.class;
  api_client_access.owner_id := c.owner_id;
  api_client_access.scopes := c.scopes;
  api_client_access.ip_allow := c.ip_allow;
  api_client_access.reads_per_minute := c.reads_per_minute;
  api_client_access.writes_per_minute := c.writes_per_minute;
  api_client_access.expires_at := c.expires_at;
  api_client_access.revoked := c.revoked_at IS NOT NULL;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER api_client_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.api_client
  FOR EACH ROW EXECUTE FUNCTION fyniq.api_client_guard();
CREATE TRIGGER api_client_no_truncate BEFORE TRUNCATE ON fyniq.api_client
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.api_client
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('api_client', 'id');

ALTER TABLE fyniq.api_client ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.api_client FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.api_client
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.api_client FROM PUBLIC;
REVOKE ALL ON FUNCTION fyniq.api_client_access(text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.api_client_access(text, timestamptz) TO fyniq_api;
GRANT SELECT ON fyniq.api_client TO fyniq_api, fyniq_worker, fyniq_readonly;
-- The API registers a client as its approval executes, and revokes it at once.
GRANT INSERT (id, kind, client_id, name, class, owner_id, scopes, ip_allow, reads_per_minute, writes_per_minute, key_thumbprint, expires_at,
              approval_id, created_by, created_at) ON fyniq.api_client TO fyniq_api;
GRANT UPDATE (class, version, revoked_at, revoked_by, revoke_reason) ON fyniq.api_client TO fyniq_api;

-- Registering an integration client, and reclassing a personal token, are requests under Appendix D's "Connected apps",
-- which the Owner approves with a second approver.
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
                               ('ai_route', 'ai_route.change'),
                               ('ai_proposal', 'ai_proposal.execute'),
                               ('corporate_detail', 'corporate_detail.record'),
                               ('corporate_officer', 'corporate_officer.record'),
                               ('nominee_entry', 'nominee_entry.record'),
                               ('statements_approval', 'statements_approval.record'),
                               ('corporate_document', 'corporate_document.record'),
                               ('api_client', 'api_client.create'), ('api_client', 'api_client.reclass'))) NOT VALID;
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
                  'migration_commit', 'tenant_export', 'ai_connection', 'ai_data_policy', 'ai_route', 'ai_proposal',
                  'corporate_detail', 'corporate_officer', 'nominee_entry', 'statements_approval',
                  'corporate_document', 'api_client')) NOT VALID;
