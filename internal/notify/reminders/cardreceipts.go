package reminders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CardReceiptsJob is the daily platform job that tells each holder of a company card which of its lines have no receipt yet, once a day, in one notice
// (PRD-001 FR-AP-08, rev 0.525; p2-bank-feeds-payments task 3.2). The days a line may wait are the setting claims.receipt_reminder_days.
const CardReceiptsJob = "notify.card_receipts"

// CardReceiptMissing is the notification type: the holder's own, naming their card's lines and nobody else's.
const CardReceiptMissing = "card.receipt_missing"

// cardDay keys each holder's notice of a day, so a rerun adds nothing.
var cardDay = uuid.MustParse("5c1e7a39-b2d4-4f60-8a1b-3e9d7f4c6a25")

// receiptDays is the days a card line may wait for its receipt in organisation u on day: claims.receipt_reminder_days.
func receiptDays(ctx context.Context, u storage.UnitOfWork, day time.Time) (int, error) {
	def, ok := settings.Lookup("claims.receipt_reminder_days")
	if !ok {
		return 3, nil
	}
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(settings.On(def, changes, day).Value)
	if err != nil || n < 1 {
		return 3, nil
	}
	return n, nil
}

// CardReceiptsAll tells, in each organisation, each cardholder of the lines of their card that have waited for a receipt too long. It returns how many
// notices were emitted; an organisation that fails does not stop the others.
func CardReceiptsAll(ctx context.Context, store storage.Store, d outbox.Dispatcher) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var told int
	var errs []error
	for _, org := range orgs {
		n, err := CardReceiptsIn(ctx, store, d, org)
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
		told += n
	}
	return told, errors.Join(errs...)
}

// CardReceiptsIn is CardReceiptsAll in one organisation. A line is missing its receipt when it is spend (money out) on a card with a holder, booked more
// than the setting's days ago, and either waits to be completed or was completed into a document that has no file attached; a holder is told once a day
// of all such lines of all their cards, with the oldest one's date.
func CardReceiptsIn(ctx context.Context, store storage.Store, d outbox.Dispatcher, org uuid.UUID) (int, error) {
	now := d.Clock.Now()
	told := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		y, m, dd := now.In(loc).Date()
		today := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		days, err := receiptDays(ctx, u, today)
		if err != nil {
			return err
		}
		cutoff := today.AddDate(0, 0, -days)
		accounts, err := u.Banking().MoneyAccounts(ctx)
		if err != nil {
			return err
		}
		type tally struct {
			n      int
			oldest time.Time
			card   string
		}
		by := map[uuid.UUID]*tally{}
		for _, a := range accounts {
			if a.Kind != storage.MoneyCard || a.CardholderID == nil || a.ClosedOn != nil {
				continue
			}
			lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &a.ID, To: &cutoff})
			if err != nil {
				return err
			}
			for _, l := range lines {
				if !strings.HasPrefix(string(l.Amount), "-") || l.BookingDate.After(cutoff) {
					continue
				}
				missing, err := missingReceipt(ctx, u, l)
				if err != nil {
					return err
				}
				if !missing {
					continue
				}
				t := by[*a.CardholderID]
				if t == nil {
					t = &tally{card: a.Name}
					by[*a.CardholderID] = t
				}
				t.n++
				if t.oldest.IsZero() || l.BookingDate.Before(t.oldest) {
					t.oldest = l.BookingDate
				}
			}
		}
		for holder, t := range by {
			key := uuid.NewSHA1(cardDay, []byte(org.String()+":"+holder.String()+":"+today.Format(time.DateOnly)))
			if done, err := u.Notifications().Emitted(ctx, CardReceiptMissing, key); err != nil || done {
				if err != nil {
					return err
				}
				continue
			}
			title := fmt.Sprintf("%d line%s of %s %s no receipt yet, the oldest from %s. Complete %s with its receipt.", t.n, plural(t.n), t.card, haveWord(t.n), t.oldest.Format("2 Jan"), pronoun(t.n))
			if _, err := notify.Emit(ctx, d, u, org, notify.Event{Type: CardReceiptMissing, RecordType: "card_receipts", RecordID: &key, Title: title, Link: "/me/claims",
				At: now, Audience: notify.Audience{Users: []uuid.UUID{holder}}}); err != nil {
				return err
			}
			told++
		}
		return nil
	})
	return told, err
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func haveWord(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "each"
}

// missingReceipt says whether a card line has no receipt: it waits in the queue, or what completing it made has no file attached.
func missingReceipt(ctx context.Context, u storage.UnitOfWork, l storage.BankLine) (bool, error) {
	if l.Status == storage.LineUnreconciled {
		return true, nil
	}
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &l.ID})
	if err != nil {
		return false, err
	}
	for _, m := range matches {
		if m.DocumentID == nil {
			continue
		}
		doc, err := u.Documents().Get(ctx, *m.DocumentID)
		if err != nil {
			return false, err
		}
		if doc.Type != storage.DocumentBankTransaction {
			return false, nil // personal spend (an advance to the holder) and any other match need no company receipt
		}
		files, err := u.Evidence().OfDocument(ctx, doc.ID, uuid.Nil, 1)
		if err != nil {
			return false, err
		}
		return len(files) == 0, nil
	}
	return false, nil
}
