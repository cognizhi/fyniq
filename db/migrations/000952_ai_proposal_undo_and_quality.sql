-- Undo of an executed AI proposal, the first view of a proposal, and the signed monthly review of approval quality (PRD-001 AI-31, AI-91, AI-92,
-- CTL-AI-07; p1-ai-proposals tasks 2.1 and 2.3).
--
-- An undo is a person's act through the domain's own reversal (design D4): its record is append-only, one per proposal (who, when, why and what was
-- reversed), and the proposal stays Executed. The first time a person who may decide a proposal opens its review is kept, once per person, so the
-- month's metrics can tell a decision made after a look from a rubber stamp. The monthly review of the metrics is signed once per month by the
-- Owner or the DPO and kept as evidence. Nothing here is ever updated or deleted (FY951).

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.ai_proposal_undo (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  proposal_id     uuid        NOT NULL,
  undone_by       uuid        NOT NULL,
  undone_at       timestamptz NOT NULL,
  reason          text        NOT NULL,
  reversed        jsonb       NOT NULL, -- what was reversed: the records and entries, as the command reported them
  CONSTRAINT ai_proposal_undo_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_proposal_undo_once UNIQUE (organisation_id, proposal_id),
  CONSTRAINT ai_proposal_undo_proposal_fkey FOREIGN KEY (organisation_id, proposal_id) REFERENCES fyniq.ai_proposal (organisation_id, id),
  CONSTRAINT ai_proposal_undo_reason CHECK (length(reason) BETWEEN 1 AND 500)
);

CREATE TABLE fyniq.ai_proposal_view (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  proposal_id     uuid        NOT NULL,
  user_id         uuid        NOT NULL,
  first_viewed_at timestamptz NOT NULL,
  CONSTRAINT ai_proposal_view_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_proposal_view_once UNIQUE (organisation_id, proposal_id, user_id),
  CONSTRAINT ai_proposal_view_proposal_fkey FOREIGN KEY (organisation_id, proposal_id) REFERENCES fyniq.ai_proposal (organisation_id, id)
);

CREATE TABLE fyniq.ai_quality_review (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  month           date        NOT NULL, -- the first day of the month reviewed
  signed_by       uuid        NOT NULL,
  signed_at       timestamptz NOT NULL,
  report          jsonb       NOT NULL, -- the metrics as they stood when it was signed
  note            text,
  CONSTRAINT ai_quality_review_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT ai_quality_review_once UNIQUE (organisation_id, month),
  CONSTRAINT ai_quality_review_month CHECK (month = date_trunc('month', month)::date),
  CONSTRAINT ai_quality_review_note CHECK (length(note) <= 1000)
);

-- +goose StatementBegin
CREATE FUNCTION fyniq.ai_append_only_guard() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, fyniq, pg_temp
AS $$
BEGIN
  RAISE EXCEPTION '% is append-only: a row is never changed or deleted', TG_TABLE_NAME USING ERRCODE = 'FY951',
    HINT = 'An undo, a first view and a signed review are kept as they were made (AI-31, CTL-AI-07).';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_proposal_undo_guard BEFORE UPDATE OR DELETE ON fyniq.ai_proposal_undo FOR EACH ROW EXECUTE FUNCTION fyniq.ai_append_only_guard();
CREATE TRIGGER ai_proposal_view_guard BEFORE UPDATE OR DELETE ON fyniq.ai_proposal_view FOR EACH ROW EXECUTE FUNCTION fyniq.ai_append_only_guard();
CREATE TRIGGER ai_quality_review_guard BEFORE UPDATE OR DELETE ON fyniq.ai_quality_review FOR EACH ROW EXECUTE FUNCTION fyniq.ai_append_only_guard();
CREATE TRIGGER ai_proposal_undo_no_truncate BEFORE TRUNCATE ON fyniq.ai_proposal_undo FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER ai_proposal_view_no_truncate BEFORE TRUNCATE ON fyniq.ai_proposal_view FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();
CREATE TRIGGER ai_quality_review_no_truncate BEFORE TRUNCATE ON fyniq.ai_quality_review FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

ALTER TABLE fyniq.ai_proposal_undo ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_proposal_undo FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_proposal_undo USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_proposal_view ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_proposal_view FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_proposal_view USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());
ALTER TABLE fyniq.ai_quality_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.ai_quality_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.ai_quality_review USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_proposal_undo
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_proposal', 'proposal_id');
CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.ai_quality_review
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('ai_quality_review', 'id');

-- The API writes each as the person; the worker reads them for the tenant export. The AI's role holds nothing (CMP-AI-01).
REVOKE ALL ON fyniq.ai_proposal_undo, fyniq.ai_proposal_view, fyniq.ai_quality_review FROM PUBLIC;
GRANT SELECT ON fyniq.ai_proposal_undo, fyniq.ai_proposal_view, fyniq.ai_quality_review TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (proposal_id, undone_by, undone_at, reason, reversed) ON fyniq.ai_proposal_undo TO fyniq_api;
GRANT INSERT (proposal_id, user_id, first_viewed_at) ON fyniq.ai_proposal_view TO fyniq_api;
GRANT INSERT (month, signed_by, signed_at, report, note) ON fyniq.ai_quality_review TO fyniq_api;
