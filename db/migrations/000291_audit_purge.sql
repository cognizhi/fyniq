-- The audit trail's purge (PRD-001 AUD-09, open question 38, the owner's decision of rev 0.230; p1-audit-explorer
-- slice 7, design D7a).
--
-- With retention.purge_after_retention switched on (off by default, dual control), the purge job removes the leading
-- run of events whose records' retention has passed. A boundary record comes first: the last purged event's sequence
-- number and hash, when it occurred, the setting change that switched the purge on, and the counts. It is also signed
-- and written once to the anchors' bucket. Verification then starts from the latest boundary.
--
-- audit_purge is the only way an event is deleted. It deletes up to a boundary at exactly that sequence, only while the
-- switch is on, and never an event less than 7 years old by the database's own clock (the shortest records' retention,
-- §11.9). It lifts the immutability trigger for its own statements by a transaction-local flag; no role holds DELETE.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.audit_boundary (
  organisation_id  uuid        NOT NULL DEFAULT fyniq.current_org(),
  id               uuid        NOT NULL DEFAULT uuidv7(),
  last_seq         bigint      NOT NULL,
  last_hash        bytea       NOT NULL,
  last_occurred_at timestamptz NOT NULL,  -- when the last purged event occurred
  approval         text        NOT NULL,  -- the setting change that switched the purge on
  counts           jsonb       NOT NULL,  -- how many events, and of them security events
  anchor_key       text        NOT NULL,  -- where its signed copy is in the anchors' bucket
  written_at       timestamptz NOT NULL,
  CONSTRAINT audit_boundary_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT audit_boundary_seq_key UNIQUE (organisation_id, last_seq),
  CONSTRAINT audit_boundary_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT audit_boundary_seq_positive CHECK (last_seq > 0),
  CONSTRAINT audit_boundary_hash_sha256 CHECK (length(last_hash) = 32),
  CONSTRAINT audit_boundary_counts_object CHECK (jsonb_typeof(counts) = 'object'),
  CONSTRAINT audit_boundary_text_lengths CHECK (length(approval) BETWEEN 1 AND 200 AND length(anchor_key) BETWEEN 1 AND 300)
);

-- A boundary names the event it ends at by its hash, moves forward only, and never changes.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_boundary_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A purge boundary never changes' USING ERRCODE = 'FY304';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.audit_event e
                  WHERE e.organisation_id = NEW.organisation_id AND e.seq = NEW.last_seq AND e.hash = NEW.last_hash) THEN
    RAISE EXCEPTION 'A purge boundary names an event of the trail by its sequence number and hash' USING ERRCODE = 'FY304';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.audit_boundary b WHERE b.organisation_id = NEW.organisation_id AND b.last_seq >= NEW.last_seq) THEN
    RAISE EXCEPTION 'A purge boundary moves forward only' USING ERRCODE = 'FY304';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_boundary_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.audit_boundary
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_boundary_guard();
CREATE TRIGGER audit_boundary_no_truncate BEFORE TRUNCATE ON fyniq.audit_boundary
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.audit_immutable();

-- The purge after retention's switch for an organisation, as in effect today: off unless a change switched it on.
-- +goose StatementBegin
CREATE FUNCTION fyniq.purge_switched_on(p_org uuid) RETURNS boolean
LANGUAGE sql STABLE
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
  SELECT coalesce((SELECT s.value FROM fyniq.organisation_setting s
                    WHERE s.organisation_id = p_org AND s.key = 'retention.purge_after_retention' AND s.effective_from <= current_date
                    ORDER BY s.effective_from DESC, s.version DESC LIMIT 1), 'false') = 'true'
$$;
-- +goose StatementEnd

-- An event is deleted only behind a boundary, while the purge is switched on, once it is at least 7 years old, and
-- under the purge's flag; it is never updated (AUD-08). Even the table's owner deletes nothing else.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_event_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('fyniq.audit_purging', true) = 'on'
     AND OLD.seq <= (SELECT max(b.last_seq) FROM fyniq.audit_boundary b WHERE b.organisation_id = OLD.organisation_id)
     AND fyniq.purge_switched_on(OLD.organisation_id)
     AND (OLD.body::jsonb ->> 'occurred_at')::timestamptz < now() - interval '7 years' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'The audit trail cannot be changed or deleted (% on %)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'FY060', HINT = 'Audit events are append-only (AUD-08); only the purge after retention removes them (AUD-09).';
END
$$;
-- +goose StatementEnd
DROP TRIGGER audit_event_immutable ON fyniq.audit_event;
CREATE TRIGGER audit_event_immutable BEFORE UPDATE OR DELETE ON fyniq.audit_event
  FOR EACH ROW EXECUTE FUNCTION fyniq.audit_event_guard();

-- The purge: the transaction's organisation's events up to p_last_seq, behind its boundary, with their details.
-- +goose StatementBegin
CREATE FUNCTION fyniq.audit_purge(p_last_seq bigint) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  org uuid := fyniq.require_org();
  n   bigint;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM fyniq.audit_boundary b WHERE b.organisation_id = org AND b.last_seq = p_last_seq) THEN
    RAISE EXCEPTION 'No purge boundary ends at event %', p_last_seq USING ERRCODE = 'FY304',
      HINT = 'Write the signed boundary record first (AUD-09).';
  END IF;
  IF NOT fyniq.purge_switched_on(org) THEN
    RAISE EXCEPTION 'The purge after retention is switched off' USING ERRCODE = 'FY304',
      HINT = 'It is switched on under dual control (retention.purge_after_retention).';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.audit_event e
              WHERE e.organisation_id = org AND e.seq <= p_last_seq
                AND (e.body::jsonb ->> 'occurred_at')::timestamptz >= now() - interval '7 years') THEN
    RAISE EXCEPTION 'An event up to % is less than 7 years old', p_last_seq USING ERRCODE = 'FY304',
      HINT = 'Records are kept at least 7 years after their financial year (§11.9).';
  END IF;
  PERFORM set_config('fyniq.audit_purging', 'on', true);
  PERFORM set_config('fyniq.audit_erasing', 'on', true);
  DELETE FROM fyniq.audit_request_detail d
   WHERE d.organisation_id = org AND d.id IN (SELECT e.id FROM fyniq.audit_event e WHERE e.organisation_id = org AND e.seq <= p_last_seq);
  DELETE FROM fyniq.audit_event e WHERE e.organisation_id = org AND e.seq <= p_last_seq;
  GET DIAGNOSTICS n = ROW_COUNT;
  PERFORM set_config('fyniq.audit_erasing', 'off', true);
  PERFORM set_config('fyniq.audit_purging', 'off', true);
  RETURN n;
END
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION fyniq.audit_purge(bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fyniq.audit_purge(bigint) TO fyniq_worker;

ALTER TABLE fyniq.audit_boundary ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.audit_boundary FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.audit_boundary
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

GRANT INSERT (last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at) ON fyniq.audit_boundary TO fyniq_worker;
GRANT SELECT ON fyniq.audit_boundary TO fyniq_api, fyniq_worker, fyniq_readonly;
