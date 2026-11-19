-- Comments and @mentions on a record (PRD-001 FR-UX-13; p1-app-shell task 3.3). A comment belongs to one record, a document or a
-- contact, and keeps who wrote it, what they wrote and whom they mentioned. Each is a new row, audited, and never updated or
-- deleted (no hard delete, FR-UX-14): a mistake is answered by a comment. Who may read or write a record's comments, and whom
-- a comment may mention, is the API's decision from the record's own Appendix D row; this table holds no figure and no personal
-- data beyond a name the writer typed.

-- +goose Up
-- Fail fast instead of queueing behind live traffic (squawk: require-lock-timeout).
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE fyniq.record_comment (
  organisation_id uuid        NOT NULL DEFAULT fyniq.current_org(),
  id              uuid        NOT NULL DEFAULT uuidv7(),
  record_type     text        NOT NULL,
  record_id       uuid        NOT NULL,
  author          uuid        NOT NULL,
  body            text        NOT NULL,
  mentions        uuid[]      NOT NULL DEFAULT '{}',
  created_at      timestamptz NOT NULL,
  CONSTRAINT record_comment_pkey PRIMARY KEY (organisation_id, id),
  CONSTRAINT record_comment_author_fkey FOREIGN KEY (organisation_id, author)
    REFERENCES fyniq.user_membership (organisation_id, user_id),
  CONSTRAINT record_comment_type_known CHECK (record_type IN ('invoice', 'credit_note', 'bill', 'vendor_credit', 'journal', 'expense_claim',
    'quote', 'sales_order', 'purchase_order', 'contact')),
  CONSTRAINT record_comment_body_length CHECK (char_length(body) BETWEEN 1 AND 2000),
  CONSTRAINT record_comment_mentions_limit CHECK (cardinality(mentions) <= 10)
);
CREATE INDEX record_comment_thread ON fyniq.record_comment (organisation_id, record_type, record_id, created_at, id);

-- Append-only, as the preferences and the saved items are (FY189): a new row is the next comment.
CREATE TRIGGER record_comment_guard BEFORE UPDATE OR DELETE ON fyniq.record_comment
  FOR EACH ROW EXECUTE FUNCTION fyniq.notification_append_only();
CREATE TRIGGER record_comment_no_truncate BEFORE TRUNCATE ON fyniq.record_comment
  FOR EACH STATEMENT EXECUTE FUNCTION fyniq.document_version_immutable();

CREATE CONSTRAINT TRIGGER require_audit AFTER INSERT ON fyniq.record_comment
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit('record_comment', 'id');

ALTER TABLE fyniq.record_comment ENABLE ROW LEVEL SECURITY;
ALTER TABLE fyniq.record_comment FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fyniq.record_comment
  USING (organisation_id = fyniq.current_org()) WITH CHECK (organisation_id = fyniq.current_org());

-- The API writes a comment as the member who wrote it and reads a record's thread; the worker reads them for the tenant export.
GRANT SELECT ON fyniq.record_comment TO fyniq_api, fyniq_worker, fyniq_readonly;
GRANT INSERT (record_type, record_id, author, body, mentions, created_at) ON fyniq.record_comment TO fyniq_api;
