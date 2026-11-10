import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BookCheck, FlaskConical, Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Chart } from "../../components/chart/Chart";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { month } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { PlanningTabs } from "./tabs";

type Plan = components["schemas"]["ForecastPlan"];
type View = components["schemas"]["ForecastView"];
type Results = components["schemas"]["ForecastResults"];
type Run = components["schemas"]["ForecastRun"];

// blank is a new forecast's plan: 24 months from the books, nothing planned yet, the workbook's tax inputs, and Base,
// Best and Worst, which the API adds while scenarios are left out.
const blank: Plan = {
  name: "",
  horizon: 24,
  cash_adjustment: "0.00",
  revenue: {
    customers: "0",
    new_customers: "0",
    new_growth: "0",
    churn: "0",
    arpa: "0.00",
    price_increase: "0",
    services: "0.00",
    services_growth: "0",
    other: "0.00",
    collection: { this_month: "1", next_month: "0", month_after: "0" },
  },
  costs: { hosting: "0", ai: "0", contractors: "0", fees: "0", salary_increase: "0" },
  opex: [],
  headcount: [],
  capex: [],
  funding: [],
  tax: { start_up_years: 3, losses_brought_forward: "0.00", payment_lag: 9 },
};

type Field<T> = { key: keyof T & string; label: string; hint?: string };
const revenueFields: Field<Plan["revenue"]>[] = [
  { key: "customers", label: "Customers at the start" },
  { key: "new_customers", label: "New customers in month 1" },
  { key: "new_growth", label: "Growth of new customers", hint: "Monthly, a fraction" },
  { key: "churn", label: "Churn", hint: "Monthly, a fraction" },
  { key: "arpa", label: "Revenue per customer a month" },
  { key: "price_increase", label: "Price increase", hint: "Yearly, a fraction" },
  { key: "services", label: "Services in month 1" },
  { key: "services_growth", label: "Services growth", hint: "Monthly, a fraction" },
  { key: "other", label: "Other revenue a month" },
];
const costFields: Field<Plan["costs"]>[] = [
  { key: "hosting", label: "Hosting", hint: "A fraction of subscription revenue" },
  { key: "ai", label: "AI and API", hint: "A fraction of revenue" },
  { key: "contractors", label: "Contractors", hint: "A fraction of services revenue" },
  { key: "fees", label: "Payment fees", hint: "A fraction of revenue" },
  { key: "salary_increase", label: "Salary increase", hint: "Each forecast year, a fraction" },
];
const leverNames: { key: keyof NonNullable<Plan["scenarios"]>[number]["levers"] & string; label: string }[] = [
  { key: "new_customers", label: "New customers ×" },
  { key: "churn", label: "Churn ×" },
  { key: "price", label: "Price ×" },
  { key: "services", label: "Services ×" },
  { key: "opex", label: "Other expenses ×" },
  { key: "direct_costs", label: "Direct costs ×" },
  { key: "fund_amount", label: "Funding ×" },
  { key: "hire_delay", label: "Hiring delay (months)" },
  { key: "fund_delay", label: "Funding delay (months)" },
];

// Forecast is Planning › Forecast (docs/design/screens Forecast; FR-PLN-02, FR-PLN-03, FR-PLN-07, FR-PLN-08; p1-planning
// design D3 to D5, D12): the driver forecast's plan, drafted and saved, recorded as the company's own (Home's), and its
// figures for each scenario as the server works them out from the books and the packs, with the funding planned and
// without it, since none of it is received yet. Every figure is the server's. Nothing here posts. Expert only.
export function Forecast() {
  const id = useId();
  const queries = useQueryClient();
  const [selected, setSelected] = useState<string | null>(null);
  const [plan, setPlan] = useState<Plan>(blank);
  const [etag, setETag] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [scenario, setScenario] = useState("");
  const list = useQuery({
    queryKey: ["forecasts"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/forecasts", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data;
    },
  });
  const one = useQuery({
    queryKey: ["forecast", selected],
    enabled: selected !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/forecasts/{forecast_id}", { params: { path: { forecast_id: selected! } } });
      if (r.error) throw r.error;
      return { view: r.data as View, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const results = useQuery({
    queryKey: ["forecast-results", selected, one.data?.view.version],
    enabled: selected !== null && !!one.data,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/forecasts/{forecast_id}/results", { params: { path: { forecast_id: selected! } } });
      if (error) throw error;
      return data;
    },
    retry: false,
  });
  useEffect(() => {
    if (!selected && list.data?.items[0]) setSelected(list.data.items[0].id);
  }, [list.data, selected]);
  const v = one.data?.view;
  useEffect(() => {
    if (v && one.data) {
      setPlan(v.plan);
      setETag(one.data.etag);
    }
  }, [v, one.data]);
  const draft = !v || v.status === "draft";
  const fieldError = (key: string) => problem?.errors?.find((e) => e.field === `body.${key}`);
  const refresh = async (newId: string) => {
    setSelected(newId);
    await queries.invalidateQueries({ queryKey: ["forecasts"] });
    await queries.invalidateQueries({ queryKey: ["forecast", newId] });
  };
  const save = async () => {
    setBusy(true);
    setProblem(null);
    const header = { "Idempotency-Key": newKey() };
    const r = selected
      ? await api.PUT("/v1/forecasts/{forecast_id}", { params: { path: { forecast_id: selected }, header: { ...header, "If-Match": etag } }, body: plan })
      : await api.POST("/v1/forecasts", { params: { header }, body: plan });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    await refresh(r.data.id);
  };
  const record = async () => {
    if (!selected) return;
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/forecasts/{forecast_id}/record", {
      params: { path: { forecast_id: selected }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
    });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    await refresh(r.data.id);
  };
  const set = <K extends keyof Plan>(key: K, value: Plan[K]) => setPlan({ ...plan, [key]: value });
  const sandbox = helpText("planning.sandbox");
  const res = results.data;
  const names = [...new Set((res?.runs ?? []).map((r) => r.scenario))];
  const shown = scenario && names.includes(scenario) ? scenario : names[0] ?? "";
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Planning</span>
          <h1 className="fy-h1">{v ? v.plan.name : "Forecast"}</h1>
          <div className="fy-row fy-row--wrap gap-2">
            {v && <span className="fy-chip fy-chip--neutral">{v.status === "recorded" ? "Recorded: the company's plan" : v.status === "superseded" ? "Superseded" : "Draft"}</span>}
            <span className="fy-badge fy-badge--quiet" title={sandbox.tooltip}>
              <Icon of={FlaskConical} />
              {sandbox.label}
            </span>
          </div>
        </div>
        <div className="fy-pagehead__actions">
          <Button
            variant="ghost"
            onClick={() => {
              setSelected(null);
              setPlan(blank);
              setETag("");
              setProblem(null);
            }}
            title={helpText("forecast.new").tooltip}
          >
            <Icon of={Plus} />
            {helpText("forecast.new").label}
          </Button>
          {draft && (
            <Button variant="secondary" onClick={() => void save()} disabled={busy} title={helpText("forecast.save").tooltip}>
              <Icon of={Save} />
              {helpText("forecast.save").label}
            </Button>
          )}
          {v?.status === "draft" && (
            <Button variant="primary" onClick={() => void record()} disabled={busy} title={helpText("forecast.record").tooltip}>
              <Icon of={BookCheck} />
              {helpText("forecast.record").label}
            </Button>
          )}
        </div>
      </div>
      <PlanningTabs current="/planning/forecast" />
      {(list.data?.items.length ?? 0) > 0 && (
        <div className="fy-field max-w-md">
          <label className="fy-label" htmlFor={`${id}-forecast`}>
            Forecast
          </label>
          <select className="fy-select" id={`${id}-forecast`} value={selected ?? ""} onChange={(e) => setSelected(e.target.value || null)}>
            <option value="">A new forecast</option>
            {list.data!.items.map((f) => (
              <option key={f.id} value={f.id}>
                {f.plan.name}: {f.status}
              </option>
            ))}
          </select>
        </div>
      )}
      {problem && <Refusal problem={problem} title="The forecast was not saved" />}

      <section className="fy-card fy-card--pad fy-stack fy-stack--4" aria-labelledby={`${id}-plan`}>
        <h2 className="fy-h3" id={`${id}-plan`}>
          Plan
        </h2>
        <fieldset disabled={!draft} className="fy-stack fy-stack--4">
          <div className="fy-grid fy-grid--3">
            <TextField id={`${id}-name`} label="Name" value={plan.name} onChange={(x) => set("name", x)} error={fieldError("name")} />
            <TextField id={`${id}-horizon`} label="Months" hint="12 to 60" value={String(plan.horizon)} onChange={(x) => set("horizon", Number.parseInt(x || "0", 10))} error={fieldError("horizon")} />
            <TextField id={`${id}-last`} label="Last actual month" hint="YYYY-MM; left empty, the last closed month" value={plan.last_actual ?? ""} onChange={(x) => set("last_actual", x || undefined)} error={fieldError("last_actual")} />
            <TextField id={`${id}-adj`} label="Cash adjustment" hint="Added to the books' cash at the start" value={plan.cash_adjustment} onChange={(x) => set("cash_adjustment", x)} error={fieldError("cash_adjustment")} />
          </div>
          <fieldset className="fy-stack fy-stack--3">
            <legend className="fy-strong">Revenue</legend>
            <div className="fy-grid fy-grid--3">
              {revenueFields.map((f) => (
                <TextField key={f.key} id={`${id}-rev-${f.key}`} label={f.label} hint={f.hint} value={String(plan.revenue[f.key])}
                  onChange={(x) => set("revenue", { ...plan.revenue, [f.key]: x })} error={fieldError(`revenue.${f.key}`)} />
              ))}
              {(["this_month", "next_month", "month_after"] as const).map((k, i) => (
                <TextField key={k} id={`${id}-coll-${k}`} label={["Collected in the month", "Collected the month after", "Collected two months after"][i]!} hint="A fraction; the three add to 1"
                  value={plan.revenue.collection[k]} onChange={(x) => set("revenue", { ...plan.revenue, collection: { ...plan.revenue.collection, [k]: x } })} error={fieldError(`revenue.collection.${k}`)} />
              ))}
            </div>
          </fieldset>
          <fieldset className="fy-stack fy-stack--3">
            <legend className="fy-strong">Direct costs and salaries</legend>
            <div className="fy-grid fy-grid--3">
              {costFields.map((f) => (
                <TextField key={f.key} id={`${id}-cost-${f.key}`} label={f.label} hint={f.hint} value={plan.costs[f.key]} onChange={(x) => set("costs", { ...plan.costs, [f.key]: x })} error={fieldError(`costs.${f.key}`)} />
              ))}
            </div>
          </fieldset>
          <Rows
            id={`${id}-opex`}
            title="Other expenses"
            rows={plan.opex}
            onChange={(rows) => set("opex", rows)}
            add={{ label: "", report_group: "", base: "0.00", growth: "0", percent: "0" }}
            columns={[
              { key: "label", label: "What it is" },
              { key: "report_group", label: "Report group" },
              { key: "base", label: "Month 1" },
              { key: "growth", label: "Monthly growth" },
              { key: "percent", label: "Share of revenue" },
            ]}
          />
          <Rows
            id={`${id}-team`}
            title="Team"
            rows={plan.headcount}
            onChange={(rows) => set("headcount", rows)}
            add={{ role: "", function: "R&D", start: "", salary: "0.00", cpf: true }}
            columns={[
              { key: "role", label: "Person or role" },
              { key: "function", label: "Function", choices: ["COGS", "R&D", "S&M", "G&A"] },
              { key: "start", label: "First month" },
              { key: "end", label: "Last month" },
              { key: "salary", label: "Monthly salary" },
              { key: "cpf", label: "CPF", check: true },
            ]}
          />
          <Rows
            id={`${id}-capex`}
            title="Purchases"
            rows={plan.capex}
            onChange={(rows) => set("capex", rows)}
            add={{ item: "", amount: "0.00", month: "", life: 36 }}
            columns={[
              { key: "item", label: "Item" },
              { key: "amount", label: "Cost" },
              { key: "month", label: "Month" },
              { key: "life", label: "Life (months)", whole: true },
            ]}
          />
          <Rows
            id={`${id}-funding`}
            title="Funding"
            rows={plan.funding}
            onChange={(rows) => set("funding", rows)}
            add={{ type: "Equity", month: "", amount: "0.00" }}
            columns={[
              { key: "type", label: "Type", choices: ["Equity", "SAFE", "Loan", "Grant"] },
              { key: "month", label: "Month" },
              { key: "amount", label: "Amount" },
              { key: "rate", label: "Loan rate" },
              { key: "tenor", label: "Loan months", whole: true },
              { key: "notes", label: "Notes" },
            ]}
          />
          <fieldset className="fy-stack fy-stack--3">
            <legend className="fy-strong">Tax</legend>
            <div className="fy-grid fy-grid--3">
              <TextField id={`${id}-sute`} label="Start-up exemption years" hint="0 to 3" value={String(plan.tax.start_up_years)}
                onChange={(x) => set("tax", { ...plan.tax, start_up_years: Number.parseInt(x || "0", 10) })} error={fieldError("tax.start_up_years")} />
              <TextField id={`${id}-losses`} label="Losses brought forward" value={plan.tax.losses_brought_forward}
                onChange={(x) => set("tax", { ...plan.tax, losses_brought_forward: x })} error={fieldError("tax.losses_brought_forward")} />
              <TextField id={`${id}-lag`} label="Months until tax is paid" value={String(plan.tax.payment_lag)}
                onChange={(x) => set("tax", { ...plan.tax, payment_lag: Number.parseInt(x || "0", 10) })} error={fieldError("tax.payment_lag")} />
            </div>
          </fieldset>
          {plan.scenarios && (
            <fieldset className="fy-stack fy-stack--3">
              <legend className="fy-strong">Scenarios</legend>
              <div className="fy-tablewrap">
                <table className="fy-table fy-table--compact">
                  <caption className="fy-sr">The scenarios' levers</caption>
                  <thead>
                    <tr>
                      <th scope="col">Scenario</th>
                      {leverNames.map((l) => (
                        <th key={l.key} scope="col" className="fy-num">
                          {l.label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {plan.scenarios.map((s, i) => (
                      <tr key={i}>
                        <th scope="row">{s.name}</th>
                        {leverNames.map((l) => (
                          <td key={l.key}>
                            <input
                              className="fy-input fy-num"
                              aria-label={`${s.name}: ${l.label}`}
                              value={String(s.levers[l.key])}
                              onChange={(e) => {
                                const all = [...plan.scenarios!];
                                const value = l.key === "hire_delay" || l.key === "fund_delay" ? Number.parseInt(e.target.value || "0", 10) : e.target.value;
                                all[i] = { ...s, levers: { ...s.levers, [l.key]: value } };
                                set("scenarios", all);
                              }}
                            />
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </fieldset>
          )}
        </fieldset>
      </section>

      {results.isError && <Refusal problem={problemOf(results.error)} title="The forecast could not be worked out" />}
      {res && names.length > 0 && (
        <>
          <nav className="fy-tabs" aria-label="Scenarios">
            {names.map((n) => (
              <button key={n} type="button" aria-current={n === shown ? "page" : undefined} onClick={() => setScenario(n)}>
                {n}
              </button>
            ))}
          </nav>
          <ScenarioResults res={res} name={shown} id={id} />
          <Comparison res={res} id={id} />
        </>
      )}
    </div>
  );
}

// TextField is one input of the plan, its value a string the API checks; the error under it is the API's.
function TextField({ id, label, hint, value, onChange, error }: { id: string; label: string; hint?: string; value: string; onChange: (v: string) => void; error?: { message?: string; fix?: string } }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      <input
        className="fy-input"
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={error ? true : undefined}
        aria-describedby={[hint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ") || undefined}
      />
      {hint && (
        <span className="fy-help" id={`${id}-hint`}>
          {hint}
        </span>
      )}
      {error && (
        <span className="fy-error" id={`${id}-error`}>
          {error.message}: {error.fix}
        </span>
      )}
    </div>
  );
}

type Column = { key: string; label: string; choices?: readonly string[]; check?: boolean; whole?: boolean };

// Rows edits a list of the plan's rows, each a line of inputs, with Add and Remove.
function Rows<T extends Record<string, unknown>>({ id, title, rows, onChange, add, columns }: { id: string; title: string; rows: T[]; onChange: (rows: T[]) => void; add: T; columns: Column[] }) {
  const put = (i: number, key: string, value: unknown) => onChange(rows.map((r, j) => (j === i ? { ...r, [key]: value } : r)));
  return (
    <fieldset className="fy-stack fy-stack--2">
      <legend className="fy-strong">{title}</legend>
      {rows.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact">
            <caption className="fy-sr">{title}</caption>
            <thead>
              <tr>
                {columns.map((c) => (
                  <th key={c.key} scope="col">
                    {c.label}
                  </th>
                ))}
                <th scope="col">Remove</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}>
                  {columns.map((c) => (
                    <td key={c.key}>
                      {c.check ? (
                        <input type="checkbox" aria-label={`${title} ${i + 1}: ${c.label}`} checked={!!r[c.key]} onChange={(e) => put(i, c.key, e.target.checked)} />
                      ) : c.choices ? (
                        <select className="fy-select" aria-label={`${title} ${i + 1}: ${c.label}`} value={String(r[c.key] ?? "")} onChange={(e) => put(i, c.key, e.target.value)}>
                          {c.choices.map((x) => (
                            <option key={x} value={x}>
                              {x}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          className="fy-input"
                          aria-label={`${title} ${i + 1}: ${c.label}`}
                          value={r[c.key] === undefined ? "" : String(r[c.key])}
                          onChange={(e) => put(i, c.key, c.whole ? Number.parseInt(e.target.value || "0", 10) : e.target.value || undefined)}
                        />
                      )}
                    </td>
                  ))}
                  <td>
                    <Button variant="ghost" size="sm" onClick={() => onChange(rows.filter((_, j) => j !== i))} aria-label={`Remove ${title.toLowerCase()} ${i + 1}`}>
                      <Icon of={Trash2} />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Button variant="ghost" size="sm" className="self-start" onClick={() => onChange([...rows, { ...add }])} aria-label={`Add to ${title.toLowerCase()}`} id={`${id}-add`}>
        <Icon of={Plus} />
        Add
      </Button>
    </fieldset>
  );
}

// runOf is a scenario's run with or without its funding plan.
function runOf(res: Results, name: string, funding: boolean): Run | undefined {
  return res.runs.find((r) => r.scenario === name && r.with_funding === funding);
}

// cashOut says the cash-out month, or that cash lasts the forecast.
function cashOut(r: Run | undefined): string {
  if (!r) return "—";
  return r.key_results.cash_out_month ? month(r.key_results.cash_out_month) : "Not within the forecast";
}

// ScenarioResults are one scenario's key results, cash by month and yearly summary, with and without its funding.
function ScenarioResults({ res, name, id }: { res: Results; name: string; id: string }) {
  const withF = runOf(res, name, true);
  const without = runOf(res, name, false);
  if (!withF || !without) return null;
  const k = without.key_results;
  const kw = withF.key_results;
  const months = withF.months ?? [];
  return (
    <>
      <div className="fy-grid fy-grid--3">
        <div className="fy-tile" data-forecast="runway">
          <span className="fy-tile__label" title={helpText("forecast.without-funding").tooltip}>
            Runway, without the funding planned
          </span>
          <span className="fy-tile__value fy-num">
            {k.runway_beyond ? `${k.runway_months}+` : k.runway_months} <small>months</small>
          </span>
          <span className="fy-tile__meta">With it: {kw.runway_beyond ? `${kw.runway_months}+` : kw.runway_months} months</span>
        </div>
        <div className="fy-tile" data-forecast="cash-out">
          <span className="fy-tile__label">Cash runs out, without the funding</span>
          <span className="fy-tile__value">{cashOut(without)}</span>
          <span className="fy-tile__meta">With it: {cashOut(withF)}</span>
        </div>
        <div className="fy-tile" data-forecast="minimum">
          <span className="fy-tile__label">Minimum cash, with the funding</span>
          <span className="fy-tile__value fy-num">
            {formatAmount(kw.minimum_cash)} <small>{res.currency}</small>
          </span>
          <span className="fy-tile__meta">In {month(kw.minimum_month)}</span>
        </div>
        <div className="fy-tile" data-forecast="break-even">
          <span className="fy-tile__label">Break-even</span>
          <span className="fy-tile__value">{kw.break_even ? month(kw.break_even) : "Not within the forecast"}</span>
          <span className="fy-tile__meta">The first month with revenue whose EBITDA is not negative</span>
        </div>
        <div className="fy-tile" data-forecast="gap">
          <span className="fy-tile__label">Funding gap</span>
          <span className="fy-tile__value fy-num">
            {formatAmount(kw.funding_gap)} <small>{res.currency}</small>
          </span>
          <span className="fy-tile__meta">
            To keep {kw.buffer_months} months of burn; without the funding {formatAmount(k.funding_gap)}
          </span>
        </div>
        <div className="fy-tile" data-forecast="revenue">
          <span className="fy-tile__label">Revenue, next 12 months</span>
          <span className="fy-tile__value fy-num">
            {formatAmount(kw.revenue_next_12)} <small>{res.currency}</small>
          </span>
          <span className="fy-tile__meta">
            ARR at the end {formatAmount(kw.arr_end)}; {kw.customers_end} customers
          </span>
        </div>
      </div>
      {months.length > 0 && (
        <section aria-labelledby={`${id}-cash`}>
          <h2 className="fy-sr" id={`${id}-cash`}>
            Cash by month
          </h2>
          <Chart
            title={`Cash by month, ${name}`}
            summary={`With the funding planned, cash is lowest at ${formatAmount(kw.minimum_cash)} in ${month(kw.minimum_month)}; without it, ${
              k.cash_out_month ? `it runs out in ${month(k.cash_out_month)}` : "it lasts the forecast"
            }.`}
            form="line"
            label="Month"
            categories={months.map((m) => month(m.month))}
            series={[
              { key: "with", name: "With the funding", slot: 0, values: months.map((m) => m.cash) },
              { key: "without", name: "Without it", slot: 1, values: (without.months ?? []).map((m) => m.cash) },
            ]}
            unit={res.currency}
            file={`forecast-cash-${name}`}
            level={3}
          />
        </section>
      )}
      <section className="fy-card" aria-labelledby={`${id}-years`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-years`}>
            By forecast year, with the funding
          </h2>
        </div>
        <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
          <table className="fy-table fy-statement">
            <caption className="fy-sr">The forecast years of {name}, in {res.currency}</caption>
            <thead>
              <tr>
                <th scope="col">Year</th>
                {(["Revenue", "Gross profit", "EBITDA", "Net profit", "Net burn", "Cash", "Customers", "Headcount"] as const).map((h) => (
                  <th key={h} scope="col" className="fy-num">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {withF.years.map((y) => (
                <tr key={y.year}>
                  <th scope="row">Year {y.year}</th>
                  {[y.revenue, y.gross_profit, y.ebitda, y.net_profit, y.net_burn, y.cash].map((x, i) => (
                    <td key={i} className="fy-num">
                      {x ? formatAmount(x) : "—"}
                    </td>
                  ))}
                  <td className="fy-num">{y.customers ?? "—"}</td>
                  <td className="fy-num">{y.headcount ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

// Comparison sets every scenario's key results side by side, without the funding planned and with it (FR-PLN-03).
function Comparison({ res, id }: { res: Results; id: string }) {
  return (
    <section className="fy-card" aria-labelledby={`${id}-compare`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-compare`} title={helpText("forecast.compare").tooltip}>
          {helpText("forecast.compare").label}
        </h2>
      </div>
      <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
        <table className="fy-table fy-table--compact">
          <caption className="fy-sr">Every scenario's key results, in {res.currency}</caption>
          <thead>
            <tr>
              <th scope="col">Scenario</th>
              <th scope="col">Funding</th>
              <th scope="col">Cash runs out</th>
              <th scope="col" className="fy-num">
                Runway
              </th>
              <th scope="col" className="fy-num">
                Minimum cash
              </th>
              <th scope="col" className="fy-num">
                Funding gap
              </th>
              <th scope="col">Break-even</th>
            </tr>
          </thead>
          <tbody>
            {res.runs.map((r) => (
              <tr key={`${r.scenario}-${r.with_funding}`}>
                <th scope="row">{r.scenario}</th>
                <td>{r.with_funding ? "With" : "Without"}</td>
                <td>{cashOut(r)}</td>
                <td className="fy-num">{r.key_results.runway_beyond ? `${r.key_results.runway_months}+` : r.key_results.runway_months}</td>
                <td className="fy-num">{formatAmount(r.key_results.minimum_cash)}</td>
                <td className="fy-num">{formatAmount(r.key_results.funding_gap)}</td>
                <td>{r.key_results.break_even ? month(r.key_results.break_even) : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
