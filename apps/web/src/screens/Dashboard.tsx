import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Gauge } from "lucide-react";
import { useMemo } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Chart } from "../components/chart/Chart";
import { formatAmount } from "../format/amount";
import { sum } from "../format/decimal";
import { month as monthName } from "../format/date";
import { useSignedIn } from "../session";
import { Icon } from "../ui";
import { listPeriods } from "./accounting/data";
import { About, figureOf, figureText, find, lastMonths, previous, Tile, useKpis, type KPI, type KPIs } from "./reports/kpi";
import { runOf } from "./reports/Statement";

type Statement = components["schemas"]["FinancialStatement"];

// Dashboard is Home's view of the money (docs/design/screens Home; FR-KPI-01, design D5 of p1-dashboard-kpis): the
// pulse card with cash, runway and net burn; the tiles, Basic's two (owed to us, we owe) and Expert's revenue, gross
// margin and net profit for the year; and in Expert the charts of revenue, costs and profit, cash, and costs by function.
// Every figure is the server's (FR-UX-22), each tile has its definition and opens what is behind it (FR-UX-04). It is
// left out when the person's roles may not read the reports, or the books have no month yet (the set-up checklist
// says what to do). Revenue and cost against budget, books health, unbilled time and GST due join with their changes.
export function Dashboard() {
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods, retry: false, staleTime: 30_000 });
  const month = useMemo(() => runOf("month", periods.data?.items ?? [], { from: "", to: "" })?.to ?? null, [periods.data]);
  const kpis = useKpis(month);
  if (month === null || kpis.isError) return null;
  if (!kpis.data) return <div className="fy-skeleton h-40" aria-hidden="true" />;
  const k = kpis.data;
  const tile = (key: string, to: string | undefined, className?: string) => {
    const x = find(k, key);
    return x ? <Tile key={key} k={x} kpis={k} to={to} className={className} /> : null;
  };
  return (
    <>
      <Pulse kpis={k} />
      <div className="fy-grid fy-grid--3">
        {tile("revenue", "/reports/statements/pl", "fy-expert-only")}
        {tile("gross_margin", "/reports/statements/pl", "fy-expert-only")}
        {tile("net_profit_ytd", "/reports/statements/pl", "fy-expert-only")}
        {tile("owed_to_us", "/sales/statements")}
        {tile("owed_by_us", "/purchases/bills")}
        <Link className="fy-tile text-foreground" to="/reports/kpis">
          <span className="fy-tile__label">
            <Icon of={Gauge} />
            All KPIs
          </span>
          <span className="fy-strong">MRR, margins, churn, CAC and the rest</span>
          <span className="fy-tile__meta">
            Each with how it is worked out
            <Icon of={ChevronRight} />
          </span>
        </Link>
      </div>
      <Charts kpis={k} months={lastMonths(periods.data?.items ?? [], k.month)} />
    </>
  );
}

// Pulse is the pulse card (MASTER.md: the only place besides the AI panel's header with the gradient band): cash at the
// month end, the runway or "Not burning", and the month's net burn.
function Pulse({ kpis }: { kpis: KPIs }) {
  const cell = (key: string, label: string, to: string, meta: (k: KPI) => string) => {
    const k = find(kpis, key);
    if (!k) return null;
    const f = figureOf(k, k.month, kpis.currency);
    return (
      <div className="fy-stack fy-stack--2" data-kpi={key}>
        <div className="fy-tile__label">
          {label}
          <About k={k} />
        </div>
        <Link className="fy-pulse__figure fy-num text-foreground" to={to}>
          {k.unit === "amount" && f.unit ? <><small>{f.unit}</small> </> : null}
          {f.text}
          {k.unit !== "amount" && f.unit ? <> <small>{f.unit}</small></> : null}
        </Link>
        <div className="fy-tile__meta">
          <span className="whitespace-normal">{f.why ? `No figure: ${f.why}` : meta(k)}</span>
        </div>
      </div>
    );
  };
  const at = monthName(kpis.month);
  return (
    <section className="fy-pulse overflow-visible" aria-labelledby="pulse-title">
      <div className="fy-pulse__band rounded-t-[inherit]" />
      <h2 className="fy-sr" id="pulse-title">
        Company pulse, {at}
      </h2>
      <div className="fy-pulse__body">
        {cell("cash", "Cash in the bank", "/reports/statements/bs", () => `At the end of ${at}, in the money accounts`)}
        {cell("runway", "Runway", "/reports/statements/cf", (k) => (k.month.words ? "The last three months brought in more cash than they used" : "At the average net burn of the last three months"))}
        {cell("net_burn", "Net burn", "/reports/statements/cf", (k) =>
          k.before ? `${monthName(previous(kpis.month))}: ${figureText(k, k.before, kpis.currency)}` : `The cash ${at}'s operations and investments used`,
        )}
      </div>
    </section>
  );
}

// statementOf reads a statement with a column per month, at the KPIs' ledger revision, so the charts agree with the
// tiles (FR-RPT-08).
async function statementOf(kind: "pl" | "cf", months: { from: string; to: string }, revision: number): Promise<Statement> {
  const { data, error } = await api.GET("/v1/reports/statements/{kind}", {
    params: { path: { kind }, query: { from: months.from, to: months.to, columns: "month", as_at_revision: revision } },
  });
  if (error) throw error;
  return data;
}

// byMonth is a block's totals by month, the total column left out.
function byMonth(s: Statement, key: string): string[] | null {
  const b = s.blocks.find((x) => x.key === key);
  if (!b) return null;
  return s.columns.flatMap((c, i) => (c.key === "total" ? [] : [b.totals[i]?.amount ?? ""]));
}

// Charts are Expert's three charts (FR-KPI-01, FR-VIZ-01): revenue, costs and profit by month and cash by month, from
// the P&L and the cash flow of the last twelve months, and costs by function for the month and the one before, from
// the KPIs. Each series keeps its colour (FR-VIZ-03). The 36-month cash forecast joins with planning.
function Charts({ kpis, months }: { kpis: KPIs; months: { from: string; to: string } | null }) {
  const { mode } = useSignedIn();
  const on = mode === "expert" && months !== null;
  const pl = useQuery({ queryKey: ["dashboard", "pl", months, kpis.ledger_revision], enabled: on, retry: false, queryFn: () => statementOf("pl", months!, kpis.ledger_revision) });
  const cf = useQuery({ queryKey: ["dashboard", "cf", months, kpis.ledger_revision], enabled: on, retry: false, queryFn: () => statementOf("cf", months!, kpis.ledger_revision) });
  if (!on) return null;
  const labels = (s: Statement) => s.columns.filter((c) => c.key !== "total").map((c) => monthName(c.from.slice(0, 7)));
  const file = (what: string) => `${what}-${months.from}-to-${months.to}`;
  const functions = (["spend_sm", "spend_rd", "spend_ga"] as const).map((key) => find(kpis, key));
  const before = monthName(previous(kpis.month));
  return (
    <div className="fy-expert-only fy-stack fy-stack--4">
      {pl.data && <ProfitChart s={pl.data} labels={labels(pl.data)} file={file("revenue-costs-profit")} />}
      <div className="fy-grid fy-grid--2">
        {cf.data && <CashChart s={cf.data} labels={labels(cf.data)} file={file("cash")} />}
        {functions.every(Boolean) && (
          <Chart
            title="Costs by function"
            summary={`Sales & marketing, research & development and general & administrative costs in ${monthName(kpis.month)} beside ${before}, from each expense account's function.`}
            form="bar-horizontal"
            label="Function"
            categories={["Sales & marketing", "Research & development", "General & administrative"]}
            series={[
              { key: "month", name: monthName(kpis.month), slot: 4, values: functions.map((k) => k!.month.value ?? "") },
              ...(functions.every((k) => k!.before) ? [{ key: "before", name: before, slot: 5, values: functions.map((k) => k!.before!.value ?? "") }] : []),
            ]}
            unit={kpis.currency}
            file={`costs-by-function-${kpis.month}`}
          />
        )}
      </div>
    </div>
  );
}

// ProfitChart draws revenue, costs (cost of sales and operating expenses, as the KPIs count them) and net profit.
function ProfitChart({ s, labels, file }: { s: Statement; labels: string[]; file: string }) {
  const revenue = byMonth(s, "revenue");
  const cost = byMonth(s, "cost_of_sales");
  const opex = byMonth(s, "operating_expenses");
  const profit = byMonth(s, "net_profit");
  if (!revenue || !profit || labels.length < 2) return null;
  const costs = labels.map((_, i) => sum([cost?.[i] ?? "", opex?.[i] ?? ""]));
  const last = labels.length - 1;
  return (
    <Chart
      title="Revenue, costs and profit by month"
      summary={`In ${labels[last]}, revenue was ${formatAmount(revenue[last] || "0")}, costs ${formatAmount(costs[last] || "0")} and net profit ${formatAmount(profit[last] || "0")}. Costs are the cost of sales and the operating expenses.`}
      form="line"
      label="Month"
      categories={labels}
      series={[
        { key: "revenue", name: "Revenue", slot: 0, values: revenue },
        { key: "costs", name: "Costs", slot: 1, values: costs },
        { key: "net_profit", name: "Net profit", slot: 2, values: profit },
      ]}
      unit={s.currency}
      file={file}
    />
  );
}

// CashChart draws the cash at each month's end, from the cash flow, which ties it to the money accounts (INV-11).
function CashChart({ s, labels, file }: { s: Statement; labels: string[]; file: string }) {
  const cash = byMonth(s, "cash_at_end");
  if (!cash || labels.length < 2) return null;
  const last = labels.length - 1;
  return (
    <Chart
      title="Cash by month"
      summary={`Cash went from ${formatAmount(cash[0] || "0")} at the end of ${labels[0]} to ${formatAmount(cash[last] || "0")} at the end of ${labels[last]}.`}
      form="line"
      label="Month"
      categories={labels}
      series={[{ key: "cash", name: "Cash at the month end", slot: 3, values: cash }]}
      unit={s.currency}
      file={file}
    />
  );
}
