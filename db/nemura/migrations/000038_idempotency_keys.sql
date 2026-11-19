-- Idempotency keys of writes (PRD-001 §8.18.1, golden rule 4; PRD-002 NM-ARC-03).
--
-- A write sent with an Idempotency-Key runs once for a person: the key is claimed by an INSERT (the primary key lets one of two identical requests in flight win), the first answer is kept for 24 hours and
-- replayed, and the same key with a different request hash is refused. The answer of an operation that may carry pay, a bank account, an NRIC or the text of a question is never stored: only its status
-- and whether it had a body, so a replay of one that returned a body is refused rather than leaked. Nothing here is personal data: the key is the caller's own random string and the hash is of the request.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE nemura.idempotency_key (
  organisation_id uuid        NOT NULL DEFAULT nemura.current_org(),
  principal_id    uuid        NOT NULL,   -- the member who sent the request: a key is the person's own
  key             text        NOT NULL,
  request_hash    bytea       NOT NULL,   -- SHA-256 of the method, the path and query, and the body
  state           text        NOT NULL DEFAULT 'in_flight',
  status          integer,
  content_type    text,
  location        text,
  etag            text,
  body            bytea,                  -- only for an answer that cannot carry pay or restricted data
  had_body        boolean     NOT NULL DEFAULT false,
  body_kept       boolean     NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL,
  expires_at      timestamptz NOT NULL,
  CONSTRAINT idempotency_key_pkey PRIMARY KEY (organisation_id, principal_id, key),
  CONSTRAINT idempotency_key_member_fkey FOREIGN KEY (organisation_id, principal_id) REFERENCES nemura.member (organisation_id, id),
  CONSTRAINT idempotency_key_length CHECK (length(key) BETWEEN 1 AND 255),
  CONSTRAINT idempotency_key_hash CHECK (octet_length(request_hash) = 32),
  CONSTRAINT idempotency_key_state CHECK (state IN ('in_flight', 'done')),
  CONSTRAINT idempotency_key_done_whole CHECK ((state = 'in_flight') = (status IS NULL)),
  CONSTRAINT idempotency_key_body_only_if_kept CHECK (body IS NULL OR (body_kept AND had_body)),
  CONSTRAINT idempotency_key_window CHECK (expires_at = created_at + interval '24 hours')
);
CREATE INDEX idempotency_key_expiry_idx ON nemura.idempotency_key (expires_at);

ALTER TABLE nemura.idempotency_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE nemura.idempotency_key FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON nemura.idempotency_key USING (organisation_id = nemura.current_org()) WITH CHECK (organisation_id = nemura.current_org());

GRANT SELECT, DELETE ON nemura.idempotency_key TO nemura_api;
GRANT INSERT (principal_id, key, request_hash, created_at, expires_at) ON nemura.idempotency_key TO nemura_api;
GRANT UPDATE (state, status, content_type, location, etag, body, had_body, body_kept) ON nemura.idempotency_key TO nemura_api;
