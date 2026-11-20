-- The link's repair, remembered (p1-nemura-fyniq-link design D21; PRD-001 FR-PPL-08, FR-PPL-15): when Fyniq asks Nemura to send a run of sequence numbers again, Nemura answers with the current state at new
-- sequence numbers, so the numbers asked for never reach Fyniq's inbox. Each request is kept (the link, the run, when) so the link's health says "asked for" and not "missing", and asks again only after a day.
-- Append-only; it holds sequence numbers and a time, never a payload.

-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.people_link_replay (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  link_id         uuid        NOT NULL,
  from_sequence   bigint      NOT NULL,
  to_sequence     bigint      NOT NULL,
  asked_at        timestamptz NOT NULL,
  CONSTRAINT people_link_replay_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT people_link_replay_link_fkey FOREIGN KEY (organisation_id, link_id) REFERENCES fyniq.people_link (organisation_id, id),
  CONSTRAINT people_link_replay_range CHECK (from_sequence >= 1 AND to_sequence >= from_sequence AND to_sequence - from_sequence < 10000)
);
CREATE INDEX people_link_replay_link ON fyniq.people_link_replay (organisation_id, link_id, asked_at);
CREATE TRIGGER people_link_replay_guard BEFORE UPDATE OR DELETE ON fyniq.people_link_replay
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER people_link_replay_no_truncate BEFORE TRUNCATE ON fyniq.people_link_replay
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.people_link_replay
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('people_link_replay', 'id');

ALTER TABLE fyniq.people_link_replay ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.people_link_replay FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.people_link_replay
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

REVOKE ALL ON fyniq.people_link_replay FROM PUBLIC;
GRANT SELECT ON fyniq.people_link_replay TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (link_id, from_sequence, to_sequence, asked_at) ON fyniq.people_link_replay TO fyniq_api;
