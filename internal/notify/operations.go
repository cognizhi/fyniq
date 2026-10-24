package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The events of the operations types (PRD-001 rev 0.110): postings, the close, recurring drafts, bills to pay and
// failed background work, each to those who act on it, in the daily digest unless they choose otherwise. The bills to
// pay are told by package reminders, which reads the ledger, so this package stays free of it (the render service
// imports it through sign-in).

// kinds name each posting document type, the approval object whose Appendix D row says who oversees it, and where it
// opens (a path ending in "/" takes the document's ID).
var kinds = map[storage.DocumentType]struct{ name, object, path string }{
	storage.DocumentJournal:           {"journal", approval.ObjectJournal, "/accounting/journals/"},
	storage.DocumentOpeningBalance:    {"the opening balances", approval.ObjectOpening, "/accounting/opening-balances"},
	storage.DocumentBill:              {"bill", approval.ObjectBill, "/purchases/bills/"},
	storage.DocumentBillPayment:       {"payment of bills", approval.ObjectPayment, "/purchases/bills"},
	storage.DocumentVendorCredit:      {"supplier credit", approval.ObjectBill, "/purchases/bills"},
	storage.DocumentCreditApplication: {"credit applied to a bill", approval.ObjectApplication, "/purchases/bills"},
	storage.DocumentSupplierRefund:    {"supplier refund", approval.ObjectRefund, "/purchases/bills"},
	storage.DocumentInvoice:           {"invoice", approval.ObjectInvoice, ""}, // no link until the invoice screens (p1-sales task 8.1)
}

// Posted tells those who approve postings of doc's kind (A on its row) that actor, named name, posted it, or voided or
// reversed it when entry reverses another; never actor, and nothing for work no person did. The reversal half of an
// edit, whose replacement posts next in the same unit of work, says nothing, so an edit reads as a posting.
func Posted(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, actor uuid.UUID, name string, doc storage.Document, entry storage.Entry) error {
	k, ok := kinds[doc.Type]
	if !ok || actor == uuid.Nil {
		return nil
	}
	verb := "posted"
	if entry.ReversesEntryID != nil {
		if doc.Status != storage.DocumentReversed {
			return nil
		}
		verb = "voided"
		if doc.Type == storage.DocumentJournal || doc.Type == storage.DocumentOpeningBalance {
			verb = "reversed"
		}
	}
	if verb == "posted" && doc.Type == storage.DocumentInvoice {
		verb = "issued" // the user's word for posting an invoice
	}
	what, link := k.name, k.path
	if doc.Number != "" {
		what += " " + doc.Number
	}
	if strings.HasSuffix(link, "/") {
		link += doc.ID.String()
	}
	if name == "" {
		name = "Someone"
	}
	_, err := Emit(ctx, d, u, org, Event{Type: "ledger.posted", RecordType: "document", RecordID: &doc.ID, Title: name + " " + verb + " " + what,
		Link: link, At: d.Clock.Now(), Audience: Audience{Capability: approval.Rows[k.object], Need: "A", Except: []uuid.UUID{actor}}})
	return err
}

// periodPeople are those who close a month or approve its reopening, and those who post to it: C or A on its row.
var periodPeople = Audience{Capability: approval.Rows[approval.ObjectPeriod], Need: "CA"}

// PeriodClosed tells periodPeople, but actor, that the month named name closed.
func PeriodClosed(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, actor, period uuid.UUID, name string) error {
	a := periodPeople
	a.Except = []uuid.UUID{actor}
	_, err := Emit(ctx, d, u, org, Event{Type: "period.closed", RecordType: "period", RecordID: &period, Link: "/accounting/close/" + period.String(),
		Title: name + " was closed: nothing more posts into it", Audience: a, At: d.Clock.Now()})
	return err
}

// PeriodReopened tells periodPeople, but the approver who reopened it, that the month named name reopened, and why.
func PeriodReopened(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, approver, period uuid.UUID, name, reason string) error {
	a := periodPeople
	a.Except = []uuid.UUID{approver}
	_, err := Emit(ctx, d, u, org, Event{Type: "period.reopened", RecordType: "period", RecordID: &period, Link: "/accounting/close/" + period.String(),
		Title: name + " was reopened: " + reason, Audience: a, At: d.Clock.Now()})
	return err
}

// RecurringMade is the recurring journals' and bills' hook (journal.Made): a draft dated on, made from the recurring
// journal or bill named name, waits for those who post its kind (P on its row) to check it and post it.
func RecurringMade(d outbox.Dispatcher) func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, name string, doc storage.Document, on time.Time) error {
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, name string, doc storage.Document, on time.Time) error {
		k, ok := kinds[doc.Type]
		if !ok {
			return fmt.Errorf("notify: a recurring %s", doc.Type)
		}
		_, err := Emit(ctx, d, u, org, Event{Type: "recurring.made", RecordType: "document", RecordID: &doc.ID, Link: k.path + doc.ID.String(),
			Title:    fmt.Sprintf("The recurring %s %q made a draft dated %s: check it and post it", k.name, name, on.Format("2 Jan 2006")),
			Audience: Audience{Capability: approval.Rows[k.object], Need: "P"}, At: d.Clock.Now()})
		return err
	}
}

// JobDead is the outbox's hook (outbox.Handler.Dead, outbox.Ticker.Dead): background work that ran out of attempts
// goes to the Owner and the Administrator (C or A on users and roles). A fan-out that died tells nobody, so a failing
// notification never notifies of itself.
func JobDead(d outbox.Dispatcher) func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, row storage.OutboxRow, why string) error {
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, row storage.OutboxRow, why string) error {
		if row.Kind == FanoutKind {
			return nil
		}
		if r := []rune(why); len(r) > 150 {
			why = string(r[:149]) + "…"
		}
		title := fmt.Sprintf("Background work %s failed after %d attempts", row.Kind, row.Attempts)
		if why != "" {
			title += ": " + why
		}
		_, err := Emit(ctx, d, u, org, Event{Type: "jobs.failed", RecordType: "outbox", RecordID: &row.ID, Title: title, Link: "/settings",
			Audience: Audience{Capability: approval.Rows[approval.ObjectMembership], Need: "CA"}, At: d.Clock.Now()})
		return err
	}
}
