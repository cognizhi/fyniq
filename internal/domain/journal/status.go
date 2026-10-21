package journal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The severities of a status (ENG-21): an error (✖) stops the journal posting, so it stays a draft with the problem
// shown; a warning (⚠) posts but is reviewed; ok (✔) is ready, or posted.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityOK      = "ok"
)

// The codes of the statuses that are not refusals of a command.
const (
	StatusReady   = "JOURNAL-OK"      // a draft the rules accept, dated in an open period: ✔ OK
	StatusPosted  = "JOURNAL-POSTED"  // a posted journal in an open period: ✔ OK
	StatusLocked  = "JOURNAL-LOCKED"  // a posted journal in a closed month: ✔ Locked (closed month)
	StatusWaiting = "JOURNAL-WAITING" // a posted journal whose automatic reversal waits for a period: ⚠ (rev 0.58)
)

// Status is one status of a journal, as the workbook's Journal sheet showed it (ENG-20): its severity, the stable code
// of the rule, its title (the workbook's words, adapted where the platform removes the cause), what is wrong in detail,
// and how to fix it.
type Status struct {
	Severity string
	Code     string
	Field    string
	Title    string
	Detail   string
	Fix      string
}

// titles are the workbook's status texts for the rules' codes, in its words (ENG-20, ENG-23). The workbook's "Enter an
// entry #", "Date not recognised", "Account not on Accounts sheet", "Use debit OR credit on a line", "Another line of
// this entry has an error" and "Lines of this entry have different dates" cannot happen here: a journal is its own
// entry, with one date, and its dates, accounts and amounts are picked or checked as they are entered. "Dated before
// books start", a warning there, is an error here: no period exists before the books start, so nothing posts there.
var titles = map[string]string{
	ledger.CodeNoDate:          "Enter the date",
	ledger.CodeTooFewLines:     "Add a second line",
	ledger.CodeZeroLine:        "Enter a debit or a credit",
	ledger.CodeUnknownAccount:  "Choose an account",
	ledger.CodeGroupAccount:    "Choose an account under the group",
	ledger.CodeInactiveAccount: "The account is inactive",
	ledger.CodeShareCapital:    "Share capital changes only through the share register",
	ledger.CodeMissingRate:     "Enter the exchange rate",
	ledger.CodeWrongRate:       "Check the exchange rate",
	ledger.CodeRateOnBase:      "No exchange rate on a base-currency line",
	ledger.CodeBaseRange:       "The amount is too large",
	ledger.CodeTooLong:         "Shorten the text",
	ledger.CodeUnbalancedBase:  "Entry does not balance",
	ledger.CodeUnbalancedCur:   "Entry does not balance",
	CodeReverseOn:              "Date the reversal after the journal",
	CodeBadDraft:               "Save the journal again",
	storage.CodeNoPeriod:       "Dated before books start, or in a year not open yet",
	storage.CodePeriodClosed:   "The month is closed",
	StatusReady:                "OK",
	StatusPosted:               "OK",
	StatusLocked:               "Locked (closed month)",
	StatusWaiting:              "Its automatic reversal is waiting",
	StatusRecurringReady:       "OK",
	StatusRecurringDue:         "Due but not created",
	StatusRecurringPaused:      "Paused",
	StatusRecurringEnded:       "Ended",
}

func status(severity, code, field, detail, fix string) Status {
	title, ok := titles[code]
	if !ok {
		title = detail
	}
	return Status{Severity: severity, Code: code, Field: field, Title: title, Detail: detail, Fix: fix}
}

// Statuses evaluates journals as the workbook's Journal sheet did (ENG-20, ENG-21): a draft has every problem the
// posting rules find, in their order, then whether a period is open for its date, or "OK" when it would post; a posted
// journal is "OK", or "Locked (closed month)" once its month is closed; a reversed one has none. It reads in u, once for
// all of docs, and writes nothing. (rev 0.58) A posted journal whose automatic reversal waits, because no period that is
// not closed covers its date, first has a warning that says so: the scheduler posts it once that period is open.
func Statuses(ctx context.Context, u storage.UnitOfWork, docs []storage.Document) (map[uuid.UUID][]Status, error) {
	out := map[uuid.UUID][]Status{}
	if len(docs) == 0 {
		return out, nil
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	periodOf := func(day time.Time) *storage.Period {
		for i := range periods {
			if !day.Before(periods[i].StartsOn) && !day.After(periods[i].EndsOn) {
				return &periods[i]
			}
		}
		return nil
	}
	for _, doc := range docs {
		if doc.Type != storage.DocumentJournal {
			continue
		}
		switch doc.Status {
		case storage.DocumentDraft:
			out[doc.ID] = draftStatuses(doc, settings, periodOf)
		case storage.DocumentPosted:
			saved, err := ReadSaved(doc)
			if err != nil {
				return nil, err
			}
			var st []Status
			if saved.ReverseOn != "" {
				on, _ := time.Parse(time.DateOnly, saved.ReverseOn)
				switch p := periodOf(on); {
				case p == nil:
					st = append(st, status(SeverityWarning, StatusWaiting, "reverse_on", "Its automatic reversal is dated "+saved.ReverseOn+", and no period covers that date yet.",
						"Open the financial year of "+saved.ReverseOn+" in Accounting › Periods and close; the reversal posts within the hour"))
				case p.Status == storage.PeriodClosed:
					st = append(st, status(SeverityWarning, StatusWaiting, "reverse_on", "Its automatic reversal is dated "+saved.ReverseOn+", in a closed month.",
						"Ask for the month of "+saved.ReverseOn+" to be reopened, or reverse the journal yourself in an open month"))
				}
			}
			day, _ := time.Parse(time.DateOnly, saved.Date)
			if p := periodOf(day); p != nil && p.Status == storage.PeriodClosed {
				st = append(st, status(SeverityOK, StatusLocked, "", "Its month is closed: nothing changes it now.", ""))
			} else {
				st = append(st, status(SeverityOK, StatusPosted, "", "It is posted.", ""))
			}
			out[doc.ID] = st
		}
	}
	return out, nil
}

func draftStatuses(doc storage.Document, settings ledger.Settings, periodOf func(time.Time) *storage.Period) []Status {
	j, err := decode(doc, settings.Base)
	var je *Error
	if errors.As(err, &je) {
		field := ""
		if je.Code == CodeReverseOn {
			field = "reverse_on"
		}
		return []Status{status(SeverityError, je.Code, field, je.Message, je.Fix)}
	}
	if err != nil {
		return []Status{status(SeverityError, CodeBadDraft, "", err.Error(), "Save the journal again")}
	}
	var out []Status
	if _, err := ledger.PostJournal(j, settings); err != nil {
		var problems ledger.Problems
		if !errors.As(err, &problems) {
			return []Status{status(SeverityError, CodeBadDraft, "", err.Error(), "Save the journal again")}
		}
		for _, p := range problems {
			out = append(out, status(SeverityError, p.Code, p.Field, p.Message, p.Fix))
		}
	}
	if !j.Date.IsZero() {
		switch p := periodOf(j.Date); {
		case p == nil:
			out = append(out, status(SeverityError, storage.CodeNoPeriod, "date", "No period covers "+j.Date.Format(time.DateOnly)+".",
				"Open the financial year in Accounting › Periods and close, or date the journal on or after the books start"))
		case p.Status == storage.PeriodClosed:
			out = append(out, status(SeverityError, storage.CodePeriodClosed, "date", "The month of "+j.Date.Format(time.DateOnly)+" is closed.",
				"Date the journal in an open month, or ask for the month to be reopened"))
		}
	}
	if len(out) == 0 {
		out = append(out, status(SeverityOK, StatusReady, "", "It balances and its month is open: it can post.", ""))
	}
	return out
}
