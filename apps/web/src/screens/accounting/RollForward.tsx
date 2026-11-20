import { useQuery } from "@tanstack/react-query";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

// The deferral roll-forward (FR-RPT-13; p2-reporting-planning task 5.1; docs/design/screens Schedules): for a run of months, what the schedules held, what was added,
// released and reversed and what they hold at the end, tied to the ledger; and per customer what was billed, collected and recognised, side by side with its deferred revenue.

type Report = components["schemas"]["RollForward"];
type Money = components["schemas"]["Money"];

const money = (m: Money | undefined | null) => (m ? formatAmount(m.amount) : "—");
const monthOf = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
const holdingWords = { prepayment: "Prepayments by supplier", deferred_revenue: "Deferred revenue by customer" } as const;

export function RollForward() {
  const now = monthOf(new Date());
  const [from, setFrom] = useState(now);
  const [to, setTo] = useState(now);
  const valid = /^[0-9]{4}-(0[1-9]|1[0-2])$/.test(from) && /^[0-9]{4}-(0[1-9]|1[0-2])$/.test(to) && from <= to;
  const q = useQuery({
    queryKey: ["schedules", "roll-forward", from, to],
    enabled: valid,
    queryFn: async (): Promise<Report> => {
      const { data, error } = await api.GET("/v1/reports/deferral-roll-forward", { params: { query: { from, to } } });
      if (error) throw error;
      return data;
    },
  });
  const id = useId();
  const r = q.data;
  return (
    <section className="fy-stack fy-stack--3" aria-label="Roll-forward">
      <p className="fy-small fy-secondary">{helpText("schedules.roll-forward").tooltip}</p>
      <div className="fy-row fy-row--wrap">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            From (YYYY-MM)
          </label>
          <input id={`${id}-from`} className="fy-input" value={from} onChange={(e) => setFrom(e.target.value)} inputMode="numeric" maxLength={7} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To (YYYY-MM)
          </label>
          <input id={`${id}-to`} className="fy-input" value={to} onChange={(e) => setTo(e.target.value)} inputMode="numeric" maxLength={7} />
        </div>
      </div>
      {!valid && <p className="fy-error">Enter the first and last month as YYYY-MM, the first not after the last.</p>}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The roll-forward could not be run" />}
      {r?.holdings.map((h) => (
        <div className="fy-tablewrap" key={h.kind}>
          <table className="fy-table fy-table--compact" aria-label={holdingWords[h.kind]}>
            <caption className="fy-small fy-strong">
              {holdingWords[h.kind]} · <span className="fy-mono">{h.account_code}</span> {h.account_name}, {r.currency}
            </caption>
            <thead>
              <tr>
                <th scope="col">{h.kind === "deferred_revenue" ? "Customer" : "Supplier"}</th>
                <th scope="col" className="fy-num">Opening</th>
                <th scope="col" className="fy-num">Added</th>
                <th scope="col" className="fy-num">Released</th>
                <th scope="col" className="fy-num">Reversed</th>
                <th scope="col" className="fy-num">Closing</th>
              </tr>
            </thead>
            <tbody>
              {h.rows.map((x) => (
                <tr key={x.party_id ?? x.party_name}>
                  <th scope="row">{x.party_name}</th>
                  <td className="fy-num">{money(x.opening)}</td>
                  <td className="fy-num">{money(x.added)}</td>
                  <td className="fy-num">{money(x.released)}</td>
                  <td className="fy-num">{money(x.reversed)}</td>
                  <td className="fy-num">{money(x.closing)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <th scope="row">Total</th>
                <td className="fy-num fy-strong">{money(h.total.opening)}</td>
                <td className="fy-num fy-strong">{money(h.total.added)}</td>
                <td className="fy-num fy-strong">{money(h.total.released)}</td>
                <td className="fy-num fy-strong">{money(h.total.reversed)}</td>
                <td className="fy-num fy-strong">{money(h.total.closing)}</td>
              </tr>
            </tfoot>
          </table>
          <p className="fy-small">
            {h.ties ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Ties to {h.account_code} in the ledger ({money(h.ledger_balance)})
              </span>
            ) : (
              <span className="fy-chip fy-chip--critical">
                <Icon of={TriangleAlert} />
                Differs from {h.account_code} by {money(h.unexplained)}: the close refuses it
              </span>
            )}
          </p>
        </div>
      ))}
      {r && r.holdings.length === 0 && (
        <p className="fy-secondary" role="status">
          The chart has no Prepayments or Deferred revenue account yet.
        </p>
      )}
      {r && r.customers.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact" aria-label="Customers billed, collected and recognised">
            <caption className="fy-small fy-strong">{helpText("schedules.customer-activity").label}, {r.currency}</caption>
            <thead>
              <tr>
                <th scope="col">Customer</th>
                <th scope="col" className="fy-num">Billed</th>
                <th scope="col" className="fy-num">Collected</th>
                <th scope="col" className="fy-num">Recognised</th>
                <th scope="col" className="fy-num">Still deferred</th>
                <th scope="col" className="fy-num">What they owe, moved by</th>
                <th scope="col">Check</th>
              </tr>
            </thead>
            <tbody>
              {r.customers.map((c) => (
                <tr key={c.party_id}>
                  <th scope="row">{c.party_name}</th>
                  <td className="fy-num">{money(c.billed)}</td>
                  <td className="fy-num">{money(c.collected)}</td>
                  <td className="fy-num" title={`Released from schedules ${money(c.released)}, straight to revenue ${money(c.direct)}`}>
                    {money(c.recognised)}
                  </td>
                  <td className="fy-num">{money(c.deferred_closing)}</td>
                  <td className="fy-num">{money(c.receivable_movement)}</td>
                  <td>
                    {c.explained ? (
                      <span className="fy-chip fy-chip--good">
                        <Icon of={CircleCheck} />
                        {Number(c.other_movement.amount) === 0 ? "Billed less collected explains it" : `Explained, with ${money(c.other_movement)} from other documents`}
                      </span>
                    ) : (
                      <span className="fy-chip fy-chip--warning">
                        <Icon of={TriangleAlert} />
                        {money(c.unexplained)} not explained by documents
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
