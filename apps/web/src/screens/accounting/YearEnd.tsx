import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { negate, sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { monthOf } from "./Periods";

type Year = components["schemas"]["YearEnd"];

// YearEnd is the close page's year end (docs/design/screens PeriodClose "Year end"; FR-GL-10, PRD-001 rev 0.69): the
// financial year that holds the month, how many of its months are closed, its result so far and the retained earnings it
// carries into the next year. Year end is computed, never posted, so there is nothing to do here but read it. The API
// gives trial-balance signs (credits negative); the page shows income, a profit and retained earnings as people read
// them, by moving the sign only: every figure is the server's.
export function YearEnd({ periodId }: { periodId: string }) {
  const id = useId();
  const h = helpText("close.year-end");
  const q = useQuery({
    queryKey: ["year-end", periodId],
    queryFn: async () => {
      const r = await api.GET("/v1/periods/{period_id}/year-end", { params: { path: { period_id: periodId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const y: Year | undefined = q.data;
  const profit = y ? sign(y.result.amount) <= 0 : true;
  const so = y?.closed ? "" : " so far";
  const re = y?.retained_earnings;
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-ye`}>
      <div className="fy-row fy-row--wrap gap-2">
        <h2 className="fy-h3" id={`${id}-ye`}>
          {h.label}
        </h2>
        {y &&
          (y.closed ? (
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              Year closed
            </span>
          ) : (
            <span className="fy-chip fy-chip--neutral">
              {y.periods_closed} of {y.periods} months closed
            </span>
          ))}
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The year end could not be shown" />}
      {y && (
        <>
          <p className="fy-small">
            Financial year {y.number}: {date(y.starts_on)} to {date(y.ends_on)}.{" "}
            {y.closed ? (
              "Its last month is closed, so its figures are final."
            ) : y.last_period.id === periodId ? (
              "Closing this month closes the year."
            ) : (
              <>
                It closes with{" "}
                <Link to="/accounting/close/$periodId" params={{ periodId: y.last_period.id }}>
                  {monthOf(y.last_period)}
                </Link>
                .
              </>
            )}
          </p>
          <dl className="fy-dl">
            <div>
              <dt>Income{so}</dt>
              <dd className="fy-num">
                {y.income.currency} {formatAmount(negate(y.income.amount))}
              </dd>
            </div>
            <div>
              <dt>Expenses{so}</dt>
              <dd className="fy-num">
                {y.expenses.currency} {formatAmount(y.expenses.amount)}
              </dd>
            </div>
            <div>
              <dt>
                {profit ? "Profit" : "Loss"}
                {so}
              </dt>
              <dd className="fy-num">
                {y.result.currency} {formatAmount(profit ? negate(y.result.amount) : y.result.amount)}
              </dd>
            </div>
          </dl>
          {re ? (
            <table className="fy-table">
              <caption className="fy-small fy-secondary" style={{ textAlign: "start", captionSide: "top" }}>
                {re.code} {re.name}, in {re.start.currency}
              </caption>
              <tbody>
                <tr>
                  <th scope="row">At the start of the year</th>
                  <td className="fy-num">{formatAmount(negate(re.start.amount))}</td>
                </tr>
                <tr>
                  <th scope="row">Posted to it in the year</th>
                  <td className="fy-num">{formatAmount(negate(re.posted.amount))}</td>
                </tr>
                <tr>
                  <th scope="row">
                    The year's {profit ? "profit" : "loss"}
                    {so}
                  </th>
                  <td className="fy-num">{formatAmount(negate(y.result.amount))}</td>
                </tr>
                <tr>
                  <th scope="row">Into the next year</th>
                  <td className="fy-num">
                    <strong>{formatAmount(negate(re.end.amount))}</strong>
                  </td>
                </tr>
              </tbody>
            </table>
          ) : (
            <div className="fy-banner fy-banner--warning" role="note">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <strong>No account holds retained earnings</strong>
                <span className="fy-small">Income and expense accounts carry on from year to year until an equity account has the retained-earnings role, as 3100 Retained earnings has in the standard chart.</span>
              </div>
            </div>
          )}
          <p className="fy-small fy-muted">{h.tooltip}</p>
        </>
      )}
    </section>
  );
}
