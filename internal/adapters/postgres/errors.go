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
}

// constraintCodes maps named constraints to the port's stable codes and plain messages.
var constraintCodes = map[string]struct{ code, message string }{
	"journal_entry_posting_identity_key": {storage.CodeDuplicate, "this posting was already made: its posting identity is in use"},
	"journal_entry_reverses_key":         {storage.CodeDuplicate, "that entry was already reversed"},
	"account_code_key":                   {storage.CodeDuplicate, "an account with this code already exists"},
	"outbox_business_key":                {storage.CodeDuplicate, "this work is already queued: its business key is in use"},
	"user_identity_pkey":                 {storage.CodeDuplicate, "this sign-in identity belongs to a user already"},
	"super_admin_grant_pkey":             {storage.CodeDuplicate, "that code exists already"},
	"user_membership_user_key":           {storage.CodeDuplicate, "the user is a member already"},
	"role_assignment_one_live":           {storage.CodeDuplicate, "the member holds that role already"},
	"role_assignment_role_known":         {storage.CodeMembership, "that is not a role a person can hold (PRD-001 §7.1)"},
	"period_no_overlap":                  {storage.CodePeriodDates, "the period overlaps another period"},
	"journal_line_amount_scale":          {storage.CodeAmount, "an amount has more decimals than its currency allows, or is not a number"},
	"journal_line_base_amount_scale":     {storage.CodeAmount, "a base amount has more decimals than the base currency allows, or is not a number"},
	"journal_line_fx_rate_positive":      {storage.CodeAmount, "an exchange rate is not a positive number"},
	"journal_line_base_currency_rate":    {storage.CodeAmount, "a line in the base currency must use the rate 1"},
	"journal_line_base_amount_rnd_fx":    {storage.CodeAmount, "a base amount is not the amount times the rate, rounded half away from zero (RND-FX)"},
	"journal_line_currency_fkey":         {storage.CodeAmount, "a currency is not one Fyniq knows"},
	"journal_line_account_fkey":          {storage.CodeAccountNotPost, "an account does not exist in this organisation"},
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
