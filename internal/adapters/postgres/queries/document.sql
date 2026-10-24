-- name: InsertDocument :one
INSERT INTO fyniq.document (type) VALUES ($1) RETURNING id, type, status, current_version;

-- name: InsertDocumentVersion :exec
INSERT INTO fyniq.document_version (document_id, version, payload, created_by) VALUES ($1, $2, $3, $4);

-- name: SetDocumentVersion :exec
-- A new version becomes the current one; document_guard refuses going back.
UPDATE fyniq.document SET current_version = sqlc.arg(version) WHERE id = sqlc.arg(id);

-- name: EditDocument :exec
-- An edit of a posted document (FR-GL-12): its next version becomes current with who posted the edit and when, in one
-- statement (document_guard, FY053); document_edit_posted checks the reversal and the new entry at commit (FY055).
UPDATE fyniq.document
SET current_version = sqlc.arg(version), posted_by = sqlc.arg(posted_by)::text, posted_at = sqlc.arg(posted_at)::timestamptz
WHERE id = sqlc.arg(id);

-- name: DueReversals :many
-- Posted journals whose automatic reversal is dated on or before a day (FR-GL-02), earliest first: the scheduler
-- reverses them. Dates are ISO text, so comparing them as text orders them as dates.
SELECT d.id
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
WHERE d.type = 'journal' AND d.status = 'posted'
  AND coalesce(v.payload->>'reverse_on', '') <> '' AND v.payload->>'reverse_on' <= sqlc.arg(on_day)::text
ORDER BY v.payload->>'reverse_on', d.id
LIMIT sqlc.arg(max_rows);

-- name: SpreadDocuments :many
-- Posted documents of one type (a bill or an invoice) whose current version spreads a line over months (FR-GL-03,
-- ENG-05; rev 0.83, invoices rev 0.118) with releases a period from from_day on may still take: a spread that runs past
-- that day, or a document dated on or after it, whose earlier months release on its date. By ID after a cursor. A
-- spread with no first month starts in the document's month.
SELECT d.id
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
WHERE d.type = sqlc.arg(doc_type)::text AND d.status = 'posted'
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(v.payload->'lines') l
    WHERE coalesce(l->>'spread_months', '') <> ''
      AND (v.payload->>'date' >= sqlc.arg(from_day)::text
        OR to_char(to_date(coalesce(nullif(l->>'spread_from', ''), v.payload->>'date'), 'YYYY-MM-DD')
                   + make_interval(months => (l->>'spread_months')::int), 'YYYY-MM-DD') > sqlc.arg(from_day)::text))
  AND (sqlc.narg(after_id)::uuid IS NULL OR d.id > sqlc.narg(after_id)::uuid)
ORDER BY d.id
LIMIT sqlc.arg(max_rows);

-- name: BillsOfOrder :many
-- The bills, drafts and posted, not reversed, whose current version has a line made from a purchase order (FR-AP-01).
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at,
       coalesce(v.payload->>'date', '')::text AS date
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.type = 'bill' AND d.status <> 'reversed'
  AND v.payload->'lines' @> jsonb_build_array(jsonb_build_object('order', jsonb_build_object('purchase_order_id', sqlc.arg(order_id)::text)))
ORDER BY d.id;

-- name: InvoicesOfQuote :many
-- The invoices, drafts and issued, not voided, whose current version has a line made from a quote or sales order
-- (FR-AR-02).
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at,
       coalesce(v.payload->>'date', '')::text AS date
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.type = 'invoice' AND d.status <> 'reversed'
  AND v.payload->'lines' @> jsonb_build_array(jsonb_build_object('quote', jsonb_build_object('quote_id', sqlc.arg(quote_id)::text)))
ORDER BY d.id;

-- name: WaitingReversals :many
-- Posted journals whose automatic reversal waits (FR-GL-02, FR-AUT-02; rev 0.58): no period that is not closed covers
-- its date, so the scheduler cannot post it until one is opened or reopened. Earliest reversal first, then ID, after
-- the cursor's (reverse_on, id).
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at,
       coalesce(v.payload->>'date', '')::text AS date
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.type = 'journal' AND d.status = 'posted' AND coalesce(v.payload->>'reverse_on', '') <> ''
  AND NOT EXISTS (
    SELECT 1 FROM fyniq.period p
    WHERE p.organisation_id = d.organisation_id AND p.status <> 'closed'
      AND to_char(p.starts_on, 'YYYY-MM-DD') <= v.payload->>'reverse_on' AND to_char(p.ends_on, 'YYYY-MM-DD') >= v.payload->>'reverse_on')
  AND (sqlc.narg(after_id)::uuid IS NULL OR (v.payload->>'reverse_on', d.id) > (sqlc.arg(after_on)::text, sqlc.narg(after_id)::uuid))
ORDER BY v.payload->>'reverse_on', d.id
LIMIT sqlc.arg(max_rows);

-- name: GetDocument :one
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1;

-- name: LockDocument :one
-- ENG-47 step 1: the source document a command changes, before the ledger lock.
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = $1
FOR UPDATE OF d;

-- name: ListDocuments :many
-- Documents of a type at their current version, newest date first (a draft without a date last), then newest ID,
-- after the cursor's (date, id). The text matches the memo or the number, as typed: no wildcards.
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at,
       coalesce(v.payload->>'date', '')::text AS date
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.type = sqlc.arg(type)
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status)::text)
  AND (sqlc.narg(date_from)::text IS NULL OR coalesce(v.payload->>'date', '') >= sqlc.narg(date_from)::text)
  AND (sqlc.narg(date_to)::text IS NULL OR (v.payload->>'date' IS NOT NULL AND v.payload->>'date' <= sqlc.narg(date_to)::text))
  AND (sqlc.narg(search)::text IS NULL
       OR strpos(lower(coalesce(v.payload->>'memo', '')), lower(sqlc.narg(search)::text)) > 0
       OR strpos(lower(coalesce(n.number, '')), lower(sqlc.narg(search)::text)) > 0)
  AND (sqlc.narg(after_id)::uuid IS NULL
       OR (coalesce(v.payload->>'date', ''), d.id) < (sqlc.arg(after_date)::text, sqlc.narg(after_id)::uuid))
ORDER BY coalesce(v.payload->>'date', '') DESC, d.id DESC
LIMIT sqlc.arg(page_size);

-- name: SetDocumentStatus :exec
-- Posting records who posted and when (FY053); reversing leaves them as they are.
UPDATE fyniq.document
SET status = sqlc.arg(status),
    posted_by = coalesce(sqlc.narg(posted_by)::text, posted_by),
    posted_at = coalesce(sqlc.narg(posted_at)::timestamptz, posted_at)
WHERE id = sqlc.arg(id);

-- name: SetDocumentStatusOnly :exec
-- A move that records no actor, such as reversing: it writes the status alone, so the worker, which may move a status
-- for the scheduler's automatic reversals but never name who posted, can run it (rev 0.55).
UPDATE fyniq.document SET status = sqlc.arg(status) WHERE id = sqlc.arg(id);

-- name: GetDocumentStatusForUpdate :one
SELECT status FROM fyniq.document WHERE id = $1 FOR UPDATE;

-- name: InsertOpeningBalance :exec
-- The organisation's one opening balances document (FR-GL-06): its key is the organisation.
INSERT INTO fyniq.opening_balance (document_id) VALUES ($1);

-- name: GetOpeningBalance :one
SELECT d.id, d.type, d.status, d.current_version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.opening_balance o
JOIN fyniq.document d ON d.organisation_id = o.organisation_id AND d.id = o.document_id
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = d.current_version
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id;

-- name: GetDocumentVersion :one
-- A document at one of its versions, as it was saved: the replay re-runs the rule over the version that posted.
SELECT d.id, d.type, d.status, v.version, v.payload, n.number, v.created_by, d.posted_by, d.posted_at
FROM fyniq.document d
JOIN fyniq.document_version v ON v.organisation_id = d.organisation_id AND v.document_id = d.id AND v.version = sqlc.arg(version)
LEFT JOIN fyniq.document_number n ON n.organisation_id = d.organisation_id AND n.id = d.id
WHERE d.id = sqlc.arg(id);
