package banking

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Incoming-funds alerts (FR-BNK-16, UC-BNK-02; p1-bank-import-reconcile task 7.3, design D10): a bank's alert that money
// arrived becomes a pending signal on its money account, pointing to the open invoice it most likely pays: the one of
// that amount and currency whose customer the payer names. The invoice shows "likely paid" until a statement line is
// reconciled to it, which closes the signal; one that no line closes expires after bank.signal_expiry_days (7 by the
// owner's decision of 26 Oct 2026), and a person may dismiss one with a reason. A match closes them in its own unit of
// work (journal.MatchInvoices, journal.MatchRecorded). A signal posts nothing and never changes
// the invoice's status or the ledger.

// CodeAlert is a message at a bank-ingest address that has no statement file and does not read as an incoming-funds
// alert, or whose currency is not its account's.
const CodeAlert = "BANK-13"

// Expirer is the scheduler's principal that expires signals (FR-AUT-02): the job bank.signals-expiry.
var Expirer = storage.Actor{Type: "scheduler", ID: "bank.signals-expiry"}

// Alert is what an incoming-funds alert says: the amount and currency that arrived, from whom, and the bank's reference.
type Alert struct {
	Amount    string // an exact decimal, without separators
	Currency  string
	Payer     string
	Reference string
}

var (
	// alertIn says money came in; alertOut says it went out, which no incoming-funds alert says.
	alertIn  = regexp.MustCompile(`(?i)\b(received|credited|incoming|deposited)\b`)
	alertOut = regexp.MustCompile(`(?i)\b(debited|withdrawn|sent|outgoing|paid out)\b`)
	// alertAmount is an amount with its currency code, in capitals, before it: "SGD 10,900.00", "S$10,900.00" or "USD 20".
	alertAmount = regexp.MustCompile(`\b(S\$|[A-Z]{3})\s?([0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(\.[0-9]{1,4})?\b`)
	// alertPayer is who it came from: "from ACME ROBOTICS", up to the end of the phrase; alertQuoted is a quoted one.
	alertPayer  = regexp.MustCompile(`(?i)\bfrom\s+([^\n.,;"“]{1,200}?)(?:\s+(?:to|into|in|on|at|ref|reference|via|for)\b|[\n.,;]|$)`)
	alertQuoted = regexp.MustCompile(`(?i)\bfrom\s+["“]([^"”\n]{1,200})["”]`)
	// alertRef is the bank's reference for it: "Ref: 2610-0001" or "Reference No. ABC123".
	alertRef = regexp.MustCompile(`(?i)\bref(?:erence)?(?:\s*no\.?)?\s*[:#]?\s*([A-Za-z0-9][A-Za-z0-9/._-]{0,139})`)
)

// ParseAlert reads an incoming-funds alert from a message's text: it must say money came in, and name an amount with
// its currency and a payer; false otherwise. The banks' alert formats are not published [verify against a redacted DBS
// IDEAL alert]: the parser reads the plain phrases DBS and other banks use, "S$10,900.00 … received … from ACME
// ROBOTICS", and a message it cannot read is left for a person.
func ParseAlert(text string) (Alert, bool) {
	if !alertIn.MatchString(text) || alertOut.MatchString(text) {
		return Alert{}, false
	}
	var cur, amount string
	for _, m := range alertAmount.FindAllStringSubmatch(text, -1) { // the first amount in a known currency: not "REF 2610"
		code := m[1]
		if code == "S$" {
			code = "SGD"
		}
		c, err := money.Lookup(code)
		if err != nil {
			continue
		}
		n := strings.ReplaceAll(m[2], ",", "") + m[3]
		if x, err := money.Parse(n, c); err == nil && x.Sign() > 0 { // no more decimals than the currency has
			cur, amount = code, n
			break
		}
	}
	if cur == "" {
		return Alert{}, false
	}
	p := alertQuoted.FindStringSubmatch(text)
	if p == nil {
		p = alertPayer.FindStringSubmatch(text)
	}
	if p == nil || strings.TrimSpace(p[1]) == "" {
		return Alert{}, false
	}
	out := Alert{Amount: amount, Currency: cur, Payer: strings.TrimSpace(p[1])}
	if r := alertRef.FindStringSubmatch(text); r != nil {
		out.Reference = strings.TrimRight(r[1], "./_-")
	}
	return out, true
}

// suffixes are the words a company's name ends with that a bank's payer name often leaves out or abbreviates.
var suffixes = map[string]bool{"pte": true, "ltd": true, "limited": true, "private": true, "llp": true, "llc": true, "inc": true,
	"co": true, "corp": true, "corporation": true, "company": true, "sdn": true, "bhd": true, "plc": true, "gmbh": true, "bv": true}

// partyKey is a name as a payer's and a customer's are compared: letters and digits, in lower case, one space between
// words, without the company suffixes at its end.
func partyKey(s string) string {
	words := strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for len(words) > 0 && suffixes[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// samePayer says whether a bank's payer name names a customer: either contains the other, once both are keys of at
// least four characters, as banks shorten and upper-case the names they show.
func samePayer(payer, customer string) bool {
	p, c := partyKey(payer), partyKey(customer)
	if len(p) < 4 || len(c) < 4 {
		return false
	}
	return strings.Contains(p, c) || strings.Contains(c, p)
}

// LikelyInvoice is the open invoice an alert most likely pays, as at the organisation's today: of the alert's amount
// and currency open, its customer named by the payer, and no other pending signal pointing to it. With none, or more
// than one, it is nil: the signal then waits without an invoice for its statement line.
func LikelyInvoice(ctx context.Context, u storage.UnitOfWork, a Alert, today time.Time) (*uuid.UUID, error) {
	cur, err := money.Lookup(a.Currency)
	if err != nil {
		return nil, err
	}
	amount, err := money.Parse(a.Amount, cur)
	if err != nil {
		return nil, err
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	control := uuid.Nil
	for _, x := range accounts {
		if string(x.Role) == ledger.RoleAccountsReceivable {
			control = x.ID
		}
	}
	if control == uuid.Nil {
		return nil, nil
	}
	pending, err := u.BankMatching().PendingSignals(ctx)
	if err != nil {
		return nil, err
	}
	taken := map[uuid.UUID]bool{}
	for _, s := range pending {
		if s.InvoiceID != nil {
			taken[*s.InvoiceID] = true
		}
	}
	l, err := receivables.Open(ctx, u, base, control, today)
	if err != nil {
		return nil, err
	}
	var found []uuid.UUID
	for _, c := range l.Customers {
		if !samePayer(a.Payer, c.Name) {
			continue
		}
		for _, it := range c.Items {
			if it.Kind == receivables.KindInvoice && it.Open.Equal(amount) && !taken[it.DocumentID] {
				found = append(found, it.DocumentID)
			}
		}
	}
	if len(found) != 1 {
		return nil, nil
	}
	return &found[0], nil
}

// RecordAlert records an alert that arrived at account as a pending signal, pointing to its likely invoice: in u, by
// the bank-ingest worker. An alert in another currency than the account's is refused (CodeAlert).
func RecordAlert(ctx context.Context, u storage.UnitOfWork, account storage.MoneyAccount, a Alert, message *uuid.UUID, receivedAt time.Time) (storage.BankSignal, error) {
	if a.Currency != account.Currency {
		return storage.BankSignal{}, &Error{Code: CodeAlert, Message: fmt.Sprintf("the alert is for %s %s, but %s holds %s", a.Amount, a.Currency, account.Name, account.Currency),
			Fix: "Send the account's alerts from its own sender, or record the money when its statement line arrives"}
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return storage.BankSignal{}, err
	}
	invoice, err := LikelyInvoice(ctx, u, a, dayStart(org, receivedAt))
	if err != nil {
		return storage.BankSignal{}, err
	}
	return u.BankMatching().RecordSignal(ctx, storage.NewBankSignal{MoneyAccountID: account.ID, Amount: storage.Decimal(a.Amount), Currency: a.Currency,
		Payer: clipText(a.Payer, 200), Reference: clipText(a.Reference, 140), ReceivedAt: receivedAt, InboundMessageID: message, InvoiceID: invoice})
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// SignalExpiry is the number of days a pending signal waits for its statement line on a day, from the setting.
func SignalExpiry(ctx context.Context, u storage.UnitOfWork, on time.Time) (int, error) {
	def, ok := settings.Lookup("bank.signal_expiry_days")
	if !ok {
		return 0, errors.New("no setting bank.signal_expiry_days")
	}
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(settings.On(def, changes, on).Value)
}

// ExpireSignals expires an organisation's pending signals received more than the setting's days before now, each with
// an audit event, as the scheduler's principal; it returns how many expired.
func ExpireSignals(ctx context.Context, store storage.Store, org uuid.UUID, now time.Time) (int, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Expirer, Reason: "No statement line reconciled the alert's invoice in time (FR-BNK-16)"})
	n := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		days, err := SignalExpiry(ctx, u, now)
		if err != nil {
			return err
		}
		pending, err := u.BankMatching().PendingSignals(ctx)
		if err != nil {
			return err
		}
		cutoff := now.AddDate(0, 0, -days)
		for _, s := range pending {
			if !s.ReceivedAt.Before(cutoff) {
				continue
			}
			if _, err := u.BankMatching().CloseSignal(ctx, s.ID, storage.SignalExpired, nil, now); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ExpireSignalsAll expires every organisation's signals that are due, one organisation at a time: one that fails does not
// stop the others.
func ExpireSignalsAll(ctx context.Context, store storage.Store, now time.Time) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, org := range orgs {
		n, err := ExpireSignals(ctx, store, org, now)
		total += n
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return total, errors.Join(errs...)
}

// DismissSignal dismisses a pending signal with a person's reason, kept in the audit trail: its invoice stops showing
// "likely paid". A signal no longer pending is refused (storage.CodeBanking).
func DismissSignal(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, reason string, at time.Time) (storage.BankSignal, error) {
	if strings.TrimSpace(reason) == "" {
		return storage.BankSignal{}, &Error{Code: CodeRequest, Message: "a dismissal needs its reason", Fix: "Say why the alert does not pay its invoice"}
	}
	return u.BankMatching().CloseSignal(storage.WithReason(ctx, reason), id, storage.SignalDismissed, nil, at)
}
