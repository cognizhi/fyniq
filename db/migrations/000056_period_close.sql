-- The period close (PRD-001 FR-GL-07, FR-GL-08, FM-4, AUD-04, ENG-46; rev 0.50; p1-general-ledger-close task 1.2).
--
-- A close is two acts by two people. Its preparer runs the close checks: period_close_check records their result and
-- the ledger revision they saw. Another person attests and closes: period_close records the attestation, the ledger
-- revision at the close and the snapshot of the trial balance at the period end, as canonical JSON whose SHA-256 the
-- database recomputes. The database refuses a close attested by the one who ran the checks, on checks that are not
-- clean or not of that period (FY153), and neither record ever changes or goes (FY152). The close itself commits only
-- if no entry dated on or before the period end has a revision later than the checks saw: the API checks that under the
-- ledger lock (ENG-47), so a back-dated posting and a close never both win (G4-053).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.period_close_check (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_id       uuid        NOT NULL,
  checked_by      text        NOT NULL,  -- "type:id", as the audit trail names the actor
  checked_at      timestamptz NOT NULL,
  ledger_revision bigint      NOT NULL,  -- the revision the checks saw (ENG-46)
  result          jsonb       NOT NULL,  -- each check: its key, severity and words
  clean           boolean     NOT NULL,  -- no check refused the close (✖)
  CONSTRAINT period_close_check_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_close_check_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT period_close_check_actor_format CHECK (checked_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT period_close_check_revision CHECK (ledger_revision >= 0),
  CONSTRAINT period_close_check_result_array CHECK (jsonb_typeof(result) = 'array' AND octet_length(result::text) <= 65536)
);
CREATE INDEX period_close_check_period ON fyniq.period_close_check (organisation_id, period_id, checked_at);

CREATE TABLE fyniq.period_close (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_id       uuid        NOT NULL,
  check_id        uuid        NOT NULL,
  attested_by     text        NOT NULL,
  attested_at     timestamptz NOT NULL,
  statement       text        NOT NULL,
  closed_revision bigint      NOT NULL,  -- the ledger revision at the close (AUD-04)
  snapshot        text        NOT NULL,  -- the trial balance at the period end, canonical JSON (RFC 8785)
  snapshot_hash   bytea       NOT NULL,
  CONSTRAINT period_close_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT period_close_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT period_close_check_fkey FOREIGN KEY (organisation_id, check_id) REFERENCES fyniq.period_close_check (organisation_id, id),
  CONSTRAINT period_close_actor_format CHECK (attested_by ~ '^[a-z][a-z_]{0,19}:[!-~]{1,200}$'),
  CONSTRAINT period_close_statement_length CHECK (length(btrim(statement)) BETWEEN 1 AND 1000),
  CONSTRAINT period_close_revision CHECK (closed_revision >= 0),
  CONSTRAINT period_close_snapshot_hash CHECK (snapshot_hash = sha256(convert_to(snapshot, 'UTF8')))
);
CREATE INDEX period_close_period ON fyniq.period_close (organisation_id, period_id, attested_at);

-- Neither record changes or goes; a close is attested by someone other than the one who ran its checks, on clean
-- checks of the same period, after them.
-- +goose StatementBegin
CREATE FUNCTION fyniq.period_close_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  c fyniq.period_close_check;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'A record of the close never changes' USING ERRCODE = 'FY152', HINT = 'Run the checks again, or reopen the period.';
  END IF;
  IF TG_TABLE_NAME = 'period_close' THEN
    SELECT * INTO c FROM fyniq.period_close_check x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.check_id;
    IF NOT c.clean OR c.period_id <> NEW.period_id OR c.checked_by = NEW.attested_by OR NEW.attested_at < c.checked_at THEN
      RAISE EXCEPTION 'A period is closed on clean checks of it, attested by someone other than the one who ran them'
        USING ERRCODE = 'FY153', HINT = 'Ask another founder to attest the close (FM-4).';
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER period_close_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.period_close_check
  FOR EACH ROW EXECUTE FUNCTION fyniq.period_close_guard();
CREATE TRIGGER period_close_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.period_close
  FOR EACH ROW EXECUTE FUNCTION fyniq.period_close_guard();
CREATE TRIGGER period_close_check_no_truncate BEFORE TRUNCATE ON fyniq.period_close_check
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER period_close_no_truncate BEFORE TRUNCATE ON fyniq.period_close
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.period_close_check ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period_close_check FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period_close_check
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.period_close ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.period_close FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.period_close
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.period_close_check
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('period_close_check', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.period_close
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('period_close', 'id');

GRANT SELECT ON fyniq.period_close_check, fyniq.period_close TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (period_id, checked_by, checked_at, ledger_revision, result, clean) ON fyniq.period_close_check TO fyniq_api;
GRANT INSERT (period_id, check_id, attested_by, attested_at, statement, closed_revision, snapshot, snapshot_hash)
  ON fyniq.period_close TO fyniq_api;

-- The close checks compare the balances with the lines at the close (INV-08), as the nightly verification does: the
-- function only reads what the API's role reads already, and changes nothing.
GRANT EXECUTE ON FUNCTION fyniq.balance_differences() TO fyniq_api;
