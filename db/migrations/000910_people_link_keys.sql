-- Fyniq's half of the link's credentials (PRD-001 FR-PPL-15, FR-PPL-09; PRD-002 NM-INT-10; p1-payroll-link design D1; p1-nemura-fyniq-link design D9, D10). Nemura holds Fyniq's public keys and Fyniq
-- holds Nemura's (people_link.link_key), and each side signs what it sends with a key of its own, so Fyniq needs two:
--
--  * an Ed25519 envelope key, which signs every event Fyniq sends (payroll.run.posted, payroll.payments.status) with the link's epoch;
--  * an ES256 client key, whose private half signs the private_key_jwt assertion that gets Fyniq's token from Nemura's own OAuth server (the client Nemura registered for Fyniq under the ID it chose
--    when it proposed the link).
--
-- A stolen token cannot sign an event and a stolen envelope key has no token. The private halves are written once by the API (when the link is approved) and read by the worker role alone; the public halves
-- and the running sequence number of what Fyniq sent are what the API may read. Sealing the private halves under the platform's key is [live-verify].

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_link_key (
  organisation_id  uuid   NOT NULL DEFAULT fyniq.current_org(),
  link_id          uuid   NOT NULL,
  nemura_client_id text   NOT NULL,           -- the ID Fyniq's client has at Nemura's OAuth server: Nemura chose it when it proposed the link
  link_public      bytea  NOT NULL,           -- Fyniq's Ed25519 public key, given to Nemura for what Fyniq sends
  client_public    jsonb  NOT NULL,           -- the public half of Fyniq's client key, as a JWK: Nemura registers it for Fyniq's client
  sequence         bigint NOT NULL DEFAULT 0, -- the last sequence number Fyniq sent on this link: for gap repair and the reconciliation checkpoint
  CONSTRAINT people_link_key_pkey PRIMARY KEY (organisation_id, link_id),
  CONSTRAINT people_link_key_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT people_link_key_public_size CHECK (octet_length(link_public) = 32),
  CONSTRAINT people_link_key_client_id CHECK (char_length(nemura_client_id) BETWEEN 1 AND 200),
  CONSTRAINT people_link_key_no_private CHECK (NOT (client_public ?| ARRAY['d', 'p', 'q', 'dp', 'dq', 'qi', 'k'])),
  CONSTRAINT people_link_key_sequence CHECK (sequence >= 0)
);

CREATE TABLE fyniq.people_link_secret (
  organisation_id uuid  NOT NULL DEFAULT fyniq.current_org(),
  link_id         uuid  NOT NULL,
  link_private    bytea NOT NULL,   -- the Ed25519 private key: signs at send time
  client_private  bytea NOT NULL,   -- the ES256 private key, PKCS #8 in DER: signs the token assertion
  CONSTRAINT people_link_secret_pkey PRIMARY KEY (organisation_id, link_id),
  CONSTRAINT people_link_secret_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT people_link_secret_sizes CHECK (octet_length(link_private) = 64 AND octet_length(client_private) BETWEEN 32 AND 400)
);

-- The keys never change once made; the sequence only goes up, one at a time.
-- +goose StatementBegin
CREATE FUNCTION fyniq.people_link_key_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'A link''s keys are kept: unlink instead' USING ERRCODE = 'FY900',
      HINT = 'Unlinking ends the link and its client; its epoch and keys are never used again (FR-PPL-16).';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF (NEW.link_id, NEW.nemura_client_id, NEW.link_public, NEW.client_public) IS DISTINCT FROM (OLD.link_id, OLD.nemura_client_id, OLD.link_public, OLD.client_public)
       OR NEW.sequence <> OLD.sequence + 1 THEN
      RAISE EXCEPTION 'A link''s keys do not change, and its sequence goes up by one' USING ERRCODE = 'FY900',
        HINT = 'Rotation is a new link at the next epoch (FR-PPL-16).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER people_link_key_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_key
  FOR EACH ROW EXECUTE FUNCTION fyniq.people_link_key_guard();
CREATE TRIGGER people_link_key_no_truncate BEFORE TRUNCATE ON fyniq.people_link_key
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE TRIGGER people_link_secret_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_secret
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER people_link_secret_no_truncate BEFORE TRUNCATE ON fyniq.people_link_secret
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.people_link_key
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_link_key', 'link_id');

ALTER TABLE fyniq.people_link_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_key
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.people_link_secret ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_secret FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_secret
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_link_key, fyniq.people_link_secret FROM PUBLIC;
GRANT SELECT ON fyniq.people_link_key TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (link_id, nemura_client_id, link_public, client_public) ON fyniq.people_link_key TO fyniq_api;
GRANT UPDATE (sequence) ON fyniq.people_link_key TO fyniq_api;
GRANT INSERT (link_id, link_private, client_private) ON fyniq.people_link_secret TO fyniq_api;
GRANT SELECT ON fyniq.people_link_secret TO fyniq_worker;
