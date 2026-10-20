-- The log of high-risk actions and their review (PRD-001 FM-3, FM-1, FR-ADM-01, rev 0.46; p1-approval-engine task 3.2).
--
-- Every high-risk change that executes is logged: each approved request (the changes FM-1 holds for a second person),
-- and each role change or invitation the Owner made alone during the founder bootstrap. Self-approvals (FM-2) and
-- break-glass (FM-8) join the log when they arrive. Another person reviews each entry, never the one who acted (FM-3),
-- once; an entry unreviewed 14 days after it was logged is overdue in Founder mode, which the API reports. Neither an
-- entry nor its review ever changes or goes (FY150).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.review_log_entry (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  kind            text        NOT NULL,
  object_type     text        NOT NULL,
  object_key      text        NOT NULL,
  summary         text        NOT NULL,
  actor           uuid,                  -- who acted: the requester, or the Owner in the bootstrap; NULL for a caller with no Fyniq user
  approval_id     uuid,                  -- the approved request, for an approved change
  logged_at       timestamptz NOT NULL,
  CONSTRAINT review_log_entry_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT review_log_entry_approval_once UNIQUE (organisation_id, approval_id),
  CONSTRAINT review_log_entry_organisation_fkey FOREIGN KEY (organisation_id) REFERENCES fyniq.organisation (organisation_id),
  CONSTRAINT review_log_entry_approval_fkey FOREIGN KEY (organisation_id, approval_id)
    REFERENCES fyniq.approval_request (organisation_id, id),
  CONSTRAINT review_log_entry_actor_fkey FOREIGN KEY (actor) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT review_log_entry_kind_known CHECK (kind IN ('approved_change', 'founder_bootstrap')),
  CONSTRAINT review_log_entry_approval_kind CHECK ((kind = 'approved_change') = (approval_id IS NOT NULL)),
  CONSTRAINT review_log_entry_texts CHECK (length(object_type) BETWEEN 1 AND 60 AND length(object_key) BETWEEN 1 AND 200
                                           AND length(btrim(summary)) BETWEEN 1 AND 500)
);
CREATE INDEX review_log_entry_logged ON fyniq.review_log_entry (organisation_id, logged_at);

CREATE TABLE fyniq.review_log_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  entry_id        uuid        NOT NULL,
  reviewed_by     uuid        NOT NULL,
  reviewed_at     timestamptz NOT NULL,
  note            text,
  CONSTRAINT review_log_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT review_log_review_once UNIQUE (organisation_id, entry_id),
  CONSTRAINT review_log_review_entry_fkey FOREIGN KEY (organisation_id, entry_id) REFERENCES fyniq.review_log_entry (organisation_id, id),
  CONSTRAINT review_log_review_by_fkey FOREIGN KEY (reviewed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT review_log_review_note_length CHECK (note IS NULL OR length(btrim(note)) BETWEEN 1 AND 500)
);

-- An entry and its review never change or go; a review is by someone other than the one who acted, after the entry.
-- +goose StatementBegin
CREATE FUNCTION fyniq.review_log_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  e fyniq.review_log_entry;
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
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER review_log_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.review_log_entry
  FOR EACH ROW EXECUTE FUNCTION fyniq.review_log_guard();
CREATE TRIGGER review_log_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.review_log_review
  FOR EACH ROW EXECUTE FUNCTION fyniq.review_log_guard();

ALTER TABLE fyniq.review_log_entry ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.review_log_entry FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.review_log_entry
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.review_log_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.review_log_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.review_log_review
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.review_log_entry
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('review_log_entry', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.review_log_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('review_log_review', 'id');

GRANT SELECT ON fyniq.review_log_entry, fyniq.review_log_review TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (kind, object_type, object_key, summary, actor, approval_id, logged_at) ON fyniq.review_log_entry TO fyniq_api;
GRANT INSERT (entry_id, reviewed_by, reviewed_at, note) ON fyniq.review_log_review TO fyniq_api;
