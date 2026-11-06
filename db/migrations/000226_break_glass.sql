-- Break-glass for a single available founder (PRD-001 FM-8; rev 0.175, the owner's decisions of 6 Nov 2026;
-- p1-approval-engine task 3.3).
--
-- In Founder mode an Owner starts a break-glass, with a reason, for 24 hours, capped by the organisation's break-glass
-- payment cap as it stood then. While it is active, that Owner releases a payment batch still waiting for approval
-- without a second person: a use of the break-glass approves it instead. Each break-glass is logged for review when it
-- starts; its review comes after it ends, and a new one waits until the last is reviewed. The database is the backstop:
--   - a break-glass starts in Founder mode, by an active Owner, for 24 hours exactly, never while an earlier one waits
--     for its review, and with its review log entry in the same transaction (FY261);
--   - it never changes but to end once, inside its window, and never goes (FY262);
--   - a use is of a draft batch at its current version, by the break-glass's Owner, inside its window, in its currency
--     and within its cap, once per batch, and never changes or goes (FY263);
--   - a batch is approved under an approved request that neither its requester nor its preparer signed, or under a use
--     of a break-glass at its current version (FY242);
--   - a release names its approval or its break-glass, never both; one by break-glass is the use's, by its Owner, at its
--     time and for its total (FY239);
--   - a break-glass's review log entry is reviewed only after the break-glass ends (FY151), by someone other than its
--     Owner, as every entry is.
-- The widened checks and the new foreign keys are added NOT VALID and validated by 000227 (squawk:
-- constraint-missing-not-valid).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.break_glass (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  started_by      uuid           NOT NULL,
  reason          text           NOT NULL,
  started_at      timestamptz    NOT NULL,
  expires_at      timestamptz    NOT NULL,
  ended_at        timestamptz,             -- ended sooner by an Owner, once
  ended_by        uuid,
  currency        text           NOT NULL, -- the base currency: what the cap and the uses are counted in
  minor_units     smallint       NOT NULL,
  cap             numeric(20, 4) NOT NULL, -- the break-glass payment cap when it started
  version         integer        NOT NULL DEFAULT 1,
  CONSTRAINT break_glass_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT break_glass_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT break_glass_started_by_fkey FOREIGN KEY (started_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT break_glass_ended_by_fkey FOREIGN KEY (ended_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT break_glass_reason_length CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
  CONSTRAINT break_glass_window CHECK (expires_at = started_at + interval '24 hours'),
  CONSTRAINT break_glass_ended CHECK ((ended_at IS NULL) = (ended_by IS NULL)
                                      AND (ended_at IS NULL OR (ended_at >= started_at AND ended_at <= expires_at))),
  CONSTRAINT break_glass_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT break_glass_minor_units CHECK (minor_units BETWEEN 0 AND 4),
  CONSTRAINT break_glass_cap_scale CHECK (cap <> 'NaN' AND cap = trunc(cap, minor_units) AND cap >= 0),
  CONSTRAINT break_glass_version_positive CHECK (version > 0)
);
CREATE INDEX break_glass_started ON fyniq.break_glass (organisation_id, started_at);

CREATE TABLE fyniq.break_glass_use (
  organisation_id uuid           NOT NULL DEFAULT fyniq.current_org(),
  id              uuid           NOT NULL DEFAULT uuidv7(),
  break_glass_id  uuid           NOT NULL,
  batch_id        uuid           NOT NULL,
  batch_version   integer        NOT NULL,
  used_by         uuid           NOT NULL,
  used_at         timestamptz    NOT NULL,
  currency        text           NOT NULL,
  minor_units     smallint       NOT NULL,
  total           numeric(20, 4) NOT NULL,
  CONSTRAINT break_glass_use_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT break_glass_use_batch_once UNIQUE (organisation_id, batch_id),
  CONSTRAINT break_glass_use_break_glass_fkey FOREIGN KEY (organisation_id, break_glass_id)
    REFERENCES fyniq.break_glass (organisation_id, id),
  CONSTRAINT break_glass_use_version_fkey FOREIGN KEY (organisation_id, batch_id, batch_version)
    REFERENCES fyniq.document_version (organisation_id, document_id, version),
  CONSTRAINT break_glass_use_by_fkey FOREIGN KEY (used_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT break_glass_use_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
  CONSTRAINT break_glass_use_minor_units CHECK (minor_units BETWEEN 0 AND 4),
  CONSTRAINT break_glass_use_total_scale CHECK (total <> 'NaN' AND total = trunc(total, minor_units) AND total > 0)
);
CREATE INDEX break_glass_use_break_glass ON fyniq.break_glass_use (organisation_id, break_glass_id);

-- +goose StatementBegin
-- A break-glass starts in Founder mode, by an active Owner, never while an earlier one waits for its review; it never
-- changes but to end once, inside its window, and never goes (FY261, FY262; FM-8).
CREATE FUNCTION fyniq.break_glass_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  mode text;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Break-glass is never deleted' USING ERRCODE = 'FY262';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Break-glass % is never deleted', OLD.id USING ERRCODE = 'FY262';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR NEW.version <> OLD.version + 1
       OR (NEW.organisation_id, NEW.id, NEW.started_by, NEW.reason, NEW.started_at, NEW.expires_at, NEW.currency, NEW.minor_units, NEW.cap)
          IS DISTINCT FROM
          (OLD.organisation_id, OLD.id, OLD.started_by, OLD.reason, OLD.started_at, OLD.expires_at, OLD.currency, OLD.minor_units, OLD.cap) THEN
      RAISE EXCEPTION 'Break-glass % only ends, once', OLD.id USING ERRCODE = 'FY262',
        HINT = 'It cannot be extended or renewed: start another once this one is reviewed (FM-8).';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.cap = 'NaN' THEN -- as break_glass_cap_scale says, before the guard looks further (§11.3)
    RAISE EXCEPTION 'The break-glass''s cap is not a number' USING ERRCODE = 'check_violation';
  END IF;
  -- One start at a time per organisation, so two never both find no break-glass waiting for review.
  PERFORM pg_advisory_xact_lock(hashtextextended('fyniq.break_glass:' || NEW.organisation_id::text, 0));
  SELECT m.mode INTO mode FROM fyniq.team_mode m WHERE m.organisation_id = NEW.organisation_id ORDER BY m.version DESC LIMIT 1;
  IF coalesce(mode, 'founder') <> 'founder' THEN
    RAISE EXCEPTION 'Break-glass is for Founder mode only' USING ERRCODE = 'FY261',
      HINT = 'In Standard and Enterprise modes another person approves each payment batch (FM-8).';
  END IF;
  IF NEW.ended_at IS NOT NULL THEN
    RAISE EXCEPTION 'A break-glass starts active' USING ERRCODE = 'FY261';
  END IF;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.role_assignment ra
         JOIN fyniq.user_membership m ON m.organisation_id = ra.organisation_id AND m.id = ra.membership_id
        WHERE ra.organisation_id = NEW.organisation_id AND m.user_id = NEW.started_by AND m.status = 'active'
          AND ra.role = 'owner' AND ra.revoked_at IS NULL AND (ra.expires_at IS NULL OR ra.expires_at > NEW.started_at)) THEN
    RAISE EXCEPTION 'Only an Owner starts break-glass' USING ERRCODE = 'FY261', HINT = 'Ask an Owner of the organisation (FM-8).';
  END IF;
  IF EXISTS (
       SELECT 1 FROM fyniq.break_glass b
        WHERE b.organisation_id = NEW.organisation_id AND b.id <> NEW.id
          AND NOT EXISTS (
                SELECT 1 FROM fyniq.review_log_entry e
                  JOIN fyniq.review_log_review r ON r.organisation_id = e.organisation_id AND r.entry_id = e.id
                 WHERE e.organisation_id = b.organisation_id AND e.kind = 'break_glass' AND e.object_key = b.id::text)) THEN
    RAISE EXCEPTION 'An earlier break-glass waits for its review' USING ERRCODE = 'FY261',
      HINT = 'Another Owner reviews it after it ends, or the Accountant once its review is due; then start another (FM-8).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER break_glass_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.break_glass
  FOR EACH ROW EXECUTE FUNCTION fyniq.break_glass_guard();
CREATE TRIGGER break_glass_no_truncate BEFORE TRUNCATE ON fyniq.break_glass
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.break_glass_guard();

-- +goose StatementBegin
-- A break-glass is logged for review as it starts, in the same transaction: its entry names it and its Owner (FY261).
CREATE FUNCTION fyniq.break_glass_logged() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.review_log_entry e
        WHERE e.organisation_id = NEW.organisation_id AND e.kind = 'break_glass' AND e.object_type = 'break_glass'
          AND e.object_key = NEW.id::text AND e.actor = NEW.started_by AND e.logged_at = NEW.started_at) THEN
    RAISE EXCEPTION 'Break-glass % is logged for review as it starts', NEW.id USING ERRCODE = 'FY261',
      HINT = 'Log it in the review log in the same transaction (FM-3, FM-8).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER break_glass_logged AFTER INSERT ON fyniq.break_glass
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.break_glass_logged();

-- +goose StatementBegin
-- A use approves a draft batch at its current version, by the break-glass's Owner, inside its window, in its currency
-- and within its cap; it never changes or goes (FY263; FM-8).
CREATE FUNCTION fyniq.break_glass_use_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  b    fyniq.break_glass;
  used numeric;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The uses of break-glass are never deleted' USING ERRCODE = 'FY263';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The use of break-glass for batch % is never changed or deleted', OLD.batch_id USING ERRCODE = 'FY263';
  END IF;
  IF NEW.total = 'NaN' THEN -- as break_glass_use_total_scale says, before the guard sums it (§11.3)
    RAISE EXCEPTION 'The use''s total is not a number' USING ERRCODE = 'check_violation';
  END IF;
  -- The break-glass's row, locked: its uses and its end are decided one at a time.
  SELECT * INTO b FROM fyniq.break_glass x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.break_glass_id FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'Break-glass % does not exist', NEW.break_glass_id USING ERRCODE = 'FY263';
  END IF;
  IF NEW.used_by <> b.started_by THEN
    RAISE EXCEPTION 'Only the Owner who started a break-glass uses it' USING ERRCODE = 'FY263',
      HINT = 'Start your own once it is reviewed, or ask for the batch''s approval (FM-8).';
  END IF;
  IF NEW.used_at < b.started_at OR NEW.used_at >= coalesce(b.ended_at, b.expires_at) THEN
    RAISE EXCEPTION 'Break-glass % is not active', b.id USING ERRCODE = 'FY263',
      HINT = 'It lasts 24 hours, or until an Owner ends it: ask for the batch''s approval instead (FM-8).';
  END IF;
  IF NEW.currency <> b.currency OR NEW.minor_units <> b.minor_units THEN
    RAISE EXCEPTION 'A use is counted in the break-glass''s currency, %', b.currency USING ERRCODE = 'FY263';
  END IF;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.document d
        WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.batch_id AND d.type = 'payment_batch'
          AND d.status = 'draft' AND d.current_version = NEW.batch_version) THEN
    RAISE EXCEPTION 'Break-glass approves a batch waiting for approval, at its current version' USING ERRCODE = 'FY263';
  END IF;
  SELECT coalesce(sum(u.total), 0) INTO used FROM fyniq.break_glass_use u
   WHERE u.organisation_id = NEW.organisation_id AND u.break_glass_id = NEW.break_glass_id;
  IF used + NEW.total > b.cap THEN
    RAISE EXCEPTION 'The batch takes break-glass % above its cap of % %', b.id, b.cap, b.currency USING ERRCODE = 'FY263',
      HINT = 'Release less under break-glass, or ask for the batch''s approval (FM-8).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER break_glass_use_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.break_glass_use
  FOR EACH ROW EXECUTE FUNCTION fyniq.break_glass_use_guard();
CREATE TRIGGER break_glass_use_no_truncate BEFORE TRUNCATE ON fyniq.break_glass_use
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.break_glass_use_guard();

-- +goose StatementBegin
-- payment_batch_approved as in 000185, and: a use of a break-glass at the batch's current version approves it too (FY242;
-- FR-BNK-12, FM-1, FM-8).
CREATE OR REPLACE FUNCTION fyniq.payment_batch_approved() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  preparer text;
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.status = 'posted' THEN
    RETURN NULL;
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.break_glass_use u
              WHERE u.organisation_id = NEW.organisation_id AND u.batch_id = NEW.id AND u.batch_version = NEW.current_version) THEN
    RETURN NULL;
  END IF;
  SELECT v.created_by INTO preparer FROM fyniq.document_version v
   WHERE v.organisation_id = NEW.organisation_id AND v.document_id = NEW.id AND v.version = NEW.current_version;
  IF NOT EXISTS (
       SELECT 1 FROM fyniq.approval_request r
        WHERE r.organisation_id = NEW.organisation_id AND r.object_type = 'payment_batch' AND r.operation = 'payment_batch.approve'
          AND r.object_key = NEW.id::text AND r.status = 'approved'
          AND NOT EXISTS (SELECT 1 FROM fyniq.approval_signature s
                           WHERE s.organisation_id = r.organisation_id AND s.request_id = r.id
                             AND (s.signer = r.requested_by OR 'user:' || s.signer::text = preparer))) THEN
    RAISE EXCEPTION 'Batch % is approved only by a second person', NEW.id USING ERRCODE = 'FY242',
      HINT = 'Ask for approval: someone who did not prepare or ask for it approves it (FR-BNK-12, FM-1).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

-- Its only reader is the adapter, which reads it as optional in the same change, and payment_batch_release_approved_once
-- still requires exactly one of the approval and the break-glass.
-- squawk-ignore ban-drop-not-null
ALTER TABLE fyniq.payment_batch_release ALTER COLUMN approval_id DROP NOT NULL;
ALTER TABLE fyniq.payment_batch_release ADD COLUMN break_glass_id uuid;  -- the break-glass it was released under (FM-8)
ALTER TABLE fyniq.payment_batch_release ADD CONSTRAINT payment_batch_release_break_glass_fkey
  FOREIGN KEY (organisation_id, break_glass_id) REFERENCES fyniq.break_glass (organisation_id, id) NOT VALID;
ALTER TABLE fyniq.payment_batch_release ADD CONSTRAINT payment_batch_release_approved_once
  CHECK (num_nonnulls(approval_id, break_glass_id) = 1) NOT VALID;

-- +goose StatementBegin
-- payment_batch_release_guard as in 000185, and: a release under break-glass is the use's, by its Owner, at its time
-- and for its total (FY239; FM-8).
CREATE OR REPLACE FUNCTION fyniq.payment_batch_release_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Releases of payment batches are never deleted' USING ERRCODE = 'FY239';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The release of batch % is never changed or deleted', OLD.batch_id USING ERRCODE = 'FY239',
      HINT = 'A payment the bank rejects is voided on its own; the release stays as it was (FR-BNK-12).';
  END IF;
  IF NEW.total = 'NaN' THEN -- as payment_batch_release_total_scale says, before the guard looks for the batch (§11.3)
    RAISE EXCEPTION 'The release''s total is not a number' USING ERRCODE = 'check_violation';
  END IF;
  IF EXISTS (SELECT 1 FROM fyniq.payment_batch_release r WHERE r.organisation_id = NEW.organisation_id AND r.batch_id = NEW.batch_id) THEN
    RAISE EXCEPTION 'Batch % is released already', NEW.batch_id USING ERRCODE = 'FY239',
      HINT = 'A batch is released once: its payments are posted and its file written (FR-BNK-12).';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM fyniq.document d WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.batch_id AND d.type = 'payment_batch'
      AND d.status = 'posted' AND d.current_version = NEW.batch_version
  ) THEN
    RAISE EXCEPTION 'Batch % is released only once approved, at its current version', NEW.batch_id USING ERRCODE = 'FY239',
      HINT = 'Ask for the batch''s approval; another person approves it first (FR-BNK-12, FM-1).';
  END IF;
  IF NEW.break_glass_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM fyniq.break_glass_use u
        WHERE u.organisation_id = NEW.organisation_id AND u.break_glass_id = NEW.break_glass_id AND u.batch_id = NEW.batch_id
          AND u.batch_version = NEW.batch_version AND u.used_by = NEW.released_by AND u.used_at = NEW.released_at
          AND u.currency = NEW.currency AND u.total = NEW.total) THEN
    RAISE EXCEPTION 'Batch % is released under break-glass only as its use approved it', NEW.batch_id USING ERRCODE = 'FY239',
      HINT = 'The Owner who used the break-glass releases the batch then, for the total it approved (FM-8).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- A break-glass's entry in the review log (FM-3, FM-8).
ALTER TABLE fyniq.review_log_entry DROP CONSTRAINT review_log_entry_kind_known;
ALTER TABLE fyniq.review_log_entry ADD CONSTRAINT review_log_entry_kind_known CHECK (
  kind IN ('approved_change', 'founder_bootstrap', 'self_approved', 'scheduled_issue', 'auto_reconciled', 'release_conflict',
           'break_glass')) NOT VALID;

-- +goose StatementBegin
-- review_log_guard as in 000051, and: a break-glass is reviewed only once it has ended (FY151; FM-8).
CREATE OR REPLACE FUNCTION fyniq.review_log_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  e fyniq.review_log_entry;
  b fyniq.break_glass;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The review log never changes' USING ERRCODE = 'FY150', HINT = 'Log a new entry, or review it once.';
  END IF;
  IF TG_TABLE_NAME = 'review_log_review' THEN
    SELECT * INTO e FROM fyniq.review_log_entry x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.entry_id;
    IF e.actor = NEW.reviewed_by OR NEW.reviewed_at < e.logged_at THEN
      RAISE EXCEPTION 'A high-risk action is reviewed by someone other than the one who acted' USING ERRCODE = 'FY151',
        HINT = 'Ask another founder to review it (FM-3).';
    END IF;
    IF e.kind = 'break_glass' THEN
      SELECT * INTO b FROM fyniq.break_glass x WHERE x.organisation_id = e.organisation_id AND x.id::text = e.object_key;
      IF NOT FOUND OR NEW.reviewed_at < coalesce(b.ended_at, b.expires_at) THEN
        RAISE EXCEPTION 'A break-glass is reviewed once it has ended' USING ERRCODE = 'FY151',
          HINT = 'Review it after it expires, or end it first (FM-8).';
      END IF;
    END IF;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

ALTER TABLE fyniq.break_glass ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.break_glass FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.break_glass
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.break_glass_use ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.break_glass_use FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.break_glass_use
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.break_glass
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('break_glass', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.break_glass_use
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('break_glass', 'break_glass_id');

REVOKE ALL ON fyniq.break_glass FROM PUBLIC;
REVOKE ALL ON fyniq.break_glass_use FROM PUBLIC;
GRANT SELECT ON fyniq.break_glass, fyniq.break_glass_use TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (started_by, reason, started_at, expires_at, currency, minor_units, cap) ON fyniq.break_glass TO fyniq_api;
GRANT UPDATE (ended_at, ended_by, version) ON fyniq.break_glass TO fyniq_api;
GRANT INSERT (break_glass_id, batch_id, batch_version, used_by, used_at, currency, minor_units, total) ON fyniq.break_glass_use TO fyniq_api;
GRANT INSERT (break_glass_id) ON fyniq.payment_batch_release TO fyniq_api;
