import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, CircleX, Download, Info, TriangleAlert } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date, month, timeZone } from "../../format/date";
import { hasHelpText, helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { listPeriods, type Period } from "../accounting/data";
import { ExportDialog } from "./ExportDialog";

type Statement = components["schemas"]["FinancialStatement"];
type Block = components["schemas"]["FinancialBlock"];
type Column = components["schemas"]["FinancialColumn"];
type Kind = Statement["kind"];

export const statementNames: Record<Kind, string> = { pl: "Profit and loss", bs: "Balance sheet", cf: "Cash flow", tb: "Trial balance" };

// runOf is a preset's run of months, from the books' periods: the month of today, the one before it, the financial year
// to date, the whole financial year and the one before it; or a run the person chooses.
type Preset = "month" | "last_month" | "ytd" | "year" | "last_year" | "custom";
const presetNames: Record<Preset, string> = {
  month: "This month",
  last_month: "Last month",
  ytd: "Year to date",
  year: "This financial year",
  last_year: "Last financial year",
  custom: "Choose the months",
};

function thisMonth(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit" }).format(new Date()).slice(0, 7);
}

const ym = (p: Period) => p.starts_on.slice(0, 7);

export function runOf(preset: Preset, periods: readonly Period[], custom: { from: string; to: string }, now = thisMonth()): { from: string; to: string } | null {
  if (preset === "custom") return custom.from && custom.to ? custom : null;
  const found = periods.findIndex((p) => ym(p) === now);
  const i = found >= 0 ? found : periods.length - 1; // today's month, or the books' last when today is past it
  const current = periods[i];
  if (!current) return null;
  const one = (p: Period | undefined) => (p ? { from: ym(p), to: ym(p) } : null);
  switch (preset) {
    case "month":
      return one(current);
    case "last_month":
      return one(periods[i - 1]);
    default: {
      const y = preset === "last_year" ? (current.financial_year ?? 0) - 1 : current.financial_year;
      const months = periods.filter((p) => y !== undefined && p.financial_year === y);
      const first = months[0];
      const last = preset === "ytd" ? current : months[months.length - 1];
      return first && last ? { from: ym(first), to: ym(last) } : null;
    }
  }
}

// columnLabel is a column's heading: its month, its run, or the total.
function columnLabel(c: Column): string {
  if (c.key === "total") return "Total";
  const from = c.from.slice(0, 7);
  const to = c.to.slice(0, 7);
  return from === to ? month(from) : `${month(from)}–${month(to)}`;
}

// Figure is a figure that opens the general ledger of its accounts over its column's run (FR-UX-04); one of more than
// 20 accounts, which the general ledger does not take at once, or a computed one, does not.
function Figure({ amount, accounts, column }: { amount: string; accounts: readonly string[]; column: Column }) {
  if (accounts.length === 0 || accounts.length > 20) return <>{formatAmount(amount)}</>;
  return (
    <Link to="/reports/general-ledger" search={{ account_id: [...accounts], from: column.from, to: column.to }} className="text-inherit no-underline hover:underline">
      {formatAmount(amount)}
    </Link>
  );
}

// Explanation is Basic mode's plain words under a section (FR-UX-23), from the help texts.
function Explanation({ k, span }: { k: string; span: number }) {
  if (!hasHelpText(`statement.${k}`)) return null;
  const h = helpText(`statement.${k}`);
  return (
    <tr className="fy-basic-only">
      <td colSpan={span} className="fy-small fy-secondary whitespace-normal">
        <span className="fy-row fy-row--top">
          <Icon of={Info} />
          <span>{h.tooltip}</span>
        </span>
      </td>
    </tr>
  );
}

function BlockRows({ b, s, percent }: { b: Block; s: Statement; percent: boolean }) {
  const [open, setOpen] = useState(true);
  const id = useId();
  const span = s.columns.length + 1 + (percent ? 1 : 0);
  const cells = (amounts: { amount: string }[], percents: string[], accounts: readonly string[]) => (
    <>
      {amounts.map((m, i) => (
        <td key={i} className={i === 0 ? "fy-num" : "fy-num fy-expert-only"}>
          {s.columns[i] ? <Figure amount={m.amount} accounts={accounts} column={s.columns[i]} /> : formatAmount(m.amount)}
        </td>
      ))}
      {percent && <td className="fy-num fy-expert-only fy-muted">{percents[0] ? `${percents[0]}%` : ""}</td>}
    </>
  );
  if (b.measure) {
    return (
      <>
        <tr className="fy-strong">
          <th scope="row">{b.title}</th>
          {cells(b.totals, b.percents, [])}
        </tr>
        <Explanation k={b.key} span={span} />
      </>
    );
  }
  const all = b.groups.flatMap((g) => g.lines.map((l) => l.account_id));
  return (
    <>
      <tr className="fy-group">
        <th scope="rowgroup" colSpan={span}>
          <button type="button" className="fy-row gap-2 border-0 bg-transparent p-0 font-semibold" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>
            <Icon of={open ? ChevronDown : ChevronRight} />
            {b.title}
          </button>
        </th>
      </tr>
      {open &&
        b.groups.map((g) =>
          g.lines.length === 0 ? (
            <tr key={g.name} id={id}>
              <td className="fy-sub">{g.name}</td>
              {cells(g.totals, g.percents, [])}
            </tr>
          ) : (
            [
              ...g.lines.map((l) => (
                <tr key={l.account_id} id={id}>
                  <td className="fy-sub">
                    <span className="fy-mono fy-muted fy-expert-only">{l.code} </span>
                    {l.name}
                  </td>
                  {cells(l.amounts, l.percents, [l.account_id])}
                </tr>
              )),
              b.groups.length > 1 && g.lines.length > 1 ? (
                <tr key={`${g.name}-total`} className="fy-expert-only">
                  <td className="fy-sub fy-muted">{g.name}</td>
                  {cells(g.totals, g.percents, g.lines.map((l) => l.account_id))}
                </tr>
              ) : null,
            ]
          ),
        )}
      <tr className="fy-strong">
        <th scope="row">Total {b.title.toLowerCase()}</th>
        {cells(b.totals, b.percents, all)}
      </tr>
      <Explanation k={b.key} span={span} />
    </>
  );
}

// save gives the person a file the server wrote, in base64.
// FinancialStatement is Reports › Financial statements (docs/design/screens FinancialStatement; FR-RPT-01, FR-RPT-02,
// FR-RPT-08): the P&L, the balance sheet, the cash flow or the trial balance for a run of months, with a comparative
// or a column per month, read at one ledger revision. Every account's figure opens its general ledger lines (FR-UX-04).
// Basic shows the current column with plain words under each section; Expert adds the codes, comparatives and % of
// revenue. The layout is provisional (PRD-001 rev 0.159) [accountant to confirm]; budget comparatives come with
// planning, dimension columns with dimensions, and the function view with its task. Export opens the export dialog.
export function FinancialStatement() {
  const id = useId();
  const { kind } = useParams({ strict: false }) as { kind: Kind };
  const { mode } = useSignedIn();
  const [preset, setPreset] = useState<Preset>("month");
  const [custom, setCustom] = useState({ from: "", to: "" });
  const [compare, setCompare] = useState<"" | "prior_period" | "prior_year">("");
  const [byMonth, setByMonth] = useState(false);
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const run = useMemo(() => runOf(preset, periods.data?.items ?? [], custom), [preset, periods.data, custom]);
  const statement = useQuery({
    queryKey: ["statement", kind, run, compare, byMonth],
    enabled: run !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/reports/statements/{kind}", {
        params: { path: { kind }, query: { from: run!.from, to: run!.to, compare: compare || undefined, columns: byMonth ? "month" : "total" } },
      });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const s = statement.data;
  const last = (periods.data?.items ?? []).find((p) => run && ym(p) === run.to);
  const failed = s?.checks.filter((c) => !c.ok) ?? [];
  const exp = helpText("statement.export");
  const [exporting, setExporting] = useState(false);
  const name = statementNames[kind] ?? "Financial statement";
  // shown is what the statement shows, as the export dialog names it.
  const shown = s
    ? `${date(s.from)} to ${date(s.to)}${byMonth ? ", a column per month" : compare === "prior_period" ? ", with the period before" : compare === "prior_year" ? ", with the same months a year earlier" : ""}`
    : "";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">{name}</h1>
          {s && (
            <p className="fy-secondary">
              {s.statement_title} · {date(s.from)} to {date(s.to)} · {s.currency}
              {s.basis ? ` · ${s.basis}` : ""}
            </p>
          )}
        </div>
        {s && run && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setExporting(true)} title={exp.tooltip}>
              <Icon of={Download} />
              {exp.label}
            </Button>
          </div>
        )}
      </div>
      {s && run && (
        <ExportDialog
          open={exporting}
          onOpenChange={setExporting}
          kind={kind}
          name={name}
          shown={shown}
          query={{ from: run.from, to: run.to, compare: compare || undefined, columns: byMonth ? "month" : "total", as_at_revision: s.ledger_revision }}
        />
      )}
      <nav className="fy-tabs" aria-label="Statements">
        {(["pl", "bs", "cf", "tb"] as const).map((k) => (
          <Link key={k} to="/reports/statements/$kind" params={{ kind: k }} aria-current={k === kind ? "page" : undefined} className={k === "tb" ? "fy-expert-only" : undefined}>
            {statementNames[k]}
          </Link>
        ))}
      </nav>
      <ReviewBanner screen="financial-statement" />
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-period`}>
            {kind === "bs" || kind === "tb" ? "At the end of" : "Period"}
          </label>
          <select className="fy-select" id={`${id}-period`} value={preset} onChange={(e) => setPreset(e.target.value as Preset)}>
            {(Object.keys(presetNames) as Preset[]).map((p) => (
              <option key={p} value={p}>
                {presetNames[p]}
              </option>
            ))}
          </select>
        </div>
        {preset === "custom" && (
          <>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-from`}>
                From
              </label>
              <input className="fy-input" id={`${id}-from`} type="month" value={custom.from} onChange={(e) => setCustom({ ...custom, from: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-to`}>
                To
              </label>
              <input className="fy-input" id={`${id}-to`} type="month" value={custom.to} onChange={(e) => setCustom({ ...custom, to: e.target.value })} />
            </div>
          </>
        )}
        <div className="fy-field fy-expert-only">
          <label className="fy-label" htmlFor={`${id}-compare`}>
            Compare with
          </label>
          <select className="fy-select" id={`${id}-compare`} value={compare} disabled={byMonth} onChange={(e) => setCompare(e.target.value as typeof compare)}>
            <option value="">Nothing</option>
            <option value="prior_period">The period before</option>
            <option value="prior_year">The same months a year earlier</option>
          </select>
        </div>
        <label className="fy-row gap-2 self-end fy-expert-only">
          <input type="checkbox" checked={byMonth} onChange={(e) => setByMonth(e.target.checked)} />A column per month
        </label>
      </div>
      {last && last.status !== "closed" && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>{month(ym(last))} is not closed yet</strong>
            <span className="fy-small">Its figures can still change until its close is finished.</span>
          </div>
          <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/accounting/close/$periodId" params={{ periodId: last.id }}>
            Open the close
          </Link>
        </div>
      )}
      {failed.map((c) => (
        <div key={c.key} className="fy-banner fy-banner--critical" role="alert">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <strong>{c.title}</strong>
            {c.detail && <span className="fy-small">{c.detail}</span>}
          </div>
        </div>
      ))}
      {periods.data && run === null && <p className="fy-secondary">The books have no months for this choice. Choose another, or open the financial year in Accounting › Periods and close.</p>}
      {statement.isError && <Refusal problem={problemOf(statement.error)} title={`The ${name.toLowerCase()} could not be shown`} />}
      {statement.isFetching && !s && <p className="fy-secondary">Reading the ledger…</p>}
      {s && (
        <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
          <div className="fy-row fy-row--between fy-row--wrap">
            <h2 className="fy-h3" id={`${id}-title`}>
              {s.columns[0] ? columnLabel(s.columns[0]) : null}
              {s.restated_presentation && (
                <span className="fy-chip fy-chip--neutral ml-2" title="An account was created or changed after the revision these figures are read at: the names and groups are today's.">
                  Restated presentation
                </span>
              )}
            </h2>
            <span className="fy-small fy-muted">Select a figure to see the lines behind it</span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact fy-statement">
              <caption className="fy-sr">
                {name} from {date(s.from)} to {date(s.to)}, in {s.currency}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{s.currency}</th>
                  {s.columns.map((c, i) => (
                    <th key={c.key} scope="col" className={i === 0 ? "fy-num" : "fy-num fy-expert-only"}>
                      {columnLabel(c)}
                    </th>
                  ))}
                  {kind === "pl" && (
                    <th scope="col" className="fy-num fy-expert-only">
                      % of revenue
                    </th>
                  )}
                </tr>
              </thead>
              <tbody>
                {s.blocks.map((b) => (
                  <BlockRows key={b.key} b={b} s={s} percent={kind === "pl"} />
                ))}
              </tbody>
            </table>
          </div>
          {kind === "cf" && s.non_cash.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-noncash`}>
              <h3 className="fy-h3" id={`${id}-noncash`}>
                Transactions that moved no money
              </h3>
              <p className="fy-small fy-secondary">Left out of the figures above, such as a cost a director paid personally or a loan converted to shares.</p>
              <ul className="fy-list">
                {s.non_cash.map((x) => (
                  <li key={`${x.source_id}-${x.date}`} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">{x.memo || x.source_type.replaceAll("_", " ")}</span>
                      <span className="fy-list__meta">{date(x.date)}</span>
                    </span>
                    <span className="fy-num">{formatAmount(x.amount.amount)}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          <p className="fy-small fy-muted">
            Read at ledger revision {s.ledger_revision}: the same revision always gives the same figures.
            {mode === "expert" ? " Laid out from each account's report group, function and cash-flow line in the chart of accounts." : ""}
          </p>
        </section>
      )}
    </div>
  );
}
