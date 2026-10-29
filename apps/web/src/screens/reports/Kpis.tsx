import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Pencil } from "lucide-react";
import { useEffect, useId, useMemo, useState, type FormEvent } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { dateTime, month as monthName } from "../../format/date";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods } from "../accounting/data";
import { figureOf, figureText, find, previous, sourceNames, Tile, useKpis, type KPI, type KPIs } from "./kpi";
import { runOf } from "./Statement";

type Counts = components["schemas"]["KPICountsOut"];

// headline are the KPIs the page leads with (docs/design/screens Kpis): both MRR measures, the burn and the runway.
const headline = ["booked_mrr", "contracted_mrr", "net_burn", "runway"];

// basic are the rows Basic shows in the table (FR-UX-23); Expert shows every KPI with its formula, source and version.
const basic = new Set(["cash", "revenue", "net_profit_ytd", "owed_to_us", "owed_by_us", "arr", "revenue_growth", "gross_margin", "headcount"]);

// enterers are the roles that may enter the counts ("Budgets, forecasts" C; design D7). The API decides; the form is
// offered only to them.
const enterers = ["owner", "controller", "accountant"];

// Kpis is Reports › KPIs (docs/design/screens Kpis; FR-KPI-02, FR-KPI-03, design D6 of p1-dashboard-kpis): a month's
// KPIs beside the month before, each with how it is worked out, its source and its definition's version; both MRR
// measures, explained; and the month's customer counts and headcount, which an Owner, Controller or Accountant enters
// (rev 0.161). A KPI without a value shows "—" with why, never 0. Benchmarks join in P2.
export function Kpis() {
  const id = useId();
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods, retry: false });
  const months = useMemo(() => [...(periods.data?.items ?? [])].map((p) => p.starts_on.slice(0, 7)).reverse(), [periods.data]);
  const [chosen, setChosen] = useState("");
  const month = chosen || (runOf("month", periods.data?.items ?? [], { from: "", to: "" })?.to ?? "");
  const kpis = useKpis(month || null);
  const k = kpis.data;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Start-up KPIs</h1>
          <p className="fy-secondary">
            {k ? `${monthName(k.month)}, in ${k.currency}, read at ledger revision ${k.ledger_revision}. ` : ""}Each figure says how it is worked out.
          </p>
        </div>
        {months.length > 0 && (
          <div className="fy-pagehead__actions">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-month`}>
                Month
              </label>
              <select className="fy-select" id={`${id}-month`} value={month} onChange={(e) => setChosen(e.target.value)}>
                {months.map((m) => (
                  <option key={m} value={m}>
                    {monthName(m)}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}
      </div>
      <ReviewBanner screen="kpis" />
      {periods.error && <Refusal problem={problemOf(periods.error)} title="The books' months could not be read" />}
      {periods.data && months.length === 0 && (
        <p className="fy-secondary">
          The books have no months yet. Open the first financial year in <Link to="/accounting/close">Accounting › Periods and close</Link>.
        </p>
      )}
      {kpis.error && <Refusal problem={problemOf(kpis.error)} title="The KPIs could not be shown" />}
      {kpis.isPending && month && <div className="fy-skeleton h-40" aria-hidden="true" />}
      {k && <Body kpis={k} />}
    </div>
  );
}

function Body({ kpis }: { kpis: KPIs }) {
  const before = monthName(previous(kpis.month));
  const at = monthName(kpis.month);
  return (
    <>
      <div className="fy-grid fy-grid--tiles">
        {headline.map((key) => {
          const x = find(kpis, key);
          return x ? <Tile key={key} k={x} kpis={kpis} /> : null;
        })}
      </div>
      <p className="fy-small fy-secondary">
        Booked MRR is the subscription revenue the ledger recognised in the month: what was earned. Contracted MRR is the monthly value of the subscriptions active at the month end: what is signed. It needs
        subscription data, which arrives in a later phase; until then it says so.
      </p>
      <section className="fy-card" aria-labelledby="kpi-all">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="kpi-all">
            All KPIs
          </h2>
          <span className="fy-tag fy-tag--expert fy-basic-only">More in Expert</span>
        </div>
        <div className="fy-tablewrap border-0 rounded-none">
          <table className="fy-table fy-table--cards">
            <caption className="fy-sr">
              The KPIs of {at} beside {before}
            </caption>
            <thead>
              <tr>
                <th scope="col">KPI</th>
                <th scope="col" className="fy-num">
                  {at}
                </th>
                <th scope="col" className="fy-num">
                  {before}
                </th>
                <th scope="col" className="fy-expert-only">
                  How it is worked out
                </th>
                <th scope="col" className="fy-expert-only">
                  Source
                </th>
              </tr>
            </thead>
            <tbody>
              {kpis.kpis.map((k) => (
                <Row key={k.key} k={k} kpis={kpis} />
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <CountsCard kpis={kpis} />
    </>
  );
}

function Row({ k, kpis }: { k: KPI; kpis: KPIs }) {
  const now = figureOf(k, k.month, kpis.currency);
  return (
    <tr className={basic.has(k.key) || headline.includes(k.key) ? undefined : "fy-expert-only"} data-kpi={k.key}>
      <td data-card="title">
        <span className="fy-stack fy-stack--1">
          <span>{k.name}</span>
          {now.why && <span className="fy-small fy-muted whitespace-normal">No figure: {now.why}</span>}
        </span>
      </td>
      <td className="fy-num" data-card="amount">
        {figureText(k, k.month, kpis.currency)}
      </td>
      <td className="fy-num" data-card="hide">
        {k.before ? figureText(k, k.before, kpis.currency) : "—"}
      </td>
      <td className="fy-expert-only whitespace-normal" data-card="meta">
        {k.formula}
        <span className="fy-small fy-muted block">Reads {k.date_basis}.</span>
      </td>
      <td className="fy-expert-only" data-card="meta">
        <span className="fy-stack fy-stack--1">
          <span>{sourceNames[k.source] ?? k.source}</span>
          <span className="fy-small fy-muted">{k.version}</span>
        </span>
      </td>
    </tr>
  );
}

const fields = [
  { key: "paying_customers", label: "Paying customers", help: "At the month's end" },
  { key: "new_customers", label: "New customers", help: "Who started paying in the month" },
  { key: "lost_customers", label: "Customers lost", help: "Who stopped paying in the month" },
  { key: "headcount", label: "Headcount", help: "People employed in the month" },
] as const;

type Draft = Record<(typeof fields)[number]["key"], string>;

const draftOf = (c: Counts | undefined): Draft => ({
  paying_customers: c?.paying_customers?.toString() ?? "",
  new_customers: c?.new_customers?.toString() ?? "",
  lost_customers: c?.lost_customers?.toString() ?? "",
  headcount: c?.headcount?.toString() ?? "",
});

// check says what is wrong with a draft, field by field, in the API's words (FR-KPI-02, ENG-23): whole numbers of zero
// or more; the three customer counts together or none; something to save.
function check(d: Draft): Partial<Record<keyof Draft, string>> {
  const out: Partial<Record<keyof Draft, string>> = {};
  for (const f of fields) {
    if (d[f.key] !== "" && !/^\d+$/.test(d[f.key].trim())) out[f.key] = `${f.label}: enter a whole number of zero or more`;
  }
  const customers = [d.paying_customers, d.new_customers, d.lost_customers].map((v) => v.trim() !== "");
  if (customers.some(Boolean) && !customers.every(Boolean)) {
    for (const f of fields.slice(0, 3)) if (d[f.key].trim() === "") out[f.key] ??= `${f.label}: enter it with the other two customer counts, or clear all three`;
  }
  if (Object.keys(out).length === 0 && fields.every((f) => d[f.key].trim() === "")) out.paying_customers = "Enter the customer counts, the headcount, or both";
  return out;
}

// CountsCard is the month's entered counts (rev 0.161): until subscriptions and payroll supply them, a person enters
// the paying, new and lost customers and the headcount each month. Saving replaces them as a new version, audited; the
// KPIs that use them are labelled Entered.
function CountsCard({ kpis }: { kpis: KPIs }) {
  const id = useId();
  const { session } = useSignedIn();
  const client = useQueryClient();
  const may = (session.roles ?? []).some((r) => enterers.includes(r));
  const counts = kpis.counts;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Draft>(draftOf(counts));
  const [errors, setErrors] = useState<Partial<Record<keyof Draft, string>>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [saved, setSaved] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setDraft(draftOf(counts));
    setEditing(false);
    setErrors({});
    setProblem(null);
  }, [kpis.month, counts]);
  const at = monthName(kpis.month);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const bad = check(draft);
    setErrors(bad);
    setSaved("");
    if (Object.keys(bad).length > 0) {
      document.getElementById(`${id}-${Object.keys(bad)[0]}`)?.focus();
      return;
    }
    const n = (v: string) => (v.trim() === "" ? undefined : Number(v.trim()));
    setBusy(true);
    const { error } = await api.PUT("/v1/kpi-inputs/{month}", {
      params: { path: { month: kpis.month }, header: { "If-Match": `"${counts?.version ?? 0}"` } },
      body: { paying_customers: n(draft.paying_customers), new_customers: n(draft.new_customers), lost_customers: n(draft.lost_customers), headcount: n(draft.headcount) },
    });
    setBusy(false);
    if (error) {
      setProblem(problemOf(error));
      return;
    }
    setProblem(null);
    setEditing(false);
    setSaved(`${at}'s counts are saved. The KPIs that use them are worked out again.`);
    await client.invalidateQueries({ queryKey: ["kpis"] });
  };

  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Customer counts and headcount, {at}
        </h2>
        {may && !editing && (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            <Icon of={Pencil} />
            {counts ? "Change" : "Enter"}
          </Button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">
          ARPA, churn, CAC, LTV and the revenue per employee need these until subscriptions and payroll supply them. Each month is entered once, and changed as a new version; every
          change is in the audit trail.
        </p>
        {saved && (
          <p className="fy-small fy-row gap-2" role="status">
            <Icon of={CircleCheck} />
            {saved}
          </p>
        )}
        {problem && <Refusal problem={problem} title="The counts were not saved" />}
        {!editing && (
          <dl className="fy-grid fy-grid--4">
            {fields.map((f) => (
              <div key={f.key} className="fy-stack fy-stack--1">
                <dt className="fy-small fy-secondary">{f.label}</dt>
                <dd className="fy-num fy-strong m-0">{counts?.[f.key] ?? "—"}</dd>
              </div>
            ))}
          </dl>
        )}
        {!editing && (
          <p className="fy-small fy-muted">
            {counts ? `Entered by ${counts.entered_by?.name ?? "someone"} on ${dateTime(counts.entered_at)}.` : `Nothing entered for ${at}.`}
            {!may && " An Owner, Controller or Accountant enters them."}
          </p>
        )}
        {editing && (
          <form className="fy-stack fy-stack--3" onSubmit={(e) => void submit(e)} noValidate>
            <div className="fy-form fy-form--2">
              {fields.map((f) => {
                const fid = `${id}-${f.key}`;
                const err = errors[f.key];
                return (
                  <div key={f.key} className="fy-field">
                    <label className="fy-label" htmlFor={fid}>
                      {f.label}
                    </label>
                    <input
                      className="fy-input fy-num"
                      id={fid}
                      inputMode="numeric"
                      autoComplete="off"
                      value={draft[f.key]}
                      onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}
                      aria-invalid={err ? true : undefined}
                      aria-describedby={`${fid}-help${err ? ` ${fid}-error` : ""}`}
                    />
                    <span className="fy-help" id={`${fid}-help`}>
                      {f.help}
                    </span>
                    {err && (
                      <span className="fy-error" id={`${fid}-error`}>
                        {err}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
            <div className="fy-row gap-2">
              <Button type="submit" disabled={busy}>
                Save
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setEditing(false);
                  setDraft(draftOf(counts));
                  setErrors({});
                }}
              >
                Cancel
              </Button>
            </div>
          </form>
        )}
      </div>
    </section>
  );
}
