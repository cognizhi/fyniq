package nemura

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// LinkOpsService is the people team's door to the link's health, its repair and its restore, and the payroll team's to a run's receipt (NM-INT-04, 10, 13).
type LinkOpsService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.LinkOps = LinkOpsService{}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }
func rfcp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := rfc(*t)
	return &s
}

func eventView(e UndeliveredEvent) api.LinkEventView {
	return api.LinkEventView{EventID: e.EventID.String(), Sequence: e.Sequence, Type: e.Type, ObjectType: e.ObjectType, ObjectID: e.ObjectID, Version: e.Version, State: e.State, Code: e.Code, Attempts: e.Attempts, QueuedAt: rfc(e.QueuedAt)}
}

func restoreView(r *RestoreRow) *api.LinkRestoreView {
	if r == nil {
		return nil
	}
	v := &api.LinkRestoreView{ID: r.ID.String(), RecordedAt: rfc(r.RecordedAt), RecordedBy: r.RecordedBy, LastSequenceQueued: r.LastQueued, LastSequenceSent: r.LastSent, LastFyniqSequence: r.LastReceived, State: r.State, Runs: []api.LinkRestoreRun{}}
	for _, x := range r.Runs {
		v.Runs = append(v.Runs, api.LinkRestoreRun{RunID: x.RunID.String(), Version: x.Version, Outcome: x.Outcome, Reference: x.Reference})
	}
	return v
}

// Health is the link's health.
func (c LinkOpsService) Health(ctx context.Context, s api.Session) (api.LinkHealthView, error) {
	out := api.LinkHealthView{Undelivered: []api.LinkEventView{}, BankReconfirm: []api.BankReconfirmView{}}
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		h, ok, err := LinkHealth(ctx, tx, c.Clock.Now())
		if err != nil || !ok {
			out.State = "none"
			return err
		}
		out.State, out.Epoch, out.Queued, out.Failed, out.Sent, out.Refused = h.State, h.Epoch, h.Queued, h.Failed, h.Sent, h.Refused
		out.OldestWaiting, out.LastSent, out.FailingSince = rfcp(h.OldestWaiting), rfcp(h.LastSent), rfcp(h.FailingSince)
		out.LastSequenceQueued, out.LastSequenceSent, out.LastFyniqSequence, out.FailingOver24Hours = h.LastSequenceQueued, h.LastSequenceSent, h.LastFyniqSequence, h.SyncFailingOver24Hour
		for _, e := range h.Undelivered {
			out.Undelivered = append(out.Undelivered, eventView(e))
		}
		out.Restore = restoreView(h.OpenRestore)
		for _, r := range h.BankReconfirm {
			out.BankReconfirm = append(out.BankReconfirm, api.BankReconfirmView{PersonID: r.Person.String(), HeldVersion: r.HeldVersion, NemuraVersion: r.NemuraVersion, FoundAt: rfc(r.FoundAt)})
		}
		return nil
	})
	return out, err
}

// Events lists the events queued after a sequence number, by identity.
func (c LinkOpsService) Events(ctx context.Context, s api.Session, after int64, limit int) ([]api.LinkEventView, error) {
	out := []api.LinkEventView{}
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		es, err := ListLinkEvents(ctx, tx, after, limit)
		for _, e := range es {
			out = append(out, eventView(e))
		}
		return err
	})
	return out, err
}

// Repair queues the current state again, for one person or for everyone.
func (c LinkOpsService) Repair(ctx context.Context, s api.Session, person uuid.UUID) (int, error) {
	n := 0
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) (err error) {
		n, err = RepairLink(ctx, tx, s.Organisation, c.Clock.Now(), user(s.Member), person)
		return err
	})
	return n, err
}

// DecideRestore records the decision on one run a restore holds.
func (c LinkOpsService) DecideRestore(ctx context.Context, s api.Session, run uuid.UUID, version int, posted bool, reference string) (bool, error) {
	if !s.Fresh(c.Clock.Now()) {
		return false, &payroll.Refusal{Code: CodeLinkStepUp, Message: "Confirm with your passkey first: reconciling a restore needs a passkey confirmed in the last five minutes."}
	}
	resumed := false
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) (err error) {
		resumed, err = ReconcileRestore(ctx, tx, s.Organisation, c.Clock.Now(), s.Member, run, version, posted, reference)
		return err
	})
	return resumed, err
}

// Receipt is what Nemura kept of a run's life with Fyniq: the approvals with their digests, each submission and where it stands, Fyniq's answers and the payment instructions it reported. No figure,
// no name and no account: it is the integrity record two databases are reconciled by (NM-INT-13).
func (c LinkOpsService) Receipt(ctx context.Context, s api.Session, run uuid.UUID) (api.RunReceiptView, error) {
	out := api.RunReceiptView{RunID: run.String(), Approvals: []api.ReceiptApproval{}, Submissions: []api.ReceiptSubmission{}, Postings: []api.RunPosting{}, Payments: []api.ReceiptPayment{}}
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		if _, _, err := RunOf(ctx, tx, run); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT version, envelope_sha256, mapping_version, approved_at FROM nemura.link_run_approval WHERE run_id = $1 ORDER BY version`, run)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a api.ReceiptApproval
			var at time.Time
			if err := rows.Scan(&a.Version, &a.EnvelopeSHA256, &a.MappingVersion, &at); err != nil {
				rows.Close()
				return err
			}
			a.ApprovedAt = rfc(at)
			out.Approvals = append(out.Approvals, a)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT event_id, sequence, object_version, state, coalesce(code, ''), settled_at FROM nemura.link_event WHERE type = $1 AND object_type = 'payroll_run' AND object_id = $2 ORDER BY sequence`, link.TypePayrollRunApproved, run.String())
		if err != nil {
			return err
		}
		for rows.Next() {
			var x api.ReceiptSubmission
			var id uuid.UUID
			var at *time.Time
			if err := rows.Scan(&id, &x.Sequence, &x.Version, &x.State, &x.Code, &at); err != nil {
				rows.Close()
				return err
			}
			x.EventID, x.SettledAt = id.String(), rfcp(at)
			out.Submissions = append(out.Submissions, x)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT version, status, coalesce(posting_ref, ''), coalesce(code, ''), coalesce(message, ''), received_at FROM nemura.link_run_posting WHERE run_id = $1 ORDER BY received_at`, run)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p api.RunPosting
			var v int
			var at time.Time
			if err := rows.Scan(&v, &p.Status, &p.Reference, &p.Code, &p.Message, &at); err != nil {
				rows.Close()
				return err
			}
			p.ReceivedAt = rfc(at)
			out.Postings = append(out.Postings, p)
		}
		rows.Close()
		rows, err = tx.Query(ctx, `SELECT person_id, coalesce(instruction_id, ''), state, source, recorded_at FROM nemura.pay_payment_event WHERE run_id = $1 ORDER BY person_id, seq`, run)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x api.ReceiptPayment
			var id uuid.UUID
			var at time.Time
			if err := rows.Scan(&id, &x.InstructionID, &x.State, &x.Source, &at); err != nil {
				return err
			}
			x.PersonID, x.RecordedAt = id.String(), rfc(at)
			out.Payments = append(out.Payments, x)
		}
		return rows.Err()
	})
	return out, err
}

// WorkspaceService answers whether the Finance | People switch shows in Nemura's header (PRD-002 NM-UX-03, design D7): while the link is active and the deployment names where Fyniq's web app is
// (NEMURA_FYNIQ_WEB_ORIGIN), when Fyniq has said that the identity this session was signed in with holds a role in its finance workspace (identity.access.changed, signed with the link's key, an answer and
// no role name). A person whose rights are in Nemura alone has no door, and neither has one Fyniq has not said anything of. It is a door and never a grant: Fyniq's own sign-in and roles decide what is
// behind it. The link carries the provider the session used, so Fyniq asks the same one and the person is not shown a sign-in page again.
type WorkspaceService struct {
	Store         *Store
	FinanceOrigin string
}

var _ api.WorkspaceSource = WorkspaceService{}

// FinanceURL is where the switch leads, or "" when it does not show.
func (w WorkspaceService) FinanceURL(ctx context.Context, s api.Session, _ []authz.Role) (string, error) {
	if w.FinanceOrigin == "" || s.Issuer == "" || s.Subject == "" { // a session made before its way in was recorded has no identity to ask about
		return "", nil
	}
	finance := false
	err := w.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		if !linkActive(ctx, tx) {
			return nil
		}
		var err error
		finance, _, _, err = FyniqAccessOf(ctx, tx, s.Issuer, s.Subject)
		return err
	})
	if err != nil || !finance {
		return "", err
	}
	u := strings.TrimSuffix(w.FinanceOrigin, "/") + "/?switch=1"
	if s.Provider != "" {
		u += "&provider=" + url.QueryEscape(s.Provider)
	}
	return u, nil
}
