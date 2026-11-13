package reportexport

// The full tenant export's catalogue (FR-MIG-04, design D10): every table of the schema that holds an organisation_id
// is exported or excluded with why, and a column an exported table leaves out says why too. A table in neither list
// stops the export; TestTheTenantExportCataloguesEveryTable fails on it first, so a new table is placed on purpose.

// Exported are the tables the export writes, each as tables/<name>.csv.
var Exported = []string{
	"access_review_attestation", "account", "accountant_review", "accountant_review_journal", "accountant_review_query",
	"accounting_framework", "accounting_policy", "allocation", "announcement", "announcement_dismissal",
	"approval_policy", "approval_request", "approval_signature", "audit_boundary", "audit_chain", "audit_event",
	"audit_recovery", "audit_recovery_acknowledgement", "audit_request_detail", "balance", "bank_feed",
	"bank_gap", "bank_line", "bank_match", "bank_reconciliation", "bank_rule", "bank_signal", "bank_statement",
	"bank_transfer", "bill_payment_plan", "brand_version", "break_glass", "break_glass_use", "capture_address",
	"capture_sender", "check_owner", "check_review", "check_run", "check_state", "claim_category", "claim_place",
	"close_checklist_entry", "close_checklist_template", "consent_purpose", "consent_record", "contact",
	"contact_document_template", "control_run", "control_state", "conversion_step", "customer_group",
	"data_inventory_entry", "data_inventory_review", "dimension", "dimension_set", "dimension_value", "document",
	"document_link", "document_number", "document_send", "document_sequence", "document_template",
	"document_template_default", "document_template_version", "document_version", "dpia_approval", "dpia_version",
	"email_event", "email_message", "email_sender", "email_suppression", "equity_step", "evidence",
	"fixed_asset_source", "fx_rate", "gst_registration", "holding_movement", "inbound_file", "inbound_message", "item",
	"item_price", "join_request", "journal_entry", "journal_line", "kpi_input", "ledger_boundary",
	"ledger_purged_entry", "ledger_revision", "member_invitation", "migration_archive", "migration_commit",
	"migration_difference", "migration_file", "migration_mapping", "migration_record", "migration_run",
	"migration_signoff", "migration_step", "money_account", "money_account_source", "notification",
	"notification_digest", "notification_event", "notification_preference", "opening_balance", "operation",
	"organisation", "organisation_connector", "organisation_setting", "owner_succession", "payee_bank_account",
	"payment_batch_payment", "payment_batch_release", "pd_anonymisation", "pd_breach", "pd_breach_exercise",
	"pd_breach_step", "pd_request", "pd_request_step", "pd_shredding_run", "pd_transfer", "period", "period_close",
	"period_close_check", "period_reopen", "period_variance_explanation", "policy_acknowledgement", "policy_draft",
	"policy_version", "quote_outcome", "recurring_bill", "recurring_bill_version", "recurring_invoice",
	"recurring_invoice_version", "recurring_journal", "recurring_journal_version", "report_export", "restore_drill",
	"review_log_entry", "review_log_review", "risk", "risk_review", "risk_version", "role_assignment", "sales_reminder",
	"security_incident", "security_incident_step", "share_issue", "sod_override", "statement_mapping",
	"statutory_pack_activation", "team_mode", "tick_drill", "user_membership", "withholding_decision",
}

// Excluded are the tables the export leaves out, with why: no secret, key, token, idempotency key or job in flight
// leaves Fyniq, and none of them is the organisation's own record.
var Excluded = map[string]string{
	"idempotency_key":               "saved answers to repeated API requests, kept a day: not the organisation's records",
	"outbox":                        "background jobs in flight: not the organisation's records",
	"authz_refusal":                 "counters of a person's refused requests, behind the lockout: not the organisation's records",
	"rate_limit_window":             "request counters behind the rate limits: not the organisation's records",
	"person_key":                    "key material: each person's data key, wrapped by the platform's key (§11.8)",
	"organisation_connector_secret": "secrets: where the integrations' credentials are kept in the secret store",
	"tenant_directory":              "the platform's directory of organisations: the organisation table holds the organisation's record",
}

// Dropped are the columns an exported table leaves out, with why: a token's hash, and a sealed value, which the export
// writes in clear instead (design D10 [counsel to confirm]).
var Dropped = map[string]map[string]string{
	"member_invitation": {"code_hash": "a token: the hash of an invitation's code"},
	"document_link":     {"token_hash": "a token: the hash of a shared link's secret"},
	"payee_bank_account": {"number_sealed": "sealed: exported in clear as the column number",
		"number_fingerprint": "a keyed hash, under the platform's key, of the number exported in clear"},
	"payment_batch_release": {"file_sealed": "sealed: the file exported in clear under files/payment-batch-releases/"},
}
