import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, FlaskConical, Plus, Save, TriangleAlert } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { PlanningTabs } from "./tabs";

type Plan = components["schemas"]["StudyPlan"];
type Study = components["schemas"]["FeasibilityStudyView"];
type Results = components["schemas"]["FeasibilityResults"];

// blank is a new study's inputs: five years at 12%, a payback of three years acceptable, nothing entered yet.
const blank: Plan = {
  name: "",
  years: 5,
  discount_rate: "0.12",
  max_payback: "3",
  investment: "0.00",
  life: 5,
  salvage: "0.00",
  working_capital: "0",
  units: "0",
  unit_growth: "0",
  price: "0.00",
  price_growth: "0",
  variable_cost: "0.00",
  variable_cost_growth: "0",
  fixed: "0.00",
  fixed_growth: "0",
};

// The inputs, as the workbook's sheet groups them; each a decimal string the API checks (STUDY-01), never a number here.
const groups: { title: string; fields: { key: keyof Plan; label: string; hint: string; whole?: boolean }[] }[] = [
  {
    title: "Period and rate",
    fields: [
      { key: "years", label: "Years", hint: "1 to 10", whole: true },
      { key: "discount_rate", label: "Discount rate", hint: "A fraction: 0.12 for 12%" },
      { key: "max_payback", label: "Longest acceptable payback", hint: "Years" },
    ],
  },
  {
    title: "Investment",
    fields: [
      { key: "investment", label: "Investment", hint: "Spent at the start" },
      { key: "life", label: "Depreciation life", hint: "Years", whole: true },
      { key: "salvage", label: "Salvage value", hint: "At the end, no more than the investment" },
      { key: "working_capital", label: "Working capital", hint: "A fraction of revenue: 0.1 for 10%" },
    ],
  },
  {
    title: "Sales",
    fields: [
      { key: "units", label: "Units in year 1", hint: "At most 1 decimal" },
      { key: "unit_growth", label: "Unit growth", hint: "A yearly fraction: 0.5 for 50%" },
      { key: "price", label: "Price per unit", hint: "Year 1" },
      { key: "price_growth", label: "Price change", hint: "A yearly fraction" },
    ],
  },
  {
    title: "Costs",
    fields: [
      { key: "variable_cost", label: "Variable cost per unit", hint: "Year 1" },
      { key: "variable_cost_growth", label: "Variable cost change", hint: "A yearly fraction" },
      { key: "fixed", label: "Fixed costs", hint: "Year 1" },
      { key: "fixed_growth", label: "Fixed cost change", hint: "A yearly fraction" },
    ],
  },
];

const verdicts: Record<Results["verdict"], { tone: string; icon: typeof CircleCheck }> = {
  feasible: { tone: "fy-banner--good", icon: CircleCheck },
  marginal: { tone: "fy-banner--warning", icon: TriangleAlert },
  not_feasible: { tone: "fy-banner--critical", icon: CircleX },
  no_figures: { tone: "fy-banner--info", icon: FlaskConical },
};

// percent shows a fraction to 4 decimals as a percentage, by moving the point: "0.2690" → "26.90%", or with trim
// "0.8" → "80%". No arithmetic.
export function percent(fraction: string, trim = false): string {
  const neg = fraction.startsWith("-");
  const [whole, part = ""] = fraction.replace("-", "").split(".");
  const digits = (whole ?? "0") + part.padEnd(4, "0");
  let shown = (digits.slice(0, -2).replace(/^0+(?=\d)/, "") || "0") + "." + digits.slice(-2);
  if (trim) shown = shown.replace(/\.?0+$/, "");
  return `${neg ? "−" : ""}${shown}%`;
}

// Feasibility is Planning › Feasibility (docs/design/screens Feasibility; FR-PLN-06, FR-PLN-08; p1-planning design D8,
// D12): the studies, one study's inputs as the workbook's sheet has them, saved as a document, and its figures as the
// server works them out at sg-cit's rate on the study's date: the verdict, NPV, IRR, the paybacks, the profitability
// index, break-even and the margin of safety, each year's cash flows, the sensitivity grid and the NPV at other rates.
// Nothing here posts. Expert only.
export function Feasibility() {
  const id = useId();
  const queries = useQueryClient();
  const [selected, setSelected] = useState<string | null>(null);
  const [plan, setPlan] = useState<Plan>(blank);
  const [etag, setETag] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = useQuery({
    queryKey: ["feasibility-studies"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/feasibility-studies", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data;
    },
  });
  const study = useQuery({
    queryKey: ["feasibility-study", selected],
    enabled: selected !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/feasibility-studies/{study_id}", { params: { path: { study_id: selected! } } });
      if (r.error) throw r.error;
      return { body: r.data as Study, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const results = useQuery({
    queryKey: ["feasibility-results", selected, study.data?.body.version],
    enabled: selected !== null && !!study.data,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/feasibility-studies/{study_id}/results", { params: { path: { study_id: selected! } } });
      if (error) throw error;
      return data;
    },
  });
  useEffect(() => {
    if (study.data) {
      setPlan(study.data.body.study);
      setETag(study.data.etag);
    }
  }, [study.data]);
  const fieldError = (key: string) => problem?.errors?.find((e) => e.field === `body.${key}`);
  const save = async () => {
    setBusy(true);
    setProblem(null);
    const headers = { "Idempotency-Key": newKey() };
    const r = selected
      ? await api.PUT("/v1/feasibility-studies/{study_id}", {
          params: { path: { study_id: selected }, header: { ...headers, "If-Match": etag } },
          body: plan,
        })
      : await api.POST("/v1/feasibility-studies", { params: { header: headers }, body: plan });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    setSelected(r.data.id);
    await queries.invalidateQueries({ queryKey: ["feasibility-studies"] });
    await queries.invalidateQueries({ queryKey: ["feasibility-study", r.data.id] });
  };
  const res = results.data;
  const sandbox = helpText("planning.sandbox");
  const saveText = helpText("feasibility.save");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Planning</span>
          <h1 className="fy-h1">{selected && plan.name ? plan.name : "Feasibility"}</h1>
          <p className="fy-secondary">Is an idea worth it? Its cash flows, NPV, IRR and payback, as the workbook's sheet works them out.</p>
          <span className="fy-badge fy-badge--quiet self-start" title={sandbox.tooltip}>
            <Icon of={FlaskConical} />
            {sandbox.label}
          </span>
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
            title={helpText("feasibility.new").tooltip}
          >
            <Icon of={Plus} />
            {helpText("feasibility.new").label}
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy} title={saveText.tooltip}>
            <Icon of={Save} />
            {saveText.label}
          </Button>
        </div>
      </div>
      <PlanningTabs current="/planning/feasibility" />

      {(list.data?.items.length ?? 0) > 0 && (
        <div className="fy-field max-w-md">
          <label className="fy-label" htmlFor={`${id}-study`}>
            Study
          </label>
          <select className="fy-select" id={`${id}-study`} value={selected ?? ""} onChange={(e) => setSelected(e.target.value || null)}>
            <option value="">A new study</option>
            {list.data!.items.map((s) => (
              <option key={s.id} value={s.id}>
                {s.study.name} ({date(s.date)})
              </option>
            ))}
          </select>
        </div>
      )}
      {problem && <Refusal problem={problem} title="The study was not saved" />}

      <section className="fy-card fy-card--pad fy-stack fy-stack--4" aria-labelledby={`${id}-inputs`}>
        <h2 className="fy-h3" id={`${id}-inputs`}>
          Inputs
        </h2>
        <div className="fy-field max-w-xl">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input
            className="fy-input"
            id={`${id}-name`}
            value={plan.name}
            maxLength={80}
            onChange={(e) => setPlan({ ...plan, name: e.target.value })}
            aria-invalid={fieldError("name") ? true : undefined}
          />
        </div>
        <div className="fy-grid fy-grid--2">
          {groups.map((g) => (
            <fieldset key={g.title} className="fy-stack fy-stack--3">
              <legend className="fy-strong">{g.title}</legend>
              {g.fields.map((f) => {
                const err = fieldError(f.key);
                return (
                  <div key={f.key} className="fy-field">
                    <label className="fy-label" htmlFor={`${id}-${f.key}`}>
                      {f.label}
                    </label>
                    <input
                      className="fy-input fy-num"
                      id={`${id}-${f.key}`}
                      inputMode="decimal"
                      value={String(plan[f.key])}
                      onChange={(e) => setPlan({ ...plan, [f.key]: f.whole ? Number.parseInt(e.target.value || "0", 10) : e.target.value })}
                      aria-invalid={err ? true : undefined}
                      aria-describedby={`${id}-${f.key}-hint${err ? ` ${id}-${f.key}-error` : ""}`}
                    />
                    <span className="fy-help" id={`${id}-${f.key}-hint`}>
                      {f.hint}
                    </span>
                    {err && (
                      <span className="fy-error" id={`${id}-${f.key}-error`}>
                        {err.message}: {err.fix}
                      </span>
                    )}
                  </div>
                );
              })}
            </fieldset>
          ))}
        </div>
      </section>

      {results.isError && <Refusal problem={problemOf(results.error)} title="The study could not be worked out" />}
      {res && <StudyResults r={res} id={id} />}
    </div>
  );
}

// StudyResults are a study's figures, each the server's text, never worked out here (FR-UX-22).
function StudyResults({ r, id }: { r: Results; id: string }) {
  const v = verdicts[r.verdict];
  const amount = (x?: string) => (x ? formatAmount(x) : "—");
  return (
    <>
      <div className={`fy-banner ${v.tone}`} role="status">
        <Icon of={v.icon} size={20} />
        <div className="fy-banner__text">
          <strong>{r.verdict_words}</strong>
          <span className="fy-small">
            At sg-cit's rate of {percent(r.tax_rate, true)} on the study's date ({r.tax_pack}), fixed for its years.
          </span>
        </div>
      </div>
      <div className="fy-grid fy-grid--3">
        <div className="fy-tile">
          <span className="fy-tile__label">NPV</span>
          <span className="fy-tile__value fy-num">
            {amount(r.npv)} <small>{r.currency}</small>
          </span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">IRR</span>
          <span className="fy-tile__value fy-num">{r.irr ? percent(r.irr) : "—"}</span>
          {!r.irr && <span className="fy-tile__meta">No rate makes the NPV zero</span>}
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Payback</span>
          <span className="fy-tile__value fy-num">
            {r.payback_years ?? "—"} <small>years</small>
          </span>
          <span className="fy-tile__meta">{r.payback_years ? `Discounted: ${r.discounted_payback_years ?? "never"}` : "Not within the period"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Profitability index</span>
          <span className="fy-tile__value fy-num">{r.profitability_index ?? "—"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Break-even</span>
          <span className="fy-tile__value fy-num">
            {r.break_even_units ?? "never"} <small>units</small>
          </span>
          <span className="fy-tile__meta">{r.break_even_revenue ? `${amount(r.break_even_revenue)} ${r.currency} of revenue` : "Price is not above the variable cost"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Margin of safety</span>
          <span className="fy-tile__value fy-num">{r.margin_of_safety_percent ? `${formatAmount(r.margin_of_safety_percent)}%` : "—"}</span>
          <span className="fy-tile__meta">Year 1's units above break-even</span>
        </div>
      </div>

      <section className="fy-card" aria-labelledby={`${id}-flows`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-flows`}>
            Cash flows
          </h2>
          <span className="fy-small fy-muted">{r.currency}, by year</span>
        </div>
        <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
          <table className="fy-table fy-statement">
            <caption className="fy-sr">The study's cash flows by year, in {r.currency}</caption>
            <thead>
              <tr>
                <th scope="col">Item</th>
                {r.years.map((y) => (
                  <th key={y.year} scope="col" className="fy-num">
                    Year {y.year}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(
                [
                  ["Revenue", "revenue"],
                  ["Variable costs", "variable_costs"],
                  ["Fixed costs", "fixed_costs"],
                  ["Depreciation", "depreciation"],
                  ["EBIT", "ebit"],
                  ["Tax", "tax"],
                  ["Working capital change", "working_capital_change"],
                  ["Salvage after tax", "salvage"],
                  ["Working capital released", "working_capital_release"],
                  ["Cash flow", "cash_flow"],
                  ["Present value", "present_value"],
                  ["Cumulative", "cumulative"],
                ] as const
              ).map(([label, key]) => (
                <tr key={key} className={key === "cash_flow" ? "fy-strong" : undefined}>
                  <th scope="row">{label}</th>
                  {r.years.map((y) => (
                    <td key={y.year} className="fy-num">
                      {formatAmount(y[key])}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <div className="fy-split">
        <section className="fy-card" aria-labelledby={`${id}-sens`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-sens`} title={helpText("feasibility.sensitivity").tooltip}>
              {helpText("feasibility.sensitivity").label}
            </h2>
          </div>
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--compact">
              <caption className="fy-sr">The NPV by sales volume (rows) and fixed costs (columns), in {r.currency}</caption>
              <thead>
                <tr>
                  <th scope="col">Volume \ fixed costs</th>
                  {["80%", "90%", "100%", "110%", "120%"].map((x) => (
                    <th key={x} scope="col" className="fy-num">
                      {x}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {r.sensitivity.map((row) => (
                  <tr key={row.volume}>
                    <th scope="row">{percent(row.volume, true)}</th>
                    {row.npv_by_fixed_costs.map((x, j) => (
                      <td key={j} className={row.volume === "1" && j === 2 ? "fy-num fy-strong" : "fy-num"}>
                        {formatAmount(x)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
        <section className="fy-card" aria-labelledby={`${id}-rates`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-rates`}>
              NPV at other rates
            </h2>
          </div>
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--compact">
              <caption className="fy-sr">The NPV at eight discount rates, in {r.currency}</caption>
              <thead>
                <tr>
                  <th scope="col">Rate</th>
                  <th scope="col" className="fy-num">
                    NPV ({r.currency})
                  </th>
                </tr>
              </thead>
              <tbody>
                {r.npv_at_rates.map((x) => (
                  <tr key={x.rate}>
                    <th scope="row">{percent(x.rate, true)}</th>
                    <td className="fy-num">{formatAmount(x.npv)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </>
  );
}
