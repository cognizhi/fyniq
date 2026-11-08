import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { date, month } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { useDimensions, valueCodes } from "../accounting/lines";

type Report = components["schemas"]["BudgetVsActual"];
type Figures = components["schemas"]["VsFigures"];

// pct shows the server's variance percentage with its sign, or "—" when the budget is zero.
function pct(f: Figures): string {
  if (!f.variance_percent) return "—";
  const p = f.variance_percent;
  return p.startsWith("-") ? `−${p.slice(1)}%` : p === "0.0" ? "0.0%" : `+${p}%`;
}

// Favourable says whether a variance is good news: income at or above its budget, costs at or below.
function Favourable({ good }: { good: boolean }) {
  return good ? (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Favourable
    </span>
  ) : (
    <span className="fy-chip fy-chip--warning">
      <Icon of={TriangleAlert} />
      Unfavourable
    </span>
  );
}

// BudgetVsActual is Reports › Budget vs Actual (FR-RPT-03; p1-planning design D6): each income and expense account, or
// account and set of dimension values, for months of one financial year, its actual from the books against the year's
// latest recorded budget, the variance, its percentage of the budget and whether it is favourable, with the income,
// cost and net totals. Every figure is the server's. Expert only.
export function BudgetVsActual() {
  const id = useId();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [by, setBy] = useState<"account" | "dimension">("account");
  const dims = useDimensions();
  const q = useQuery({
    queryKey: ["budget-vs-actual", from, to, by],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/reports/budget-vs-actual", { params: { query: { from: from || undefined, to: to || undefined, by } } });
      if (error) throw error;
      return data;
    },
    retry: false,
  });
  const r: Report | undefined = q.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Budget vs Actual</h1>
          {r && (
            <p className="fy-secondary">
              {month(r.from)} to {month(r.to)} · {r.currency} ·{" "}
              {r.budget_id ? (
                <Link to="/planning/budgets">{r.budget_name}</Link>
              ) : (
                `No recorded budget for the year from ${date(r.financial_year)}: every budget figure is 0`
              )}
            </p>
          )}
        </div>
      </div>
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            From
          </label>
          <input className="fy-input" id={`${id}-from`} type="month" value={from} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To
          </label>
          <input className="fy-input" id={`${id}-to`} type="month" value={to} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div className="fy-segmented self-end" role="group" aria-label="Rows" title={helpText("budget-vs-actual.by").tooltip}>
          <button type="button" aria-pressed={by === "account"} onClick={() => setBy("account")}>
            By account
          </button>
          <button type="button" aria-pressed={by === "dimension"} onClick={() => setBy("dimension")}>
            By dimension
          </button>
        </div>
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="Budget vs Actual could not be shown" />}
      {r && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact fy-statement">
            <caption className="fy-sr">
              Budget vs Actual from {month(r.from)} to {month(r.to)}, in {r.currency}
            </caption>
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Actual
                </th>
                <th scope="col" className="fy-num">
                  Budget
                </th>
                <th scope="col" className="fy-num">
                  Variance
                </th>
                <th scope="col" className="fy-num">
                  %
                </th>
                <th scope="col">
                  <span title={helpText("budget-vs-actual.favourable").tooltip}>{helpText("budget-vs-actual.favourable").label}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {r.rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="fy-secondary">
                    No income or expense in these months, and nothing budgeted.
                  </td>
                </tr>
              )}
              {r.rows.map((x, i) => (
                <tr key={`${x.account_id}-${i}`}>
                  <th scope="row">
                    <span className="fy-acct">{x.code}</span>
                    {x.name}
                    {by === "dimension" && (x.dimension_value_ids?.length ?? 0) > 0 && (
                      <span className="fy-small fy-muted block">{valueCodes(x.dimension_value_ids ?? [], dims.dimensions, dims.byId)}</span>
                    )}
                  </th>
                  <td className="fy-num">{formatAmount(x.actual)}</td>
                  <td className="fy-num">{formatAmount(x.budget)}</td>
                  <td className="fy-num">{formatAmount(x.variance)}</td>
                  <td className="fy-num">{pct(x)}</td>
                  <td>
                    <Favourable good={x.favourable} />
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              {(
                [
                  ["Income", r.income, true],
                  ["Costs", r.costs, false],
                  ["Net", r.net, true],
                ] as const
              ).map(([label, f, income]) => (
                <tr key={label} className="fy-strong">
                  <th scope="row">{label}</th>
                  <td className="fy-num">{formatAmount(f.actual)}</td>
                  <td className="fy-num">{formatAmount(f.budget)}</td>
                  <td className="fy-num">{formatAmount(f.variance)}</td>
                  <td className="fy-num">{pct(f)}</td>
                  <td>
                    <Favourable good={income ? !f.variance.startsWith("-") : f.variance.startsWith("-") || /^0*\.?0*$/.test(f.variance)} />
                  </td>
                </tr>
              ))}
            </tfoot>
          </table>
        </div>
      )}
      {r && (
        <p className="fy-small fy-muted">
          Read at ledger revision {r.ledger_revision}. Amounts are signed as the P&L shows them. The actuals are the books'; the budget is the year's latest
          recorded version.
        </p>
      )}
    </div>
  );
}
