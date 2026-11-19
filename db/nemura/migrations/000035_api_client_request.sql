-- From the day Nemura is linked to Fyniq, registering one of Nemura's own API clients needs a second approver (PRD-002 NM-ARC-04, rev 0.14; p1-payroll-link design D11): an Owner or
-- Administrator other than the one who asked, with a passkey confirmed in the last five minutes. The request keeps what the client will be (its name, scopes, algorithm and public key: never a
-- private key) until someone decides; an organisation that has never linked, or whose link has ended, keeps the single Owner's registration. The decision is a row of nemura.approval_request.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.api_client_request (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  name            text        NOT NULL,
  scopes          text[]      NOT NULL,
  alg             text        NOT NULL,
  public_key      jsonb       NOT NULL,
  requested_by    uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  CONSTRAINT api_client_request_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT api_client_request_by_fkey FOREIGN KEY (organisation_id, requested_by) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT api_client_request_name CHECK (length(name) BETWEEN 1 AND 100),
  CONSTRAINT api_client_request_no_private CHECK (NOT (public_key ?| ARRAY['d', 'p', 'q', 'dp', 'dq', 'qi', 'k']))
);
CREATE TRIGGER api_client_request_guard BEFORE UPDATE OR DELETE ON nemura.api_client_request FOR EACH ROW EXECUTE FUNCTION nemura.link_append_only();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON nemura.api_client_request DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nemura.require_audit('api_client_request', 'id');

ALTER TABLE nemura.api_client_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.api_client_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.api_client_request USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());
REVOKE ALL ON nemura.api_client_request FROM PUBLIC;
GRANT SELECT ON nemura.api_client_request TO nemura_api, nemura_worker, nemura_readonly;
GRANT INSERT (id, name, scopes, alg, public_key, requested_by, created_at) ON nemura.api_client_request TO nemura_api;
