package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/contact"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/organisation"
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

// LedgerCodes are the engine's validation codes: posting a journal can return any of them.
var LedgerCodes = []string{ledger.CodeNoDate, ledger.CodeTooFewLines, ledger.CodeZeroLine, ledger.CodeUnknownAccount,
	ledger.CodeGroupAccount, ledger.CodeInactiveAccount, ledger.CodeMissingRate, ledger.CodeWrongRate, ledger.CodeUnbalancedBase,
	ledger.CodeUnbalancedCur, ledger.CodeTooLong, ledger.CodeNotPosted, ledger.CodeRateOnBase, ledger.CodeBaseRange,
	ledger.CodeShareCapital, ledger.CodeReversalDate}

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
)

// codeFacts are the status and title of every code the API returns, the catalogue the OpenAPI
// document lists per operation.
var codeFacts = map[string]struct {
	Status int
	Title  string
}{
	CodeUnauthenticated:        {401, "Sign-in required"},
	CodeCrossSite:              {403, "Sent from another site"},
	CodeForbidden:              {403, "Not allowed for your roles"},
	CodeNoOrganisation:         {403, "No organisation"},
	CodeNoPolicy:               {403, "Operation not authorised"},
	CodeMalformed:              {400, "The request cannot be read"},
	CodeInvalid:                {422, "A field is not valid"},
	CodeTooLarge:               {413, "The request is too large"},
	CodeMediaType:              {415, "The request is not JSON"},
	CodeCursor:                 {422, "The cursor is not valid"},
	CodeNotFound:               {404, "Not found"},
	CodeStale:                  {412, "The record changed"},
	CodeIfMatchRequired:        {428, "If-Match required"},
	CodeIdempotencyReused:      {422, "The Idempotency-Key was used for another request"},
	CodeIdempotencyBusy:        {409, "The first request with this Idempotency-Key is still running"},
	CodeIdempotencyOld:         {422, "The Idempotency-Key expired"},
	CodeFailure:                {500, "Something went wrong"},
	CodeUnavailable:            {503, "Not available here"},
	CodeSessionRequired:        {403, "A person must be signed in"},
	CodeSetupRefused:           {409, "Setup refused"},
	CodeStepUpRequired:         {403, "Step-up required"},
	CodeNotSuperAdmin:          {403, "Not a Super admin"},
	CodeCodeRefused:            {409, "Code refused"},
	CodeSuperAdminRefused:      {409, "Super admin change refused"},
	CodeRateLimited:            {429, "Too many requests"},
	organisation.CodeText:      {422, "A text is blank or too long"},
	organisation.CodeUEN:       {422, "Not a UEN"},
	organisation.CodeEmail:     {422, "Not an email address"},
	organisation.CodeTimeZone:  {422, "Not a time zone"},
	organisation.CodeYearStart: {422, "Not a month"},
	organisation.CodeFirstYear: {422, "The first financial year does not fit"},
	organisation.CodeBank:      {422, "Not a bank account number"},
	organisation.CodePrefix:    {422, "Not a prefix"},
	organisation.CodeNext:      {422, "The next number does not fit"},
	organisation.CodeDigits:    {422, "Not a number of digits"},
	storage.CodeSequence:       {409, "The number sequence cannot be set that way"},
	CodeChartLoaded:            {409, "The organisation has accounts already"},
	contact.CodeName:           {422, "The legal name is not valid"},
	contact.CodeRole:           {422, "Neither a customer nor a vendor"},
	contact.CodeEmail:          {422, "Not an email address"},
	contact.CodeCountry:        {422, "Not a country code"},
	contact.CodeCurrency:       {422, "Not a currency"},
	contact.CodeTerms:          {422, "Payment terms out of range"},
	contact.CodeGSTCode:        {422, "Not a GST code"},
	contact.CodePeppol:         {422, "Not a Peppol participant ID"},
	contact.CodeText:           {422, "A text is too long"},
	contact.CodeCategory:       {422, "Not an account that takes postings"},
	chart.CodeAccountCode:      {422, "Not an account code"},
	chart.CodeText:             {422, "A text is blank or too long"},
	chart.CodeType:             {422, "Not an account type"},
	chart.CodeFunction:         {422, "Not a function"},
	chart.CodeGSTCode:          {422, "Not a GST code"},
	chart.CodeCurrency:         {422, "Not a currency"},
	chart.CodeRole:             {422, "Not a system role"},
	chart.CodeProtected:        {422, "Changes only with an approval"},
	chart.CodeSystem:           {422, "A system account stays active"},
	storage.CodeAccountFixed:   {409, "The account cannot change that way"},
	storage.CodeAccountTree:    {409, "The account cannot sit there"},
	storage.CodeVersion:        {412, "The record changed since you read it"},
	storage.CodeOrganisation:   {422, "The organisation's details are not valid"},
	storage.CodeOverlap:        {409, "The dates overlap another version"},
	gst.CodeDates:              {422, "The registration's dates do not fit"},
	gst.CodeNumber:             {422, "The GST registration number is not valid"},
	gst.CodeFrequency:          {422, "Not a return frequency"},
	gst.CodeQuarter:            {422, "Not a quarters' start month"},
	gst.CodeBasis:              {422, "An accounting basis Fyniq does not offer"},
	journal.CodeNotJournal:     {404, "Not a manual journal"},
	journal.CodeNotDraft:       {409, "The journal is not a draft"},
	journal.CodeNotPosted:      {409, "The journal is not posted"},
	journal.CodeBadDraft:       {422, "The saved journal cannot be read"},
	ledger.CodeNoDate:          {422, "The journal has no date"},
	ledger.CodeTooFewLines:     {422, "The journal has too few lines"},
	ledger.CodeZeroLine:        {422, "A line is zero"},
	ledger.CodeUnknownAccount:  {422, "An account does not exist"},
	ledger.CodeGroupAccount:    {422, "An account is a group heading"},
	ledger.CodeInactiveAccount: {422, "An account is inactive"},
	ledger.CodeMissingRate:     {422, "A line needs an exchange rate"},
	ledger.CodeWrongRate:       {422, "An exchange rate does not fit"},
	ledger.CodeUnbalancedBase:  {422, "The journal does not balance"},
	ledger.CodeUnbalancedCur:   {422, "A currency does not balance"},
	ledger.CodeTooLong:         {422, "A text is too long"},
	ledger.CodeNotPosted:       {422, "The entry is not posted"},
	ledger.CodeRateOnBase:      {422, "A base-currency line has a rate"},
	ledger.CodeBaseRange:       {422, "A base amount is too large"},
	ledger.CodeShareCapital:    {422, "A line posts to share capital"},
	ledger.CodeReversalDate:    {422, "The reversal is dated before its entry"},
	money.CodeNotDecimal:       {422, "An amount is not a plain decimal"},
	money.CodePrecision:        {422, "An amount has too many decimals"},
	money.CodeRange:            {422, "An amount is too large"},
	money.CodeCurrency:         {422, "A currency is not valid"},
	money.CodeRate:             {422, "An exchange rate is not valid"},
	storage.CodeUnbalanced:     {422, "The entry does not balance"},
	storage.CodeShareCapital:   {422, "A manual journal cannot post to share capital"},
	storage.CodeImmutable:      {409, "Posted history cannot change"},
	storage.CodePeriodClosed:   {409, "The period is closed"},
	storage.CodeAccountNotPost: {422, "The account cannot take postings"},
	storage.CodeAmount:         {422, "An amount or rate is not valid"},
	storage.CodeNoTenant:       {404, "Not found"},
	storage.CodeDocumentStatus: {409, "The document cannot move to that status"},
	storage.CodeDuplicate:      {409, "Already done"},
	storage.CodeRetry:          {503, "Busy: try again"},
	storage.CodeLockTimeout:    {409, "Another change is in progress"},
	storage.CodeInvalid:        {422, "The request is not valid"},
	storage.CodeStorageFailure: {500, "Something went wrong"},
}

// storageFixes say how to fix a storage refusal whose database check gave no hint (ENG-23).
var storageFixes = map[string]string{
	storage.CodeOverlap:      "Change the dates so they neither overlap another version nor end before they start",
	storage.CodeOrganisation: "Correct the value the message names and send the change again",
	storage.CodeVersion:      "Read the record again, check what changed, and send your change with its new ETag",
	storage.CodeSequence:     "Move the next number forward, or use another prefix; a number is never issued twice",
	storage.CodeDuplicate:    "Check whether it was done already; for a new record, use a code or key not in use",
	storage.CodeInvalid:      "Correct the value the message names and send it again",
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
	s = strings.ToUpper(s[:1]) + s[1:]
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
