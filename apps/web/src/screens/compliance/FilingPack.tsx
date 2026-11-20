import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";

type Paper = components["schemas"]["TaxWorkingPaper"];
type Adjustment = components["schemas"]["TaxAdjustment"];
type Stored = components["schemas"]["TaxPaperView"];

const preparers = ["accountant", "tax_agent"];
const sgd = (v: string) => `S$${formatAmount(v)}`;

async function updates() {
  const { data, error } = await api.GET("/v1/statutory-packs/updates", {});
  if (error) throw error;
  return data.items ?? [];
}

async function papers() {
  const { data, error } = await api.GET("/v1/tax/working-papers", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// FilingPack is the tax agent's working paper of a financial year (FR-TAX-06): the ledger's profit and revenue, the adjustments given with their reasons, the exemption, the tax,
// and the form the revenue calls for, as the server works them (FR-UX-22); and the statutory pack updates waiting to be accepted, with what each would change (ENG-33).
export function FilingPack() {
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const [form, setForm] = useState({ financial_year_end: "", scheme: "pte", cash: false });
  const [rows, setRows] = useState<Adjustment[]>([]);
  const [paper, setPaper] = useState<Paper | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const u = useQuery({ queryKey: ["pack-updates"], queryFn: updates });
  const confirm = useConfirm();
  const queries = useQueryClient();
  const saved = useQuery({ queryKey: ["tax-papers"], queryFn: papers });
  const [status, setStatus] = useState("");
  const refresh = () => queries.invalidateQueries({ queryKey: ["tax-papers"] });
  const body = () => ({
    financial_year_end: form.financial_year_end,
    scheme: form.scheme as "sute" | "pte",
    cash_grant_eligible: form.cash,
    adjustments: rows.map((r) => ({ kind: r.kind, amount: r.amount, reason: r.reason })),
  });
  const keep = async () => {
    setBusy(true);
    setProblem(null);
    setStatus("");
    const { data, error } = await api.POST("/v1/tax/working-papers", { params: { header: { "Idempotency-Key": newKey() } }, body: body() });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setStatus(`Kept as a draft for the year ending ${data.paper.financial_year_end}: attach what supports the adjustments, then record it.`);
    await refresh();
  };
  const record = async (x: Stored, etag: string) => {
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/tax/working-papers/{paper_id}/record", { params: { path: { paper_id: x.id }, header: { "Idempotency-Key": newKey(), "If-Match": etag } } }));
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus(`Recorded: the paper for the year ending ${out.data.paper.financial_year_end} is kept with the figures it worked out.`);
    await refresh();
  };
  const work = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const { data, error } = await api.POST("/v1/tax/working-paper", {
      params: { header: { "Idempotency-Key": newKey() } },
      body: { financial_year_end: form.financial_year_end, scheme: form.scheme as "sute" | "pte", cash_grant_eligible: form.cash, adjustments: rows },
    });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setPaper(data);
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Compliance</span>
          <h1 className="fy-h1" title={helpText("compliance.filing-packs").tooltip}>
            Year-end tax working paper
          </h1>
          <p className="fy-secondary">The tax-adjusted profit of a financial year for the tax agent, and the statutory updates waiting to be accepted.</p>
        </div>
      </div>
      {problem && <Refusal problem={problem} title="The working paper was not made" />}
      {status && (
        <p className="fy-secondary" role="status">
          {status}
        </p>
      )}
      {may && (
        <form className="fy-card" onSubmit={(e) => void work(e)} aria-label="Work out a financial year">
          <div className="fy-card__body fy-stack fy-stack--4">
            <label className="fy-field">
              <span className="fy-label">Financial year ends</span>
              <input className="fy-input" type="date" required value={form.financial_year_end} onChange={(e) => setForm({ ...form, financial_year_end: e.target.value })} />
            </label>
            <label className="fy-field">
              <span className="fy-label">Exemption</span>
              <select className="fy-input" value={form.scheme} onChange={(e) => setForm({ ...form, scheme: e.target.value })}>
                <option value="pte">Partial tax exemption</option>
                <option value="sute">Start-up tax exemption (first three Years of Assessment)</option>
              </select>
            </label>
            <label className="fy-check">
              <input type="checkbox" checked={form.cash} onChange={(e) => setForm({ ...form, cash: e.target.checked })} />
              <span>The company met the cash grant's local employee condition</span>
            </label>
            {rows.map((r, i) => (
              <div className="fy-row fy-row--gap" key={i}>
                <label className="fy-field">
                  <span className="fy-label">Adjustment {i + 1} amount (negative takes off)</span>
                  <input className="fy-input" inputMode="decimal" value={r.amount} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, amount: e.target.value } : x)))} />
                </label>
                <label className="fy-field">
                  <span className="fy-label">Adjustment {i + 1} reason</span>
                  <input className="fy-input" value={r.reason} onChange={(e) => setRows(rows.map((x, j) => (j === i ? { ...x, reason: e.target.value } : x)))} />
                </label>
              </div>
            ))}
            <div className="fy-row fy-row--gap">
              <Button type="button" variant="ghost" onClick={() => setRows([...rows, { kind: "non_deductible", amount: "", reason: "" }])}>
                Add an adjustment
              </Button>
              <Button type="submit" variant="primary" disabled={busy}>
                Work it out
              </Button>
              <Button type="button" variant="secondary" disabled={busy || !form.financial_year_end} onClick={() => void keep()}>
                Keep as a draft paper
              </Button>
            </div>
          </div>
        </form>
      )}
      {paper && (
        <section className="fy-card" aria-labelledby="paper-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="paper-title">
              Year of Assessment {paper.ya}
            </h2>
          </div>
          <dl className="fy-card__body fy-stack fy-stack--2">
            {(
              [
                ["Revenue", paper.revenue],
                ["Profit before tax", paper.profit_before_tax],
                ["Adjustments", paper.adjustments],
                ["Chargeable income", paper.chargeable_income],
                ["Exempt", paper.exempt],
                ["Taxed at " + paper.rate, paper.normal_chargeable],
                ["Tax before the rebate", paper.tax_before_rebate],
                ["Rebate", paper.rebate],
                ["Tax payable", paper.tax_payable_rounded],
              ] as const
            ).map(([k, v]) => (
              <div className="fy-row fy-row--between" key={k}>
                <dt>{k}</dt>
                <dd className="fy-num">{sgd(v)}</dd>
              </div>
            ))}
          </dl>
          <p className="fy-small fy-secondary">
            Form {paper.form}
            {paper.optional_form ? `, or Form ${paper.optional_form}` : ""}, for revenue of {sgd(paper.revenue)}. Worked with {paper.cit_pack} and {paper.forms_pack}; the tax is rounded once, at the end.
          </p>
        </section>
      )}
      {saved.isError && <Refusal problem={problemOf(saved.error)} title="The kept papers could not be shown" />}
      {(saved.data ?? []).length > 0 && (
        <section className="fy-card" aria-labelledby="kept-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="kept-title">
              Kept papers
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--4">
            {(saved.data ?? []).map((x) => (
              <div key={x.id} className="fy-stack fy-stack--2">
                <p>
                  <strong>Year ending {x.paper.financial_year_end}</strong>: {x.status === "draft" ? "draft" : x.status === "recorded" ? "recorded" : "superseded"}, {x.paper.adjustments.length} adjustment
                  {x.paper.adjustments.length === 1 ? "" : "s"}
                  {x.figures ? `, tax payable ${sgd(x.figures.tax_payable_rounded)}` : ""}
                </p>
                {x.checks.map((c) => (
                  <p key={c.code} className="fy-small fy-secondary">
                    {c.message}. {c.fix}.
                  </p>
                ))}
                {x.status === "draft" && (
                  <>
                    <EvidencePanel record={{ kind: "tax-paper", id: x.id, etag: `"${x.version}-draft"` }} draft mayAttach={may} onChanged={(t) => { setStatus(t); void refresh(); }} />
                    {may && (
                      <Button type="button" variant="primary" onClick={() => void record(x, `"${x.version}-draft"`)}>
                        Record the paper for the year ending {x.paper.financial_year_end}
                      </Button>
                    )}
                  </>
                )}
              </div>
            ))}
          </div>
        </section>
      )}
      <section className="fy-card" aria-labelledby="updates-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="updates-title" title={helpText("compliance.pack-updates").tooltip}>
            Statutory updates waiting
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          {u.isError && <Refusal problem={problemOf(u.error)} title="The updates could not be shown" />}
          {u.data && u.data.length === 0 && <p className="fy-secondary">Every statutory pack you have activated is the newest in force.</p>}
          {(u.data ?? []).map((x) => (
            <details key={x.pack}>
              <summary>
                {x.pack}: {x.current} to {x.available}, {x.changes.length} change{x.changes.length === 1 ? "" : "s"}
              </summary>
              <ul className="fy-list">
                {x.changes.map((c) => (
                  <li key={c.table + c.key + c.kind} className="fy-list__item">
                    {c.kind} in {c.table}: {c.key}
                  </li>
                ))}
              </ul>
            </details>
          ))}
        </div>
      </section>
    </div>
  );
}
