import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookOpen, CalendarClock, ChartBarDecreasing, ChartColumn, ChevronRight, Gauge, HandCoins, Landmark, ReceiptText, Scale, ScrollText, Search, Truck, Wallet, type LucideIcon } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { month } from "../../format/date";
import { Icon } from "../../ui";
import { listPeriods } from "../accounting/data";
import { AccountantExports } from "./AccountantExports";
import { runOf, statementNames } from "./Statement";

type Statement = components["schemas"]["FinancialStatement"];

// essentials are Basic's curated set (docs/design/screens Reports), each with a plain line.
const essentials: { kind: "pl" | "bs" | "cf"; icon: LucideIcon; line: string }[] = [
  { kind: "pl", icon: Scale, line: "Did we make or lose money? Revenue, costs and the result for any month." },
  { kind: "bs", icon: Landmark, line: "What the company owns and owes at a month's end, and what is left for the shareholders." },
  { kind: "cf", icon: Wallet, line: "Where the cash came from and went, and how it ties to the bank." },
];

// more are the other reports, each where it already lives: Expert's list, with the essentials' links.
const more: { name: string; line: string; icon: LucideIcon; to: string; params?: Record<string, string>; expert?: boolean }[] = [
  { name: "Start-up KPIs", line: "MRR, burn, runway, margins, churn and the rest, each with how it is worked out.", icon: Gauge, to: "/reports/kpis" },
  { name: "Who owes us", line: "Customers' statements of account and what each owes.", icon: HandCoins, to: "/sales/statements" },
  { name: "Whom we owe", line: "Bills to pay, by when they fall due.", icon: ReceiptText, to: "/purchases/bills" },
  { name: "Sales by customer and item", line: "What each customer and each item was invoiced, net of GST.", icon: ChartColumn, to: "/reports/management/$report", params: { report: "sales-by-customer" }, expert: true },
  { name: "Spend by supplier", line: "What each supplier billed, net of the GST you can claim.", icon: Truck, to: "/reports/management/$report", params: { report: "spend-by-supplier" }, expert: true },
  { name: "Top expenses", line: "The ten largest expense accounts, and the rest as one line.", icon: ChartBarDecreasing, to: "/reports/management/$report", params: { report: "top-expenses" }, expert: true },
  { name: statementNames.tb, line: "Every account's balance at a month's end, debits and credits.", icon: BookOpen, to: "/reports/statements/$kind", params: { kind: "tb" }, expert: true },
  { name: "General ledger", line: "Every posted line of the accounts you choose, with a running balance.", icon: ScrollText, to: "/reports/general-ledger", expert: true },
  { name: "Schedules", line: "Costs and income spread over months, and what each has recognised.", icon: CalendarClock, to: "/accounting/schedules", expert: true },
];

// headline is the essential's figure for the month, from its statement: the net profit, the equity, the cash at the end.
function headline(s: Statement | undefined): string | null {
  if (!s) return null;
  const key = s.kind === "pl" ? "net_profit" : s.kind === "bs" ? "equity" : "cash_at_end";
  const b = s.blocks.find((x) => x.key === key);
  const first = b?.totals[0];
  return first ? formatAmount(first.amount) : null;
}

const headlineWords: Record<"pl" | "bs" | "cf", string> = { pl: "Net profit", bs: "Equity", cf: "Cash at the end" };

function Essential({ kind, icon, line, run }: (typeof essentials)[number] & { run: { from: string; to: string } | null }) {
  const s = useQuery({
    queryKey: ["statement", kind, run, "", false],
    enabled: run !== null,
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/reports/statements/{kind}", { params: { path: { kind }, query: { from: run!.from, to: run!.to } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const h = headline(s.data);
  return (
    <article className="fy-card fy-card--pad fy-stack fy-stack--3">
      <div className="fy-row fy-row--top">
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={icon} size={20} />
        </span>
        <div className="fy-stack fy-stack--2 fy-grow">
          <h3 className="fy-h3">
            <Link to="/reports/statements/$kind" params={{ kind }}>
              {statementNames[kind]}
            </Link>
          </h3>
          <p className="fy-small fy-secondary">{line}</p>
        </div>
      </div>
      {h && run && (
        <p className="fy-small">
          {headlineWords[kind]}, {month(run.to)}: <span className="fy-num fy-strong">{h}</span> {s.data?.currency}
        </p>
      )}
    </article>
  );
}

// Reports is the reports index (docs/design/screens Reports; FR-RPT-03, FR-RPT-10): Basic's essentials with this
// month's headline figure, and the other reports where they live. Saved reports, favourites and the report builder are
// P2. In Expert, the management reports are listed, and the accountant exports run in the background.
export function Reports() {
  const id = useId();
  const [find, setFind] = useState("");
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const run = runOf("month", periods.data?.items ?? [], { from: "", to: "" });
  const matches = (text: string) => text.toLowerCase().includes(find.trim().toLowerCase());
  const shown = essentials.filter((e) => matches(`${statementNames[e.kind]} ${e.line}`));
  const others = more.filter((m) => matches(`${m.name} ${m.line}`));
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Reports</h1>
          <p className="fy-secondary">Start with the essentials. Every figure opens to the lines behind it.</p>
        </div>
      </div>
      <div className="fy-field max-w-[520px]">
        <label className="fy-label fy-sr" htmlFor={`${id}-find`}>
          Find a report
        </label>
        <div className="fy-inputgroup">
          <span className="fy-inputgroup__addon">
            <Icon of={Search} />
          </span>
          <input className="fy-input" id={`${id}-find`} placeholder="Find a report, such as balance sheet" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
      </div>
      {shown.length > 0 && (
        <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-ess`}>
          <h2 className="fy-h2" id={`${id}-ess`}>
            The essentials
          </h2>
          <div className="fy-grid fy-grid--2 fy-stagger">
            {shown.map((e) => (
              <Essential key={e.kind} {...e} run={run} />
            ))}
          </div>
        </section>
      )}
      {others.length > 0 && (
        <nav className="fy-card" aria-label="More reports">
          <ul className="fy-list">
            {others.map((m) => (
              <li key={m.name} className={m.expert ? "fy-expert-only" : undefined}>
                <Link className="fy-list__item" to={m.to} params={m.params}>
                  <span className="fy-list__icon">
                    <Icon of={m.icon} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title">{m.name}</span>
                    <span className="fy-list__meta whitespace-normal">{m.line}</span>
                  </span>
                  <Icon of={ChevronRight} />
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      )}
      {shown.length === 0 && others.length === 0 && <p className="fy-secondary">No report matches “{find}”.</p>}
      <AccountantExports />
    </div>
  );
}
