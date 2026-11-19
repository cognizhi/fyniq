-- The key for the ask at release (PRD-001 FR-PPL-13; p1-payroll-link task 6.3, design D10; decided 20 Nov 2026, owner to confirm). When Fyniq releases a payment to a person it asks Nemura, the hold's
-- authority, whether the person is held now, within five seconds, and pays no one it gets no answer for. The API makes that call, and the API must not hold the keys that send the link's events (the worker's
-- alone, people_link_secret), so the ask has a client and a key of its own: an ES256 key whose public half Nemura registers for a client with the one scope link.holds.read, which reads a hold's status and
-- writes nothing. The private half is readable by the API and no other role. Sealing it under the platform's key is [live-verify].

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_link_ask_key (
  organisation_id      uuid  NOT NULL DEFAULT fyniq.current_org(),
  id                   uuid  NOT NULL DEFAULT uuidv7(),
  link_id              uuid  NOT NULL,
  nemura_ask_client_id text  NOT NULL,   -- the ID the asking client has at Nemura's OAuth server: Nemura chose it when it proposed the link
  ask_public           jsonb NOT NULL,   -- the public half as a JWK, which Nemura registers
  ask_private          bytea NOT NULL,   -- the ES256 private key, PKCS #8 in DER: signs the token assertion for link.holds.read
  CONSTRAINT people_link_ask_key_pkey PRIMARY KEY (organisation_id, link_id),
  CONSTRAINT people_link_ask_key_id_key UNIQUE (organisation_id, id),
  CONSTRAINT people_link_ask_key_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT people_link_ask_key_client_id CHECK (char_length(nemura_ask_client_id) BETWEEN 1 AND 200),
  CONSTRAINT people_link_ask_key_no_private CHECK (NOT (ask_public ?| ARRAY['d', 'p', 'q', 'dp', 'dq', 'qi', 'k'])),
  CONSTRAINT people_link_ask_key_size CHECK (octet_length(ask_private) BETWEEN 32 AND 400)
);
CREATE TRIGGER people_link_ask_key_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_ask_key
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER people_link_ask_key_no_truncate BEFORE TRUNCATE ON fyniq.people_link_ask_key
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.people_link_ask_key
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_link_ask_key', 'link_id');

ALTER TABLE fyniq.people_link_ask_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_ask_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_ask_key
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_link_ask_key FROM PUBLIC;
GRANT SELECT ON fyniq.people_link_ask_key TO fyniq_api;
GRANT INSERT (link_id, nemura_ask_client_id, ask_public, ask_private) ON fyniq.people_link_ask_key TO fyniq_api;
