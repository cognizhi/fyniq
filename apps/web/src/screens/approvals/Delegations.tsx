import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, ChevronLeft, CircleCheck, CircleX, Plus, UserRoundCheck } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Delegation = components["schemas"]["ApprovalDelegation"];
type Answer = { data?: unknown; error?: unknown; response: Response };

async function listDelegations(): Promise<Delegation[]> {
  const r = await api.GET("/v1/approval-delegations", { params: { query: { page_size: 100 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

async function listMembers() {
  const r = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
  return r.data?.items ?? [];
}

// day is a calendar date n days from today, as the date field writes it.
function day(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function Chip({ d }: { d: Delegation }) {
  if (d.status === "ended")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={CircleX} />
        Ended
      </span>
    );
  if (d.in_force)
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        In force today
      </span>
    );
  return (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={CalendarClock} />
      Not today
    </span>
  );
}

// Delegations is where an approver hands their approvals to another member for some days, for example while on leave (FR-APR-06; docs/user-guide approvals.md
// "Delegate your approvals"). A delegation lends no right: the delegate must hold an approval right themselves, signs as themselves with their own passkey, and
// the signature says on whose behalf. Basic mode: an approver on the way out of the office needs it as much as an expert does.
export function Delegations() {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const list = useQuery({ queryKey: ["approval-delegations"], queryFn: listDelegations });
  const members = useQuery({ queryKey: ["members", "delegates"], queryFn: listMembers });
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [form, setForm] = useState({ delegate: "", starts_on: day(0), ends_on: day(7), reason: "" });
  const h = helpText("approvals.delegations");
  const candidates = (members.data ?? []).filter((m) => m.user_id !== session.user_id);

  const run = async (call: () => Promise<Answer>): Promise<boolean> => {
    setBusy(true);
    setProblem(null);
    const r = await guarded<Answer>(confirm, call);
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return false;
    }
    await queries.invalidateQueries({ queryKey: ["approval-delegations"] });
    await queries.invalidateQueries({ queryKey: ["approvals"] });
    return true;
  };
  const add = async () => {
    const key = newKey();
    const ok = await run(() =>
      api.POST("/v1/approval-delegations", { params: { header: { "Idempotency-Key": key } }, body: { delegate: form.delegate, starts_on: form.starts_on, ends_on: form.ends_on, reason: form.reason.trim() } }),
    );
    if (ok) {
      setAdding(false);
      setForm({ delegate: "", starts_on: day(0), ends_on: day(7), reason: "" });
    }
  };
  const end = (d: Delegation) => {
    const key = newKey();
    return run(() => api.POST("/v1/approval-delegations/{delegation_id}/end", { params: { path: { delegation_id: d.id }, header: { "If-Match": `"${d.version}"`, "Idempotency-Key": key } } }));
  };

  const items = list.data ?? [];
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/inbox">
        <Icon of={ChevronLeft} />
        Inbox
      </Link>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Approvals</span>
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        <div className="fy-pagehead__actions">
          <Button variant="primary" onClick={() => setAdding(true)} disabled={busy} title={helpText("approvals.delegate").tooltip}>
            <Icon of={Plus} />
            {helpText("approvals.delegate").label}
          </Button>
        </div>
      </div>
      {problem && !adding && <Refusal problem={problem} title="That was not changed" />}
      {list.error && <Refusal problem={problemOf(list.error)} title="The delegations could not be read" />}
      <section className="fy-card" aria-labelledby={`${id}-list`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-list`}>
            Yours and those made to you
          </h2>
        </div>
        <div className="fy-card__body">
          {list.isSuccess && items.length === 0 && <p className="fy-secondary">No delegation yet. Delegate before a leave so the approvals that need you do not wait.</p>}
          <ul className="fy-list">
            {items.map((d) => (
              <li key={d.id} className="fy-list__item fy-stack fy-stack--2">
                <div className="fy-row fy-row--wrap gap-2">
                  <span className="fy-list__title">
                    {d.delegator_name} <span className="fy-secondary">delegates to</span> {d.delegate_name}
                  </span>
                  <Chip d={d} />
                </div>
                <span className="fy-small fy-secondary">
                  {date(d.starts_on)} to {date(d.ends_on)} · {d.reason}
                </span>
                {d.status === "active" && (
                  <div className="fy-row fy-row--wrap gap-2">
                    <Button size="sm" variant="ghost" onClick={() => end(d)} disabled={busy} aria-label={`End: ${d.delegator_name} to ${d.delegate_name}`}>
                      <Icon of={CircleX} />
                      End now
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </div>
      </section>
      <p className="fy-small fy-secondary">
        A delegation lends no right. Your delegate signs as themselves, with their own passkey, and only what you could sign; the approval says it was made for you. You and your delegate never both count
        towards one approval.
      </p>

      <Dialog
        open={adding}
        onOpenChange={(o) => !o && setAdding(false)}
        dismissible={!busy}
        eyebrow="Approvals"
        title={helpText("approvals.delegate").label}
        description="Someone who holds an approval right themselves may sign for you for up to 90 days."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={add} disabled={busy || !form.delegate || !form.reason.trim() || form.ends_on < form.starts_on}>
              <Icon of={UserRoundCheck} />
              {helpText("approvals.delegate").label}
            </Button>
          </>
        }
      >
        {problem && <Refusal problem={problem} title="The delegation was not made" />}
        <div className="fy-stack fy-stack--3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-who`}>
              Delegate
            </label>
            <select className="fy-select" id={`${id}-who`} value={form.delegate} onChange={(e) => setForm({ ...form, delegate: e.target.value })}>
              <option value="">Choose a member</option>
              {candidates.map((m) => (
                <option key={m.user_id} value={m.user_id}>
                  {m.display_name}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-from`}>
                First day
              </label>
              <input className="fy-input" id={`${id}-from`} type="date" value={form.starts_on} onChange={(e) => setForm({ ...form, starts_on: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-to`}>
                Last day
              </label>
              <input className="fy-input" id={`${id}-to`} type="date" value={form.ends_on} onChange={(e) => setForm({ ...form, ends_on: e.target.value })} />
            </div>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-why`}>
              Reason
            </label>
            <input className="fy-input" id={`${id}-why`} value={form.reason} maxLength={300} placeholder="Annual leave" onChange={(e) => setForm({ ...form, reason: e.target.value })} />
          </div>
        </div>
      </Dialog>
    </div>
  );
}
