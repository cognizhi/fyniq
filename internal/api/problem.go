package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/domain/accounting"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/contact"
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/customercredit"
	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/item"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/member"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/purchaseorder"
	"github.com/cognizhi/fyniq/internal/domain/quote"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Problem is an RFC 9457 problem details object (FR-INT-07): a stable code (the same codes as
// ENG-23), a plain-English detail, the fix, and every invalid field at once.
type Problem struct {
	Type     string         `json:"type" format:"uri" doc:"A URI naming the problem; one per code." example:"urn:fyniq:problem:JOURNAL-02"`
	Title    string         `json:"title" doc:"A short summary of the problem type; it never changes between occurrences." example:"The journal is not a draft"`
	Status   int            `json:"status" doc:"The HTTP status code." example:"409"`
	Detail   string         `json:"detail" doc:"What went wrong this time, in plain English." example:"The journal is posted, not a draft."`
	Code     string         `json:"code" doc:"The stable error code (ENG-23)." example:"JOURNAL-02"`
	Fix      string         `json:"fix,omitempty" doc:"How to fix it." example:"Only a draft can be posted; reverse a posted journal instead."`
	Instance string         `json:"instance,omitempty" format:"uri" doc:"The request this occurrence belongs to; quote it to support." example:"urn:fyniq:request:01JA2B3C4D5E6F7G8H9J0K1M2N"`
	Errors   []FieldProblem `json:"errors,omitempty" doc:"Every invalid field, all at once."`

	headers http.Header
}

// FieldProblem is one invalid field.
type FieldProblem struct {
	Code    string `json:"code" doc:"The stable error code." example:"LEDGER-04"`
	Field   string `json:"field" doc:"The JSON path of the field: in the request body (body.…), the query (query.…), a header (header.…), or the saved record a command acts on." example:"lines[0].account_id"`
	Message string `json:"message" doc:"What is wrong with it." example:"the account does not exist in this organisation"`
	Fix     string `json:"fix" doc:"How to fix it." example:"Choose an account from the chart of accounts"`
}

func (p *Problem) Error() string                { return p.Code + ": " + p.Detail }
func (p *Problem) GetStatus() int               { return p.Status }
func (p *Problem) GetHeaders() http.Header      { return p.headers }
func (p *Problem) ContentType(ct string) string { return problemContentType(ct) }

func problemContentType(ct string) string {
	if ct == "application/json" {
		return "application/problem+json"
	}
	return ct
}

// LedgerCodes are the engine's validation codes, and the cascade's FX-01 for a line it finds no rate for (ENG-40):
// posting a journal can return any of them.
var LedgerCodes = []string{ledger.CodeNoDate, ledger.CodeTooFewLines, ledger.CodeZeroLine, ledger.CodeUnknownAccount,
	ledger.CodeGroupAccount, ledger.CodeInactiveAccount, ledger.CodeMissingRate, ledger.CodeWrongRate, ledger.CodeUnbalancedBase,
	ledger.CodeUnbalancedCur, ledger.CodeTooLong, ledger.CodeNotPosted, ledger.CodeRateOnBase, ledger.CodeBaseRange,
	ledger.CodeShareCapital, ledger.CodeReversalDate, ledger.CodeForeignOpening, fx.CodeNoRate}

// The API's own codes. The domain's codes (JOURNAL-*, LEDGER-*, MONEY-*, STORAGE-*) pass through.
const (
	CodeUnauthenticated   = "API-AUTH-01"        // no valid credentials
	CodeCrossSite         = "API-AUTH-02"        // a state change the session cookie authenticated, sent from another site
	CodeForbidden         = "API-AUTHZ-01"       // the caller's roles hold no right for the operation
	CodeNoOrganisation    = "API-AUTHZ-02"       // the caller is not in an organisation
	CodeNoPolicy          = "API-AUTHZ-03"       // the operation has no policy: refused (SEC-02)
	CodeMalformed         = "API-INPUT-01"       // the request cannot be read
	CodeInvalid           = "API-INPUT-02"       // a field is invalid
	CodeTooLarge          = "API-INPUT-03"       // the body is too large
	CodeMediaType         = "API-INPUT-04"       // the body is not JSON
	CodeCursor            = "API-CURSOR-01"      // the cursor is not one this list gave
	CodeNotFound          = "API-NOT-FOUND-01"   // no such record in this organisation
	CodeStale             = "API-IF-MATCH-01"    // If-Match names an older version (412)
	CodeIfMatchRequired   = "API-IF-MATCH-02"    // an update without If-Match (428)
	CodeIdempotencyReused = "API-IDEMPOTENCY-01" // the key came with a different request (422)
	CodeIdempotencyBusy   = "API-IDEMPOTENCY-02" // the first request with the key has not finished (409)
	CodeIdempotencyOld    = "API-IDEMPOTENCY-03" // the key expired (422)
	CodeFailure           = "API-FAILURE-01"     // something went wrong on our side
	CodeUnavailable       = "API-UNAVAILABLE-01" // the service is not configured for this
	CodeSessionRequired   = "API-AUTHZ-04"       // the operation needs a person signed in, not an OAuth client or the AI
	CodeSetupRefused      = "API-SETUP-01"       // the setup code is not valid, or setup is done
	CodeStepUpRequired    = "API-AUTHZ-05"       // the operation needs step-up authentication in the last 5 minutes
	CodeNotSuperAdmin     = "API-AUTHZ-06"       // the operation is for a Super admin
	CodeCodeRefused       = "API-SUPERADMIN-01"  // an invitation, recovery or confirmation code is not valid, used or expired
	CodeSuperAdminRefused = "API-SUPERADMIN-02"  // at most two Super admins, never none; nobody confirms their own recovery
	CodeRateLimited       = "API-RATE-01"        // the client's requests this minute reached its limit (429)
	CodeSecondFactor      = "API-AUTHZ-07"       // the session passed no second factor (SEC-01)
	CodePasskeyRefused    = "API-PASSKEY-01"     // #nosec G101 -- a problem code's name, not a credential: the passkey's answer failed a check
	CodeNoPasskey         = "API-PASSKEY-02"     // #nosec G101 -- a problem code's name, not a credential: no passkey to step up with
	CodeChallenge         = "API-PASSKEY-03"     // the challenge is unknown, another session's, used or expired
)

// codeFacts are the status and title of every code the API returns, the catalogue the OpenAPI
// document lists per operation.
var codeFacts = map[string]struct {
	Status int
	Title  string
}{
	CodeUnauthenticated:           {401, "Sign-in required"},
	CodeCrossSite:                 {403, "Sent from another site"},
	CodeForbidden:                 {403, "Not allowed for your roles"},
	CodeNoOrganisation:            {403, "No organisation"},
	CodeNoPolicy:                  {403, "Operation not authorised"},
	CodeMalformed:                 {400, "The request cannot be read"},
	CodeInvalid:                   {422, "A field is not valid"},
	CodeTooLarge:                  {413, "The request is too large"},
	CodeMediaType:                 {415, "The request is not JSON"},
	CodeCursor:                    {422, "The cursor is not valid"},
	CodeNotFound:                  {404, "Not found"},
	CodeStale:                     {412, "The record changed"},
	CodeIfMatchRequired:           {428, "If-Match required"},
	CodeIdempotencyReused:         {422, "The Idempotency-Key was used for another request"},
	CodeIdempotencyBusy:           {409, "The first request with this Idempotency-Key is still running"},
	CodeIdempotencyOld:            {422, "The Idempotency-Key expired"},
	CodeFailure:                   {500, "Something went wrong"},
	CodeUnavailable:               {503, "Not available here"},
	CodeSessionRequired:           {403, "A person must be signed in"},
	CodeSetupRefused:              {409, "Setup refused"},
	CodeStepUpRequired:            {403, "Step-up required"},
	CodeNotSuperAdmin:             {403, "Not a Super admin"},
	CodeCodeRefused:               {409, "Code refused"},
	CodeSuperAdminRefused:         {409, "Super admin change refused"},
	CodeRateLimited:               {429, "Too many requests"},
	CodeSecondFactor:              {403, "A second factor is required"},
	CodePasskeyRefused:            {422, "The passkey's answer was refused"},
	CodeNoPasskey:                 {409, "You have no passkey"},
	CodeChallenge:                 {409, "The challenge cannot be used"},
	storage.CodePasskey:           {409, "The passkey or its challenge cannot be used that way"},
	organisation.CodeText:         {422, "A text is blank or too long"},
	organisation.CodeUEN:          {422, "Not a UEN"},
	organisation.CodeEmail:        {422, "Not an email address"},
	organisation.CodeTimeZone:     {422, "Not a time zone"},
	organisation.CodeYearStart:    {422, "Not a month"},
	organisation.CodeFirstYear:    {422, "The first financial year does not fit"},
	organisation.CodeBank:         {422, "Not a bank account number"},
	organisation.CodePrefix:       {422, "Not a prefix"},
	organisation.CodeNext:         {422, "The next number does not fit"},
	organisation.CodeDigits:       {422, "Not a number of digits"},
	storage.CodeSequence:          {409, "The number sequence cannot be set that way"},
	CodeChartLoaded:               {409, "The organisation has accounts already"},
	storage.CodePeriodDates:       {409, "A period overlaps another"},
	storage.CodePeriodState:       {409, "The period is already closed, or not closed"},
	period.CodeNoYear:             {409, "The financial years are not set"},
	period.CodeTooFar:             {409, "The year starts too far ahead"},
	period.CodeNotNext:            {409, "Not the next year to open"},
	period.CodeYearsOpen:          {409, "The financial years are fixed"},
	closing.CodeNotOpen:           {409, "The period is not open"},
	balance.CodeTooManyLines:      {422, "Too many lines for one report"},
	balance.CodeCrossesYear:       {422, "The range crosses a financial year"},
	closing.CodeStale:             {409, "The checks are stale"},
	closing.CodeOrder:             {409, "An earlier period is open"},
	closing.CodeNotClean:          {409, "The checks refused the close"},
	closing.CodeSelfAttest:        {403, "Another founder attests the close"},
	closing.CodeWrongCheck:        {409, "Not the checks of this period"},
	closing.CodeNotClosed:         {409, "The period is not closed"},
	closing.CodeLater:             {409, "A later period is closed"},
	closing.CodeNoVariance:        {409, "The account has no variance to explain"},
	closing.CodeMoved:             {409, "The totals changed since they were read"},
	closing.CodeTemplate:          {422, "The checklist template is not valid"},
	closing.CodeNoTask:            {409, "No such task on the checklist"},
	closing.CodeSkipReason:        {422, "Skipping a task needs a reason"},
	storage.CodeClose:             {409, "The close is not attested that way"},
	storage.CodeVariance:          {409, "The variance is not explained that way"},
	storage.CodeChecklist:         {409, "The checklist does not change that way"},
	storage.CodeFXRate:            {409, "The exchange rate does not change that way"},
	storage.CodeFXRevaluation:     {409, "The scheduler cannot save that revaluation"},
	storage.CodeTemplate:          {409, "The template does not change that way"},
	fx.CodeNoRate:                 {422, "No exchange rate for this currency"},
	fx.CodeEntry:                  {422, "The exchange rate is not valid"},
	fx.CodeWithdrawn:              {409, "The rate is withdrawn already"},
	fx.CodeReason:                 {422, "Withdrawing a rate needs a reason"},
	fx.CodeNotOpen:                {409, "The period is not open"},
	fx.CodeNotEnded:               {409, "The period has not ended"},
	fx.CodeUnrealised:             {409, "No account takes unrealised FX"},
	contact.CodeName:              {422, "The legal name is not valid"},
	contact.CodeRole:              {422, "Neither a customer nor a vendor"},
	contact.CodeEmail:             {422, "Not an email address"},
	contact.CodeCountry:           {422, "Not a country code"},
	contact.CodeCurrency:          {422, "Not a currency"},
	contact.CodeTerms:             {422, "Payment terms out of range"},
	contact.CodeGSTCode:           {422, "Not a GST code"},
	contact.CodePeppol:            {422, "Not a Peppol participant ID"},
	contact.CodeText:              {422, "A text is too long"},
	contact.CodeCategory:          {422, "Not an account that takes postings"},
	contact.CodeDiscount:          {422, "The early-payment discount terms are not a percentage and a number of days"},
	bill.CodePlan:                 {422, "The payment date cannot be planned"},
	member.CodeRole:               {422, "Not a role a member can hold"},
	member.CodeName:               {422, "The name is not valid"},
	member.CodeRoles:              {422, "No role, or a role named twice"},
	member.CodeExpiry:             {422, "Not an expiry for these roles"},
	member.CodeLastOwner:          {409, "The last Owner keeps the role"},
	member.CodeHeld:               {409, "The member holds that role already, or does not hold it"},
	settings.CodeValue:            {422, "Not a value for this setting"},
	settings.CodeBackdated:        {422, "A setting never changes in the past"},
	settings.CodeShortened:        {422, "The setting can only lengthen"},
	settings.CodeFixed:            {409, "Set by the platform"},
	CodeNoEmailAddress:            {409, "The organisation has no email address"},
	CodeSenderAddress:             {422, "Not a sender address"},
	CodeSenderDomain:              {422, "Not a domain email is sent from"},
	CodeReleased:                  {409, "Released already"},
	CodeCaptureName:               {422, "Not an address's name"},
	CodeCaptureReserved:           {422, "A reserved name"},
	CodeCaptureTaken:              {409, "The name belongs to another organisation"},
	CodeCaptureNoUser:             {422, "Not a person with an address of their own"},
	CodeSenderNotAddress:          {422, "Not one email address"},
	CodeNotifyLocked:              {409, "A security notice is always on"},
	CodeNotifyTime:                {422, "Not a time of day"},
	CodeNotifyType:                {404, "No such notification type"},
	CodeBannerStays:               {409, "A banner that stays until it ends"},
	CodeBannerNone:                {404, "No such banner showing"},
	CodeBannerEnded:               {409, "The announcement has ended"},
	approval.CodeOwnRequest:       {403, "You asked for it, so another person approves it"},
	approval.CodeNotPending:       {409, "The request was decided already"},
	approval.CodeExpired:          {409, "The request expired"},
	approval.CodeChanged:          {409, "The envelope differs: nothing was changed"},
	approval.CodeNotApprover:      {403, "You hold no approval right on it"},
	approval.CodePending:          {409, "A change to it is waiting for approval already"},
	approval.CodeReason:           {422, "A rejection needs its reason"},
	approval.CodeSteps:            {422, "The policy's steps cannot be signed"},
	approval.CodeFixed:            {409, "This approval policy is fixed"},
	approval.CodeAuthority:        {409, "An earlier approver no longer holds the right: nothing was changed"},
	approval.CodeNotYourStep:      {403, "The request's current step needs someone else"},
	approval.CodeNotYours:         {403, "Only the person who asked for it withdraws it"},
	approval.CodeReviewOwn:        {403, "Another person reviews what you did"},
	approval.CodeReviewed:         {409, "Reviewed already"},
	pack.CodeTable:                {409, "This release cannot read that pack version"},
	pack.CodePinned:               {409, "A pinned statutory test fails"},
	pack.CodeApproval:             {409, "Activating it needs a second approver"},
	pack.CodeActive:               {409, "That pack version is activated already"},
	storage.CodeApproval:          {409, "The approval cannot be made that way"},
	team.CodeConflicts:            {409, "Conflicts of duties block the switch"},
	team.CodeConflict:             {409, "The roles would conflict"},
	team.CodeUnavailable:          {422, "That team mode is not available"},
	team.CodeAction:               {403, "Your roles conflict for this action"},
	team.CodeSame:                 {409, "The organisation is in that mode already"},
	team.CodeOverride:             {422, "The override is not valid"},
	settings.CodeValueOrReset:     {422, "Send a value or a reset"},
	item.CodeCode:                 {422, "Not an item code"},
	item.CodeName:                 {422, "The item's name is not valid"},
	item.CodeType:                 {422, "Neither a product nor a service"},
	item.CodeText:                 {422, "A text is too long"},
	item.CodeCurrency:             {422, "Not a currency, or priced twice"},
	item.CodePrice:                {422, "Not a unit price"},
	item.CodeGSTCode:              {422, "Not a GST code"},
	item.CodeAccount:              {422, "Not a revenue account that takes postings"},
	chart.CodeAccountCode:         {422, "Not an account code"},
	chart.CodeText:                {422, "A text is blank or too long"},
	chart.CodeType:                {422, "Not an account type"},
	chart.CodeFunction:            {422, "Not a function"},
	chart.CodeGSTCode:             {422, "Not a GST code"},
	chart.CodeCurrency:            {422, "Not a currency"},
	chart.CodeRole:                {422, "Not a system role"},
	chart.CodeProtected:           {422, "Changes only with an approval"},
	chart.CodeSystem:              {422, "A system account stays active"},
	chart.CodeRevalue:             {422, "The account is not revalued at month end"},
	storage.CodeAccountFixed:      {409, "The account cannot change that way"},
	storage.CodeMembership:        {409, "The membership or role cannot change that way"},
	storage.CodeAccountTree:       {409, "The account cannot sit there"},
	storage.CodeVersion:           {412, "The record changed since you read it"},
	storage.CodeOrganisation:      {422, "The organisation's details are not valid"},
	storage.CodeOverlap:           {409, "The dates overlap another version"},
	gst.CodeDates:                 {422, "The registration's dates do not fit"},
	gst.CodeNumber:                {422, "The GST registration number is not valid"},
	gst.CodeFrequency:             {422, "Not a return frequency"},
	gst.CodeQuarter:               {422, "Not a quarters' start month"},
	gst.CodeBasis:                 {422, "An accounting basis Fyniq does not offer"},
	journal.CodeNotJournal:        {404, "Not a manual journal"},
	journal.CodeNotDraft:          {409, "The journal is not a draft"},
	journal.CodeNotPosted:         {409, "The journal is not posted"},
	journal.CodeBadDraft:          {422, "The saved journal cannot be read"},
	journal.CodeReverseOn:         {422, "The automatic reversal is not after the journal"},
	journal.CodeRecurringSchedule: {422, "The schedule has no occurrence"},
	journal.CodeRecurringPaused:   {409, "The recurring journal is not in that state"},
	storage.CodeRecurring:         {409, "Not the recurring journal's next occurrence"},
	accounting.CodeInput:          {422, "Not a framework or policy version that can be recorded"},
	accounting.CodeOwn:            {403, "You wrote it, so another accountant signs it"},
	accounting.CodeSigned:         {409, "Signed already, or replaced"},
	storage.CodeAccounting:        {409, "A framework or policy version never changes"},
	CodeNeedsApproval:             {409, "The journal needs another person's approval"},
	CodeVoidApproval:              {409, "Reversing needs a second person"},
	CodeOwnJournal:                {403, "You prepared it, so another person approves it"},
	CodeEditApproval:              {409, "Editing needs a second person"},
	bill.CodeNotBill:              {404, "Not a bill"},
	purchaseorder.CodeNotOrder:    {404, "Not a purchase order"},
	purchaseorder.CodeBadDraft:    {422, "The saved order cannot be read"},
	purchaseorder.CodeSupplier:    {422, "The order's supplier is not an active vendor"},
	purchaseorder.CodeNoDate:      {422, "The order has no date"},
	purchaseorder.CodeLines:       {422, "The order has no lines, or no total"},
	purchaseorder.CodeAccount:     {422, "A line's account cannot take it"},
	purchaseorder.CodeQuantity:    {422, "A line's quantity or price is not one an order takes"},
	purchaseorder.CodeGSTCode:     {422, "Not a GST code for purchases"},
	purchaseorder.CodeTooLong:     {422, "The order's text is too long"},
	purchaseorder.CodeNotDraft:    {409, "The order is not a draft"},
	purchaseorder.CodeNotApproved: {409, "The order is not approved, or is closed"},
	purchaseorder.CodeLeft:        {422, "More than is left of the order"},
	purchaseorder.CodeOrderLine:   {422, "A bill's line takes what its order cannot give"},
	CodeOrderNeedsApproval:        {409, "The order needs another person's approval"},
	CodeOwnOrder:                  {403, "You prepared it, so another person approves it"},
	doctemplate.CodeKind:          {422, "Not a template Fyniq serves"},
	doctemplate.CodeName:          {422, "The template's name is blank or too long"},
	doctemplate.CodeSchema:        {422, "Not this template format"},
	doctemplate.CodePage:          {422, "A paper size or margin outside the choices"},
	doctemplate.CodeTheme:         {422, "A font Fyniq does not ship, or an accent colour too pale to read"},
	doctemplate.CodeBlock:         {422, "A block the document does not take, or one repeated"},
	doctemplate.CodeLocked:        {422, "IRAS particulars cannot be removed or hidden"},
	doctemplate.CodeText:          {422, "A text the block does not take, or too long"},
	doctemplate.CodeField:         {422, "Not a field of the catalogue"},
	doctemplate.CodeSubject:       {422, "The email's subject is blank, on several lines or too long"},
	doctemplate.CodeBody:          {422, "The email's body is blank or too long"},
	doctemplate.CodeUnchange:      {409, "That version is in force already"},
	doctemplate.CodeVersion:       {422, "No such version of the template"},
	doctemplate.CodeApproval:      {409, "Publishing needs another person's approval"},
	doctemplate.CodeOwnDraft:      {403, "You saved the draft, so another person approves it"},
	doctemplate.CodeTest:          {422, "A test document lacks an IRAS particular"},
	doctemplate.CodeRenderer:      {503, "The document could not be rendered now"},
	doctemplate.CodeBundle:        {422, "Not a bundle of templates Fyniq reads"},
	doctemplate.CodeChoice:        {422, "Not a published template of this kind and document type"},
	doctemplate.CodeParty:         {422, "The contact does not receive this document"},
	bill.CodeNotDraft:             {409, "The bill is not a draft"},
	bill.CodeNotPosted:            {409, "The bill is not posted"},
	bill.CodeBadDraft:             {422, "The saved bill cannot be read"},
	bill.CodeSupplier:             {422, "The bill's supplier is not an active vendor"},
	bill.CodeGSTCode:              {422, "Not a GST code for purchases"},
	bill.CodeDueDate:              {422, "The due date is before the bill's date"},
	bill.CodeTooLong:              {422, "A text of the bill is too long"},
	bill.CodeInputTax:             {422, "An input-tax fact of the bill is not one Fyniq reads"},
	bill.CodeBusinessShare:        {422, "A line's business share is not a percentage from 0 to 100"},
	bill.CodeNature:               {422, "The bill does not say what the non-resident supplier's payment is for"},
	bill.CodeWithholding:          {422, "The withholding decision cannot be recorded"},
	CodeBillNeedsApproval:         {409, "The bill needs another person's approval"},
	CodeBillVoidApproval:          {409, "Voiding needs a second person"},
	CodeOwnBill:                   {403, "You prepared it, so another person approves it"},
	CodeBillEditApproval:          {409, "Editing needs a second person"},
	bill.CodePaid:                 {409, "Payments settle the bill"},
	invoice.CodeNotInvoice:        {404, "Not an invoice"},
	invoice.CodeNotDraft:          {409, "The invoice is not a draft"},
	invoice.CodeNotPosted:         {409, "The invoice is not issued"},
	invoice.CodeBadDraft:          {422, "The saved invoice cannot be read"},
	invoice.CodeCustomer:          {422, "The invoice's customer is not an active customer"},
	invoice.CodeGSTCode:           {422, "Not a GST code for supplies"},
	invoice.CodeDueDate:           {422, "The due date is before the invoice's date, or on a credit note"},
	invoice.CodeTooLong:           {422, "A text of the invoice is too long"},
	invoice.CodeLine:              {422, "A line's quantity, price or discount cannot be one"},
	invoice.CodeSettled:           {409, "Receipts or credits settle the invoice"},
	invoice.CodeCredited:          {422, "The credit note cannot credit that invoice"},
	CodeInvoiceNeedsApproval:      {409, "The invoice needs another person's approval"},
	CodeInvoiceVoidApproval:       {409, "Voiding needs a second person"},
	CodeOwnInvoice:                {403, "You prepared it, so another person approves it"},
	CodeInvoiceEditApproval:       {409, "Editing needs a second person"},
	receipt.CodeNotReceipt:        {404, "Not a customer receipt"},
	receipt.CodeBadDraft:          {422, "The saved receipt cannot be read"},
	receipt.CodeCustomer:          {422, "The receipt's customer is not a customer"},
	receipt.CodeInvoice:           {422, "Not an issued invoice of the receipt's customer in its currency"},
	receipt.CodeOverpaid:          {422, "More than the invoice has open"},
	receipt.CodeNotPosted:         {409, "The receipt is not posted"},
	receipt.CodeVoidDate:          {422, "The void is dated before the receipt"},
	receipt.CodeTooLong:           {422, "A text of the receipt is too long"},
	receipt.CodeTooMany:           {422, "The receipt settles too many invoices"},
	receipt.CodeTolerance:         {422, "What is left of the invoice is above the settlement tolerance"},
	billpayment.CodeTolerance:     {422, "What is left of the bill is above the settlement tolerance"},
	CodeReceiptVoidApproval:       {409, "Voiding needs a second person"},
	writeoff.CodeNotWriteOff:      {404, "Not a write-off"},
	writeoff.CodeBadDraft:         {422, "The saved write-off cannot be read"},
	writeoff.CodeInvoice:          {422, "Not an issued invoice"},
	writeoff.CodeTooMuch:          {422, "More than the invoice has open"},
	writeoff.CodeReason:           {422, "Not a reason for a write-off"},
	writeoff.CodeAccount:          {422, "No account for the reason"},
	writeoff.CodeNotDraft:         {409, "The write-off is not a draft"},
	writeoff.CodeNotPosted:        {409, "The write-off is not posted"},
	writeoff.CodeVoidDate:         {422, "The void is dated before the write-off"},
	writeoff.CodeTooLong:          {422, "The explanation is missing or too long"},
	CodeWriteOffVoidApproval:      {409, "Voiding needs a second person"},
	CodeOwnWriteOff:               {403, "Another person approves the write-off (SOD-9)"},
	billpayment.CodeNotPayment:    {404, "Not a payment of bills"},
	billpayment.CodeBadDraft:      {422, "The saved payment cannot be read"},
	billpayment.CodeSupplier:      {422, "The payment's supplier is not a vendor"},
	billpayment.CodeBill:          {422, "Not a posted bill of the supplier in the payment's currency"},
	billpayment.CodeOverpaid:      {422, "More than the bill has open"},
	billpayment.CodeNotPosted:     {409, "The payment is not posted"},
	billpayment.CodeVoidDate:      {422, "The void is dated before the payment"},
	billpayment.CodeTooLong:       {422, "A text of the payment is too long"},
	billpayment.CodeTooMany:       {422, "The payment settles too many bills"},
	billpayment.CodeHeld:          {409, "A bill waits for its withholding tax decision"},
	CodePaymentVoidApproval:       {409, "Voiding needs a second person"},
	credits.CodeNotApplication:    {404, "Not a credit application"},
	credits.CodeBadDraft:          {422, "The saved application cannot be read"},
	credits.CodeCredit:            {422, "Not a posted vendor credit or a payment with an amount unapplied"},
	credits.CodeBill:              {422, "Not a posted bill of the credit's supplier in its currency"},
	credits.CodeTooMuch:           {422, "More than the bill has open or the credit has left"},
	credits.CodeNotPosted:         {409, "The application is not posted"},
	credits.CodeVoidDate:          {422, "The void is dated before the application"},
	credits.CodeApplied:           {409, "The credit is applied to bills"},
	CodeApplicationVoidApproval:   {409, "Voiding needs a second person"},
	credits.CodeNotRefund:         {404, "Not a supplier refund"},
	CodeRefundVoidApproval:        {409, "Voiding needs a second person"},
	ledger.CodeNotMoneyAccount:    {422, "Not an active money account"},
	ledger.CodePaymentAmount:      {422, "A payment's amount is not positive or not in its currency"},
	ledger.CodePaidAmount:         {422, "What a base-currency account paid is missing or not wanted"},
	storage.CodeAllocation:        {409, "An allocation never changes and is reversed exactly"},
	ledger.CodeBillTotal:          {422, "A bill's total is more than zero"},
	ledger.CodeSystemRole:         {422, "A system account is missing"},
	ledger.CodeRuleAccount:        {422, "The line is on an account the rule posts to"},
	ledger.CodeLineCurrency:       {422, "A line is in another currency"},
	ledger.CodeSpread:             {422, "A line's spread is not one the rules allow"},
	ledger.CodeClaimShare:         {422, "A share of a line's GST is not a percentage from 0 to 100"},
	ledger.CodeWithholding:        {422, "The bill's withholding tax cannot be worked out"},
	ledger.CodeInvoiceTotal:       {422, "An invoice's total is not more than zero"},
	ledger.CodeGSTRate:            {422, "A line's GST rate is not one the rules read"},
	journal.CodeNotOpening:        {404, "Not the opening balances"},
	journal.CodeNoBooksStart:      {409, "The books start is not set"},
	ledger.CodeNoDate:             {422, "The journal has no date"},
	ledger.CodeTooFewLines:        {422, "The journal has too few lines"},
	ledger.CodeZeroLine:           {422, "A line is zero"},
	ledger.CodeUnknownAccount:     {422, "An account does not exist"},
	ledger.CodeGroupAccount:       {422, "An account is a group heading"},
	ledger.CodeInactiveAccount:    {422, "An account is inactive"},
	ledger.CodeMissingRate:        {422, "A line needs an exchange rate"},
	ledger.CodeWrongRate:          {422, "An exchange rate does not fit"},
	ledger.CodeUnbalancedBase:     {422, "The journal does not balance"},
	ledger.CodeUnbalancedCur:      {422, "A currency does not balance"},
	ledger.CodeTooLong:            {422, "A text is too long"},
	ledger.CodeNotPosted:          {422, "The entry is not posted"},
	ledger.CodeRateOnBase:         {422, "A base-currency line has a rate"},
	ledger.CodeBaseRange:          {422, "A base amount is too large"},
	ledger.CodeShareCapital:       {422, "A line posts to share capital"},
	ledger.CodeReversalDate:       {422, "The reversal is dated before its entry"},
	ledger.CodeForeignOpening:     {422, "An opening balance not in the base currency"},
	money.CodeNotDecimal:          {422, "An amount is not a plain decimal"},
	money.CodePrecision:           {422, "An amount has too many decimals"},
	money.CodeRange:               {422, "An amount is too large"},
	money.CodeCurrency:            {422, "A currency is not valid"},
	money.CodeRate:                {422, "An exchange rate is not valid"},
	storage.CodeUnbalanced:        {422, "The entry does not balance"},
	storage.CodeShareCapital:      {422, "A manual journal cannot post to share capital"},
	storage.CodeImmutable:         {409, "Posted history cannot change"},
	storage.CodePeriodClosed:      {409, "The period is closed"},
	storage.CodeAccountNotPost:    {422, "The account cannot take postings"},
	storage.CodeAmount:            {422, "An amount or rate is not valid"},
	storage.CodeNoTenant:          {404, "Not found"},
	storage.CodeDocumentStatus:    {409, "The document cannot move to that status"},
	storage.CodeDocumentEdit:      {409, "An edit posts its reversal and its new version together"},
	storage.CodeDuplicate:         {409, "Already done"},
	storage.CodeRetry:             {503, "Busy: try again"},
	storage.CodeLockTimeout:       {409, "Another change is in progress"},
	storage.CodeInvalid:           {422, "The request is not valid"},
	storage.CodeStorageFailure:    {500, "Something went wrong"},
	storage.CodeEvidence:          {409, "Evidence is append-only once its record is final"},
	evidence.CodeType:             {422, "Not a file type Fyniq accepts"},
	evidence.CodeSize:             {422, "The file is empty or larger than 25 MB"},
	evidence.CodeMalware:          {422, "The malware scan flagged the file"},
	evidence.CodeText:             {422, "The evidence type is blank, or a text is too long"},
	evidence.CodeClass:            {422, "Not a data class this record can hold"},
	evidence.CodeSource:           {422, "Not how a file arrives here"},
	evidence.CodeImage:            {422, "The picture cannot be read"},
	evidence.CodeMacro:            {422, "Office files with macros are refused"},
	evidence.CodeArchive:          {422, "The office file's archive is not safe to open"},

	// Customer credits applied to invoices (FR-AR-04).
	customercredit.CodeNotApplication:   {404, "Not a customer credit application"},
	customercredit.CodeBadDraft:         {422, "The saved application cannot be read"},
	customercredit.CodeCredit:           {422, "Not an issued credit note or a receipt with an amount unapplied"},
	customercredit.CodeInvoice:          {422, "Not an issued invoice of the credit's customer in its currency"},
	customercredit.CodeTooMuch:          {422, "More than the invoice has open or the credit has left"},
	customercredit.CodeNotPosted:        {409, "The application is not posted"},
	customercredit.CodeVoidDate:         {422, "The void is dated before the application"},
	customercredit.CodeApplied:          {409, "The receipt's unapplied amount is applied to invoices"},
	CodeCustomerApplicationVoidApproval: {409, "Voiding needs a second person"},
	customercredit.CodeNotRefund:        {404, "Not a customer refund"},
	CodeCustomerRefundVoidApproval:      {409, "Voiding needs a second person"},

	// Customer deposits (FR-AR-09).
	deposit.CodeNotDeposit:  {404, "Not a customer deposit"},
	deposit.CodeBadDraft:    {422, "The saved deposit cannot be read"},
	deposit.CodeCustomer:    {422, "The deposit's customer is not a customer"},
	deposit.CodeKind:        {422, "Not a part payment or a security deposit"},
	deposit.CodeGSTCode:     {422, "The deposit's GST code is not one it takes"},
	deposit.CodeNotPosted:   {409, "The deposit is not posted"},
	deposit.CodeVoidDate:    {422, "The void is dated before the deposit"},
	deposit.CodeUsed:        {409, "Invoices or refunds use the deposit"},
	deposit.CodeTooLong:     {422, "A text of the deposit is too long"},
	CodeDepositVoidApproval: {409, "Voiding needs a second person"},
	invoice.CodeDeposit:     {422, "The invoice cannot deduct that deposit"},
	ledger.CodeDeposit:      {422, "A deposit the invoice deducts is not one it takes"},

	// Quotes and sales orders (FR-AR-01).
	quote.CodeNotQuote: {404, "Not a quote or a sales order"},
	quote.CodeBadDraft: {422, "The saved quote cannot be read"},
	quote.CodeValidity: {422, "The quote's validity date is missing or before its date"},
	quote.CodeNotDraft: {409, "The quote is not a draft"},
	quote.CodeNotOpen:  {409, "The quote is not sent"},
	quote.CodeContent:  {422, "A quote credits no invoice and deducts no deposit"},
}

// storageFixes say how to fix a storage refusal whose database check gave no hint (ENG-23).
var storageFixes = map[string]string{
	storage.CodeOverlap:       "Change the dates so they neither overlap another version nor end before they start",
	storage.CodeOrganisation:  "Correct the value the message names and send the change again",
	storage.CodeVersion:       "Read the record again, check what changed, and send your change with its new ETag",
	storage.CodeMembership:    "Check the member's roles: the organisation keeps an Owner, and each role is held once",
	storage.CodeSequence:      "Move the next number forward, or use another prefix; a number is never issued twice",
	storage.CodeDuplicate:     "Check whether it was done already; for a new record, use a code or key not in use",
	storage.CodeInvalid:       "Correct the value the message names and send it again",
	storage.CodeApproval:      "Read the request again: it is signed once, by someone other than its requester, with a second factor, while it is pending",
	storage.CodeEvidence:      "Files can be removed only while the record is a draft; add a newer file instead",
	storage.CodeAccounting:    "Record a new version instead; another member who holds the Accountant role confirms or approves it",
	storage.CodeVariance:      "Explain an income or expense account's variance while its period is open; a new explanation replaces the earlier one",
	storage.CodeChecklist:     "Mark checklist tasks while the period is open; save a new template version rather than changing one",
	storage.CodeFXRate:        "Enter rates of currencies other than the base, a monthly rate on its month's first day; withdraw a rate and enter another rather than changing one",
	storage.CodeFXRevaluation: "Revalue the period while it is open, from its own last day; the scheduler saves only the revaluation the application worked out",
	storage.CodeTemplate:      "Publish a new version, or roll back to an earlier one: a published version never changes and a template is never deleted",
}

// problem builds a Problem for one of the catalogue's codes.
func problem(code, detail, fix string, fields ...FieldProblem) *Problem {
	f, ok := codeFacts[code]
	if !ok {
		f.Status, f.Title = http.StatusUnprocessableEntity, http.StatusText(http.StatusUnprocessableEntity)
	}
	return &Problem{Type: "urn:fyniq:problem:" + code, Title: f.Title, Status: f.Status, Detail: detail, Code: code, Fix: fix, Errors: fields}
}

func (p *Problem) withHeader(name, value string) *Problem {
	if p.headers == nil {
		p.headers = http.Header{}
	}
	p.headers.Set(name, value)
	return p
}

// newError replaces huma.NewError, so huma's own errors (validation, unreadable bodies) are problem
// details with stable codes too. A *Problem passed in errs is returned as it is.
func newError(status int, msg string, errs ...error) huma.StatusError {
	for _, e := range errs {
		var p *Problem
		if errors.As(e, &p) {
			return p
		}
	}
	code := map[int]string{
		http.StatusBadRequest: CodeMalformed, http.StatusUnprocessableEntity: CodeInvalid,
		http.StatusRequestEntityTooLarge: CodeTooLarge, http.StatusUnsupportedMediaType: CodeMediaType,
		http.StatusNotFound: CodeNotFound, http.StatusUnauthorized: CodeUnauthenticated, http.StatusForbidden: CodeForbidden,
		http.StatusPreconditionFailed: CodeStale, http.StatusPreconditionRequired: CodeIfMatchRequired,
	}[status]
	if code == "" {
		if status >= 500 || status == 0 {
			code = CodeFailure
		} else {
			code = "API-HTTP-" + strconv.Itoa(status)
		}
	}
	p := problem(code, sentence(msg), "Check the request against the API reference and send it again.")
	if status != 0 {
		p.Status = status
	}
	if p.Title == http.StatusText(http.StatusUnprocessableEntity) && status != http.StatusUnprocessableEntity {
		p.Title = http.StatusText(status)
	}
	for _, e := range errs {
		var d huma.ErrorDetailer
		if errors.As(e, &d) {
			detail := d.ErrorDetail()
			p.Errors = append(p.Errors, FieldProblem{Code: code, Field: detail.Location, Message: detail.Message,
				Fix: fmt.Sprintf("Send %s as the API reference describes it", orWhole(detail.Location))})
		}
	}
	if code == CodeInvalid && len(p.Errors) > 0 {
		p.Detail = fmt.Sprintf("%d field(s) are not valid; each is listed in errors.", len(p.Errors))
		p.Fix = "Correct every field listed in errors and send the request again."
	}
	if code == CodeFailure {
		p.Detail = "Something went wrong on our side; nothing was changed."
		p.Fix = "Try again. If it keeps happening, quote the instance to support."
	}
	return p
}

func orWhole(location string) string {
	if location == "" {
		return "the request"
	}
	return location
}

func sentence(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s) // the first character, not byte: a message may start with ✖ or ⚠
	s = string(unicode.ToUpper(r)) + s[n:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

var lineField = regexp.MustCompile(`^lines\[(\d+)\]\.(\w+)$`)

// journalField turns the engine's field names (lines numbered from 1) into the JSON paths of the
// journal resource (lines numbered from 0).
func journalField(f string) string {
	m := lineField.FindStringSubmatch(f)
	if m == nil {
		return f
	}
	n, _ := strconv.Atoi(m[1])
	name := map[string]string{"account": "account_id", "rate": "fx_rate", "currency": "amount.currency"}[m[2]]
	if name == "" {
		name = m[2]
	}
	return fmt.Sprintf("lines[%d].%s", n-1, name)
}

// problemFrom turns any error a handler returns into a Problem.
func problemFrom(ctx context.Context, err error) *Problem {
	var (
		p   *Problem
		se  huma.StatusError
		lp  ledger.Problems
		je  *journal.Error
		be  *bill.Error
		ie  *invoice.Error
		re  *receipt.Error
		we  *writeoff.Error
		bpe *billpayment.Error
		cre *credits.Error
		poe *purchaseorder.Error
		cce *customercredit.Error
		dpe *deposit.Error
		qe  *quote.Error
		pe  *period.Error
		ae  *accounting.Error
		ce  *closing.Error
		fe  *fx.Error
		me  *money.Error
		ste *storage.Error
	)
	switch {
	case errors.As(err, &p):
		return p
	case errors.As(err, &lp) && len(lp) > 0:
		fields := make([]FieldProblem, 0, len(lp))
		for _, x := range lp {
			fields = append(fields, FieldProblem{Code: x.Code, Field: journalField(x.Field), Message: x.Message, Fix: x.Fix})
		}
		return problem(lp[0].Code, fmt.Sprintf("The journal cannot post: %d problem(s), each listed in errors.", len(lp)),
			"Correct the draft and post it again.", fields...)
	case errors.As(err, &je):
		return problem(je.Code, sentence(je.Message), sentence(je.Fix))
	case errors.As(err, &be):
		return problem(be.Code, sentence(be.Message), sentence(be.Fix))
	case errors.As(err, &ie):
		return problem(ie.Code, sentence(ie.Message), sentence(ie.Fix))
	case errors.As(err, &re):
		return problem(re.Code, sentence(re.Message), sentence(re.Fix))
	case errors.As(err, &we):
		return problem(we.Code, sentence(we.Message), sentence(we.Fix))
	case errors.As(err, &bpe):
		return problem(bpe.Code, sentence(bpe.Message), sentence(bpe.Fix))
	case errors.As(err, &cre):
		return problem(cre.Code, sentence(cre.Message), sentence(cre.Fix))
	case errors.As(err, &poe):
		return problem(poe.Code, sentence(poe.Message), sentence(poe.Fix))
	case errors.As(err, &cce):
		return problem(cce.Code, sentence(cce.Message), sentence(cce.Fix))
	case errors.As(err, &dpe):
		return problem(dpe.Code, sentence(dpe.Message), sentence(dpe.Fix))
	case errors.As(err, &qe):
		return problem(qe.Code, sentence(qe.Message), sentence(qe.Fix))
	case errors.As(err, &pe):
		return problem(pe.Code, sentence(pe.Message), sentence(pe.Fix))
	case errors.As(err, &ae):
		return problem(ae.Code, sentence(ae.Message), sentence(ae.Fix))
	case errors.As(err, &ce):
		return problem(ce.Code, sentence(ce.Message), sentence(ce.Fix))
	case errors.As(err, &fe):
		if fe.Field != "" {
			return problem(fe.Code, sentence(fe.Message), sentence(fe.Fix), FieldProblem{Code: fe.Code, Field: fe.Field, Message: fe.Message, Fix: fe.Fix})
		}
		return problem(fe.Code, sentence(fe.Message), sentence(fe.Fix))
	case errors.As(err, &me):
		return problem(me.Code, sentence(me.Message), sentence(me.Fix),
			FieldProblem{Code: me.Code, Field: me.Field, Message: me.Message, Fix: me.Fix})
	case errors.Is(err, ports.ErrNotFound):
		return problem(CodeNotFound, "No record with that ID exists in this organisation.",
			"Check the ID. Records of other organisations are never shown.")
	case errors.As(err, &ste):
		if _, known := codeFacts[ste.Code]; !known || ste.Code == storage.CodeStorageFailure {
			logFailure(ctx, err)
			return newError(http.StatusInternalServerError, "").(*Problem)
		}
		if ste.Code == storage.CodeNoTenant {
			return problem(CodeNotFound, "No record with that ID exists in this organisation.",
				"Check the ID. Records of other organisations are never shown.")
		}
		fix := ste.Hint
		if fix == "" {
			fix = storageFixes[ste.Code]
		}
		out := problem(ste.Code, sentence(ste.Message), sentence(fix))
		switch ste.Code {
		case storage.CodeRetry, storage.CodeLockTimeout:
			out.withHeader("Retry-After", "1")
			out.Fix = "Send the same request again, with the same Idempotency-Key, in a moment."
		}
		return out
	case errors.As(err, &se):
		if hp, ok := newError(se.GetStatus(), err.Error()).(*Problem); ok {
			return hp
		}
	}
	logFailure(ctx, err)
	return newError(http.StatusInternalServerError, "").(*Problem)
}
