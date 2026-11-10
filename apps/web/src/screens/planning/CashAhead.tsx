import { useQuery } from "@tanstack/react-query";
import { CircleCheck, CircleX, FlaskConical, RotateCcw, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Chart } from "../../components/chart/Chart";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";

type Projection = components["schemas"]["CashProjection"];
type Movement = components["schemas"]["CashMovement"];
type Move = { to?: string; leave_out?: boolean };

// The ranges a person picks: 30, 60, 90 or 180 days, or the 13 weeks (FR-PLN-09, FR-PLN-05).
const ranges = [
  { days: 30, label: "30 days" },
  { days: 60, label: "60 days" },
  { days: 90, label: "90 days" },
  { days: 180, label: "180 days" },
  { days: 91, label: "13 weeks" },
] as const;
type Days = (typeof ranges)[number]["days"];

// readProjection asks for the projection, or, with moves, the what-if: it saves nothing either way (FR-PLN-08).
async function readProjection(days: Days, moves: Record<string, Move>): Promise<Projection> {
  const list = Object.entries(moves).map(([key, m]) => ({ key, ...m }));
  if (list.length === 0) {
    const { data, error } = await api.GET("/v1/cash-projection", { params: { query: { days } } });
    if (error) throw error;
    return data;
  }
  const { data, error } = await api.POST("/v1/cash-projection:what-if", { body: { days, moves: list } });
  if (error) throw error;
  return data;
}

// what names a movement in plain words.
export function what(m: Movement): string {
  const who = m.contact_name ? `, ${m.contact_name}` : "";
  switch (m.kind) {
    case "invoice":
      return `${m.number || "Invoice"}${who}, expected`;
    case "bill":
      return `${m.number || "Bill"}${who}, to pay`;
    case "recurring_invoice":
      return `${m.number}, its ${date(m.occurrence)} invoice${who}`;
    case "recurring_bill":
      return `${m.number}, its ${date(m.occurrence)} bill${who}`;
    default:
      return `Payment batch ${m.number}`;
  }
}

// why says why a movement is on its day.
export function why(m: Movement): string {
  switch (m.basis) {
    case "history":
      return m.days_late ? `Due ${date(m.due_date)}; pays ${m.days_late} days late on average` : `Due ${date(m.due_date)}; pays on time`;
    case "due":
      return `Due ${date(m.due_date)}`;
    case "planned":
      return "Planned payment date";
    case "overdue":
      return m.due_date ? `Overdue since ${date(m.due_date)}` : "Overdue";
    case "scheduled":
      return "Batch payment date";
    default:
      return "Moved in this what-if";
  }
}

// CashAhead is Planning › Cash ahead (docs/design/screens CashForecast; FR-PLN-09, FR-PLN-05, FR-PLN-08; p1-planning design
// D7, D12): the money accounts' balance day by day from today, or in 13 weeks, from the books' open items, recurring
// documents and approved batches on their expected days; the lowest balance, the balance at the end and the buffer line,
// with a warning when the balance falls below it; the chart with its table view; what moves it; and what the projection
// does not hold yet, in both modes. Expert adds the what-ifs, which move or leave out a movement and save nothing. Every
// figure is the server's, never worked out here (FR-UX-22).
export function CashAhead() {
  const id = useId();
  const { mode } = useSignedIn();
  const [days, setDays] = useState<Days>(60);
  const [moves, setMoves] = useState<Record<string, Move>>({});
  const q = useQuery({ queryKey: ["cash-projection", days, moves], queryFn: () => readProjection(days, moves), retry: false });
  const p = q.data;
  const whatIf = Object.keys(moves).length > 0;
  const weekly = days === 91;
  const last = p?.daily[p.daily.length - 1];
  const move = (key: string, m: Move | null) =>
    setMoves((all) => {
      const next = { ...all };
      if (m) next[key] = m;
      else delete next[key];
      return next;
    });
  const sandbox = helpText("planning.sandbox");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Planning</span>
          <h1 className="fy-h1">Cash ahead</h1>
          <p className="fy-secondary">
            {p ? `From today's ${formatAmount(p.opening.amount)} ${p.currency} in the bank, ` : "From today's bank balances, "}
            with the receipts and payments the books expect.
          </p>
          <span className="fy-badge fy-badge--quiet self-start" title={sandbox.tooltip}>
            <Icon of={FlaskConical} />
            {sandbox.label}
          </span>
        </div>
        <div className="fy-pagehead__actions">
          <div className="fy-segmented" role="group" aria-label="Range" title={helpText("cash.range").tooltip}>
            {ranges.map((r) => (
              <button key={r.days} type="button" aria-pressed={days === r.days} onClick={() => setDays(r.days)}>
                {r.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {q.isError && <Refusal problem={problemOf(q.error)} title="The cash projection could not be shown" />}
      {q.isFetching && !p && <p className="fy-secondary">Reading the books…</p>}
      {p && (
        <>
          {p.below_buffer_on && (
            <div className="fy-banner fy-banner--warning" role="status">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <strong>
                  {p.below_buffer_on === p.today ? "Cash is below the buffer today" : `Cash falls below the buffer on ${date(p.below_buffer_on)}`}
                </strong>
                <span className="fy-small">
                  The buffer is {p.buffer.months} months of net burn: {formatAmount(p.buffer.line?.amount ?? "0")} {p.currency}. It is based on
                  the inputs Fyniq has; see what is not included yet below.
                </span>
              </div>
            </div>
          )}
          {whatIf && (
            <div className="fy-banner fy-banner--info" role="status">
              <Icon of={FlaskConical} size={20} />
              <div className="fy-banner__text">
                <strong>What-if: {Object.keys(moves).length === 1 ? "1 change" : `${Object.keys(moves).length} changes`}</strong>
                <span className="fy-small">Nothing is posted, planned or sent.</span>
              </div>
              <Button variant="ghost" size="sm" onClick={() => setMoves({})} title={helpText("cash.reset").tooltip}>
                <Icon of={RotateCcw} />
                {helpText("cash.reset").label}
              </Button>
            </div>
          )}
          <div className="fy-grid fy-grid--3">
            <div className="fy-tile" data-cash="lowest">
              <span className="fy-tile__label" title={helpText("cash.lowest").tooltip}>
                {helpText("cash.lowest").label}
              </span>
              <span className="fy-tile__value fy-num">
                {formatAmount(p.lowest.amount)} <small>{p.currency}</small>
              </span>
              <span className="fy-tile__meta">On {date(p.lowest_on)}</span>
            </div>
            <div className="fy-tile" data-cash="end">
              <span className="fy-tile__label">{weekly ? "Balance after 13 weeks" : `Balance in ${p.days} days`}</span>
              <span className="fy-tile__value fy-num">
                {last ? formatAmount(last.close.amount) : "—"} <small>{p.currency}</small>
              </span>
              <span className="fy-tile__meta">{last ? date(last.date) : ""}</span>
            </div>
            <div className="fy-tile" data-cash="buffer">
              <span className="fy-tile__label" title={helpText("cash.buffer").tooltip}>
                {helpText("cash.buffer").label}
              </span>
              <span className="fy-tile__value fy-num">
                {p.buffer.line ? (
                  <>
                    {formatAmount(p.buffer.line.amount)} <small>{p.currency}</small>
                  </>
                ) : (
                  "—"
                )}
              </span>
              <span className="fy-tile__meta">
                {!p.buffer.line ? (
                  <span className="whitespace-normal">No line: {p.buffer.why}</span>
                ) : p.below_buffer_on ? (
                  <span className="fy-chip fy-chip--critical">
                    <Icon of={CircleX} />
                    Below from {date(p.below_buffer_on)}
                  </span>
                ) : (
                  <span className="fy-chip fy-chip--good">
                    <Icon of={CircleCheck} />
                    Never below
                  </span>
                )}
              </span>
            </div>
          </div>

          <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-chart`}>
            <h2 className="fy-sr" id={`${id}-chart`}>
              Projected balance
            </h2>
            <Chart
              title={weekly ? "Projected balance by week" : "Projected balance"}
              summary={`The balance goes from ${formatAmount(p.opening.amount)} ${p.currency} today to ${last ? formatAmount(last.close.amount) : "—"} on ${
                last ? date(last.date) : "the last day"
              }; its lowest is ${formatAmount(p.lowest.amount)} on ${date(p.lowest_on)}.${p.buffer.line ? ` The buffer is ${formatAmount(p.buffer.line.amount)}.` : ""}`}
              form="line"
              label={weekly ? "Week ending" : "Day"}
              categories={weekly ? p.weekly.map((w) => date(w.to)) : p.daily.map((d) => date(d.date))}
              series={[
                { key: "balance", name: "Balance", slot: 0, values: weekly ? p.weekly.map((w) => w.close.amount) : p.daily.map((d) => d.close.amount) },
                ...(p.buffer.line ? [{ key: "buffer", name: "Buffer", slot: 7, values: (weekly ? p.weekly : p.daily).map(() => p.buffer.line!.amount) }] : []),
              ]}
              unit={p.currency}
              file={`cash-ahead-${p.today}-${p.days}`}
              level={3}
            />
          </section>

          <div className="fy-split">
            <section className="fy-card" aria-labelledby={`${id}-moves`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-moves`}>
                  What moves it
                </h2>
              </div>
              {p.movements.length === 0 ? (
                <p className="fy-card__body fy-secondary">Nothing the books expect in these days.</p>
              ) : (
                <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
                  <table className="fy-table fy-table--cards">
                    <caption className="fy-sr">The receipts and payments expected, by day, in {p.currency}</caption>
                    <thead>
                      <tr>
                        <th scope="col">Date</th>
                        <th scope="col">Item</th>
                        <th scope="col" className="fy-num">
                          Amount ({p.currency})
                        </th>
                        {mode === "expert" && <th scope="col">What if</th>}
                      </tr>
                    </thead>
                    <tbody>
                      {p.movements.map((m) => (
                        <tr key={m.key}>
                          <td data-card="meta">{date(m.expected_on)}</td>
                          <td data-card="title">
                            {what(m)}
                            <span className="fy-small fy-muted block">{why(m)}</span>
                          </td>
                          <td className="fy-num" data-card="amount">
                            {formatAmount(m.amount.amount)}
                          </td>
                          {mode === "expert" && (
                            <td data-card="meta">
                              <WhatIf m={m} today={p.today} onMove={(x) => move(m.key, x)} />
                            </td>
                          )}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
            <aside className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-missing`}>
              <h2 className="fy-h3" id={`${id}-missing`} title={helpText("cash.not-included").tooltip}>
                {helpText("cash.not-included").label}
              </h2>
              <p className="fy-small fy-muted">The lowest point may be lower than shown until these join.</p>
              <ul className="fy-stack fy-stack--2">
                {p.not_included.map((x) => (
                  <li key={x.what} className="fy-small">
                    <span className="fy-strong">{x.what}</span>: {x.why}
                  </li>
                ))}
              </ul>
            </aside>
          </div>
        </>
      )}
    </div>
  );
}

// WhatIf moves one movement to another day, or leaves it out, for the what-if (Expert; design D12).
function WhatIf({ m, today, onMove }: { m: Movement; today: string; onMove: (m: Move | null) => void }) {
  const id = useId();
  const moved = m.basis === "moved";
  return (
    <div className="fy-row fy-row--wrap gap-2">
      <label className="fy-sr" htmlFor={`${id}-to`}>
        Move {what(m)} to
      </label>
      <input
        className="fy-input max-w-[11rem]"
        id={`${id}-to`}
        type="date"
        min={today}
        value={moved ? m.expected_on : ""}
        title={helpText("cash.move").tooltip}
        onChange={(e) => onMove(e.target.value ? { to: e.target.value } : null)}
      />
      <Button variant="ghost" size="sm" onClick={() => onMove({ leave_out: true })} title={helpText("cash.leave-out").tooltip} aria-label={`Leave out ${what(m)}`}>
        {helpText("cash.leave-out").label}
      </Button>
    </div>
  );
}
