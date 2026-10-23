import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, ChevronLeft, ChevronRight, CircleCheck, ClockAlert, Percent, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Bill = components["schemas"]["Bill"];
type Reminder = components["schemas"]["BillReminder"];

// today is the browser's date, as the bill page takes it.
export function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

const short = (iso: string) => new Date(`${iso}T00:00:00`).toLocaleDateString("en-SG", { day: "numeric", month: "short" });

// unpaid reports whether a posted bill still has something to pay.
const unpaid = (b: Bill) => b.status === "posted" && b.states.payment !== "paid";

// The columns of the bills board (FR-AP-13; docs/design/screens BillsBoard): where each bill stands, by its states and
// its planned payment date. A voided bill and a credit note are on no column.
const columns: [string, string, (b: Bill) => boolean][] = [
  ["review", "To review", (b) => b.status === "draft" && b.states.approval !== "awaiting_approval"],
  ["approval", "Awaiting approval", (b) => b.status === "draft" && b.states.approval === "awaiting_approval"],
  ["approved", "Approved", (b) => unpaid(b) && b.states.due !== "overdue" && !b.planned_payment_on],
  ["scheduled", "Scheduled", (b) => unpaid(b) && b.states.due !== "overdue" && Boolean(b.planned_payment_on)],
  ["paid", "Paid", (b) => b.status === "posted" && b.states.payment === "paid"],
  ["overdue", "Overdue", (b) => unpaid(b) && b.states.due === "overdue"],
];

// DiscountChip is a bill's early-payment discount while it can still be taken.
function DiscountChip({ b }: { b: Bill }) {
  const d = b.early_payment_discount;
  if (!d || !unpaid(b) || d.until < today()) return null;
  return (
    <span className="fy-chip fy-chip--info self-start">
      <Icon of={Percent} />
      {d.percent}% off by {short(d.until)}
    </span>
  );
}

function Card({ b }: { b: Bill }) {
  const when = b.planned_payment_on && unpaid(b) ? `pay ${short(b.planned_payment_on)}` : b.due_date ? `due ${short(b.due_date)}` : b.date ? short(b.date) : "no date";
  return (
    <Link className="fy-board__card" to="/purchases/bills/$billId" params={{ billId: b.id }}>
      <span className="fy-strong">{b.supplier?.name ?? "No supplier yet"}</span>
      <span className="fy-num">
        {formatAmount(b.total.amount)} {b.total.currency}
      </span>
      <span className="fy-small fy-muted">
        <span className="fy-mono">{b.number ?? "Draft"}</span> · {when}
      </span>
      <DiscountChip b={b} />
    </Link>
  );
}

// Board is the bills by where they stand (FR-AP-13): columns from medium screens up; on a phone, a status filter over a
// list of cards.
export function Board({ bills }: { bills: Bill[] }) {
  const id = useId();
  const [pick, setPick] = useState("review");
  const groups = columns.map(([key, title, test]) => ({ key, title, items: bills.filter(test) }));
  const picked = groups.find((g) => g.key === pick) ?? groups[0]!;
  return (
    <>
      <div className="fy-only-compact fy-stack fy-stack--3">
        <div className="fy-chipset flex-nowrap overflow-x-auto" role="group" aria-label="Status">
          {groups.map((g) => (
            <button key={g.key} type="button" className="fy-filter whitespace-nowrap" aria-pressed={pick === g.key} onClick={() => setPick(g.key)}>
              {g.title}
              {g.items.length > 0 && <span className="fy-badge fy-badge--quiet">{g.items.length}</span>}
            </button>
          ))}
        </div>
        {picked.items.length === 0 ? (
          <p className="fy-small fy-muted">No bill is {picked.title.toLowerCase()}.</p>
        ) : (
          <div className="fy-stack fy-stack--2">
            {picked.items.map((b) => (
              <Card key={b.id} b={b} />
            ))}
          </div>
        )}
      </div>
      <div className="fy-board fy-board--fit fy-from-medium fy-stagger" aria-label="Bills by status">
        {groups.map((g) => (
          <section key={g.key} className="fy-board__col" aria-labelledby={`${id}-${g.key}`}>
            <h2 className="fy-row fy-small fy-strong" id={`${id}-${g.key}`}>
              {g.title} {g.items.length > 0 && <span className="fy-badge fy-badge--quiet">{g.items.length}</span>}
            </h2>
            {g.items.length === 0 ? (
              <div className="fy-empty p-4">
                <Icon of={CircleCheck} size={24} />
                <span className="fy-small">None</span>
              </div>
            ) : (
              g.items.map((b) => <Card key={b.id} b={b} />)
            )}
          </section>
        ))}
      </div>
    </>
  );
}

const days = (from: string, n: number) => {
  const d = new Date(`${from}T00:00:00`);
  d.setDate(d.getDate() + n);
  return d.toLocaleDateString("en-CA");
};

const monday = (iso: string) => {
  const d = new Date(`${iso}T00:00:00`);
  return days(iso, -((d.getDay() + 6) % 7));
};

// Calendar is two weeks of payment dates (FR-AP-13): each unpaid bill on its planned payment date, else on its due
// date. A bill is planned from its page, or from Plan payment on the bill: no dragging is needed (SC 2.5.7).
export function Calendar({ bills }: { bills: Bill[] }) {
  const id = useId();
  const now = today();
  const [from, setFrom] = useState(() => monday(now));
  const dates = Array.from({ length: 14 }, (_, i) => days(from, i));
  const on = (d: string) => bills.filter((b) => unpaid(b) && (b.planned_payment_on ?? b.due_date) === d);
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head flex-wrap">
        <h2 className="fy-h3" id={id}>
          {helpText("bill.calendar").label}
        </h2>
        <span className="fy-row gap-1">
          <Button variant="ghost" size="sm" type="button" aria-label="Two weeks earlier" onClick={() => setFrom(days(from, -14))}>
            <Icon of={ChevronLeft} />
          </Button>
          <span className="fy-small">
            {short(dates[0]!)} to {short(dates[13]!)}
          </span>
          <Button variant="ghost" size="sm" type="button" aria-label="Two weeks later" onClick={() => setFrom(days(from, 14))}>
            <Icon of={ChevronRight} />
          </Button>
        </span>
      </div>
      <div className="fy-card__body">
        <div className="fy-cal" role="table" aria-label={`${short(dates[0]!)} to ${short(dates[13]!)}`}>
          {/* Rows are display: contents, so the days stay the grid's own cells. */}
          <div role="row" className="contents">
            {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) => (
              <div key={d} className="fy-cal__dow" role="columnheader">
                {d}
              </div>
            ))}
          </div>
          {[0, 7].map((week) => (
            <div key={week} role="row" className="contents">
              {dates.slice(week, week + 7).map((d, i) => (
                <div key={d} role="cell" className={`fy-cal__day${d === now ? " fy-cal__day--today" : ""}${i >= 5 ? " fy-cal__day--muted" : ""}`}>
                  <span className="fy-cal__n">{Number(d.slice(8))}</span>
                  {on(d).map((b) => (
                    <Link key={b.id} className={`fy-cal__ev${b.planned_payment_on ? "" : " fy-cal__ev--due"}`} to="/purchases/bills/$billId" params={{ billId: b.id }}>
                      {b.supplier?.name ?? b.number}
                      <span className="fy-sr">{b.planned_payment_on ? ", planned payment" : ", due"}</span>
                    </Link>
                  ))}
                </div>
              ))}
            </div>
          ))}
        </div>
        <p className="fy-small fy-muted mt-3">{helpText("bill.calendar").tooltip}</p>
      </div>
    </section>
  );
}

// PlanPayment plans, changes or clears a posted bill's payment date (FR-AP-13): it posts nothing.
function PlanPayment({ open, bill, onClose, onDone }: { open: boolean; bill: Bill; onClose: () => void; onDone: (message: string) => void }) {
  const id = useId();
  const [on, setOn] = useState(bill.planned_payment_on ?? bill.early_payment_discount?.until ?? bill.due_date ?? today());
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const send = async (planned: string | undefined) => {
    setBusy(true);
    setProblem(null);
    const read = await api.GET("/v1/bills/{bill_id}", { params: { path: { bill_id: bill.id } } });
    if (!read.data) {
      setBusy(false);
      return setProblem(problemOf(read.error));
    }
    const out = await api.POST("/v1/bills/{bill_id}/payment-plan", {
      params: { path: { bill_id: bill.id }, header: { "If-Match": read.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } },
      body: planned ? { planned_on: planned } : {},
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(planned ? `Payment planned for ${date(planned)}.` : "The payment is no longer planned.");
  };
  const h = helpText("bill.plan-payment");
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={bill.number} title="Plan the payment" description={h.tooltip}>
      {problem && <Refusal problem={problem} title="The payment was not planned" />}
      <form
        className="fy-stack fy-stack--3"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          void send(on);
        }}
        noValidate
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Pay on
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={on} min={bill.date} onChange={(e) => setOn(e.target.value)} aria-describedby={`${id}-help`} />
          <span className="fy-help" id={`${id}-help`}>
            {bill.due_date ? `Due ${date(bill.due_date)}.` : ""}
            {bill.early_payment_discount ? ` ${bill.early_payment_discount.percent}% off if paid by ${date(bill.early_payment_discount.until)}.` : ""}
          </span>
        </div>
        <div className="fy-row justify-end gap-2">
          {bill.planned_payment_on && (
            <Button variant="ghost" type="button" onClick={() => void send(undefined)} disabled={busy}>
              Unschedule
            </Button>
          )}
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !on} title={h.tooltip}>
            <Icon of={CalendarClock} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// PaymentPlanCard is a posted bill's planned payment date and its early-payment discount (FR-AP-13), with Plan
// payment for whoever prepares bills.
export function PaymentPlanCard({ bill, mayPlan, onDone }: { bill: Bill; mayPlan: boolean; onDone: (message: string) => void }) {
  const id = useId();
  const [planning, setPlanning] = useState(false);
  const d = bill.early_payment_discount;
  if (bill.kind !== "bill" || (!unpaid(bill) && !d)) return null;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Paying it
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <dl className="fy-dl">
          {unpaid(bill) && (
            <>
              <dt>Planned payment</dt>
              <dd>{bill.planned_payment_on ? date(bill.planned_payment_on) : "Not planned"}</dd>
            </>
          )}
          {d && (
            <>
              <dt>Early-payment discount</dt>
              <dd>
                {d.percent}% ({formatAmount(d.amount.amount)} {d.amount.currency}) if paid by {date(d.until)}
                {d.until < today() && unpaid(bill) && <span className="fy-small fy-muted"> · passed</span>}
              </dd>
            </>
          )}
        </dl>
        {d && unpaid(bill) && d.until >= today() && <p className="fy-small fy-muted">To take it, pay the rest and enter the supplier&apos;s credit note for the discount.</p>}
        {mayPlan && unpaid(bill) && (
          <div>
            <Button type="button" onClick={() => setPlanning(true)} title={helpText("bill.plan-payment").tooltip}>
              <Icon of={CalendarClock} />
              {bill.planned_payment_on ? "Change the plan…" : "Plan payment…"}
            </Button>
          </div>
        )}
      </div>
      {planning && (
        <PlanPayment
          open={planning}
          bill={bill}
          onClose={() => setPlanning(false)}
          onDone={(message) => {
            setPlanning(false);
            onDone(message);
          }}
        />
      )}
    </section>
  );
}

const whyWords: Record<Reminder["why"], [string, typeof ClockAlert]> = {
  overdue: ["Overdue", TriangleAlert],
  due: ["Due soon", ClockAlert],
  planned: ["Planned", CalendarClock],
};

// BillsToPay is the Inbox's bills to pay soon (FR-AP-13): overdue, or due or planned within the organisation's reminder
// days, for the members who record bill payments.
export function BillsToPay() {
  const id = useId();
  const list = useQuery({
    queryKey: ["bill-reminders"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bill-reminders", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data;
    },
  });
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The bills to pay could not be listed" />;
  if (!list.data) return null;
  const items = list.data.items;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {helpText("inbox.bills-to-pay").label}
        </h2>
        <span className="fy-badge fy-badge--quiet">{items.length}</span>
      </div>
      {items.length === 0 ? (
        <p className="fy-card__body fy-small fy-secondary">No bill is overdue, or due or planned in the next {list.data.days === 1 ? "day" : `${list.data.days} days`}.</p>
      ) : (
        <ul className="fy-list fy-stagger" aria-labelledby={id}>
          {items.map((r) => {
            const [words, icon] = whyWords[r.why];
            return (
              <li key={r.bill_id}>
                <Link className="fy-list__item" to="/purchases/bills/$billId" params={{ billId: r.bill_id }}>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      {r.supplier.name} · {formatAmount(r.open.amount)} {r.open.currency}
                    </span>
                    <span className="fy-list__meta whitespace-normal">
                      <span className="fy-mono">{r.number}</span>
                      {r.due_date ? ` · due ${date(r.due_date)}` : ""}
                      {r.planned_payment_on ? ` · planned ${date(r.planned_payment_on)}` : ""}
                    </span>
                  </span>
                  <span className={`fy-chip ${r.why === "overdue" ? "fy-chip--serious" : "fy-chip--info"}`}>
                    <Icon of={icon} />
                    {words}
                  </span>
                  <Icon of={ChevronRight} />
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
