-- name: InsertPayeeBankAccount :one
INSERT INTO fyniq.payee_bank_account (change_id, change_version, contact_id, member_user_id, currency, number_sealed, number_last4,
                                      number_fingerprint)
VALUES (sqlc.arg(change_id), sqlc.arg(change_version), sqlc.narg(contact_id), sqlc.narg(member_user_id), sqlc.arg(currency),
        sqlc.arg(number_sealed), sqlc.arg(number_last4), sqlc.arg(number_fingerprint))
RETURNING *;

-- name: PayeeBankAccountOfChange :one
SELECT * FROM fyniq.payee_bank_account WHERE change_id = sqlc.arg(change_id) AND change_version = sqlc.arg(change_version);

-- name: PayeeBankAccountsOfContact :many
SELECT a.* FROM fyniq.payee_bank_account a
JOIN fyniq.document d ON d.organisation_id = a.organisation_id AND d.id = a.change_id AND d.current_version = a.change_version
WHERE a.contact_id = sqlc.arg(contact_id)
ORDER BY a.change_id DESC;

-- name: PayeeBankAccountsOfMember :many
SELECT a.* FROM fyniq.payee_bank_account a
JOIN fyniq.document d ON d.organisation_id = a.organisation_id AND d.id = a.change_id AND d.current_version = a.change_version
WHERE a.member_user_id = sqlc.arg(member_user_id)
ORDER BY a.change_id DESC;

-- name: PayeeBankAccountsWithFingerprint :many
SELECT * FROM fyniq.payee_bank_account WHERE number_fingerprint = sqlc.arg(number_fingerprint) AND state = 'in_force' ORDER BY contact_id, member_user_id, currency;

-- name: MovePayeeBankAccount :execrows
UPDATE fyniq.payee_bank_account SET state = sqlc.arg(state) WHERE id = sqlc.arg(id) AND state <> sqlc.arg(state);
