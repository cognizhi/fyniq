import { useQuery } from "@tanstack/react-query";
import { CalendarClock } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { Icon } from "../../ui";

type Payables = components["schemas"]["Payables"];
type Row = components["schemas"]["PayablesRow"];

const buckets: [keyof Row, string][] = [
  ["current", "Not yet due"],
  ["days_1_30", "1–30 days"],
  ["days_31_60", "31–60 days"],
  ["days_61_90", "61–90 days"],
  ["days_90_plus", "Over 90 days"],
  ["credits", "Credits"],
  ["total", "Total"],
];

const zero = (s: string) => /^-?0(\.0*)?$/.test(s);

// PayablesSummary is what the company owes its suppliers (FR-AP-06; docs/design/screens BillsBoard's AP ageing summary
// and cash requirement): in both modes the cash the bills due in the next 30 days need, with what is overdue; in Expert
// mode the ageing by days past due. Figures come from the server only (FR-UX-22).
export function PayablesSummary() {
  const id = useId();
  const q = useQuery({
    queryKey: ["payables"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/payables", { params: { query: { within: 30 } } });
      if (error) throw error;
      return data as Payables;
    },
  });
  const p = q.data;
  if (!p || (p.totals ?? []).length === 0) return null;
  const requirement = p.requirement ?? [];
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          What we owe suppliers
        </h2>
        <span className="fy-small fy-muted">As at today</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {requirement.length === 0 ? (
          <p className="fy-plain fy-small">Nothing falls due in the next 30 days.</p>
        ) : (
          <ul className="fy-stack fy-stack--1">
            {requirement.map((r) => (
              <li key={r.currency} className="fy-row items-start gap-2">
                <Icon of={CalendarClock} size={16} />
                <span className="min-w-0">
                  <span className="fy-strong fy-num">
                    {formatAmount(r.total.amount)} {r.currency}
                  </span>{" "}
                  to pay in the next 30 days
                  {!zero(r.overdue.amount) && (
                    <>
                      , <span className="fy-num">{formatAmount(r.overdue.amount)}</span> of it overdue
                    </>
                  )}
                  .
                </span>
              </li>
            ))}
          </ul>
        )}
        <div className="fy-tablewrap fy-expert-only">
          <table className="fy-table fy-table--compact" aria-label="AP ageing">
            <thead>
              <tr>
                <th scope="col">Currency</th>
                {buckets.map(([k, label]) => (
                  <th key={k} scope="col" className="fy-num">
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(p.totals ?? []).map((r) => (
                <tr key={r.currency}>
                  <th scope="row">{r.currency}</th>
                  {buckets.map(([k]) => {
                    const m = r[k] as components["schemas"]["Money"];
                    return (
                      <td key={k} className="fy-num">
                        {zero(m.amount) ? "—" : formatAmount(m.amount)}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}
