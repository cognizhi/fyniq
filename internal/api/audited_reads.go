package api

// Every read of a full identifier and every export is audited (PRD-001 AUD-05; p1-audit-explorer design D3). An
// operation whose success carries a file (a schema with content_base64) or reveals a full identifier writes the audit
// event named here, in the unit of work of its read, so the answer never goes out unless the event was saved. Each
// event is a security event (AUD-06): its IP address and device are kept beside the chain for the security-log
// retention. TestEveryFileAndRevealIsAudited finds such operations in the contract and refuses one missing here, and
// the authorisation matrix checks that each allowed call wrote its event.
var auditedReads = map[string]string{
	"exportAccessReview":        "access_review.exported",
	"exportCapTable":            "report.exported",
	"exportDirectorStatement":   "report.exported",
	"exportDocumentTemplates":   "document_template_bundle.exported",
	"exportFinancialStatement":  "report.exported",
	"exportRelatedParties":      "report.exported",
	"getEvidenceFile":           "evidence.opened",
	"getPaymentBatchFile":       "payment_batch.file_downloaded",
	"getReportExportFile":       "report_export.downloaded",
	"requestReportExport":       "report_export.requested",
	"requestAuditTrailExport":   "report_export.requested",
	"revealClaimantBankAccount": "document.bank_account_revealed",
	"revealMyBankAccount":       "document.bank_account_revealed",
	"revealPayeeBankAccount":    "document.bank_account_revealed",
}

// unauditedFiles are the operations that carry a file but read no data of the books or of a person, each with why.
var unauditedFiles = map[string]string{
	"getBrandFile": "the organisation's own logo or letterhead, the image printed on every document it sends; no data of the books or of a person",
}
