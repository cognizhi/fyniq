import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, CalendarRange, CircleCheck, CircleX, Hourglass, TriangleAlert, Undo2 } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date, month, timeZone } from "../../format/date";
import { isZero } from "../../format/decimal";
import { Icon, problemOf } from "../../ui";

type Report = components["schemas"]["Schedules"];
type Schedule = components["schemas"]["Schedule"];
type Holding = components["schemas"]["ScheduleHolding"];
type Month = components["schemas"]["ScheduleMonth"];

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// keyOf identifies a schedule: its document and line.
const keyOf = (s: Schedule) => `${s.source_id}:${s.line}`;

const holdingWords: Record<Holding["kind"], string> = {
  prepayment: "Prepayments still to expense",
  deferred_revenue: "Income still to earn",
};

const groupWords: Record<Schedule["kind"], string> = {
  prepayment: "Prepaid costs",
  deferred_revenue: "Income received in advance",
};

const sourceWords: Record<Schedule["source_type"], string> = { bill: "Bill", invoice: "Invoice", bank_transaction: "Bank line" };

function Source({ s }: { s: Schedule }) {
  const label = s.number ?? sourceWords[s.source_type];
  if (s.source_type === "bill")
    return (
      <Link className="fy-mono" to="/purchases/bills/$billId" params={{ billId: s.source_id }}>
        {label}
      </Link>
    );
  if (s.source_type === "invoice")
    return (
      <Link className="fy-mono" to="/sales/invoices/$invoiceId" params={{ invoiceId: s.source_id }}>
        {label}
      </Link>
    );
  return <span className="fy-mono">{label}</span>;
}

// span is a schedule's months and how many are posted: "Sep 2026–Dec 2026 · 2 of 4".
function span(s: Schedule): string {
  const first = s.months[0]?.month;
  const last = s.months[s.months.length - 1]?.month;
  const range = first === last ? month(first) : `${month(first)}–${month(last)}`;
  return `${range} · ${s.months_posted} of ${s.months.length}`;
}

// Tie is how a holding account stands against its schedules (INV-07), as the close checks it.
function Tie({ h, asAt }: { h: Holding; asAt: string }) {
  if (h.status === "differs")
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={CircleX} />
        Differs by {formatAmount(h.unexplained.amount)}: the close refuses it
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Ties to the ledger at {date(asAt)}
    </span>
  );
}

function HoldingTile({ h, asAt }: { h: Holding; asAt: string }) {
  const others = h.explained.map((e) => `${formatAmount(e.amount.amount)} from ${e.source_type === "manual_journal" ? "manual journals" : e.source_type === "opening_balance" ? "opening balances" : e.source_type.replaceAll("_", " ")}`);
  return (
    <div className="fy-tile">
      <span className="fy-tile__label">
        {holdingWords[h.kind]} · <span className="fy-mono">{h.account_code}</span>
      </span>
      <span className="fy-tile__value fy-num">
        {formatAmount(h.balance.amount)}
        <small>{h.balance.currency}</small>
      </span>
      <span className="fy-tile__meta">
        {h.open} {h.open === 1 ? "schedule" : "schedules"} hold {formatAmount(h.schedules.amount)}
      </span>
      <Tie h={h} asAt={asAt} />
      {!isZero(h.direct.amount) && (
        <span className="fy-small">
          <Icon of={TriangleAlert} /> {formatAmount(h.direct.amount)} entered on {h.account_code} directly, which nothing releases
        </span>
      )}
      {others.length > 0 && (
        <span className="fy-small">
          <Icon of={TriangleAlert} /> {others.join(", ")}
        </span>
      )}
    </div>
  );
}

function MonthState({ m }: { m: Month }) {
  switch (m.status) {
    case "posted":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Posted
        </span>
      );
    case "reversed":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Undo2} />
          Reversed
        </span>
      );
    case "waiting":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={Hourglass} />
          Waiting
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={CalendarClock} />
          Future
        </span>
      );
  }
}

function Peek({ s, holding }: { s: Schedule; holding?: Holding }) {
  const id = useId();
  return (
    <aside className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <div className="fy-stack fy-stack--2">
          <span className="fy-eyebrow">
            {s.number ?? sourceWords[s.source_type]} · line {s.line}
          </span>
          <h2 className="fy-h3" id={id}>
            {s.description || sourceWords[s.source_type]}
          </h2>
        </div>
      </div>
      <table className="fy-table fy-table--compact">
        <caption className="fy-sr">The months of {s.description || "the schedule"}</caption>
        <thead>
          <tr>
            <th scope="col">Month</th>
            <th scope="col" className="fy-num">
              Amount
            </th>
            <th scope="col">State</th>
          </tr>
        </thead>
        <tbody>
          {s.months.map((m) => (
            <tr key={m.month}>
              <td className={m.status === "projected" ? "fy-muted" : undefined}>{month(m.month)}</td>
              <td className={m.status === "projected" ? "fy-num fy-muted" : "fy-num"}>{formatAmount(m.amount.amount)}</td>
              <td>
                <MonthState m={m} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="fy-card__body">
        <p className="fy-small fy-muted">
          Months are rounded on the running total, so they can differ by a cent and add up to exactly {formatAmount(s.total.amount)} (<span className="fy-mono">ALLOC-EVEN</span>). Each posts{" "}
          {s.kind === "prepayment" ? (
            <>
              Dr <span className="fy-mono">{s.account_code}</span> {s.account_name}, Cr <span className="fy-mono">{holding?.account_code ?? "1210"}</span> {holding?.account_name ?? "Prepayments"}
            </>
          ) : (
            <>
              Dr <span className="fy-mono">{holding?.account_code ?? "2400"}</span> {holding?.account_name ?? "Deferred revenue"}, Cr <span className="fy-mono">{s.account_code}</span> {s.account_name}
            </>
          )}
          , on the month's first day, once that day is reached.
        </p>
      </div>
    </aside>
  );
}

// Schedules is Accounting › Schedules (docs/design/screens Schedules; FR-GL-03, INV-07): every cost and income spread
// over months as at a day, what each has recognised and what remains, each month posted, waiting or still ahead, and
// 1210 Prepayments and 2400 Deferred revenue tied to them as the close checks them. The roll-forward is P2 (FR-RPT-13).
export function Schedules() {
  const id = useId();
  const [asAt, setAsAt] = useState(today);
  const [finished, setFinished] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const report = useQuery({
    queryKey: ["schedules", asAt, finished],
    enabled: Boolean(asAt),
    queryFn: async (): Promise<Report> => {
      const r = await api.GET("/v1/reports/schedules", { params: { query: { as_at: asAt, status: finished ? "all" : "open" } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const items = report.data?.schedules ?? [];
  const chosen = items.find((s) => keyOf(s) === selected) ?? items[0];
  const holdingOf = (kind: Schedule["kind"]) => report.data?.holdings.find((h) => h.kind === kind);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Prepayment and deferral schedules</h1>
          <p className="fy-secondary">Costs and income spread over the months they belong to. Each month posts only once that month is reached.</p>
        </div>
      </div>
      <ReviewBanner screen="schedules" />
      <div className="fy-card fy-card--pad fy-form fy-form--2">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-as-at`}>
            As at
          </label>
          <input className="fy-input" id={`${id}-as-at`} type="date" value={asAt} onChange={(e) => setAsAt(e.target.value)} />
        </div>
        <label className="fy-row gap-2 self-end">
          <input type="checkbox" checked={finished} onChange={(e) => setFinished(e.target.checked)} />
          Show finished schedules
        </label>
      </div>
      {report.isError && <Refusal problem={problemOf(report.error)} title="The schedules could not be shown" />}
      {report.isFetching && !report.data && <p className="fy-secondary">Reading the schedules…</p>}
      {report.data && report.data.holdings.length > 0 && (
        <div className="fy-grid fy-grid--2 fy-stagger">
          {report.data.holdings.map((h) => (
            <HoldingTile key={h.kind} h={h} asAt={report.data.as_at} />
          ))}
        </div>
      )}
      {report.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={CalendarRange} size={24} />
          <h2 className="fy-h3">Nothing is spread over months</h2>
          <p className="fy-secondary">Spread a bill line's cost, an invoice line's income or a bank line over the months it covers, and its schedule shows here, month by month.</p>
        </div>
      )}
      {items.length > 0 && chosen && (
        <div className="fy-split">
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards">
              <caption className="fy-sr">Schedules as at {date(report.data?.as_at)}, in {report.data?.currency}</caption>
              <thead>
                <tr>
                  <th scope="col">Schedule</th>
                  <th scope="col" className="fy-from-medium">
                    Source
                  </th>
                  <th scope="col" className="fy-num fy-from-medium">
                    Total
                  </th>
                  <th scope="col" className="fy-from-expanded">
                    Months
                  </th>
                  <th scope="col" className="fy-num fy-from-expanded">
                    Recognised
                  </th>
                  <th scope="col" className="fy-num">
                    Remaining
                  </th>
                </tr>
              </thead>
              <tbody>
                {(["prepayment", "deferred_revenue"] as const).flatMap((kind) => {
                  const rows = items.filter((s) => s.kind === kind);
                  if (rows.length === 0) return [];
                  return [
                    <tr key={kind} className="fy-group">
                      <td colSpan={6}>{groupWords[kind]}</td>
                    </tr>,
                    ...rows.map((s) => (
                      <tr key={keyOf(s)} aria-selected={keyOf(s) === keyOf(chosen)}>
                        <td data-card="title">
                          <button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-start font-[inherit] text-[var(--link)] underline-offset-2 hover:underline" onClick={() => setSelected(keyOf(s))}>
                            {s.description || sourceWords[s.source_type]}
                          </button>
                          <span className="fy-small fy-muted block">
                            <span className="fy-mono">{s.account_code}</span> {s.account_name}
                          </span>
                        </td>
                        <td data-card="meta" className="fy-from-medium">
                          <Source s={s} />
                        </td>
                        <td data-card="hide" className="fy-num fy-from-medium">
                          {formatAmount(s.total.amount)}
                        </td>
                        <td data-card="hide" className="fy-from-expanded">
                          {span(s)}
                        </td>
                        <td data-card="hide" className="fy-num fy-from-expanded">
                          {formatAmount(s.recognised.amount)}
                        </td>
                        <td data-card="amount" className="fy-num">
                          {formatAmount(s.remaining.amount)}
                        </td>
                      </tr>
                    )),
                  ];
                })}
              </tbody>
            </table>
          </div>
          <Peek s={chosen} holding={holdingOf(chosen.kind)} />
        </div>
      )}
    </div>
  );
}
