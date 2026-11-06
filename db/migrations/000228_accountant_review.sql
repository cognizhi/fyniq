-- The accountant's review (PRD-001 FM-7; rev 0.176, the owner's decisions of 6 Nov 2026; p1-approval-engine task 3.4).
--
-- A review covers one closed month: the close it reviews, with that close's snapshot (AUD-04). The Accountant opens it,
-- raises queries and links adjusting journals; an Owner or the Controller answers each query, once; the Accountant
-- signs it off once every query has its answer. Reopening the month supersedes its reviews, which are kept. The
-- database is the backstop:
--   - a review opens on a closed month's latest close, once per close, in review (FY264);
--   - it changes only as it gains a query, an answer or a journal while in review, as it is signed off once every query
--     has its answer, or as the month reopens; it is never deleted (FY264);
--   - a reopening leaves no review of the close it undoes live (FY264, at commit);
--   - a query is asked and answered while its review is in review, answered once, and never changed otherwise (FY265);
--   - a linked journal is a journal, linked while the review is in review, and the link never changes (FY266).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.accountant_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  period_id       uuid        NOT NULL,
  close_id        uuid        NOT NULL,  -- the close it reviews, whose snapshot it covers (AUD-04)
  opened_by       uuid        NOT NULL,
  opened_at       timestamptz NOT NULL,
  status          text        NOT NULL DEFAULT 'in_review',
  signed_by       uuid,
  signed_at       timestamptz,
  statement       text,                  -- what the Accountant signs off
  superseded_at   timestamptz,
  reopen_id       uuid,                  -- the reopening that superseded it
  version         integer     NOT NULL DEFAULT 1,
  CONSTRAINT accountant_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT accountant_review_close_once UNIQUE (organisation_id, close_id),
  CONSTRAINT accountant_review_period_fkey FOREIGN KEY (organisation_id, period_id) REFERENCES fyniq.period (organisation_id, id),
  CONSTRAINT accountant_review_close_fkey FOREIGN KEY (organisation_id, close_id) REFERENCES fyniq.period_close (organisation_id, id),
  CONSTRAINT accountant_review_reopen_fkey FOREIGN KEY (organisation_id, reopen_id) REFERENCES fyniq.period_reopen (organisation_id, id),
  CONSTRAINT accountant_review_opened_by_fkey FOREIGN KEY (opened_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT accountant_review_signed_by_fkey FOREIGN KEY (signed_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT accountant_review_status_known CHECK (status IN ('in_review', 'signed_off', 'superseded')),
  CONSTRAINT accountant_review_signed CHECK ((signed_by IS NULL) = (signed_at IS NULL) AND (signed_by IS NULL) = (statement IS NULL)
                                             AND (status <> 'signed_off' OR signed_by IS NOT NULL)),
  CONSTRAINT accountant_review_superseded CHECK ((status = 'superseded') = (superseded_at IS NOT NULL)
                                                 AND (status = 'superseded') = (reopen_id IS NOT NULL)),
  CONSTRAINT accountant_review_statement_length CHECK (statement IS NULL OR length(btrim(statement)) BETWEEN 1 AND 1000),
  CONSTRAINT accountant_review_version_positive CHECK (version > 0)
);
CREATE INDEX accountant_review_period ON fyniq.accountant_review (organisation_id, period_id);

CREATE TABLE fyniq.accountant_review_query (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  review_id       uuid        NOT NULL,
  asked_by        uuid        NOT NULL,
  asked_at        timestamptz NOT NULL,
  question        text        NOT NULL,
  answered_by     uuid,
  answered_at     timestamptz,
  answer          text,
  CONSTRAINT accountant_review_query_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT accountant_review_query_review_fkey FOREIGN KEY (organisation_id, review_id) REFERENCES fyniq.accountant_review (organisation_id, id),
  CONSTRAINT accountant_review_query_asked_by_fkey FOREIGN KEY (asked_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT accountant_review_query_answered_by_fkey FOREIGN KEY (answered_by) REFERENCES fyniq.app_user (user_id),
  CONSTRAINT accountant_review_query_question_length CHECK (length(btrim(question)) BETWEEN 1 AND 2000),
  CONSTRAINT accountant_review_query_answered CHECK ((answered_by IS NULL) = (answered_at IS NULL) AND (answered_by IS NULL) = (answer IS NULL)
                                                     AND (answer IS NULL OR length(btrim(answer)) BETWEEN 1 AND 2000))
);
CREATE INDEX accountant_review_query_review ON fyniq.accountant_review_query (organisation_id, review_id);

CREATE TABLE fyniq.accountant_review_journal (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  review_id       uuid        NOT NULL,
  journal_id      uuid        NOT NULL,  -- an adjusting journal, which posts through FR-GL-01 approval
  linked_by       uuid        NOT NULL,
  linked_at       timestamptz NOT NULL,
  CONSTRAINT accountant_review_journal_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT accountant_review_journal_once UNIQUE (organisation_id, review_id, journal_id),
  CONSTRAINT accountant_review_journal_review_fkey FOREIGN KEY (organisation_id, review_id) REFERENCES fyniq.accountant_review (organisation_id, id),
  CONSTRAINT accountant_review_journal_document_fkey FOREIGN KEY (organisation_id, journal_id) REFERENCES fyniq.document (organisation_id, id),
  CONSTRAINT accountant_review_journal_by_fkey FOREIGN KEY (linked_by) REFERENCES fyniq.app_user (user_id)
);

-- +goose StatementBegin
-- A review opens on a closed month's latest close, in review; it changes only by a version as it gains a query, an
-- answer or a journal while in review, by its sign-off once every query has its answer, or by the month's reopening;
-- it is never deleted (FY264; FM-7).
CREATE FUNCTION fyniq.accountant_review_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'Accountant reviews are never deleted' USING ERRCODE = 'FY264';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'Accountant review % is never deleted', OLD.id USING ERRCODE = 'FY264';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.status <> 'in_review' OR NEW.signed_by IS NOT NULL OR NEW.reopen_id IS NOT NULL THEN
      RAISE EXCEPTION 'An accountant review opens in review' USING ERRCODE = 'FY264';
    END IF;
    IF NOT EXISTS (
         SELECT 1 FROM fyniq.period p
          WHERE p.organisation_id = NEW.organisation_id AND p.id = NEW.period_id AND p.status = 'closed')
       OR NEW.close_id IS DISTINCT FROM (
         SELECT c.id FROM fyniq.period_close c
          WHERE c.organisation_id = NEW.organisation_id AND c.period_id = NEW.period_id
          ORDER BY c.attested_at DESC, c.id DESC LIMIT 1) THEN
      RAISE EXCEPTION 'An accountant review covers a closed month''s latest close' USING ERRCODE = 'FY264',
        HINT = 'Close the month first; the review covers its close snapshot (FM-7, AUD-04).';
    END IF;
    RETURN NEW;
  END IF;
  IF (NEW.organisation_id, NEW.id, NEW.period_id, NEW.close_id, NEW.opened_by, NEW.opened_at)
     IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.period_id, OLD.close_id, OLD.opened_by, OLD.opened_at)
     OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'Accountant review % keeps what it covers, and changes one version at a time', OLD.id USING ERRCODE = 'FY264';
  END IF;
  IF OLD.status = 'superseded' THEN
    RAISE EXCEPTION 'Accountant review % is superseded: it never changes again', OLD.id USING ERRCODE = 'FY264';
  END IF;
  IF NEW.status = 'superseded' THEN
    IF NOT EXISTS (SELECT 1 FROM fyniq.period_reopen r
                    WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.reopen_id AND r.close_id = NEW.close_id)
       OR (NEW.signed_by, NEW.signed_at, NEW.statement) IS DISTINCT FROM (OLD.signed_by, OLD.signed_at, OLD.statement) THEN
      RAISE EXCEPTION 'Accountant review % is superseded only by the reopening of the close it covers', OLD.id USING ERRCODE = 'FY264';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status = 'signed_off' THEN
    RAISE EXCEPTION 'Accountant review % is signed off: it changes only when its month reopens', OLD.id USING ERRCODE = 'FY264';
  END IF;
  IF NEW.status = 'in_review' THEN -- a query, an answer or a journal: its version only
    IF NEW.signed_by IS NOT NULL OR NEW.superseded_at IS NOT NULL THEN
      RAISE EXCEPTION 'Accountant review % is still in review', OLD.id USING ERRCODE = 'FY264';
    END IF;
    RETURN NEW;
  END IF;
  -- Signed off, once every query has its answer.
  IF NEW.signed_by IS NULL OR NEW.signed_at < OLD.opened_at OR EXISTS (
       SELECT 1 FROM fyniq.accountant_review_query q
        WHERE q.organisation_id = NEW.organisation_id AND q.review_id = NEW.id AND q.answer IS NULL) THEN
    RAISE EXCEPTION 'Accountant review % is signed off once every query has its answer', OLD.id USING ERRCODE = 'FY264',
      HINT = 'An Owner or the Controller answers each query first (FM-7).';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER accountant_review_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.accountant_review
  FOR EACH ROW EXECUTE FUNCTION fyniq.accountant_review_guard();
CREATE TRIGGER accountant_review_no_truncate BEFORE TRUNCATE ON fyniq.accountant_review
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.accountant_review_guard();

-- +goose StatementBegin
-- A reopening leaves no review of the close it undoes live: they are superseded in its transaction (FY264; FM-7).
CREATE FUNCTION fyniq.accountant_review_reopened() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM fyniq.accountant_review a
              WHERE a.organisation_id = NEW.organisation_id AND a.close_id = NEW.close_id AND a.status <> 'superseded') THEN
    RAISE EXCEPTION 'The reopening of a reviewed month supersedes its review' USING ERRCODE = 'FY264',
      HINT = 'Supersede the reviews of the close it undoes in the same transaction (FM-7).';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER accountant_review_reopened AFTER INSERT ON fyniq.period_reopen
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.accountant_review_reopened();

-- +goose StatementBegin
-- A query is asked and answered while its review is in review, answered once, and never changed otherwise (FY265).
CREATE FUNCTION fyniq.accountant_review_query_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
DECLARE
  r fyniq.accountant_review;
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The queries of accountant reviews are never deleted' USING ERRCODE = 'FY265';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'The query % is never deleted', OLD.id USING ERRCODE = 'FY265';
  END IF;
  SELECT * INTO r FROM fyniq.accountant_review x WHERE x.organisation_id = NEW.organisation_id AND x.id = NEW.review_id;
  IF NOT FOUND OR r.status <> 'in_review' THEN
    RAISE EXCEPTION 'A query is asked and answered while its review is in review' USING ERRCODE = 'FY265';
  END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.answer IS NOT NULL OR NEW.asked_at < r.opened_at THEN
      RAISE EXCEPTION 'A query is asked unanswered, after its review opened' USING ERRCODE = 'FY265';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.answer IS NOT NULL OR NEW.answer IS NULL OR NEW.answered_at < OLD.asked_at
     OR (NEW.organisation_id, NEW.id, NEW.review_id, NEW.asked_by, NEW.asked_at, NEW.question)
        IS DISTINCT FROM (OLD.organisation_id, OLD.id, OLD.review_id, OLD.asked_by, OLD.asked_at, OLD.question) THEN
    RAISE EXCEPTION 'The query % is answered once, and never changed otherwise', OLD.id USING ERRCODE = 'FY265';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER accountant_review_query_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.accountant_review_query
  FOR EACH ROW EXECUTE FUNCTION fyniq.accountant_review_query_guard();
CREATE TRIGGER accountant_review_query_no_truncate BEFORE TRUNCATE ON fyniq.accountant_review_query
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.accountant_review_query_guard();

-- +goose StatementBegin
-- A linked journal is a journal, linked while its review is in review, and the link never changes (FY266).
CREATE FUNCTION fyniq.accountant_review_journal_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'The journals of accountant reviews are never unlinked' USING ERRCODE = 'FY266';
  END IF;
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'The link of journal % to its review never changes', OLD.journal_id USING ERRCODE = 'FY266';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM fyniq.accountant_review r
                  WHERE r.organisation_id = NEW.organisation_id AND r.id = NEW.review_id AND r.status = 'in_review')
     OR NOT EXISTS (SELECT 1 FROM fyniq.document d
                     WHERE d.organisation_id = NEW.organisation_id AND d.id = NEW.journal_id AND d.type = 'journal') THEN
    RAISE EXCEPTION 'A review in review links a journal' USING ERRCODE = 'FY266';
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER accountant_review_journal_guard BEFORE INSERT OR UPDATE OR DELETE ON fyniq.accountant_review_journal
  FOR EACH ROW EXECUTE FUNCTION fyniq.accountant_review_journal_guard();
CREATE TRIGGER accountant_review_journal_no_truncate BEFORE TRUNCATE ON fyniq.accountant_review_journal
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.accountant_review_journal_guard();

ALTER TABLE fyniq.accountant_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.accountant_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.accountant_review
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.accountant_review_query ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.accountant_review_query FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.accountant_review_query
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.accountant_review_journal ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.accountant_review_journal FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.accountant_review_journal
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.accountant_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('accountant_review', 'id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT OR UPDATE ON fyniq.accountant_review_query
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('accountant_review', 'review_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.accountant_review_journal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('accountant_review', 'review_id');

REVOKE ALL ON fyniq.accountant_review FROM PUBLIC;
REVOKE ALL ON fyniq.accountant_review_query FROM PUBLIC;
REVOKE ALL ON fyniq.accountant_review_journal FROM PUBLIC;
GRANT SELECT ON fyniq.accountant_review, fyniq.accountant_review_query, fyniq.accountant_review_journal
  TO fyniq_api, fyniq_worker, fyniq_executor, fyniq_readonly;
GRANT INSERT (period_id, close_id, opened_by, opened_at) ON fyniq.accountant_review TO fyniq_api;
GRANT UPDATE (status, signed_by, signed_at, statement, superseded_at, reopen_id, version) ON fyniq.accountant_review TO fyniq_api;
GRANT INSERT (review_id, asked_by, asked_at, question) ON fyniq.accountant_review_query TO fyniq_api;
GRANT UPDATE (answered_by, answered_at, answer) ON fyniq.accountant_review_query TO fyniq_api;
GRANT INSERT (review_id, journal_id, linked_by, linked_at) ON fyniq.accountant_review_journal TO fyniq_api;
