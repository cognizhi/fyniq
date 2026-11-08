import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { month as monthName } from "../../format/date";
import { helpText } from "../../i18n";

type Summary = components["schemas"]["PlanningSummary"];
type Figures = components["schemas"]["VsFigures"];

// readSummary reads what Home shows of the plans for a month (GET /v1/planning/summary; FR-KPI-01, FR-PLN-03).
export async function readSummary(month: string): Promise<Summary> {
  const { data, error } = await api.GET("/v1/planning/summary", { params: { query: { month } } });
  if (error) throw error;
  return data;
}

// varianceText says how far a year to date is from its budget, and whether that is good: income above its budget, or
// costs below theirs, is favourable (p1-planning design D6). The percentage is the server's (RND-PERCENT); nothing is
// worked out here but the words.
export function varianceText(f: Figures, income: boolean): string {
  const under = f.variance.startsWith("-");
  const zero = /^-?0*\.?0*$/.test(f.variance);
  const budget = `Budget ${formatAmount(f.budget)}`;
  if (zero) return `${budget}, on budget`;
  if (!f.variance_percent) return `${budget}: nothing budgeted`;
  const pct = f.variance_percent.replace(/^-/, "");
  const good = income ? !under : under;
  return `${budget}, ${pct}% ${under ? "under" : "over"}: ${good ? "favourable" : "unfavourable"}`;
}

// PlanningTiles are Home's tiles from the plans (FR-KPI-01; p1-planning design D4, D6): revenue and costs against the
// year's budget, the financial year to the KPIs' month, in Expert; and in both modes the month cash runs out in the
// latest recorded forecast, without the funding planned, since none of it is received yet (FR-PLN-03). A tile without
// a figure says why. They are left out when the person's roles may not read the plans (Appendix D "Budgets").
export function PlanningTiles({ month }: { month: string }) {
  const s = useQuery({ queryKey: ["planning-summary", month], queryFn: () => readSummary(month), retry: false, staleTime: 30_000 });
  if (!s.data) return null;
  const { budget, currency } = s.data;
  const tile = (key: "revenue" | "costs", help: string, income: boolean) => {
    const f = budget?.[key];
    return (
      <div className="fy-tile fy-expert-only" data-planning={help}>
        <span className="fy-tile__label" title={helpText(help).tooltip}>
          {helpText(help).label}
        </span>
        <span className="fy-tile__value fy-num">
          {f ? (
            <>
              {formatAmount(f.actual)} <small>{currency}</small>
            </>
          ) : (
            "—"
          )}
        </span>
        <span className="fy-tile__meta">
          <span className="whitespace-normal">
            {f && budget ? `${monthName(budget.from)} to ${monthName(month)}. ${varianceText(f, income)}` : `No figure: ${s.data.budget_missing ?? "no approved budget"}`}
          </span>
        </span>
      </div>
    );
  };
  return (
    <>
      {tile("revenue", "home.revenue-vs-budget", true)}
      {tile("costs", "home.costs-vs-budget", false)}
      <CashOut s={s.data} />
    </>
  );
}

// CashOut is the forecast's cash-out month without the funding planned, and beside it the month with the funding.
function CashOut({ s }: { s: Summary }) {
  const f = s.forecast;
  const words = (month: string | undefined, beyond: boolean) => (month ? monthName(month) : beyond ? "Not within the forecast" : "—");
  return (
    <div className="fy-tile" data-planning="home.forecast-cash-out">
      <span className="fy-tile__label" title={helpText("home.forecast-cash-out").tooltip}>
        {helpText("home.forecast-cash-out").label}
      </span>
      <span className="fy-tile__value fy-num">{f ? words(f.cash_out_month, f.runway_beyond) : "—"}</span>
      <span className="fy-tile__meta">
        <span className="whitespace-normal">
          {f
            ? `${f.note}. ${f.scenario} scenario of ${f.name}. With the funding: ${words(f.cash_out_month_with_funding, f.runway_beyond_with_funding)}`
            : `No figure: ${s.forecast_missing ?? "no forecast is recorded"}`}
        </span>
      </span>
    </div>
  );
}
