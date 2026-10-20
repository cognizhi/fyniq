package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fyCodes maps the database's own SQLSTATEs (db/README.md) to the port's stable codes.
var fyCodes = map[string]string{
	"FY001": storage.CodeUnbalanced,
	"FY002": storage.CodeImmutable,
	"FY003": storage.CodePeriodClosed,
	"FY004": storage.CodeAccountNotPost,
	"FY005": storage.CodeAmount,
	"FY006": storage.CodeNoTenant,
	"FY007": storage.CodeShareCapital,
	"FY014": storage.CodeOrganisation,
	"FY021": storage.CodeNoTenant,
	"FY030": storage.CodeNoPeriod,
	"FY031": storage.CodePeriodDates,
	"FY040": storage.CodeAccountFixed,
	"FY041": storage.CodeAccountTree,
	"FY046": storage.CodeRevision,
	"FY050": storage.CodeDocument,
	"FY051": storage.CodeDocumentStatus,
	"FY052": storage.CodeDocument,
	"FY060": storage.CodeAudit,
	"FY061": storage.CodeAudit,
	"FY070": storage.CodeOutbox,
	"FY071": storage.CodeOutbox,
	"FY080": storage.CodeSetup,
	"FY081": storage.CodeMembership,
	"FY082": storage.CodeMembership,
	"FY083": storage.CodeSuperAdmin,
	"FY084": storage.CodeSuperAdmin,
	"FY085": storage.CodeGrant,
	"FY090": storage.CodeSequence,
	"FY091": storage.CodeInvalid,
	"FY092": storage.CodeGrant,
	"FY093": storage.CodeMembership,
	"FY100": storage.CodeApproval,
	"FY101": storage.CodeApproval,
	"FY102": storage.CodeApproval,
	"FY103": storage.CodeApproval,
	"FY104": storage.CodeApproval,
	"FY105": storage.CodeApproval,
	"FY106": storage.CodeApproval,
	"FY130": storage.CodeVersion,
	"FY042": storage.CodeAccountFixed,
	"FY110": storage.CodePack,
	"FY120": storage.CodeEvidence,
	"FY121": storage.CodeEvidence,
	"FY122": storage.CodeEvidence,
	"FY123": storage.CodeEvidence,
	"FY124": storage.CodeEvidence,
}

// constraintCodes maps named constraints to the port's stable codes and plain messages.
var constraintCodes = map[string]struct{ code, message string }{
	"statutory_pack_activation_two_people":  {storage.CodePack, "the person who proposed activating the pack version cannot approve it (SOD-10)"},
	"statutory_pack_activation_version_key": {storage.CodeDuplicate, "the organisation has activated that pack version already"},
	"journal_entry_posting_identity_key":    {storage.CodeDuplicate, "this posting was already made: its posting identity is in use"},
	"journal_entry_reverses_key":            {storage.CodeDuplicate, "that entry was already reversed"},
	"account_code_key":                      {storage.CodeDuplicate, "an account with this code already exists"},
	"outbox_business_key":                   {storage.CodeDuplicate, "this work is already queued: its business key is in use"},
	"user_identity_pkey":                    {storage.CodeDuplicate, "this sign-in identity belongs to a user already"},
	"super_admin_grant_pkey":                {storage.CodeDuplicate, "that code exists already"},
	"user_membership_user_key":              {storage.CodeDuplicate, "the user is a member already"},
	"role_assignment_one_live":              {storage.CodeDuplicate, "the member holds that role already"},
	"role_assignment_role_known":            {storage.CodeMembership, "that is not a role a person can hold (PRD-001 §7.1)"},
	"period_no_overlap":                     {storage.CodePeriodDates, "the period overlaps another period"},
	"journal_line_amount_scale":             {storage.CodeAmount, "an amount has more decimals than its currency allows, or is not a number"},
	"journal_line_base_amount_scale":        {storage.CodeAmount, "a base amount has more decimals than the base currency allows, or is not a number"},
	"journal_line_fx_rate_positive":         {storage.CodeAmount, "an exchange rate is not a positive number"},
	"journal_line_base_currency_rate":       {storage.CodeAmount, "a line in the base currency must use the rate 1"},
	"journal_line_base_amount_rnd_fx":       {storage.CodeAmount, "a base amount is not the amount times the rate, rounded half away from zero (RND-FX)"},
	"journal_line_currency_fkey":            {storage.CodeAmount, "a currency is not one Fyniq knows"},
	"journal_line_account_fkey":             {storage.CodeAccountNotPost, "an account does not exist in this organisation"},
	"organisation_profile_lengths":          {storage.CodeOrganisation, "a text of the organisation's profile is empty or too long"},
	"organisation_uen_format":               {storage.CodeOrganisation, "a UEN is not 9 or 10 capital letters and digits"},
	"organisation_fy_start_month_range":     {storage.CodeOrganisation, "the financial year's start month is not 1 to 12"},
	"organisation_first_year":               {storage.CodeOrganisation, "the first financial year does not end after the books start, on the day before a year starts"},
	"document_number_unique":                {storage.CodeSequence, "that number is already used for this document type"},
	"document_sequence_prefix_format":       {storage.CodeSequence, "the prefix is not 1 to 12 capital letters, digits, slashes or hyphens, starting with a letter or digit"},
	"document_sequence_next_positive":       {storage.CodeSequence, "the next number is not between 1 and 999,999,999,999"},
	"document_sequence_digits_range":        {storage.CodeSequence, "the number of digits is not 1 to 12"},
	"contact_role_known":                    {storage.CodeInvalid, "a contact is a customer, a vendor or both"},
	"contact_lengths":                       {storage.CodeInvalid, "a text of the contact is empty or too long"},
	"contact_country_format":                {storage.CodeInvalid, "the country is not a two-letter ISO 3166 code"},
	"contact_currency_format":               {storage.CodeInvalid, "the currency is not a three-letter ISO 4217 code"},
	"contact_terms_range":                   {storage.CodeInvalid, "the payment terms are not 0 to 365 days"},
	"contact_gst_code_format":               {storage.CodeInvalid, "the default GST code is not 2 to 4 capital letters"},
	"contact_default_account_fkey":          {storage.CodeAccountNotPost, "the default category is not an account of this organisation"},
	"member_invitation_roles_known":         {storage.CodeMembership, "an invitation names 1 to 16 roles Fyniq knows"},
	"member_invitation_owner_never_expires": {storage.CodeMembership, "the Owner's role never expires"},
	"member_invitation_name_length":         {storage.CodeMembership, "the invitation's name is empty or longer than 200 characters"},
	"organisation_setting_version_key":      {storage.CodeVersion, "the setting changed since it was read: another change took its next version"},
	"approval_request_one_pending":          {storage.CodeApproval, "a request for the same record is waiting for approval already"},
	"approval_request_envelope_hashed":      {storage.CodeApproval, "the envelope's hash is not the SHA-256 of the envelope"},
	"approval_request_mfa_known":            {storage.CodeApproval, "the requester's second factor is not one Fyniq knows"},
	"approval_request_mfa_fresh":            {storage.CodeApproval, "the requester's second factor is older than 5 minutes"},
	"approval_request_expiry":               {storage.CodeApproval, "a request expires after it is made, within 30 days"},
	"approval_request_object_known":         {storage.CodeApproval, "that is not an object and operation the approval engine knows"},
	"approval_request_requested_by_fkey":    {storage.CodeApproval, "the requester is not a user of this installation"},
	"approval_signature_once":               {storage.CodeApproval, "this person signed the request already"},
	"approval_signature_hash_sha256":        {storage.CodeApproval, "a signed approval needs the SHA-256 of the envelope it signs"},
	"approval_signature_mfa_known":          {storage.CodeApproval, "a signed approval needs a second factor Fyniq knows"},
	"approval_signature_mfa_fresh":          {storage.CodeApproval, "a signed approval needs a second factor from the last 5 minutes"},
	"approval_signature_signer_fkey":        {storage.CodeApproval, "the signer is not a user of this installation"},
	"approval_signature_reason":             {storage.CodeApproval, "a rejection, and only a rejection, gives its reason"},
	"approval_request_policy_steps_valid":   {storage.CodeApproval, "a policy has one to five steps, each a quorum of 1 to 5 with roles or members"},
	"approval_policy_steps_valid":           {storage.CodeApproval, "a policy has one to five steps, each a quorum of 1 to 5 with roles or members"},
	"approval_policy_object_known":          {storage.CodeApproval, "that object type has no policy an organisation may change"},
	"approval_policy_version_once":          {storage.CodeApproval, "that version of the policy exists already"},
	"approval_policy_request_once":          {storage.CodeApproval, "that request added a version of a policy already"},
	"approval_policy_version_range":         {storage.CodeApproval, "a policy's version is from 1 to 1,000,000"},
	"approval_policy_created_by_fkey":       {storage.CodeApproval, "the requester is not a user of this installation"},
	"approval_policy_request_fkey":          {storage.CodeApproval, "that is not an approval request of this organisation"},
	"organisation_setting_region_fixed":     {storage.CodeInvalid, "the data region is set by the platform and never changes"},
	"organisation_setting_key_format":       {storage.CodeInvalid, "the setting's key is not one Fyniq knows"},
	"organisation_setting_value_length":     {storage.CodeInvalid, "the setting's value is empty or longer than 200 characters"},
	"item_code_key":                         {storage.CodeDuplicate, "an item with this code already exists"},
	"item_code_format":                      {storage.CodeInvalid, "the item code is not 1 to 30 letters, digits, dots, underscores, slashes or hyphens"},
	"item_lengths":                          {storage.CodeInvalid, "a text of the item is empty or too long"},
	"item_type_known":                       {storage.CodeInvalid, "an item is a product or a service"},
	"item_gst_code_format":                  {storage.CodeInvalid, "the GST code is not 2 to 4 capital letters"},
	"item_revenue_account_fkey":             {storage.CodeAccountNotPost, "the revenue account is not an account of this organisation"},
	"item_price_currency_key":               {storage.CodeInvalid, "the item has two prices in one currency"},
	"item_price_currency_format":            {storage.CodeInvalid, "the price's currency is not a three-letter ISO 4217 code"},
	"item_price_unit_price_valid":           {storage.CodeInvalid, "the unit price is not a number of zero or more"},
	"gst_registration_no_overlap":           {storage.CodeOverlap, "the GST registration overlaps another registration's dates"},
	"gst_registration_dates":                {storage.CodeOverlap, "the GST registration is deregistered on or before the date it starts"},
	"gst_registration_basis_known":          {storage.CodeOrganisation, "the GST accounting basis is not one Fyniq offers: invoice basis only (ENG-45)"},
	"gst_registration_frequency_known":      {storage.CodeOrganisation, "the GST return frequency is not quarterly or monthly"},
	"gst_registration_quarter_start":        {storage.CodeOrganisation, "the quarters' start month is not 1, 2 or 3"},
	"gst_registration_number_length":        {storage.CodeOrganisation, "the GST registration number is blank or longer than 20 characters"},
	"evidence_document_fkey":                {storage.CodeInvalid, "the evidence's record is not a document of this organisation"},
	"evidence_staging_key":                  {storage.CodeDuplicate, "that staged file is attached already"},
	"evidence_lengths":                      {storage.CodeInvalid, "a text of the evidence is empty or too long"},
	"evidence_media_type_known":             {storage.CodeInvalid, "the file's type is not one Fyniq accepts"},
	"evidence_size_range":                   {storage.CodeInvalid, "the file is empty or larger than 25 MB"},
	"evidence_hash_format":                  {storage.CodeInvalid, "a file hash is not a SHA-256 in hexadecimal"},
	"evidence_source_known":                 {storage.CodeInvalid, "the evidence's source is not upload, paste, email or API"},
	"evidence_data_class_known":             {storage.CodeInvalid, "the data class is not general, personal, bank, payroll or identity"},
	"evidence_key_format":                   {storage.CodeInvalid, "a store key is not where evidence is kept"},
	"evidence_state_fields":                 {storage.CodeEvidence, "the evidence's state does not match its removal or its committed copy"},
}

// mapError turns database errors into storage errors with stable codes (ENG-23) and "no rows" into
// ports.ErrNotFound. Errors it does not recognise keep their text behind STORAGE-FAILURE-01.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var se *storage.Error
	if errors.As(err, &se) {
		return err
	}
	if notFound(err) {
		return ports.ErrNotFound
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return &storage.Error{Code: storage.CodeStorageFailure, Message: err.Error(), Err: err}
	}
	if code, ok := fyCodes[pg.Code]; ok {
		return &storage.Error{Code: code, Message: pg.Message, Hint: pg.Hint, Err: err}
	}
	if c, ok := constraintCodes[pg.ConstraintName]; ok {
		return &storage.Error{Code: c.code, Message: c.message, Err: err}
	}
	switch pg.Code {
	case "40001", "40P01": // serialization failure, deadlock (ENG-47)
		return &storage.Error{Code: storage.CodeRetry, Message: "the database asked to run the work again",
			Hint: "Retry the request with the same Idempotency-Key", Err: err}
	case "55P03": // lock_timeout
		return &storage.Error{Code: storage.CodeLockTimeout, Message: "another change held the records for too long",
			Hint: "Try again in a moment", Err: err}
	case "42501": // insufficient privilege, including a row outside the tenant's policy
		return &storage.Error{Code: storage.CodeNoTenant, Message: "this role or organisation may not make that change", Err: err}
	case "23503", "23514", "23505", "23P01", "22003", "22P02", "23502":
		return &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%s (%s)", pg.Message, pg.ConstraintName), Err: err}
	}
	return &storage.Error{Code: storage.CodeStorageFailure, Message: pg.Message, Err: err}
}
