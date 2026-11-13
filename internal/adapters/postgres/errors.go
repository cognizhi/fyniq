package postgres

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

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
	"FY008": storage.CodeAmount,
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
	"FY053": storage.CodeDocumentActor,
	"FY054": storage.CodeDocument,
	"FY055": storage.CodeDocumentEdit,
	"FY056": storage.CodeDocumentEdit,
	"FY057": storage.CodeRecurring,
	"FY058": storage.CodeRecurring,
	"FY152": storage.CodeClose,
	"FY153": storage.CodeClose,
	"FY154": storage.CodeClose,
	"FY155": storage.CodeClose,
	"FY160": storage.CodeAccounting,
	"FY161": storage.CodeAccounting,
	"FY162": storage.CodeVariance,
	"FY163": storage.CodeVariance,
	"FY164": storage.CodeChecklist,
	"FY165": storage.CodeChecklist,
	"FY166": storage.CodeFXRate,
	"FY167": storage.CodeFXRate,
	"FY168": storage.CodeFXRate,
	"FY169": storage.CodeFXRevaluation,
	"FY170": storage.CodeAllocation,
	"FY171": storage.CodeAllocation,
	"FY172": storage.CodeBillRelease,
	"FY173": storage.CodePurchaseOrder,
	"FY174": storage.CodeWithholding,
	"FY175": storage.CodeBillPlan,
	"FY176": storage.CodeTemplate,
	"FY177": storage.CodeTemplate,
	"FY178": storage.CodeTemplate,
	"FY179": storage.CodeVersion,
	"FY180": storage.CodeVersion,
	"FY181": storage.CodeVersion,
	"FY182": storage.CodeVersion,
	"FY183": storage.CodeCaptureName,
	"FY184": storage.CodeVersion,
	"FY185": storage.CodeVersion,
	"FY186": storage.CodeVersion,
	"FY187": storage.CodeVersion,
	"FY188": storage.CodeVersion,
	"FY189": storage.CodeVersion,
	"FY190": storage.CodeVersion,
	"FY191": storage.CodeVersion,
	"FY192": storage.CodeBanner,
	"FY193": storage.CodeQuoteOutcome,
	"FY194": storage.CodeQuoteOutcome,
	"FY195": storage.CodeSend,
	"FY196": storage.CodeSend,
	"FY197": storage.CodeLink,
	"FY198": storage.CodeLink,
	"FY199": storage.CodeRecurring,
	"FY200": storage.CodeRecurring,
	"FY201": storage.CodeScheduled,
	"FY202": storage.CodeScheduled,
	"FY203": storage.CodeReminder,
	"FY204": storage.CodeStatement,
	"FY205": storage.CodeConnector,
	"FY206": storage.CodeConnector,
	"FY207": storage.CodeConnector,
	"FY208": storage.CodeConnector,
	"FY209": storage.CodeConnectorChange,
	"FY210": storage.CodeConnectorChange,
	"FY211": storage.CodeConnectorChange,
	"FY212": storage.CodeConnectorChange,
	"FY213": storage.CodeConnectorChange,
	"FY214": storage.CodeBootstrap,
	"FY215": storage.CodeVersion,
	"FY216": storage.CodeBanking,
	"FY217": storage.CodeBanking,
	"FY218": storage.CodeBanking,
	"FY219": storage.CodeBanking,
	"FY220": storage.CodeBanking,
	"FY221": storage.CodeBanking,
	"FY222": storage.CodeBanking,
	"FY223": storage.CodeBanking,
	"FY224": storage.CodeBanking,
	"FY225": storage.CodeBanking,
	"FY226": storage.CodeBanking,
	"FY227": storage.CodeBillRelease,
	"FY228": storage.CodeBanking,
	"FY229": storage.CodeBanking,
	"FY230": storage.CodeBanking,
	"FY231": storage.CodeBanking,
	"FY232": storage.CodeClaim,
	"FY233": storage.CodeClaim,
	"FY234": storage.CodeClaim,
	"FY235": storage.CodeAsset,
	"FY236": storage.CodeAsset,
	"FY237": storage.CodePayee,
	"FY238": storage.CodePayee,
	"FY239": storage.CodeBatch,
	"FY240": storage.CodeBatch,
	"FY241": storage.CodeBatchHeld,
	"FY242": storage.CodeBatch,
	"FY243": storage.CodeReportExport,
	"FY244": storage.CodeKPIInput,
	"FY245": storage.CodeCheck,
	"FY246": storage.CodeCheckReview,
	"FY247": storage.CodeAccessReview,
	"FY248": storage.CodeMembership,
	"FY249": storage.CodeParty,
	"FY250": storage.CodeParty,
	"FY251": storage.CodeParty,
	"FY252": storage.CodeParty,
	"FY253": storage.CodeParty,
	"FY254": storage.CodeBatch,
	"FY255": storage.CodeShareIssue,
	"FY256": storage.CodeConversionStep,
	"FY257": storage.CodeDimension,
	"FY258": storage.CodeDimensionFixed,
	"FY259": storage.CodeDimensionRequirement,
	"FY261": storage.CodeBreakGlass,
	"FY262": storage.CodeBreakGlass,
	"FY263": storage.CodeBreakGlass,
	"FY264": storage.CodeAccountantReview,
	"FY265": storage.CodeAccountantReview,
	"FY266": storage.CodeAccountantReview,
	"FY267": storage.CodeBanking,
	"FY268": storage.CodeSuccession,
	"FY270": storage.CodeIdentityStepUp,  // a sign-in linked or removed without a recent second factor (000239)
	"FY271": storage.CodeIdentity,        // a sign-in linked already, or the last or current one removed (000239)
	"FY272": storage.CodeSignInMethod,    // a provider's switch that leaves a Super admin no way in, or a bad domain (000240)
	"FY273": storage.CodeVersion,         // a provider's switches changed since they were read (000240)
	"FY275": storage.CodeRecovery,        // recovery codes made without a recent second factor (000242)
	"FY276": storage.CodeRecovery,        // a confirmation of one's own recovery, or by someone cooling off (000242)
	"FY277": storage.CodeRecovery,        // a recovery code or confirmation wrong, used or expired (000242)
	"FY278": storage.CodeCoolingOff,      // a sign-in linked during the cooling-off after a recovery (000242)
	"FY279": storage.CodeRecovery,        // an account recovery changed (000242)
	"FY280": storage.CodeJoin,            // a request to join refused, or a person with an account by now (000244)
	"FY281": storage.CodeJoin,            // a domain another organisation claimed (000244)
	"FY282": storage.CodeVersion,         // a brand version changed or removed (000246)
	"FY283": storage.CodeVersion,         // a brand version saved over a newer one (000246)
	"FY284": storage.CodeImmutable,       // an operation changed or removed (000249)
	"FY285": storage.CodeEquityRecord,    // a holder record with more than an identifier's last 4 characters, or a full NRIC (000250)
	"FY286": storage.CodeEquityRecord,    // a share class whose account is not active share capital (000250)
	"FY287": storage.CodeShareIssue,      // a share issue whose class is not a recorded class of its account (000250)
	"FY288": storage.CodeHolding,         // a holding movement outside its rules, or a share issue with none (000252)
	"FY289": storage.CodeHolding,         // an entry from an equity transaction that no conversion between classes names (000252)
	"FY290": storage.CodeEquityStep,      // an equity transaction's ACRA step outside its rules (000254)
	"FY291": storage.CodeInstrument,      // an instrument or template outside its rules, or its holder unmarked (000255)
	"FY292": storage.CodeInstrument,      // a line on 2700 naming no recorded instrument, or another line naming one (000257)
	"FY293": storage.CodeOption,          // a pool resolution or grant outside its rules, or a grant beyond the pool (000261)
	"FY294": storage.CodeRoundScenario,   // a round scenario's version outside its own inputs (000266)
	"FY295": storage.CodeInvestorRights,  // investor rights outside their rules, or their holder unmarked (000268)
	"FY296": storage.CodeControllerEntry, // a controller entry outside its rules, or its holder unmarked (000268)
	"FY297": storage.CodeAcraExtract,     // an ACRA extract outside its rules (000270)
	"FY298": storage.CodeGSTForecast,     // a GST forecast outside its rules (000272)
	"FY299": storage.CodeStatutoryFiling, // a filing record outside its rules, or a second live one of its item (000274)
	"FY300": storage.CodeForecast,        // a forecast outside its keys, limits or decimal strings (000277)
	"FY301": storage.CodeBudget,          // a budget outside its keys, limits or decimal strings (000279)
	"FY302": storage.CodeFeasibility,     // a feasibility study outside its keys, limits or decimal strings (000283)
	"FY303": storage.CodeAudit,           // a security event's request details changed, or erased early or not by the job (000285)
	"FY304": storage.CodeAudit,           // a purge with no boundary, switched off, too early, or a boundary changed (000291)
	"FY305": storage.CodeLedgerPurge,     // a ledger boundary or purge outside its rules, or a carry-forward outside the purge (000292)
	"FY306": storage.CodeAudit,           // a recovery out of its epoch order, or changed but for its acknowledgement (000293)
	"FY307": storage.CodeCheck,           // a verification's run changed or deleted (000294)
	"FY308": storage.CodeCheck,           // a control's state written other than at version 1, changed out of order, or deleted (000295)
	"FY309": storage.CodeRisk,            // a risk's version out of order, a review of an earlier version or with no later date, or either changed (000296)
	"FY310": storage.CodePolicy,          // a policy draft or version out of order, or anything of the library changed or deleted (000297)
	"FY311": storage.CodeConsent,         // a consent given twice, withdrawn when none stands, for someone the purpose is not for, or any change (000302)
	"FY312": storage.CodeInventory,       // an inventory entry's version out of order, or the inventory changed or deleted (000303)
	"FY313": storage.CodeDPIA,            // an impact assessment's version out of order, an approval of an earlier one, or any change (000304)
	"FY314": storage.CodePDRequest,       // a request's step out of order, an extension late or to an earlier day, or any change (000305)
	"FY315": storage.CodeBreach,          // a breach's step out of order, prescribed data decided not notifiable, or any change (000306)
	"FY316": storage.CodeTransfer,        // a transfer's version out of order, a changed flow, or any change (000307)
	"FY317": storage.CodeShredding,       // a person key changed, or deleted outside its approved run (000308, 000309)
	"FY318": storage.CodeShredding,       // a shredding run out of order, or an anonymisation not recorded first (000309)
	"FY319": storage.CodePrivateEndpoint, // a private endpoint retired, not a private range, a bad pin or no such organisation (000310)
	"FY320": storage.CodeVersion,         // a private endpoint changed since it was read (000310)
	"FY321": storage.CodeImmutable,       // a private endpoint's version changed or removed (000310)
	"FY322": storage.CodeIncident,        // an incident's step out of order, or any change (000316)
	"FY323": storage.CodeRestoreDrill,    // a restore drill changed or removed (000317)
	"FY324": storage.CodeHeartbeatDrill,  // a heartbeat drill changed or removed (000318)
	"FY325": storage.CodeMigration,       // a migration in an organisation with a posted entry, a step changed twice, or anything removed (000325)
	"FY326": storage.CodeMigration,       // a mapping version out of order, a sign-off of anything but the latest version, or any change (000327)
	"FY327": storage.CodeMigration,       // a change once the migration has a commit, or a commit or record out of its rules (000330)
	"FY328": storage.CodeMigration,       // a register decision before the commit, on a closed migration, out of its versions, or removed (000332)
	"FY329": storage.CodeMigration,       // an archive copy before the parallel month's sign-off, of another file, or changed (000338)
	"FY330": storage.CodeAIConnection,    // an AI connection removed, switched on at creation, a version skipped or unrecorded, or rewritten (000343)
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
	"FY107": storage.CodePack,
	"FY108": storage.CodeApproval,
	"FY109": storage.CodeApproval,
	"FY140": storage.CodePasskey,
	"FY141": storage.CodePasskey,
	"FY142": storage.CodeApproval,
	"FY150": storage.CodeApproval,
	"FY151": storage.CodeApproval,
	"FY110": storage.CodePack,
	"FY120": storage.CodeEvidence,
	"FY121": storage.CodeEvidence,
	"FY122": storage.CodeEvidence,
	"FY123": storage.CodeEvidence,
	"FY124": storage.CodeEvidence,
}

// constraintCodes maps named constraints to the port's stable codes and plain messages.
var constraintCodes = map[string]struct{ code, message string }{
	"statutory_pack_activation_two_people":      {storage.CodePack, "the person who proposed activating the pack version cannot approve it (SOD-10)"},
	"statutory_pack_activation_version_key":     {storage.CodeDuplicate, "the organisation has activated that pack version already"},
	"statutory_pack_activation_approval_fkey":   {storage.CodePack, "an activation names an approval request of this organisation"},
	"journal_entry_posting_identity_key":        {storage.CodeDuplicate, "this posting was already made: its posting identity is in use"},
	"migration_run_one":                         {storage.CodeDuplicate, "the organisation has a migration already"},
	"ai_connection_name":                        {storage.CodeDuplicate, "the organisation has an AI connection of that name already"},
	"migration_step_one_validating":             {storage.CodeDuplicate, "a workbook of this step is being validated already"},
	"migration_mapping_version_key":             {storage.CodeVersion, "the mapping changed since it was read"},
	"migration_signoff_mapping":                 {storage.CodeDuplicate, "that version of the mapping is signed already"},
	"migration_commit_one":                      {storage.CodeDuplicate, "the migration is committed already"},
	"migration_record_key":                      {storage.CodeDuplicate, "the record is imported already"},
	"quote_outcome_once":                        {storage.CodeQuoteOutcome, "the quote has an outcome already"},
	"kpi_input_pkey":                            {storage.CodeVersion, "the month's counts were entered meanwhile"},
	"check_state_pkey":                          {storage.CodeVersion, "another run wrote the check meanwhile"},
	"check_owner_pkey":                          {storage.CodeVersion, "the check's owner was named meanwhile"},
	"check_owner_person_fkey":                   {storage.CodeInvalid, "the person named is not a member of the organisation"},
	"sales_reminder_once":                       {storage.CodeReminder, "that reminder was sent or skipped already"},
	"installation_connector_live":               {storage.CodeDuplicate, "that connector is installed already: configure it, or uninstall it first"},
	"organisation_connector_live":               {storage.CodeDuplicate, "that connector is installed already: configure it, or uninstall it first"},
	"installation_connector_no_secret":          {storage.CodeConnector, "a connector's configuration never holds one of its secrets: set the secret on its own"},
	"organisation_connector_no_secret":          {storage.CodeConnector, "a connector's configuration never holds one of its secrets: set the secret on its own"},
	"money_account_account_key":                 {storage.CodeDuplicate, "that chart account has a money account already"},
	"money_account_identifier":                  {storage.CodeDuplicate, "another money account has that identifier in that currency"},
	"money_account_source_from_key":             {storage.CodeBanking, "the money account has a source from that date already"},
	"money_account_source_email_senders":        {storage.CodeBanking, "reports by email need the bank's sender addresses (1 to 10) and their format"},
	"money_account_source_connector_kind":       {storage.CodeBanking, "a connector source names its installed connector, and only a connector source does"},
	"statement_mapping_name_key":                {storage.CodeDuplicate, "a saved mapping has that name already"},
	"bank_line_position_key":                    {storage.CodeDuplicate, "a statement has one line per position"},
	"bank_line_amount_scale":                    {storage.CodeInvalid, "a bank line's amount is not zero and has at most its currency's decimals"},
	"bank_match_live_line":                      {storage.CodeBanking, "a bank line has one live match: undo it before matching it again"},
	"bank_match_live_recorded":                  {storage.CodeBanking, "a receipt or payment recorded by hand is matched by one line at a time"},
	"bank_transfer_from_line":                   {storage.CodeBanking, "a line is the paying side of one transfer"},
	"bank_transfer_to_line":                     {storage.CodeBanking, "a line is the receiving side of one transfer"},
	"bank_reconciliation_period_key":            {storage.CodeDuplicate, "the money account has a reconciliation for that period already"},
	"bank_rule_auto_apply_approved":             {storage.CodeBanking, "a rule auto-applies only under an approved request (FR-BNK-14)"},
	"installation_connector_secret_once":        {storage.CodeConnector, "that secret version is set already"},
	"organisation_connector_secret_once":        {storage.CodeConnector, "that secret version is set already"},
	"installation_change_another_person":        {storage.CodeConnectorChange, "the person who asked for a change cannot decide it (FR-ADM-07)"},
	"customer_group_name_key":                   {storage.CodeDuplicate, "another customer group has that name"},
	"document_send_message_once":                {storage.CodeSend, "the email message carries another send already"},
	"document_link_message_once":                {storage.CodeLink, "the email message carries another link already"},
	"document_link_token_pkey":                  {storage.CodeLink, "the link's token is in use"},
	"journal_entry_reverses_key":                {storage.CodeDuplicate, "that entry was already reversed"},
	"allocation_reversed_once":                  {storage.CodeDuplicate, "that allocation was already reversed"},
	"account_code_key":                          {storage.CodeDuplicate, "an account with this code already exists"},
	"outbox_business_key":                       {storage.CodeDuplicate, "this work is already queued: its business key is in use"},
	"user_identity_pkey":                        {storage.CodeDuplicate, "this sign-in identity belongs to a user already"},
	"super_admin_grant_pkey":                    {storage.CodeDuplicate, "that code exists already"},
	"user_membership_user_key":                  {storage.CodeDuplicate, "the user is a member already"},
	"role_assignment_one_live":                  {storage.CodeDuplicate, "the member holds that role already"},
	"role_assignment_role_known":                {storage.CodeMembership, "that is not a role a person can hold (PRD-001 §7.1)"},
	"period_no_overlap":                         {storage.CodePeriodDates, "the period overlaps another period"},
	"account_revalue_monetary":                  {storage.CodeInvalid, "only an asset or a liability settled in money is revalued at month end (FR-GL-04)"},
	"fx_rate_day_current":                       {storage.CodeDuplicate, "the organisation has a current rate of that currency for that day already: withdraw it first"},
	"fx_rate_rnd_rate":                          {storage.CodeFXRate, "the rate is not its quote under RND-RATE"},
	"journal_line_amount_scale":                 {storage.CodeAmount, "an amount has more decimals than its currency allows, or is not a number"},
	"journal_line_base_amount_scale":            {storage.CodeAmount, "a base amount has more decimals than the base currency allows, or is not a number"},
	"journal_line_fx_rate_positive":             {storage.CodeAmount, "an exchange rate is not a positive number"},
	"journal_line_base_currency_rate":           {storage.CodeAmount, "a line in the base currency must use the rate 1"},
	"journal_line_base_amount_rnd_fx":           {storage.CodeAmount, "a base amount is not the amount times the rate, rounded half away from zero (RND-FX)"},
	"journal_line_currency_fkey":                {storage.CodeAmount, "a currency is not one Fyniq knows"},
	"journal_line_account_fkey":                 {storage.CodeAccountNotPost, "an account does not exist in this organisation"},
	"organisation_profile_lengths":              {storage.CodeOrganisation, "a text of the organisation's profile is empty or too long"},
	"organisation_uen_format":                   {storage.CodeOrganisation, "a UEN is not 9 or 10 capital letters and digits"},
	"organisation_fy_start_month_range":         {storage.CodeOrganisation, "the financial year's start month is not 1 to 12"},
	"organisation_first_year":                   {storage.CodeOrganisation, "the first financial year does not end after the books start, on the day before a year starts"},
	"document_number_unique":                    {storage.CodeSequence, "that number is already used for this document type"},
	"document_sequence_prefix_format":           {storage.CodeSequence, "the prefix is not 1 to 12 capital letters, digits, slashes or hyphens, starting with a letter or digit"},
	"document_sequence_next_positive":           {storage.CodeSequence, "the next number is not between 1 and 999,999,999,999"},
	"document_sequence_digits_range":            {storage.CodeSequence, "the number of digits is not 1 to 12"},
	"contact_role_known":                        {storage.CodeInvalid, "a contact is a customer, a vendor, a director or a shareholder"},
	"contact_member_person":                     {storage.CodeParty, "only a director or shareholder signs in as a member"},
	"contact_member_once":                       {storage.CodeParty, "the member signs in as another director or shareholder already"},
	"contact_member_fkey":                       {storage.CodeParty, "the member is not a member of this organisation"},
	"journal_line_party_fkey":                   {storage.CodeParty, "the person is not a contact of this organisation"},
	"contact_lengths":                           {storage.CodeInvalid, "a text of the contact is empty or too long"},
	"contact_country_format":                    {storage.CodeInvalid, "the country is not a two-letter ISO 3166 code"},
	"contact_currency_format":                   {storage.CodeInvalid, "the currency is not a three-letter ISO 4217 code"},
	"contact_terms_range":                       {storage.CodeInvalid, "the payment terms are not 0 to 365 days"},
	"contact_gst_code_format":                   {storage.CodeInvalid, "the default GST code is not 2 to 4 capital letters"},
	"contact_default_account_fkey":              {storage.CodeAccountNotPost, "the default category is not an account of this organisation"},
	"member_invitation_roles_known":             {storage.CodeMembership, "an invitation names 1 to 16 roles Fyniq knows"},
	"member_invitation_owner_never_expires":     {storage.CodeMembership, "the Owner's role never expires"},
	"member_invitation_name_length":             {storage.CodeMembership, "the invitation's name is empty or longer than 200 characters"},
	"organisation_setting_version_key":          {storage.CodeVersion, "the setting changed since it was read: another change took its next version"},
	"approval_request_one_pending":              {storage.CodeApproval, "a request for the same record is waiting for approval already"},
	"approval_request_envelope_hashed":          {storage.CodeApproval, "the envelope's hash is not the SHA-256 of the envelope"},
	"approval_request_mfa_known":                {storage.CodeApproval, "the requester's second factor is not one Fyniq knows"},
	"approval_request_mfa_fresh":                {storage.CodeApproval, "the requester's second factor is older than 5 minutes"},
	"approval_request_expiry":                   {storage.CodeApproval, "a request expires after it is made, within 30 days"},
	"approval_request_object_known":             {storage.CodeApproval, "that is not an object and operation the approval engine knows"},
	"approval_request_requested_by_fkey":        {storage.CodeApproval, "the requester is not a user of this installation"},
	"approval_signature_once":                   {storage.CodeApproval, "this person signed the request already"},
	"approval_signature_hash_sha256":            {storage.CodeApproval, "a signed approval needs the SHA-256 of the envelope it signs"},
	"approval_signature_mfa_known":              {storage.CodeApproval, "a signed approval needs a second factor Fyniq knows"},
	"approval_signature_mfa_fresh":              {storage.CodeApproval, "a signed approval needs a second factor from the last 5 minutes"},
	"approval_signature_signer_fkey":            {storage.CodeApproval, "the signer is not a user of this installation"},
	"approval_signature_reason":                 {storage.CodeApproval, "a rejection, and only a rejection, gives its reason"},
	"approval_request_policy_steps_valid":       {storage.CodeApproval, "a policy has one to five steps, each a quorum of 1 to 5 with roles or members"},
	"approval_policy_steps_valid":               {storage.CodeApproval, "a policy has one to five steps, each a quorum of 1 to 5 with roles or members"},
	"approval_policy_object_known":              {storage.CodeApproval, "that object type has no policy an organisation may change"},
	"approval_policy_version_once":              {storage.CodeApproval, "that version of the policy exists already"},
	"approval_policy_request_once":              {storage.CodeApproval, "that request added a version of a policy already"},
	"approval_policy_version_range":             {storage.CodeApproval, "a policy's version is from 1 to 1,000,000"},
	"approval_policy_created_by_fkey":           {storage.CodeApproval, "the requester is not a user of this installation"},
	"approval_policy_request_fkey":              {storage.CodeApproval, "that is not an approval request of this organisation"},
	"webauthn_credential_once":                  {storage.CodePasskey, "that passkey is registered already"},
	"webauthn_credential_user_fkey":             {storage.CodePasskey, "the passkey's user is not a user of this installation"},
	"webauthn_credential_id_length":             {storage.CodePasskey, "a passkey's ID is 16 to 1,023 bytes"},
	"webauthn_credential_key_length":            {storage.CodePasskey, "a passkey's public key is 16 to 4,096 bytes"},
	"webauthn_credential_count_range":           {storage.CodePasskey, "a passkey's signature counter is a 32-bit count"},
	"webauthn_credential_aaguid_length":         {storage.CodePasskey, "an authenticator's AAGUID is 16 bytes"},
	"webauthn_credential_transports_few":        {storage.CodePasskey, "a passkey names at most 8 transports"},
	"webauthn_credential_backup_flags":          {storage.CodePasskey, "a passkey that cannot be backed up is never backed up"},
	"webauthn_credential_name_length":           {storage.CodePasskey, "a passkey's name is 1 to 100 characters"},
	"webauthn_credential_used_after_created":    {storage.CodePasskey, "a passkey is used after it is registered"},
	"webauthn_credential_revoked_after_created": {storage.CodePasskey, "a passkey is revoked after it is registered"},
	"webauthn_challenge_once":                   {storage.CodePasskey, "that challenge was issued already"},
	"webauthn_challenge_user_fkey":              {storage.CodePasskey, "the challenge's user is not a user of this installation"},
	"webauthn_challenge_session_fkey":           {storage.CodePasskey, "the challenge's session does not exist"},
	"webauthn_challenge_answer_fkey":            {storage.CodePasskey, "the challenge's answer is not a passkey"},
	"webauthn_challenge_purpose_known":          {storage.CodePasskey, "a challenge registers a passkey or steps a session up"},
	"webauthn_challenge_random":                 {storage.CodePasskey, "a challenge is 32 random bytes"},
	"webauthn_challenge_lifetime":               {storage.CodePasskey, "a challenge expires within 5 minutes"},
	"webauthn_challenge_taken_in_time":          {storage.CodePasskey, "a challenge is taken before it expires"},
	"webauthn_challenge_answered_once_taken":    {storage.CodePasskey, "a challenge is answered once it is taken"},
	"approval_signature_passkey_shape":          {storage.CodeApproval, "a passkey-signed approval keeps all of its assertion, any other none"},
	"user_session_user_agent_length":            {storage.CodeInvalid, "a session keeps at most 300 characters of its browser's User-Agent"},
	"user_session_step_up_method":               {storage.CodeInvalid, "a session's step-up is at sign-in, or by a passkey after it began"},
	"review_log_entry_approval_once":            {storage.CodeApproval, "that approval is logged already"},
	"review_log_entry_approval_fkey":            {storage.CodeApproval, "that is not an approval request of this organisation"},
	"review_log_entry_actor_fkey":               {storage.CodeApproval, "the actor is not a user of this installation"},
	"review_log_entry_kind_known":               {storage.CodeApproval, "a review log entry is an approved change or a founder-bootstrap change"},
	"review_log_entry_approval_kind":            {storage.CodeApproval, "an approved change, and only one, names its approval"},
	"review_log_entry_texts":                    {storage.CodeApproval, "a review log entry's texts are empty or too long"},
	"review_log_review_once":                    {storage.CodeApproval, "that entry is reviewed already"},
	"review_log_review_entry_fkey":              {storage.CodeApproval, "that is not an entry of this organisation's review log"},
	"review_log_review_by_fkey":                 {storage.CodeApproval, "the reviewer is not a user of this installation"},
	"review_log_review_note_length":             {storage.CodeApproval, "a review's note is blank or longer than 500 characters"},
	"team_mode_version_once":                    {storage.CodeApproval, "that version of the team mode exists already"},
	"team_mode_request_once":                    {storage.CodeApproval, "that request added a version of the team mode already"},
	"team_mode_request_fkey":                    {storage.CodeApproval, "that is not an approval request of this organisation"},
	"team_mode_created_by_fkey":                 {storage.CodeApproval, "the requester is not a user of this installation"},
	"team_mode_mode_known":                      {storage.CodeApproval, "the team mode is founder, standard or enterprise"},
	"team_mode_version_range":                   {storage.CodeApproval, "the team mode's version is from 1 to 1,000,000"},
	"sod_override_team_mode_fkey":               {storage.CodeApproval, "an override is for a version of the team mode"},
	"sod_override_request_fkey":                 {storage.CodeApproval, "that is not an approval request of this organisation"},
	"sod_override_user_fkey":                    {storage.CodeApproval, "the member is not a user of this installation"},
	"sod_override_created_by_fkey":              {storage.CodeApproval, "the requester is not a user of this installation"},
	"sod_override_conflict_known":               {storage.CodeApproval, "an override names a conflict from SOD-1 to SOD-10"},
	"sod_override_reason_length":                {storage.CodeApproval, "an override gives its reason, of at most 500 characters"},
	"sod_override_once":                         {storage.CodeApproval, "that request overrode that conflict already"},
	"organisation_setting_region_fixed":         {storage.CodeInvalid, "the data region is set by the platform and never changes"},
	"organisation_setting_key_format":           {storage.CodeInvalid, "the setting's key is not one Fyniq knows"},
	"organisation_setting_value_length":         {storage.CodeInvalid, "the setting's value is empty or longer than 200 characters"},
	"item_code_key":                             {storage.CodeDuplicate, "an item with this code already exists"},
	"item_code_format":                          {storage.CodeInvalid, "the item code is not 1 to 30 letters, digits, dots, underscores, slashes or hyphens"},
	"item_lengths":                              {storage.CodeInvalid, "a text of the item is empty or too long"},
	"item_type_known":                           {storage.CodeInvalid, "an item is a product or a service"},
	"item_gst_code_format":                      {storage.CodeInvalid, "the GST code is not 2 to 4 capital letters"},
	"item_revenue_account_fkey":                 {storage.CodeAccountNotPost, "the revenue account is not an account of this organisation"},
	"item_price_currency_key":                   {storage.CodeInvalid, "the item has two prices in one currency"},
	"item_price_currency_format":                {storage.CodeInvalid, "the price's currency is not a three-letter ISO 4217 code"},
	"item_price_unit_price_valid":               {storage.CodeInvalid, "the unit price is not a number of zero or more"},
	"gst_registration_no_overlap":               {storage.CodeOverlap, "the GST registration overlaps another registration's dates"},
	"gst_registration_dates":                    {storage.CodeOverlap, "the GST registration is deregistered on or before the date it starts"},
	"gst_registration_basis_known":              {storage.CodeOrganisation, "the GST accounting basis is not one Fyniq offers: invoice basis only (ENG-45)"},
	"gst_registration_frequency_known":          {storage.CodeOrganisation, "the GST return frequency is not quarterly or monthly"},
	"gst_registration_quarter_start":            {storage.CodeOrganisation, "the quarters' start month is not 1, 2 or 3"},
	"gst_registration_number_length":            {storage.CodeOrganisation, "the GST registration number is blank or longer than 20 characters"},
	"evidence_document_fkey":                    {storage.CodeInvalid, "the evidence's record is not a document of this organisation"},
	"evidence_staging_key":                      {storage.CodeDuplicate, "that staged file is attached already"},
	"opening_balance_pkey":                      {storage.CodeDuplicate, "the organisation has its opening balances already: save a new version of them"},
	"fixed_asset_source_line_once":              {storage.CodeDuplicate, "the line is in the asset register already: open the asset registered from it"},
	"fixed_asset_source_line_live":              {storage.CodeDuplicate, "the line is in the asset register already: open the asset registered from it, or void that asset first"},
	"fixed_asset_source_asset_once":             {storage.CodeDuplicate, "the asset has its source already: an asset comes from one line"},
	"payee_bank_account_in_force":               {storage.CodeDuplicate, "the vendor has an account in force in that currency already: a change replaces it"},
	"payee_bank_account_version_once":           {storage.CodeDuplicate, "that version of the change has its account already"},
	"evidence_lengths":                          {storage.CodeInvalid, "a text of the evidence is empty or too long"},
	"evidence_media_type_known":                 {storage.CodeInvalid, "the file's type is not one Fyniq accepts"},
	"evidence_size_range":                       {storage.CodeInvalid, "the file is empty or larger than 25 MB"},
	"evidence_hash_format":                      {storage.CodeInvalid, "a file hash is not a SHA-256 in hexadecimal"},
	"evidence_source_known":                     {storage.CodeInvalid, "the evidence's source is not upload, paste, email or API"},
	"evidence_data_class_known":                 {storage.CodeInvalid, "the data class is not general, personal, bank, payroll or identity"},
	"evidence_key_format":                       {storage.CodeInvalid, "a store key is not where evidence is kept"},
	"evidence_state_fields":                     {storage.CodeEvidence, "the evidence's state does not match its removal or its committed copy"},

	// The dimensions and their values (FR-DIM-01): the API checks each first, so these answer only a race between two writes.
	"dimension_slot_key":       {storage.CodeDuplicate, "the organisation has that dimension already: Department and Project once each, a custom one in each place once"},
	"dimension_name_key":       {storage.CodeDuplicate, "another dimension has that name"},
	"dimension_value_code_key": {storage.CodeDuplicate, "the dimension has a value with that code already"},

	// The access review's attestations (FR-CTL-06).
	"access_review_attestation_quarter_key":       {storage.CodeDuplicate, "that quarter's access review is attested already"},
	"access_review_attestation_after_its_quarter": {storage.CodeAccessReview, "a quarter's access review is attested only once the quarter has ended"},
}

// entryID is how the database names an entry in INV-01's refusals ("Journal entry <id> does not balance").
var entryID = regexp.MustCompile(`^Journal entry \S+ `)

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
	if pg.Code == "FY001" { // INV-01, said without the entry's internal ID (ENG-23)
		return &storage.Error{Code: storage.CodeUnbalanced, Message: entryID.ReplaceAllString(pg.Message, "the entry "),
			Hint: "Debits and credits must be equal (INV-01). An amount in another currency must be worth at least a cent in " +
				"the base currency at its rate, or its lines may round to nothing", Err: err}
	}
	if code, ok := fyCodes[pg.Code]; ok {
		return &storage.Error{Code: code, Message: pg.Message, Hint: pg.Hint, Err: err}
	}
	// A period's balance is NUMERIC(20,4) too (§11.3): a posting that would take one past 16 digits before the decimal
	// point is refused whole, never truncated, by the trigger that keeps the balances (INV-08).
	if pg.Code == "22003" && strings.Contains(pg.Where, "journal_line_balances") {
		return &storage.Error{Code: storage.CodeAmount,
			Message: "an account's balance for the period would have more than 16 digits before the decimal point, more than the books hold",
			Hint:    "Check the amounts: one this large is almost always a typing error", Err: err}
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
