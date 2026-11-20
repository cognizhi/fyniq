package reportexport

// The full tenant export's catalogue (FR-MIG-04, design D10): every table of the schema that holds an organisation_id
// is exported or excluded with why, and a column an exported table leaves out says why too. A table in neither list
// stops the export; TestTheTenantExportCataloguesEveryTable fails on it first, so a new table is placed on purpose.

// Exported are the tables the export writes, each as tables/<name>.csv.
var Exported = []string{
	"access_review_attestation", "account", "accountant_review", "accountant_review_journal", "accountant_review_query",
	"accounting_framework", "accounting_policy", "ai_budget", "ai_call", "ai_connection", "ai_connection_version", "ai_conversation", "ai_feedback", "ai_data_policy", "ai_model", "ai_model_list_read", "ai_price", "ai_price_rate", "ai_proposal", "ai_proposal_undo", "ai_proposal_view", "ai_quality_review", "ai_qualification", "ai_reservation", "ai_route", "ai_route_step", "ai_tool_call", "ai_turn", "agent_run", "allocation", "announcement", "announcement_dismissal", "invoicenow_submission", "invoicenow_event", "invoicenow_exclusion", "invoicenow_contingency", "invoicenow_inbound",
	"api_client", "group_entity", "group_membership", "group_settings", "group_account_map", "consolidation_rate", "consolidation_run", "approval_delegation", "approval_policy", "approval_reminder", "approval_request", "approval_signature", "audit_boundary", "automation_rule", "audit_chain", "audit_event",
	"audit_recovery", "audit_recovery_acknowledgement", "audit_request_detail", "balance", "bank_feed",
	"bank_gap", "bank_line", "bank_match", "bank_reconciliation", "bank_rule", "bank_signal", "bank_statement", "compliance_task",
	"bank_transfer", "bill_payment_plan", "brand_version", "break_glass", "break_glass_use", "capture_address",
	"capture_sender", "check_owner", "check_review", "check_run", "check_state", "claim_category", "claim_place",
	"close_checklist_entry", "close_checklist_template", "consent_purpose", "consent_record", "contact",
	"contact_document_template", "control_run", "control_state", "conversion_step", "customer_group",
	"data_inventory_entry", "data_inventory_review", "dimension", "dimension_set", "dimension_value", "document", "document_capture",
	"document_link", "document_number", "document_send", "document_sequence", "document_template",
	"document_template_default", "document_template_version", "document_version", "dpia_approval", "dpia_version",
	"email_event", "email_message", "email_sender", "email_suppression", "equity_step", "evidence",
	"fixed_asset_source", "fx_rate", "gst_registration", "holding_movement", "inbound_file", "inbound_message", "item",
	"item_price", "join_request", "journal_entry", "journal_line", "kb_lint_finding", "kb_note", "kb_page_version", "kb_snapshot", "kb_snapshot_page", "kpi_input", "ledger_boundary",
	"ledger_purged_entry", "ledger_revision", "mcp_call", "mcp_seen", "mcp_user_optin", "member_invitation", "migration_archive", "migration_commit",
	"migration_difference", "migration_file", "migration_mapping", "migration_record", "migration_run",
	"migration_signoff", "migration_step", "money_account", "money_account_source", "notification",
	"notification_digest", "notification_event", "notification_preference", "opening_balance", "operation",
	"organisation", "organisation_connector", "organisation_setting", "owner_succession", "payee_bank_account",
	"payment_batch_payment", "payment_batch_release", "pd_anonymisation", "pd_breach", "pd_breach_exercise",
	"pd_breach_step", "pd_request", "pd_request_step", "pd_shredding_run", "pd_transfer", "period", "period_close",
	"period_close_check", "period_reopen", "period_variance_explanation", "policy_acknowledgement", "policy_draft",
	"policy_version", "project", "project_member", "project_task", "billing_rate", "person_cost_rate", "project_billed_item", "project_milestone", "quote_outcome", "time_entry", "time_timer", "timesheet_submission", "recurring_bill", "recurring_bill_version", "recurring_invoice",
	"recurring_invoice_version", "recurring_journal", "recurring_journal_version", "report_export", "restore_drill", "retention_hold",
	"review_log_entry", "review_log_review", "risk", "risk_review", "risk_version", "role_assignment", "role_assignment_scope", "sales_reminder",
	"security_assessment", "security_finding", "security_finding_step", "security_incident", "security_incident_step", "share_issue", "sod_override", "statement_mapping",
	"statutory_pack_activation", "team_mode", "tick_drill", "user_help_state", "user_membership", "user_preference", "user_saved_item", "record_comment", "withholding_decision", "people_source", "salary_batch", "payroll_arrangement", "payroll_import", "person_projection", "payroll_mapping", "payroll_mapping_entry", "payroll_run_receipt", "payroll_run_refusal", "people_leave",
	"psp_account", "psp_balance", "psp_dispute_credit", "psp_dispute_loss", "psp_event", "psp_payout", "psp_txn", "payment_link", "payment_link_payment", "portal_link", "invoice_dispute", "subscription_contract", "subscription_amendment", "project_revenue_election", "project_revenue_run", "psp_customer", "psp_subscription", "psp_invoice", "psp_credit_note", "psp_charge_invoice", "webhook_endpoint", "webhook_delivery", "webhook_attempt", "integration_task", "mcp_third_party_app",
	"report_definition", "report_schedule", "report_schedule_run", "board_pack", "investor_update", "investor_update_read", "kpi_alert", "dashboard_layout", "cap_table_grant",
	"report_definition", "report_schedule", "report_schedule_run", "board_pack", "investor_update", "investor_update_read", "kpi_alert", "dashboard_layout", "cap_table_grant", "auditor_engagement", "auditor_request", "auditor_sample", "cash_snapshot",
}

// Excluded are the tables the export leaves out, with why: no secret, key, token, idempotency key or job in flight
// leaves Fyniq, and none of them is the organisation's own record.
var Excluded = map[string]string{
	"people_link":                   "the link to the people platform: a link is made again with its approval and a new epoch, never restored (FR-PPL-16); its key and client are not the organisation's records",
	"people_link_key":               "the link's public keys and the running number of what Fyniq sent: credentials of the connection, not the organisation's records (FR-PPL-15)",
	"people_link_ask_key":           "the private key of the link's read-only client for the ask at release: a secret of the connection, never exported (FR-PPL-13, SEC-15)",
	"people_link_secret":            "the link's private keys: a secret of the connection, never exported (FR-PPL-15, SEC-15)",
	"people_link_inbox":             "the events the link received: identities and digests of another system's messages, kept to dedupe them, not the organisation's records (FR-PPL-15)",
	"people_link_replay":            "the runs of Nemura's sequence numbers Fyniq asked it to send again, with when: a record of the connection's repair, not the organisation's records (FR-PPL-08)",
	"people_link_access":            "what Nemura last said of each sign-in identity (an active member there, an approver of pay runs): a hint from another system that decides a switch and a reminder, never the organisation's records or a grant (FR-PPL-02)",
	"salary_batch_line":             "each person's payment line of a salary batch (their pay and, sealed, their account): need-to-know under FR-PPL-17, and Nemura, the payroll's system of record, holds it; the export carries the batch's header",
	"salary_line_event":             "the states of each salary payment by person: need-to-know under FR-PPL-17; Nemura holds the payment states (FR-PPL-12)",
	"salary_release":                "a release of a salary batch and its payment file, sealed: the file holds names and account numbers, so it never leaves in an export (FR-PPL-17, SEC-11)",
	"salary_release_line":           "which person's line went in which release, with its instruction ID: need-to-know under FR-PPL-17",
	"idempotency_key":               "saved answers to repeated API requests, kept a day: not the organisation's records",
	"outbox":                        "background jobs in flight: not the organisation's records",
	"authz_refusal":                 "counters of a person's refused requests, behind the lockout: not the organisation's records",
	"rate_limit_window":             "request counters behind the rate limits: not the organisation's records",
	"person_key":                    "key material: each person's data key, wrapped by the platform's key (§11.8)",
	"organisation_connector_secret": "secrets: where the integrations' credentials are kept in the secret store",
	"tenant_directory":              "the platform's directory of organisations: the organisation table holds the organisation's record",
	"practice_organisation":         "the platform's registry of practice organisations and who holds one: no organisation's own data (FR-UX-36)",
	"ai_grant":                      "tokens: the assistant's delegated read grants, each live for one turn of at most 5 minutes",
}

// Dropped are the columns an exported table leaves out, with why: a token's hash, and a sealed value, which the export
// writes in clear instead (design D10 [counsel to confirm]).
var Dropped = map[string]map[string]string{
	"member_invitation": {"code_hash": "a token: the hash of an invitation's code"},
	"document_link":     {"token_hash": "a token: the hash of a shared link's secret"},
	"portal_link":       {"token_hash": "a token: the hash of a customer portal link's secret"},
	"payee_bank_account": {"number_sealed": "sealed: exported in clear as the column number",
		"number_fingerprint": "a keyed hash, under the platform's key, of the number exported in clear"},
	"payment_batch_release": {"file_sealed": "sealed: the file exported in clear under files/payment-batch-releases/"},
	"psp_event":             {"payload": "sealed: a payment provider's raw webhook payload, which holds customers' names and emails; the export keeps its SHA-256 [counsel to confirm]"},
}
