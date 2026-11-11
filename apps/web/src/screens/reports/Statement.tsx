import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouter, useSearch } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, CircleX, Download, Info, TriangleAlert } from "lucide-react";
import { createContext, useContext, useId, useMemo, useRef, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date, month, timeZone } from "../../format/date";
import { hasHelpText, helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { TracePanel, type TraceFigure } from "../../shell/Trace";
import { Icon, problemOf } from "../../ui";
import { Chart } from "../../components/chart/Chart";
import { listPeriods, type Period } from "../accounting/data";
import { useDimensions, valueCodes } from "../accounting/lines";
import { ExportDialog } from "./ExportDialog";

type Statement = components["schemas"]["FinancialStatement"];
type Block = components["schemas"]["FinancialBlock"];
type Column = components["schemas"]["FinancialColumn"];
type Kind = Statement["kind"];

export const statementNames: Record<Kind, string> = { pl: "Profit and loss", bs: "Balance sheet", cf: "Cash flow", tb: "Trial balance" };

// runOf is a preset's run of months, from the books' periods: the month of today, the one before it, the financial year
// to date, the whole financial year and the one before it; or a run the person chooses.
export type Preset = "month" | "last_month" | "ytd" | "year" | "last_year" | "custom";
export const presetNames: Record<Preset, string> = {
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
  if (c.label) return c.label; // a P&L by dimension's column (FR-DIM-02)
  if (c.key === "total") return "Total";
  const from = c.from.slice(0, 7);
  const to = c.to.slice(0, 7);
  return from === to ? month(from) : `${month(from)}–${month(to)}`;
}

// TraceOpen opens the Trace panel at a figure: its column's index and its row's key.
const TraceOpen = createContext<((col: number, key: string) => void) | null>(null);

// Figure is a figure that opens the Trace panel (FR-UX-04): its accounts, their ledger lines over its column's run as at
// the statement's revision, of the column's dimension value and the statement's own when it has them (FR-DIM-02), then
// a line's source and its history. A computed one, or one of the lines with no value of a dimension, which the general
// ledger cannot pick out, does not open; nor does the budget's, which is no ledger figure.
function Figure({ amount, accounts, column, col, traceKey }: { amount: string; accounts: readonly string[]; column: Column; col: number; traceKey: string }) {
  const open = useContext(TraceOpen);
  if (!open || accounts.length === 0 || column.key === "none" || column.key === "budget") return <>{formatAmount(amount)}</>;
  return (
    <button type="button" className="cursor-pointer border-0 bg-transparent p-0 font-[inherit] text-inherit hover:underline" onClick={() => open(col, traceKey)}
      title={helpText("statement.trace").tooltip}>
      {formatAmount(amount)}
    </button>
  );
}

// valuesOf are the dimension values a column's figures are of: the statement's own and the column's (FR-DIM-02).
function valuesOf(column: Column, only?: string): string[] {
  return [only, column.key.startsWith("value:") ? column.key.slice(6) : ""].filter((v): v is string => !!v);
}

// figureOf is the figure a trace names, "<column>~<key>": a line's (a:<account>), a group's total (g:<block>:<group>) or
// a block's total (b:<block>), from the statement shown; undefined when it names none.
export function figureOf(s: Statement, trace: string | undefined, name: string, only?: string): TraceFigure | undefined {
  if (!trace) return undefined;
  const at = trace.indexOf("~");
  const col = Number(trace.slice(0, at));
  const key = trace.slice(at + 1);
  const column = s.columns[col];
  if (at < 0 || !Number.isInteger(col) || !column) return undefined;
  const base = { currency: s.currency, context: `${name}, ${columnLabel(column)}`, from: column.from, to: column.to, revision: s.ledger_revision, values: valuesOf(column, only) };
  const accountsOf = (lines: Block["groups"][number]["lines"]) => lines.map((l) => ({ id: l.account_id, code: l.code, name: l.name, amount: l.amounts[col]?.amount }));
  for (const b of s.blocks) {
    if (key === `b:${b.key}` && !b.measure) {
      return { ...base, title: `Total ${b.title.toLowerCase()}`, amount: b.totals[col]?.amount ?? "", accounts: b.groups.flatMap((g) => accountsOf(g.lines)) };
    }
    for (const g of b.groups) {
      if (key === `g:${b.key}:${g.name}` && g.lines.length > 0) return { ...base, title: g.name, amount: g.totals[col]?.amount ?? "", accounts: accountsOf(g.lines) };
      const l = g.lines.find((x) => key === `a:${x.account_id}`);
      if (l) return { ...base, title: l.name, amount: l.amounts[col]?.amount ?? "", accounts: accountsOf([l]) };
    }
  }
  return undefined;
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

// forecastOf is the forecast's figure for a P&L block (compare=forecast; p1-planning design D6), "" for a block or line it
// has none for: its lines are drivers, not accounts.
function forecastOf(s: Statement, section?: string): string {
  const x = section ? s.forecast?.sections.find((f) => f.key === section) : undefined;
  return x ? formatAmount(x.amount.amount) : "";
}

function BlockRows({ b, s, percent }: { b: Block; s: Statement; percent: boolean }) {
  const [open, setOpen] = useState(true);
  const id = useId();
  const span = s.columns.length + 1 + (percent ? 1 : 0) + (s.forecast ? 1 : 0);
  // section is the block a total row closes, whose forecast figure it shows; the forecast has none for a line.
  const cells = (amounts: { amount: string }[], percents: string[], accounts: readonly string[], traceKey: string, section?: string) => (
    <>
      {amounts.map((m, i) => (
        <td key={i} className={i === 0 ? "fy-num" : "fy-num fy-expert-only"}>
          {s.columns[i] ? <Figure amount={m.amount} accounts={accounts} column={s.columns[i]} col={i} traceKey={traceKey} /> : formatAmount(m.amount)}
        </td>
      ))}
      {s.forecast && <td className="fy-num fy-expert-only">{forecastOf(s, section)}</td>}
      {percent && <td className="fy-num fy-expert-only fy-muted">{percents[0] ? `${percents[0]}%` : ""}</td>}
    </>
  );
  if (b.measure) {
    return (
      <>
        <tr className="fy-strong">
          <th scope="row">{b.title}</th>
          {cells(b.totals, b.percents, [], "", b.key)}
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
          <button type="button" className="fy-row gap-2 border-0 bg-transparent p-0 font-semibold" aria-expanded={open} aria-controls={open && b.groups.length > 0 ? id : undefined} onClick={() => setOpen(!open)}>
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
              {cells(g.totals, g.percents, [], "")}
            </tr>
          ) : (
            [
              ...g.lines.map((l) => (
                <tr key={l.account_id} id={id}>
                  <td className="fy-sub">
                    <span className="fy-mono fy-muted fy-expert-only">{l.code} </span>
                    {l.name}
                  </td>
                  {cells(l.amounts, l.percents, [l.account_id], `a:${l.account_id}`)}
                </tr>
              )),
              b.groups.length > 1 && g.lines.length > 1 ? (
                <tr key={`${g.name}-total`} className="fy-expert-only">
                  <td className="fy-sub fy-muted">{g.name}</td>
                  {cells(g.totals, g.percents, g.lines.map((l) => l.account_id), `g:${b.key}:${g.name}`)}
                </tr>
              ) : null,
            ]
          ),
        )}
      <tr className="fy-strong">
        <th scope="row">Total {b.title.toLowerCase()}</th>
        {cells(b.totals, b.percents, all, `b:${b.key}`, b.key)}
      </tr>
      <Explanation k={b.key} span={span} />
    </>
  );
}

// save gives the person a file the server wrote, in base64.
// trendLines are the P&L's lines its monthly trend draws, each in its fixed colour (FR-VIZ-03).
const trendLines: { key: string; name: string; slot: number }[] = [
  { key: "revenue", name: "Revenue", slot: 0 },
  { key: "gross_profit", name: "Gross profit", slot: 1 },
  { key: "net_profit", name: "Net profit", slot: 2 },
];

// Trend is the P&L's monthly trend (p1-reports task 5.3; FR-VIZ-01): revenue, gross profit and net profit a line each,
// month by month, from the statement shown with a column per month, so it reads the figures the table does.
function Trend({ s }: { s: Statement }) {
  const months = s.columns.map((c, i) => ({ c, i })).filter(({ c }) => c.key !== "total");
  if (months.length < 2) return null;
  const series = trendLines.flatMap((l) => {
    const b = s.blocks.find((x) => x.key === l.key);
    return b ? [{ key: l.key, name: l.name, slot: l.slot, values: months.map(({ i }) => b.totals[i]?.amount ?? "") }] : [];
  });
  const net = series.find((x) => x.key === "net_profit");
  const first = months[0]!;
  const last = months[months.length - 1]!;
  const summary = net
    ? `Net profit went from ${formatAmount(net.values[0] ?? "0")} in ${columnLabel(first.c)} to ${formatAmount(net.values[net.values.length - 1] ?? "0")} in ${columnLabel(last.c)}.`
    : "Revenue and profit, month by month.";
  return (
    <div className="fy-expert-only">
      <Chart
        title="The trend, month by month"
        summary={summary}
        form="line"
        label="Month"
        categories={months.map(({ c }) => columnLabel(c))}
        series={series}
        unit={s.currency}
        file={`profit-and-loss-trend-${s.from.slice(0, 7)}-to-${s.to.slice(0, 7)}`}
      />
    </div>
  );
}

// FinancialStatement is Reports › Financial statements (docs/design/screens FinancialStatement; FR-RPT-01, FR-RPT-02,
// FR-RPT-08): the P&L, the balance sheet, the cash flow or the trial balance for a run of months, with a comparative
// or a column per month, read at one ledger revision. Every account's figure opens its general ledger lines (FR-UX-04).
// Basic shows the current column with plain words under each section; Expert adds the codes, comparatives and % of
// revenue, and for a P&L a column per value of a dimension or the P&L of one value (FR-DIM-02). The layout is provisional
// (PRD-001 rev 0.159) [accountant to confirm]. A P&L also compares with its budget, a column by account, or with the
// recorded forecast, a figure per section (p1-planning design D6); the function view comes with its task. Export opens
// the export dialog, which takes the budget column and not yet the forecast.
export function FinancialStatement() {
  const id = useId();
  const { kind } = useParams({ strict: false }) as { kind: Kind };
  const { mode } = useSignedIn();
  const [preset, setPreset] = useState<Preset>("month");
  const [custom, setCustom] = useState({ from: "", to: "" });
  const [compare, setCompare] = useState<"" | "prior_period" | "prior_year" | "budget" | "forecast">("");
  const [byMonth, setByMonth] = useState(false);
  // A P&L by dimension (FR-DIM-02): a column per value of pivot, and the lines of only.
  const [pivot, setPivot] = useState("");
  const [only, setOnly] = useState("");
  const dims = useDimensions();
  const pl = kind === "pl";
  const columns = pl && pivot ? "dimension" : byMonth ? "month" : "total";
  // The budget and the forecast compare a P&L only (p1-planning design D6); a by-dimension P&L takes no comparative.
  const compared = columns === "dimension" || (!pl && (compare === "budget" || compare === "forecast")) ? undefined : compare || undefined;
  const dimensionQuery = pl ? { dimension_id: pivot || undefined, dimension_value_id: only ? [only] : undefined } : {};
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const run = useMemo(() => runOf(preset, periods.data?.items ?? [], custom), [preset, periods.data, custom]);
  const statement = useQuery({
    queryKey: ["statement", kind, run, compare, byMonth, pl ? pivot : "", pl ? only : ""],
    enabled: run !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/reports/statements/{kind}", {
        params: { path: { kind }, query: { from: run!.from, to: run!.to, compare: compared, columns, ...dimensionQuery } },
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
  // The Trace panel (FR-UX-04): the figure the trace search parameter names, so Back closes it and the report keeps its
  // choices and place; one the person opened here closes by going back, one a link opened by dropping the parameter.
  const navigate = useNavigate();
  const router = useRouter();
  const search = useSearch({ strict: false }) as { trace?: string };
  const opened = useRef(false);
  const figure = s ? figureOf(s, search.trace, name, pl ? only || undefined : undefined) : undefined;
  const openTrace = (col: number, key: string) => {
    opened.current = true;
    void navigate({ to: ".", search: (prev: Record<string, unknown>) => ({ ...prev, trace: `${col}~${key}` }) } as never);
  };
  const closeTrace = () => {
    if (opened.current) {
      opened.current = false;
      router.history.back();
      return;
    }
    void navigate({ to: ".", replace: true, search: ({ trace: _, ...rest }: Record<string, unknown>) => rest } as never);
  };
  // shown is what the statement shows, as the export dialog names it.
  const shown = s
    ? `${date(s.from)} to ${date(s.to)}${
        columns === "dimension"
          ? `, a column per ${dims.dimensions.find((d) => d.id === pivot)?.name ?? "dimension value"}`
          : byMonth
            ? ", a column per month"
            : compare === "prior_period"
              ? ", with the period before"
              : compare === "prior_year"
                ? ", with the same months a year earlier"
                : compared === "budget"
                  ? ", with the budget"
                  : ""
      }${pl && only ? `, only ${valueCodes([only], dims.dimensions, dims.byId)}` : ""}`
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
          query={{ from: run.from, to: run.to, compare: compared === "forecast" ? undefined : compared, columns, ...dimensionQuery, as_at_revision: s.ledger_revision }}
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
          <select className="fy-select" id={`${id}-compare`} value={compare} disabled={byMonth || columns === "dimension"} onChange={(e) => setCompare(e.target.value as typeof compare)}>
            <option value="">Nothing</option>
            <option value="prior_period">The period before</option>
            <option value="prior_year">The same months a year earlier</option>
            {pl && <option value="budget">The budget</option>}
            {pl && <option value="forecast">The forecast</option>}
          </select>
        </div>
        <label className="fy-row gap-2 self-end fy-expert-only">
          <input type="checkbox" checked={byMonth} disabled={columns === "dimension"} onChange={(e) => setByMonth(e.target.checked)} />A column per month
        </label>
        {pl && dims.dimensions.length > 0 && (
          <>
            <div className="fy-field fy-expert-only">
              <label className="fy-label" htmlFor={`${id}-pivot`} title={helpText("statement.by-dimension").tooltip}>
                Columns by dimension
              </label>
              <select className="fy-select" id={`${id}-pivot`} value={pivot} onChange={(e) => setPivot(e.target.value)}>
                <option value="">None</option>
                {dims.dimensions.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field fy-expert-only">
              <label className="fy-label" htmlFor={`${id}-only`}>
                Only for
              </label>
              <select className="fy-select" id={`${id}-only`} value={only} onChange={(e) => setOnly(e.target.value)}>
                <option value="">Every line</option>
                {dims.dimensions.map((d) => (
                  <optgroup key={d.id} label={d.name}>
                    {dims.values
                      .filter((v) => v.dimension_id === d.id)
                      .map((v) => (
                        <option key={v.id} value={v.id}>
                          {v.code} {v.name}
                        </option>
                      ))}
                  </optgroup>
                ))}
              </select>
            </div>
          </>
        )}
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
      {s && kind === "pl" && byMonth && <Trend s={s} />}
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
            <span className="fy-small fy-muted">Select a figure to trace it to its lines, its source and its history</span>
          </div>
          {compared === "budget" && !s.columns.some((c) => c.key === "budget") && (
            <p className="fy-small fy-secondary fy-expert-only">No budget column: a month of this period is in a financial year with no approved budget.</p>
          )}
          {compared === "forecast" && !s.forecast && (
            <p className="fy-small fy-secondary fy-expert-only">No forecast column: no recorded forecast covers every month of this period. A forecast starts after the last actual month.</p>
          )}
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
                  {s.forecast && (
                    <th scope="col" className="fy-num fy-expert-only" title={`${s.forecast.scenario} scenario of ${s.forecast.name}, by section: its lines are drivers, not accounts`}>
                      Forecast
                    </th>
                  )}
                  {kind === "pl" && (
                    <th scope="col" className="fy-num fy-expert-only">
                      % of revenue
                    </th>
                  )}
                </tr>
              </thead>
              <tbody>
                <TraceOpen.Provider value={openTrace}>
                  {s.blocks.map((b) => (
                    <BlockRows key={b.key} b={b} s={s} percent={kind === "pl"} />
                  ))}
                </TraceOpen.Provider>
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
                      <span className="fy-list__title">{x.label || x.memo || x.source_type.replaceAll("_", " ")}</span>
                      <span className="fy-list__meta">
                        {date(x.date)}
                        {x.label && x.memo ? ` · ${x.memo}` : ""}
                      </span>
                    </span>
                    <span className="fy-num">{formatAmount(x.amount.amount)}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {figure && <TracePanel figure={figure} onClose={closeTrace} />}
          <p className="fy-small fy-muted">
            Read at ledger revision {s.ledger_revision}: the same revision always gives the same figures.
            {mode === "expert" ? " Laid out from each account's report group, function and cash-flow line in the chart of accounts." : ""}
          </p>
        </section>
      )}
    </div>
  );
}
