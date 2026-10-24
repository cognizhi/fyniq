import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BellRing, CircleCheck, Eye, Mail, Pencil, Plus, SkipForward, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { contactEditors } from "../contacts/Contacts";
import { issuers, preparers, SalesTabs } from "./Invoices";

type Reminder = components["schemas"]["SalesReminder"];
type Statement = components["schemas"]["Statement"];
type Group = components["schemas"]["CustomerGroup"];

// stepWords is a reminder's day in its schedule in words: 3 days before the due date, on it, 7 days after.
export function stepWords(step: number, quote = false): string {
  const what = quote ? "the validity date" : "the due date";
  if (step === 0) return `On ${what}`;
  const n = Math.abs(step);
  return `${n} ${n === 1 ? "day" : "days"} ${step < 0 ? "before" : "after"} ${what}`;
}

// daysOf reads a schedule as typed: days from the due date, negative before it, such as "-3, 0, 7, 30".
export function daysOf(text: string): { days?: number[]; error?: string } {
  const parts = text
    .split(/[,\s]+/)
    .map((p) => p.trim())
    .filter(Boolean);
  const days: number[] = [];
  for (const p of parts) {
    if (!/^-?\d{1,3}$/.test(p)) return { error: "Enter whole days from the due date, such as -3, 0, 7, 30." };
    days.push(Number(p));
  }
  if (days.length > 6) return { error: "Keep at most 6 reminders." };
  if (days.some((d) => d < -30 || d > 365)) return { error: "Use -30 (before the due date) to 365 (after it)." };
  for (let i = 1; i < days.length; i++) if (days[i]! <= days[i - 1]!) return { error: "List each day once, earliest first, such as -3, 0, 7, 30." };
  return { days };
}

const scheduleWords = (days: readonly number[]) => (days.length === 0 ? "No reminders" : days.map((d) => stepWords(d)).join(" · "));

// Skip skips a reminder due, with a reason if given.
function Skip({ reminder, onClose, onDone }: { reminder: Reminder; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("reminder.skip");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/sales-reminders/{document_id}/skip", {
        params: { path: { document_id: reminder.document_id }, header: { "Idempotency-Key": newKey() } },
        body: { step: reminder.step, reason: reason.trim() || undefined },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Skipped the reminder of ${reminder.number}.`);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={reminder.number} title="Skip this reminder?" description="Nothing is sent. The next reminder in the schedule is listed when it comes.">
      {problem && <Refusal problem={problem} title="The reminder was not skipped" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-reason`} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} placeholder="Paid by cheque; receipt to record" />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={SkipForward} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Reminders is the queue of reminders due (FR-AR-08; the owner's decision of 24 Oct 2026): while receipts are recorded
// by hand, nothing goes by itself, so each waits here, as in the Inbox, to be sent or skipped.
function Reminders({ maySend, onDone }: { maySend: boolean; onDone: (message: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [skipping, setSkipping] = useState<Reminder | null>(null);
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = useQuery({
    queryKey: ["sales-reminders"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/sales-reminders", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data;
    },
  });
  const items = list.data?.items ?? [];
  const done = async (message: string) => {
    await queries.invalidateQueries({ queryKey: ["sales-reminders"] });
    onDone(message);
  };
  const send = async (r: Reminder) => {
    setBusy(r.document_id);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/sales-reminders/{document_id}/send", { params: { path: { document_id: r.document_id }, header: { "Idempotency-Key": newKey() } }, body: { step: r.step } }),
    );
    setBusy("");
    if (!out.data) return setProblem(problemOf(out.error));
    await done(`Sent the reminder of ${r.number} to ${r.recipient ?? "the customer"}.`);
  };
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {helpText("sales.reminders").label}
        </h2>
        <span className="fy-small fy-muted">{items.length === 1 ? "1 due" : `${items.length} due`}</span>
      </div>
      <p className="fy-card__body fy-small whitespace-normal">Until bank import records payments by itself, no reminder goes by itself: send or skip each one, so a customer who has paid is never chased.</p>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The reminders could not be listed" />}
      {problem && <Refusal problem={problem} title="The reminder was not sent" />}
      {list.data && items.length === 0 && <p className="fy-card__body fy-small">No reminder is due.</p>}
      {items.length > 0 && (
        <ul className="fy-list">
          {items.map((r) => (
            <li key={`${r.document_id}-${r.step}`} className="fy-list__item">
              <span className={`fy-list__icon${r.days_overdue > 0 ? " fy-list__icon--warning" : ""}`}>
                <Icon of={r.days_overdue > 0 ? TriangleAlert : BellRing} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  {r.customer.name} ·{" "}
                  {r.kind === "invoice" ? (
                    <Link className="fy-mono" to="/sales/invoices/$invoiceId" params={{ invoiceId: r.document_id }}>
                      {r.number}
                    </Link>
                  ) : (
                    <Link className="fy-mono" to="/sales/quotes/$quoteId" params={{ quoteId: r.document_id }}>
                      {r.number}
                    </Link>
                  )}{" "}
                  · {formatAmount(r.amount.amount)} {r.amount.currency}
                </span>
                <span className="fy-list__meta whitespace-normal">
                  {stepWords(r.step, r.kind === "quote")} ({date(r.due_on)}){r.days_overdue > 0 ? ` · ${r.days_overdue} days overdue` : ""} · {r.recipient ? `to ${r.recipient}` : "the customer has no billing email"}
                </span>
              </span>
              {maySend && (
                <span className="fy-row gap-2">
                  <Button size="sm" variant="primary" type="button" onClick={() => void send(r)} disabled={busy !== "" || !r.recipient} title={helpText("reminder.send").tooltip}>
                    <Icon of={Mail} />
                    {helpText("reminder.send").label}
                  </Button>
                  <Button size="sm" type="button" onClick={() => setSkipping(r)} disabled={busy !== ""} title={helpText("reminder.skip").tooltip}>
                    <Icon of={SkipForward} />
                    {helpText("reminder.skip").label}…
                  </Button>
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      {skipping && (
        <Skip
          reminder={skipping}
          onClose={() => setSkipping(null)}
          onDone={(message) => {
            setSkipping(null);
            void done(message);
          }}
        />
      )}
    </section>
  );
}

// GroupDialog adds or changes a customer group's reminder schedule (FR-AR-08).
function GroupDialog({ group, onClose, onDone }: { group: Group | "new"; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const isNew = group === "new";
  const [name, setName] = useState(isNew ? "" : group.name);
  const [days, setDays] = useState(isNew ? "-3, 0, 7, 30" : group.reminder_days.join(", "));
  const [active, setActive] = useState(isNew ? true : group.active);
  const [errors, setErrors] = useState<{ name?: string; days?: string }>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    const read = daysOf(days);
    const errs = { name: name.trim() ? undefined : "Name the group, such as Key accounts.", days: read.error };
    setErrors(errs);
    if (errs.name || errs.days || !read.days) return;
    setBusy(true);
    setProblem(null);
    const body = { name: name.trim(), reminder_days: read.days, active: isNew ? undefined : active };
    let out;
    if (isNew) {
      out = await guarded(confirm, () => api.POST("/v1/customer-groups", { params: { header: { "Idempotency-Key": newKey() } }, body }));
    } else {
      const cur = await api.GET("/v1/customer-groups/{customer_group_id}", { params: { path: { customer_group_id: group.id } } });
      out = cur.error ? cur : await guarded(confirm, () => api.PUT("/v1/customer-groups/{customer_group_id}", { params: { path: { customer_group_id: group.id }, header: { "If-Match": etagOf(cur.response) } }, body }));
    }
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(isNew ? `Added the group ${out.data.name}.` : `Saved the group ${out.data.name}.`);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow="Reminder schedules"
      title={isNew ? "New customer group" : `Change ${group.name}`}
      description="A customer group names when its customers' open invoices are due a reminder. A customer joins a group on its contact."
    >
      {problem && <Refusal problem={problem} title="The group was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} aria-invalid={errors.name ? true : undefined} aria-describedby={errors.name ? `${id}-name-error` : undefined} />
          {errors.name && (
            <span className="fy-error" id={`${id}-name-error`}>
              {errors.name}
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-days`}>
            Reminder days
          </label>
          <input
            className="fy-input"
            id={`${id}-days`}
            value={days}
            onChange={(e) => setDays(e.target.value)}
            aria-invalid={errors.days ? true : undefined}
            aria-describedby={errors.days ? `${id}-days-error` : `${id}-days-help`}
          />
          {errors.days ? (
            <span className="fy-error" id={`${id}-days-error`}>
              {errors.days}
            </span>
          ) : (
            <span className="fy-help" id={`${id}-days-help`}>
              Days from the due date, earliest first: -3 is 3 days before, 0 the due date, 7 a week after. Leave it empty for no reminders.
            </span>
          )}
        </div>
        {!isNew && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
            Active: offered to customers
          </label>
        )}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {isNew ? "Add the group" : "Save the group"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Schedules are the reminder schedules: the standard one, and each customer group's.
function Schedules({ mayEdit, onDone }: { mayEdit: boolean; onDone: (message: string) => void }) {
  const queries = useQueryClient();
  const id = useId();
  const [editing, setEditing] = useState<Group | "new" | null>(null);
  const list = useQuery({
    queryKey: ["customer-groups"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/customer-groups", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data;
    },
  });
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {helpText("sales.reminder-schedules").label}
        </h2>
        {mayEdit && (
          <Button size="sm" type="button" onClick={() => setEditing("new")}>
            <Icon of={Plus} />
            New group
          </Button>
        )}
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The schedules could not be shown" />}
      {list.data && (
        <ul className="fy-list">
          <li className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">Standard</span>
              <span className="fy-list__meta whitespace-normal">{scheduleWords(list.data.standard_reminder_days)} · customers in no group</span>
            </span>
          </li>
          {(list.data.items ?? []).map((g) => (
            <li key={g.id} className="fy-list__item">
              <span className="fy-list__main">
                <span className={`fy-list__title${g.active ? "" : " fy-muted"}`}>
                  {g.name}
                  {g.active ? "" : " (inactive)"}
                </span>
                <span className="fy-list__meta whitespace-normal">{scheduleWords(g.reminder_days)}</span>
              </span>
              {mayEdit && (
                <Button size="sm" variant="ghost" type="button" onClick={() => setEditing(g)} aria-label={`Change ${g.name}`}>
                  <Icon of={Pencil} />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <GroupDialog
          group={editing}
          onClose={() => setEditing(null)}
          onDone={(message) => {
            setEditing(null);
            void queries.invalidateQueries({ queryKey: ["customer-groups"] }).then(() => onDone(message));
          }}
        />
      )}
    </section>
  );
}

function firstOfMonth(): string {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth(), 1).toLocaleDateString("en-CA");
}

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// StatementCard works out a customer's statement of account for a period (FR-AR-08; PRD-001 rev 0.138): the balance
// brought forward, each document with a running balance, and the balance due with an ageing that adds up to it; made,
// it is kept as it said and emailed with its PDF.
function StatementCard({ mayMake, mayEmail, onDone }: { mayMake: boolean; mayEmail: boolean; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const id = useId();
  const [customer, setCustomer] = useState("");
  const [from, setFrom] = useState(firstOfMonth);
  const [to, setTo] = useState(today);
  const [currency, setCurrency] = useState("");
  const [asked, setAsked] = useState<{ customer: string; from: string; to: string; currency: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const customers = useQuery({
    queryKey: ["contacts", "customer"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "customer", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.active);
    },
  });
  const worked = useQuery({
    queryKey: ["customer-statement", asked],
    enabled: asked !== null,
    queryFn: async () => {
      const a = asked!;
      const { data, error } = await api.GET("/v1/contacts/{contact_id}/statement", { params: { path: { contact_id: a.customer }, query: { from: a.from, to: a.to, currency: a.currency || undefined } } });
      if (error) throw error;
      return data;
    },
  });
  const made = useQuery({
    queryKey: ["statements", customer],
    enabled: Boolean(customer),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/statements", { params: { query: { customer_id: customer, page_size: 20 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const s: Statement | undefined = worked.data;
  const show = (e: FormEvent) => {
    e.preventDefault();
    if (!customer) return;
    setProblem(null);
    setAsked({ customer, from, to, currency });
  };
  const makeAndEmail = async () => {
    if (!s || !asked) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/statements", { params: { header: { "Idempotency-Key": newKey() } }, body: { customer_id: asked.customer, from: asked.from, to: asked.to, currency: s.currency } }),
    );
    if (!out.data) {
      setBusy(false);
      return setProblem(problemOf(out.error));
    }
    const madeOne = out.data;
    if (!mayEmail) {
      setBusy(false);
      await queries.invalidateQueries({ queryKey: ["statements"] });
      return onDone(`Made ${madeOne.number ?? "the statement"}: an AR specialist or an Accountant emails it.`);
    }
    const sent = await guarded(confirm, () =>
      api.POST("/v1/statements/{statement_id}/email", { params: { path: { statement_id: madeOne.id ?? "" }, header: { "If-Match": etagOf(out.response), "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    await queries.invalidateQueries({ queryKey: ["statements"] });
    if (!sent.data) return setProblem(problemOf(sent.error));
    onDone(`Made ${madeOne.number ?? "the statement"} and emailed it to ${sent.data.sends?.[0]?.to ?? "the customer"}.`);
  };
  const h = helpText("statement.make");
  const money = (m: components["schemas"]["Money"]) => `${formatAmount(m.amount)}`;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {helpText("sales.statements").label}
        </h2>
      </div>
      <form className="fy-card__body fy-form fy-form--2" onSubmit={show} noValidate>
        <div className="fy-field fy-field--full">
          <label className="fy-label" htmlFor={`${id}-customer`}>
            Customer
          </label>
          <select className="fy-select" id={`${id}-customer`} value={customer} onChange={(e) => setCustomer(e.target.value)}>
            <option value="">Choose the customer</option>
            {(customers.data ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.legal_name}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            From
          </label>
          <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To
          </label>
          <input className="fy-input" id={`${id}-to`} type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-currency`}>
            Currency (optional)
          </label>
          <input className="fy-input fy-mono" id={`${id}-currency`} value={currency} maxLength={3} placeholder="The customer's own" onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
        </div>
        <div className="fy-field self-end">
          <Button type="submit" disabled={!customer} title={helpText("statement.show").tooltip}>
            <Icon of={Eye} />
            {helpText("statement.show").label}
          </Button>
        </div>
      </form>
      {worked.isError && <Refusal problem={problemOf(worked.error)} title="The statement could not be worked out" />}
      {problem && <Refusal problem={problem} title="The statement was not made or sent" />}
      {s && (
        <div className="fy-card__body fy-stack fy-stack--3">
          <div className={`fy-banner fy-banner--${s.adds_up ? "info" : "critical"}`} role="status">
            <Icon of={s.adds_up ? CircleCheck : TriangleAlert} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">{s.adds_up ? "It adds up" : "It does not add up"}</span>
              <span className="fy-small">
                {s.adds_up
                  ? `${s.customer.name}, ${date(s.from)} to ${date(s.to)}, in ${s.currency}: the balance brought forward and the lines come to the balance due.`
                  : `The lines come to ${s.difference ? formatAmount(s.difference.amount) : "a different amount"} less than the balance due, so it cannot be made. Ask the accountant to look at the customer's documents.`}
                {(s.currencies ?? []).length > 1 ? ` The customer also has documents in ${(s.currencies ?? []).filter((c) => c !== s.currency).join(", ")}: a statement each.` : ""}
              </span>
            </div>
          </div>
          <table className="fy-table fy-table--compact" aria-label="Statement lines">
            <thead>
              <tr>
                <th scope="col">Date</th>
                <th scope="col">Details</th>
                <th scope="col" className="fy-num">
                  Amount ({s.currency})
                </th>
                <th scope="col" className="fy-num">
                  Balance
                </th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>{date(s.from)}</td>
                <td>Balance brought forward</td>
                <td className="fy-num" />
                <td className="fy-num">{money(s.opening_balance)}</td>
              </tr>
              {s.lines.map((l, i) => (
                <tr key={`${l.document_id}-${l.kind}-${i}`}>
                  <td>{date(l.date)}</td>
                  <td>{l.description}</td>
                  <td className="fy-num">{money(l.amount)}</td>
                  <td className="fy-num">{money(l.balance)}</td>
                </tr>
              ))}
              <tr>
                <td>{date(s.to)}</td>
                <td className="fy-strong">Balance due</td>
                <td className="fy-num" />
                <td className="fy-num fy-strong">{money(s.balance_due)}</td>
              </tr>
            </tbody>
          </table>
          <table className="fy-table fy-table--compact" aria-label="Ageing of the balance">
            <thead>
              <tr>
                <th scope="col" className="fy-num">
                  Not yet due
                </th>
                <th scope="col" className="fy-num">
                  1–30 days
                </th>
                <th scope="col" className="fy-num">
                  31–60
                </th>
                <th scope="col" className="fy-num">
                  61–90
                </th>
                <th scope="col" className="fy-num">
                  90+
                </th>
                <th scope="col" className="fy-num">
                  Credits
                </th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td className="fy-num">{money(s.ageing.not_yet_due)}</td>
                <td className="fy-num">{money(s.ageing.overdue_1_30)}</td>
                <td className="fy-num">{money(s.ageing.overdue_31_60)}</td>
                <td className="fy-num">{money(s.ageing.overdue_61_90)}</td>
                <td className="fy-num">{money(s.ageing.overdue_over_90)}</td>
                <td className="fy-num">{money(s.ageing.unapplied_credits)}</td>
              </tr>
            </tbody>
          </table>
          {mayMake && (
            <div>
              <Button variant="primary" type="button" onClick={() => void makeAndEmail()} disabled={busy || !s.adds_up} title={h.tooltip}>
                <Icon of={Mail} />
                {mayEmail ? h.label : helpText("statement.make-only").label}
              </Button>
            </div>
          )}
        </div>
      )}
      {(made.data ?? []).length > 0 && (
        <div className="fy-card__body">
          <h3 className="fy-h4">Statements made</h3>
          <ul className="fy-list">
            {(made.data ?? []).map((m) => (
              <li key={m.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {m.number} · {date(m.from)} to {date(m.to)} · {m.currency} {formatAmount(m.balance_due.amount)}
                  </span>
                  <span className="fy-list__meta">{(m.sends ?? []).length > 0 ? `Emailed to ${m.sends?.[0]?.to}` : "Not emailed"}</span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

// Statements is Sales › Statements and reminders (docs/design/screens CustomerStatements; FR-AR-08): the reminders due,
// a customer's statement of account, and the reminder schedules.
export function Statements() {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const [status, setStatus] = useState("");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
          </nav>
          <h1 className="fy-h1">Statements and reminders</h1>
          <p className="fy-secondary">Chase what customers owe: the reminders due, a statement of account, and when each customer is reminded.</p>
        </div>
      </div>
      <ReviewBanner screen="statements-reminders" />
      <SalesTabs />
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      <Reminders maySend={roles.some((r) => issuers.includes(r))} onDone={setStatus} />
      <StatementCard mayMake={roles.some((r) => preparers.includes(r))} mayEmail={roles.some((r) => issuers.includes(r))} onDone={setStatus} />
      <Schedules mayEdit={roles.some((r) => contactEditors.includes(r))} onDone={setStatus} />
    </div>
  );
}
