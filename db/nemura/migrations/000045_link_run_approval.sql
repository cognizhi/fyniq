-- The proof a pay run carries to Fyniq while the two apps are linked (PRD-002 NM-INT-03, 06, 07; p1-nemura-fyniq-link task 3.1, p1-payroll-link design D3, D14).
--
--  * link_run_approval keeps, once and for ever, what the approver signed: the approval envelope's canonical bytes over the run's figures, its SHA-256 (the passkey's challenge), the mapping version it was
--    calculated against, the preparer and the approver as issuer and subject, and the approver's passkey assertion over that digest. The submission to Fyniq is built from these bytes and never from the run
--    again, so what Fyniq posts is what was approved. It is written by the API in the transaction of the approval and read by the worker that sends it.
--  * member_identity gives the sign-in identity (issuer and subject) of a member of the current organisation: the identity the shared sign-in knows them by, which Fyniq matches their passkey to (NM-INT-07,
--    NM-INT-12). The directory itself stays unreadable to the API; this is the one narrow read, for the transaction's own organisation, and it never returns an email.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.link_run_approval (
  organisation_id    uuid        NOT NULL DEFAULT nemura.current_org(),
  id                 uuid        NOT NULL DEFAULT uuidv7(),
  run_id             uuid        NOT NULL,
  version            integer     NOT NULL,
  envelope           bytea       NOT NULL,   -- the canonical bytes the approver's passkey signed the digest of
  envelope_sha256    text        NOT NULL,
  mapping_version    integer     NOT NULL,
  approver_member_id uuid        NOT NULL,
  approver_issuer    text        NOT NULL,
  approver_subject   text        NOT NULL,
  preparer_issuer    text        NOT NULL,
  preparer_subject   text        NOT NULL,
  authenticator_data bytea       NOT NULL,
  client_data        bytea       NOT NULL,
  signature          bytea       NOT NULL,
  approved_at        timestamptz NOT NULL,
  CONSTRAINT link_run_approval_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT link_run_approval_once UNIQUE (organisation_id, run_id, version),
  CONSTRAINT link_run_approval_run_fkey FOREIGN KEY (organisation_id, run_id) REFERENCES nemura.pay_run (organisation_id, id),
  CONSTRAINT link_run_approval_approver_fkey FOREIGN KEY (organisation_id, approver_member_id) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT link_run_approval_digest CHECK (envelope_sha256 ~ '^[0-9a-f]{64}$'),
  CONSTRAINT link_run_approval_version CHECK (version >= 1 AND mapping_version >= 1),
  CONSTRAINT link_run_approval_not_self CHECK ((approver_issuer, approver_subject) <> (preparer_issuer, preparer_subject))
);
CREATE TRIGGER link_run_approval_guard BEFORE UPDATE OR DELETE ON nemura.link_run_approval FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.link_run_approval DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('link_run_approval', 'id');

ALTER TABLE nemura.link_run_approval ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.link_run_approval FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.link_run_approval USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.link_run_approval FROM PUBLIC;
GRANT SELECT ON nemura.link_run_approval TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (run_id, version, envelope, envelope_sha256, mapping_version, approver_member_id, approver_issuer, approver_subject, preparer_issuer, preparer_subject,
              authenticator_data, client_data, signature, approved_at) ON nemura.link_run_approval TO nemura_api;

-- +goose StatementBegin
CREATE FUNCTION nemura.member_identity(p_member uuid) RETURNS TABLE (issuer text, subject text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, nemura, pg_temp
AS $$
  SELECT d.issuer, d.subject
    FROM nemura.identity_directory d
   WHERE d.organisation_id = nemura.require_org() AND d.member_id = p_member AND d.active
   ORDER BY d.linked_at, d.issuer, d.subject
   LIMIT 1
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION nemura.member_identity(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION nemura.member_identity(uuid) TO nemura_api;
