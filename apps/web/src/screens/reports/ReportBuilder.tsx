import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, Play, Save, Trash2 } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods } from "../accounting/data";

type Saved = components["schemas"]["ReportDefinition"];
type Body = components["schemas"]["ReportDefinitionBody"];
type Row = components["schemas"]["ReportRowBody"];
type Run = components["schemas"]["ReportRun"];
type Schedule = components["schemas"]["ReportSchedule"];

const columnKeys = [
  { key: "current", name: "The run" },
  { key: "prior_period", name: "The period before" },
  { key: "prior_year", name: "The same months a year earlier" },
  { key: "ytd", name: "The year to date" },
] as const;

const blank = (): Body => ({
  schema: "fyniq-report@1",
  rows: [{ id: "rev", label: "Revenue", kind: "group", group: "Revenue" }],
  columns: [{ key: "current" }],
  filters: {},
});

// rowIdOf is the next free row ID, r1, r2…: a formula names a row by it.
const rowIdOf = (rows: Row[]) => {
  let n = rows.length + 1;
  while (rows.some((r) => r.id === `r${n}`)) n += 1;
  return `r${n}`;
};

// ReportBuilder is Reports › Report builder (docs/design/screens ReportBuilder; FR-RPT-04, FR-RPT-07): a person chooses rows (an account, a
// report group, a calculated row or a heading), columns (the run, the period before, the same months a year earlier, the year to date) and
// filters, sees the figures as they edit, saves the report, shares it with everyone who may read the whole ledger's reports, and schedules it.
// A calculated row divides, adds and multiplies exactly and shows "not available", never 0, where it divides by zero. Every figure is read
// at one ledger revision, which the run shows.
export function ReportBuilder() {
  const id = useId();
  const client = useQueryClient();
  const saved = useQuery({
    queryKey: ["report-definitions"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/report-definitions", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const months = useMemo(() => [...(periods.data?.items ?? [])].map((p) => p.starts_on.slice(0, 7)).reverse(), [periods.data]);
  const [current, setCurrent] = useState<Saved | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"private" | "shared">("private");
  const [def, setDef] = useState<Body>(blank());
  const [to, setTo] = useState("");
  const [run, setRun] = useState<Run | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const h = helpText("reports.builder");

  const open = (r: Saved | null) => {
    setCurrent(r);
    setName(r?.name ?? "");
    setDescription(r?.description ?? "");
    setVisibility(r?.visibility ?? "private");
    setDef(r?.definition ?? blank());
    setRun(null);
    setProblem(null);
    setMessage("");
  };
  const setRow = (i: number, patch: Partial<Row>) => setDef({ ...def, rows: def.rows.map((r, j) => (j === i ? { ...r, ...patch } : r)) });
  const toggleColumn = (key: (typeof columnKeys)[number]["key"]) => {
    const has = def.columns.some((c) => c.key === key);
    const columns = has ? def.columns.filter((c) => c.key !== key) : [...def.columns, { key }];
    if (columns.length > 0) setDef({ ...def, columns });
  };

  const runIt = async () => {
    setBusy(true);
    setMessage("");
    const { data, error } = await api.POST("/v1/report-builder/run", { body: { definition: def, to: to || undefined } });
    setBusy(false);
    if (error) {
      setProblem(problemOf(error));
      setRun(null);
      return;
    }
    setProblem(null);
    setRun(data);
  };

  const save = async () => {
    setBusy(true);
    setMessage("");
    const body = { name, description, visibility, definition: def };
    const r = current
      ? await api.PUT("/v1/report-definitions/{id}", { params: { path: { id: current.id }, header: { "If-Match": `"${current.version}"` } }, body })
      : await api.POST("/v1/report-definitions", { body });
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    setProblem(null);
    setCurrent(r.data); // the server's answer (FR-UX-22)
    setMessage(`"${r.data.name}" is saved as version ${r.data.version}.`);
    await client.invalidateQueries({ queryKey: ["report-definitions"] });
  };

  const archive = async () => {
    if (!current) return;
    setBusy(true);
    const { error } = await api.POST("/v1/report-definitions/{id}/archive", { params: { path: { id: current.id }, header: { "If-Match": `"${current.version}"` } } });
    setBusy(false);
    if (error) {
      setProblem(problemOf(error));
      return;
    }
    open(null);
    setMessage("The report is archived. Its schedules stop; it stays for anything that named it.");
    await client.invalidateQueries({ queryKey: ["report-definitions"] });
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Report builder</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        <div className="fy-pagehead__actions">
          <Button variant="secondary" onClick={() => open(null)}>
            <Icon of={Plus} /> New report
          </Button>
        </div>
      </div>

      {saved.error && <Refusal problem={problemOf(saved.error)} title="The saved reports could not be shown" />}
      <div className="fy-grid-2">
        <section className="fy-card fy-card--pad" aria-labelledby={`${id}-saved`}>
          <h2 className="fy-h3" id={`${id}-saved`}>
            Saved reports
          </h2>
          {(saved.data ?? []).length === 0 && <p className="fy-secondary">No report is saved yet.</p>}
          <ul className="fy-list">
            {(saved.data ?? []).map((r) => (
              <li key={r.id}>
                <button type="button" className="fy-link" aria-current={current?.id === r.id ? "true" : undefined} onClick={() => open(r)}>
                  {r.name}
                </button>{" "}
                <span className="fy-muted fy-small">{r.visibility === "shared" ? "shared" : "private"}</span>
              </li>
            ))}
          </ul>
        </section>

        <section className="fy-card fy-card--pad" aria-labelledby={`${id}-def`}>
          <h2 className="fy-h3" id={`${id}-def`}>
            {current ? "Change the report" : "A new report"}
          </h2>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-desc`}>
              What it is for
            </label>
            <input className="fy-input" id={`${id}-desc`} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-vis`}>
              Who can run it
            </label>
            <select className="fy-select" id={`${id}-vis`} value={visibility} onChange={(e) => setVisibility(e.target.value as "private" | "shared")}>
              <option value="private">Only me</option>
              <option value="shared">Everyone who may read the whole ledger's reports</option>
            </select>
          </div>
          <fieldset className="fy-field">
            <legend className="fy-label">Columns</legend>
            {columnKeys.map((c) => (
              <label key={c.key} className="fy-check">
                <input type="checkbox" checked={def.columns.some((x) => x.key === c.key)} onChange={() => toggleColumn(c.key)} /> {c.name}
              </label>
            ))}
          </fieldset>
          <label className="fy-check">
            <input type="checkbox" checked={def.filters.include_zero ?? false} onChange={(e) => setDef({ ...def, filters: { ...def.filters, include_zero: e.target.checked } })} /> Show rows
            of zeros
          </label>
        </section>
      </div>

      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-rows`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-rows`}>
            Rows
          </h2>
          <Button size="sm" variant="secondary" onClick={() => setDef({ ...def, rows: [...def.rows, { id: rowIdOf(def.rows), label: "", kind: "account", code: "" }] })}>
            <Icon of={Plus} /> Add a row
          </Button>
        </div>
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact">
            <caption className="fy-sr">The report's rows, in order</caption>
            <thead>
              <tr>
                <th scope="col">ID</th>
                <th scope="col">Label</th>
                <th scope="col">Kind</th>
                <th scope="col">Account, group or formula</th>
                <th scope="col">Format</th>
                <th scope="col">
                  <span className="fy-sr">Remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {def.rows.map((r, i) => (
                <tr key={i}>
                  <td>
                    <input className="fy-input" aria-label={`Row ${i + 1} ID`} value={r.id} maxLength={24} onChange={(e) => setRow(i, { id: e.target.value })} />
                  </td>
                  <td>
                    <input className="fy-input" aria-label={`Row ${i + 1} label`} value={r.label} maxLength={80} onChange={(e) => setRow(i, { label: e.target.value })} />
                  </td>
                  <td>
                    <select
                      className="fy-select"
                      aria-label={`Row ${i + 1} kind`}
                      value={r.kind}
                      onChange={(e) => setRow(i, { kind: e.target.value as Row["kind"], code: undefined, group: undefined, formula: undefined, format: e.target.value === "calc" ? "amount" : undefined })}
                    >
                      <option value="account">Account</option>
                      <option value="group">Report group</option>
                      <option value="calc">Calculation</option>
                      <option value="heading">Heading</option>
                    </select>
                  </td>
                  <td>
                    {r.kind === "account" && <input className="fy-input" aria-label={`Row ${i + 1} account code`} value={r.code ?? ""} onChange={(e) => setRow(i, { code: e.target.value })} />}
                    {r.kind === "group" && <input className="fy-input" aria-label={`Row ${i + 1} report group`} value={r.group ?? ""} onChange={(e) => setRow(i, { group: e.target.value })} />}
                    {r.kind === "calc" && <input className="fy-input" aria-label={`Row ${i + 1} formula`} value={r.formula ?? ""} placeholder="(rev - cogs) / rev" onChange={(e) => setRow(i, { formula: e.target.value })} />}
                  </td>
                  <td>
                    {r.kind === "calc" && (
                      <select className="fy-select" aria-label={`Row ${i + 1} format`} value={r.format ?? "amount"} onChange={(e) => setRow(i, { format: e.target.value as Row["format"] })}>
                        <option value="amount">Amount</option>
                        <option value="percent">Percent</option>
                        <option value="ratio">Ratio</option>
                      </select>
                    )}
                  </td>
                  <td>
                    <Button size="sm" variant="ghost" aria-label={`Remove row ${i + 1}`} onClick={() => setDef({ ...def, rows: def.rows.filter((_, j) => j !== i) })}>
                      <Icon of={Trash2} />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              Last month of the run
            </label>
            <select className="fy-select" id={`${id}-to`} value={to} onChange={(e) => setTo(e.target.value)}>
              <option value="">The current month</option>
              {months.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <Button onClick={runIt} disabled={busy}>
            <Icon of={Play} /> Show the figures
          </Button>
          <Button variant="secondary" onClick={save} disabled={busy || name.trim() === ""}>
            <Icon of={Save} /> {current ? "Save as the next version" : "Save the report"}
          </Button>
          {current && (
            <Button variant="ghost" onClick={archive} disabled={busy}>
              Archive
            </Button>
          )}
        </div>
      </section>

      {problem && <Refusal problem={problem} title="The report could not be worked out or saved" />}
      {message && (
        <p className="fy-secondary" role="status">
          {message}
        </p>
      )}
      {run && <RunTable run={run} />}
      {current && <Schedules report={current} />}
    </div>
  );
}

function RunTable({ run }: { run: Run }) {
  return (
    <div className="fy-card">
      <div className="fy-tablewrap">
        <table className="fy-table fy-table--compact">
          <caption className="fy-sr">
            The report from {run.from} to {run.to}, in {run.currency}
          </caption>
          <thead>
            <tr>
              <th scope="col">Row</th>
              {run.columns.map((c) => (
                <th scope="col" className="fy-num" key={c.key}>
                  {c.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {run.rows.map((r) => (
              <tr key={r.id}>
                <th scope="row">{r.label}</th>
                {r.cells.map((c, i) => (
                  <td className="fy-num" key={i}>
                    {r.kind === "heading" ? "" : c.not_available ? <span title="It cannot be worked out here, for instance a division by zero">not available</span> : cell(c.value ?? "", r.format)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="fy-small fy-muted fy-card--pad">
        Read at ledger revision {run.ledger_revision}, in {run.currency}. Amounts are exact; a percent or ratio is rounded once, half away from zero.
      </p>
    </div>
  );
}

const cell = (value: string, format?: string) => (format === "percent" ? `${value}%` : format === "ratio" ? value : formatAmount(value));

type Member = components["schemas"]["Member"];

function Schedules({ report }: { report: Saved }) {
  const id = useId();
  const client = useQueryClient();
  const list = useQuery({
    queryKey: ["report-schedules", report.id],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/report-schedules", { params: { query: { definition_id: report.id } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const members = useQuery({
    queryKey: ["members", "report-recipients"],
    queryFn: async () => {
      const r = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
      return r.error ? ([] as Member[]) : (r.data.items ?? []);
    },
  });
  const [cadence, setCadence] = useState<"weekly" | "monthly">("monthly");
  const [weekday, setWeekday] = useState(1);
  const [day, setDay] = useState(5);
  const [rule, setRule] = useState<"last_closed_month" | "month_to_date">("last_closed_month");
  const [to, setTo] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const make = async () => {
    const { error } = await api.POST("/v1/report-schedules", {
      body: { definition_id: report.id, cadence, weekday: cadence === "weekly" ? weekday : undefined, day_of_month: cadence === "monthly" ? day : undefined, period_rule: rule, channel: "email", recipients: to, active: true },
    });
    if (error) {
      setProblem(problemOf(error));
      return;
    }
    setProblem(null);
    await client.invalidateQueries({ queryKey: ["report-schedules", report.id] });
  };
  const toggle = async (s: Schedule) => {
    const { error } = await api.PUT("/v1/report-schedules/{id}", {
      params: { path: { id: s.id }, header: { "If-Match": `"${s.version}"` } },
      body: { cadence: s.cadence, weekday: s.weekday, day_of_month: s.day_of_month, period_rule: s.period_rule, channel: s.channel, slack_channel: s.slack_channel, recipients: s.recipients, active: !s.active },
    });
    if (error) setProblem(problemOf(error));
    await client.invalidateQueries({ queryKey: ["report-schedules", report.id] });
  };

  return (
    <section className="fy-card fy-card--pad" aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`}>
        Send it on a schedule
      </h2>
      <p className="fy-secondary">
        Each person is told by a notification that links to the report. It carries no figure and no file, and each person's right is checked again when it is sent.
      </p>
      {(list.data ?? []).map((s) => (
        <p key={s.id}>
          {s.cadence === "weekly" ? `Weekly, weekday ${s.weekday}` : `Monthly, on day ${s.day_of_month}`}, for {s.period_rule === "last_closed_month" ? "the last closed month" : "the month so far"}; next {s.next_run_on};{" "}
          {s.recipients.length} recipient(s); {s.active ? "on" : "off"}.{" "}
          <Button size="sm" variant="ghost" onClick={() => toggle(s)}>
            {s.active ? "Switch off" : "Switch on"}
          </Button>
        </p>
      ))}
      <div className="fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-cad`}>
            How often
          </label>
          <select className="fy-select" id={`${id}-cad`} value={cadence} onChange={(e) => setCadence(e.target.value as "weekly" | "monthly")}>
            <option value="monthly">Monthly</option>
            <option value="weekly">Weekly</option>
          </select>
        </div>
        {cadence === "weekly" ? (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-wd`}>
              Weekday
            </label>
            <select className="fy-select" id={`${id}-wd`} value={weekday} onChange={(e) => setWeekday(Number(e.target.value))}>
              {["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"].map((d, i) => (
                <option key={d} value={i + 1}>
                  {d}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-day`}>
              Day of the month (1 to 28)
            </label>
            <input className="fy-input" id={`${id}-day`} type="number" min={1} max={28} value={day} onChange={(e) => setDay(Number(e.target.value))} />
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-rule`}>
            For
          </label>
          <select className="fy-select" id={`${id}-rule`} value={rule} onChange={(e) => setRule(e.target.value as "last_closed_month" | "month_to_date")}>
            <option value="last_closed_month">The last closed month</option>
            <option value="month_to_date">The month so far</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To
          </label>
          <select className="fy-select" id={`${id}-to`} multiple value={to} onChange={(e) => setTo(Array.from(e.target.selectedOptions, (o) => o.value))}>
            {(members.data ?? []).map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.display_name}
              </option>
            ))}
          </select>
        </div>
        <Button onClick={make} disabled={to.length === 0}>
          Schedule it
        </Button>
      </div>
      {problem && <Refusal problem={problem} title="The schedule could not be saved" />}
    </section>
  );
}
