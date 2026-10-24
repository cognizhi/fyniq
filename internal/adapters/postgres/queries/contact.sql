-- name: InsertContact :one
INSERT INTO fyniq.contact (legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone,
  address, country, tax_residence, currency, payment_terms_days, discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id)
VALUES (sqlc.arg(legal_name), sqlc.arg(is_customer), sqlc.arg(is_vendor), sqlc.narg(uen_or_tax_id), sqlc.narg(peppol_id),
  sqlc.narg(billing_email), sqlc.narg(remittance_email), sqlc.narg(phone), sqlc.narg(address), sqlc.narg(country), sqlc.narg(tax_residence), sqlc.narg(currency),
  sqlc.narg(payment_terms_days), sqlc.narg(discount_percent)::text::numeric, sqlc.narg(discount_days), sqlc.narg(default_account_id), sqlc.narg(default_gst_code), sqlc.narg(customer_group_id))
RETURNING id, legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address, country, tax_residence, currency, payment_terms_days, coalesce(trim_scale(discount_percent)::text, '')::text AS discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id, is_active, version;

-- name: GetContact :one
SELECT id, legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address, country, tax_residence, currency, payment_terms_days, coalesce(trim_scale(discount_percent)::text, '')::text AS discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id, is_active, version FROM fyniq.contact WHERE id = sqlc.arg(id);

-- name: LockContact :one
SELECT id, legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address, country, tax_residence, currency, payment_terms_days, coalesce(trim_scale(discount_percent)::text, '')::text AS discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id, is_active, version FROM fyniq.contact WHERE id = sqlc.arg(id) FOR UPDATE;

-- name: UpdateContact :one
UPDATE fyniq.contact SET legal_name = sqlc.arg(legal_name), is_customer = sqlc.arg(is_customer), is_vendor = sqlc.arg(is_vendor),
  uen_or_tax_id = sqlc.narg(uen_or_tax_id), peppol_id = sqlc.narg(peppol_id), billing_email = sqlc.narg(billing_email),
  remittance_email = sqlc.narg(remittance_email), phone = sqlc.narg(phone), address = sqlc.narg(address), country = sqlc.narg(country),
  tax_residence = sqlc.narg(tax_residence), currency = sqlc.narg(currency), payment_terms_days = sqlc.narg(payment_terms_days),
  discount_percent = sqlc.narg(discount_percent)::text::numeric, discount_days = sqlc.narg(discount_days), default_account_id = sqlc.narg(default_account_id),
  default_gst_code = sqlc.narg(default_gst_code), customer_group_id = sqlc.narg(customer_group_id), is_active = sqlc.arg(is_active), version = version + 1
WHERE id = sqlc.arg(id)
RETURNING id, legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address, country, tax_residence, currency, payment_terms_days, coalesce(trim_scale(discount_percent)::text, '')::text AS discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id, is_active, version;

-- name: PageContacts :many
-- By ID, which is time-ordered: the order contacts were added (FR-INT-10's cursor needs a stable key).
SELECT id, legal_name, is_customer, is_vendor, uen_or_tax_id, peppol_id, billing_email, remittance_email, phone, address, country, tax_residence, currency, payment_terms_days, coalesce(trim_scale(discount_percent)::text, '')::text AS discount_percent, discount_days, default_account_id, default_gst_code, customer_group_id, is_active, version FROM fyniq.contact
WHERE id > sqlc.arg(after)::uuid
  AND (sqlc.narg(role)::text IS NULL OR (sqlc.narg(role)::text = 'customer' AND is_customer) OR (sqlc.narg(role)::text = 'vendor' AND is_vendor))
ORDER BY id LIMIT sqlc.arg(page_size);

-- name: ContactDuplicates :many
-- Other contacts with the same name (case and spacing aside), UEN or tax ID, or email (FR-CT-04).
SELECT id, legal_name,
  lower(regexp_replace(btrim(legal_name), '\s+', ' ', 'g')) = lower(regexp_replace(btrim(sqlc.arg(legal_name)::text), '\s+', ' ', 'g')) AS same_name,
  coalesce(upper(uen_or_tax_id) = upper(sqlc.narg(uen_or_tax_id)::text), false)::boolean AS same_uen,
  coalesce(lower(billing_email) IN (lower(sqlc.narg(billing_email)::text), lower(sqlc.narg(remittance_email)::text))
        OR lower(remittance_email) IN (lower(sqlc.narg(billing_email)::text), lower(sqlc.narg(remittance_email)::text)), false)::boolean AS same_email
FROM fyniq.contact
WHERE id <> sqlc.arg(id)::uuid
  AND (lower(legal_name) = lower(sqlc.arg(legal_name)::text)
    OR lower(regexp_replace(btrim(legal_name), '\s+', ' ', 'g')) = lower(regexp_replace(btrim(sqlc.arg(legal_name)::text), '\s+', ' ', 'g'))
    OR upper(uen_or_tax_id) = upper(sqlc.narg(uen_or_tax_id)::text)
    OR lower(billing_email) IN (lower(sqlc.narg(billing_email)::text), lower(sqlc.narg(remittance_email)::text))
    OR lower(remittance_email) IN (lower(sqlc.narg(billing_email)::text), lower(sqlc.narg(remittance_email)::text)))
ORDER BY id
LIMIT 20;
