-- name: InsertDocumentSend :one
INSERT INTO fyniq.document_send (document_id, document_version, message_id, evidence_id, recipient, template_source, template_id,
  template_version, template_hash, pdf_sha256, sent_by)
VALUES (sqlc.arg(document_id), sqlc.arg(document_version), sqlc.arg(message_id), sqlc.arg(evidence_id), sqlc.arg(recipient),
  sqlc.arg(template_source), sqlc.narg(template_id), sqlc.arg(template_version), sqlc.arg(template_hash), sqlc.arg(pdf_sha256), sqlc.arg(sent_by))
RETURNING id, sent_at;

-- name: DocumentSendsOf :many
-- The sends of the documents named, newest first within each, with their message's status and the kind of the
-- provider's latest event about it.
SELECT s.id, s.document_id, s.document_version, s.message_id, s.evidence_id, s.recipient, s.template_source, s.template_id,
       s.template_version, s.template_hash, s.pdf_sha256, s.sent_by, s.sent_at, m.status AS message_status,
       coalesce((SELECT e.kind FROM fyniq.email_event e
                 WHERE e.organisation_id = s.organisation_id AND e.message_id = s.message_id
                 ORDER BY e.occurred_at DESC, e.id DESC LIMIT 1), '')::text AS last_event
FROM fyniq.document_send s
JOIN fyniq.email_message m ON m.organisation_id = s.organisation_id AND m.id = s.message_id
WHERE s.document_id = ANY (sqlc.arg(document_ids)::uuid[])
ORDER BY s.document_id, s.sent_at DESC, s.id DESC;
