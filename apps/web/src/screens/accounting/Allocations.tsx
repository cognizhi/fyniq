import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, Eye, FilePlus, Plus, Save, Trash2 } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, listDimensions, listPeriods, postable } from "./data";

type Rule = components["schemas"]["AllocationRule"];
type Input = components["schemas"]["AllocationRuleInput"];
type Report = components["schemas"]["AllocationReport"];
type Target = components["schemas"]["AllocationTarget"];

const keys: Record<string, string> = { fixed: "Fixed percentages", headcount: "Headcount of each department", usage: "Usage from a supplier's file" };
const empty: Input = { name: "", accounts: [], key: "fixed", targets: [{ label: "", values: [""], percent: "" }] };

// monthEnd is the last day of a month, "YYYY-MM-DD" (a rule runs over whole months).
function monthEnd(month: string): string {
  const [y, m] = month.split("-").map(Number) as [number, number];
  return new Date(Date.UTC(y, m, 0)).toISOString().slice(0, 10);
}

// usageRows reads a usage file pasted as lines of "code, quantity, billed".
function usageRows(text: string): components["schemas"]["AllocationUsageRow"][] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "")
    .map((l) => {
      const [code = "", quantity = "", billed = ""] = l.split(",").map((x) => x.trim());
      return { code, quantity, billed };
    });
}

// Allocations is Accounting › Allocations (docs/design/screens Allocations; FR-DIM-03, Expert): a shared cost moved between departments, projects or customers by a rule, worked out by ALLOC-PRORATA
// with what the key cannot place left unallocated, and run into a draft journal an approver posts like any other. Nothing here posts.
export function Allocations() {
  const id = useId();
  const client = useQueryClient();
  const h = helpText("accounting.allocations");
  const rules = useQuery({
    queryKey: ["allocation-rules"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/allocation-rules", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const accounts = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const dims = useQuery({ queryKey: ["dimensions", "all"], queryFn: listDimensions });
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const expenses = useMemo(() => postable(accounts.data ?? []).filter((a) => a.type === "expense"), [accounts.data]);
  const months = useMemo(() => [...(periods.data?.items ?? [])].map((p) => p.starts_on.slice(0, 7)).reverse(), [periods.data]);
  const [openId, setOpenId] = useState("");
  const [etag, setEtag] = useState("");
  const [form, setForm] = useState<Input>(empty);
  const [month, setMonth] = useState("");
  const [usage, setUsage] = useState({ bill: "", total: "", rows: "" });
  const [report, setReport] = useState<Report | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);

  const choose = async (next: string) => {
    setOpenId(next);
    setReport(null);
    setProblem(null);
    if (!next) {
      setForm(empty);
      setEtag("");
      return;
    }
    const { data, response, error } = await api.GET("/v1/allocation-rules/{rule_id}", { params: { path: { rule_id: next } } });
    if (error || !data) return setProblem(problemOf(error));
    setForm({ name: data.name, accounts: data.accounts, key: data.key, targets: data.targets, archived: data.archived });
    setEtag(response.headers.get("ETag") ?? "");
  };
  const setTarget = (i: number, patch: Partial<Target>) => setForm({ ...form, targets: form.targets.map((t, k) => (k === i ? { ...t, ...patch } : t)) });
  const refresh = async () => client.invalidateQueries({ queryKey: ["allocation-rules"] });

  const save = async (archived = false) => {
    setBusy(true);
    const body: Input = { ...form, archived: archived || undefined, targets: form.targets.map((t) => ({ ...t, values: t.values.filter((v) => v !== ""), percent: form.key === "fixed" ? t.percent : undefined, code: form.key === "usage" ? t.code : undefined })) };
    const r = openId
      ? await api.PUT("/v1/allocation-rules/{rule_id}", { params: { path: { rule_id: openId }, header: { "If-Match": etag } }, body })
      : await api.POST("/v1/allocation-rules", { body });
    setBusy(false);
    if (r.error || !r.data) return setProblem(problemOf(r.error));
    setProblem(null);
    setOpenId(r.data.id);
    setEtag(r.response.headers.get("ETag") ?? "");
    await refresh();
  };

  const runBody = () => {
    const from = `${month || months[0]}-01`;
    const body: components["schemas"]["AllocationRunInput"] = { rule_id: openId, from, to: monthEnd(month || months[0]!) };
    if (form.key === "usage") body.usage = { bill_id: usage.bill, billed_total: usage.total, rows: usageRows(usage.rows) };
    return body;
  };
  const preview = async () => {
    if (!openId) return;
    setBusy(true);
    const { data, error } = await api.POST("/v1/allocation-previews", { body: runBody() });
    setBusy(false);
    if (error || !data) return setProblem(problemOf(error)), setReport(null);
    setProblem(null);
    setReport(data);
  };
  const propose = async () => {
    if (!openId) return;
    setBusy(true);
    const { data, error } = await api.POST("/v1/allocation-runs", { body: runBody() });
    setBusy(false);
    if (error || !data) return setProblem(problemOf(error));
    setProblem(null);
    setReport(data);
  };

  const valueName = (vid: string) => dims.data?.values.find((v) => v.id === vid)?.name ?? vid;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <Icon of={ChevronRight} />
            <span>Allocations</span>
          </nav>
          <h1 className="fy-h1">Allocations</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {rules.error && <Refusal problem={problemOf(rules.error)} title="The allocation rules could not be shown" />}
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-open`}>
            Open a rule
          </label>
          <select className="fy-select" id={`${id}-open`} value={openId} onChange={(e) => void choose(e.target.value)}>
            <option value="">A new rule</option>
            {(rules.data ?? []).map((r: Rule) => (
              <option key={r.id} value={r.id}>
                {r.name}
                {r.archived ? " (out of use)" : ""}
              </option>
            ))}
          </select>
        </div>
      </div>
      {problem && <Refusal problem={problem} title="The allocation could not be made" />}
      <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-rule`}>
        <h2 className="fy-h3" id={`${id}-rule`}>
          The rule
        </h2>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input className="fy-input" id={`${id}-name`} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>
        <fieldset className="fy-field">
          <legend className="fy-label">Expense accounts whose cost is shared</legend>
          {expenses.map((a) => (
            <label className="fy-check" key={a.id}>
              <input type="checkbox" checked={form.accounts.includes(a.id)} onChange={(e) => setForm({ ...form, accounts: e.target.checked ? [...form.accounts, a.id] : form.accounts.filter((x) => x !== a.id) })} />
              {a.code} {a.name}
            </label>
          ))}
        </fieldset>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-key`}>
            Key
          </label>
          <select className="fy-select" id={`${id}-key`} value={form.key} onChange={(e) => setForm({ ...form, key: e.target.value as Input["key"] })}>
            {Object.entries(keys).map(([k, label]) => (
              <option key={k} value={k}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-stack fy-stack--2">
          <h3 className="fy-h4">Targets</h3>
          {form.targets.map((t, i) => (
            <div className="fy-row fy-row--wrap gap-3" key={i}>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-t${i}-label`}>
                  Target {i + 1}
                </label>
                <input className="fy-input" id={`${id}-t${i}-label`} value={t.label} onChange={(e) => setTarget(i, { label: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-t${i}-value`}>
                  Department, project or other value
                </label>
                <select className="fy-select" id={`${id}-t${i}-value`} value={t.values[0] ?? ""} onChange={(e) => setTarget(i, { values: [e.target.value] })}>
                  <option value="">Choose a value</option>
                  {(dims.data?.values ?? []).filter((v) => v.active).map((v) => (
                    <option key={v.id} value={v.id}>
                      {dims.data?.dimensions.find((d) => d.id === v.dimension_id)?.name}: {v.name}
                    </option>
                  ))}
                </select>
              </div>
              {form.key === "fixed" && (
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-t${i}-pct`}>
                    Percent
                  </label>
                  <input className="fy-input" id={`${id}-t${i}-pct`} inputMode="decimal" value={t.percent ?? ""} onChange={(e) => setTarget(i, { percent: e.target.value })} />
                </div>
              )}
              {form.key === "usage" && (
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-t${i}-code`}>
                    Code in the usage file
                  </label>
                  <input className="fy-input" id={`${id}-t${i}-code`} value={t.code ?? ""} onChange={(e) => setTarget(i, { code: e.target.value })} />
                </div>
              )}
              <Button variant="ghost" onClick={() => setForm({ ...form, targets: form.targets.filter((_, k) => k !== i) })} disabled={form.targets.length === 1}>
                <Icon of={Trash2} /> Remove target {i + 1}
              </Button>
            </div>
          ))}
          <div>
            <Button variant="secondary" onClick={() => setForm({ ...form, targets: [...form.targets, { label: "", values: [""], percent: "" }] })}>
              <Icon of={Plus} /> Add a target
            </Button>
          </div>
        </div>
        <div className="fy-row gap-3">
          <Button onClick={() => void save()} disabled={busy}>
            <Icon of={Save} /> {openId ? "Save the next version" : "Save the rule"}
          </Button>
          {openId && !form.archived && (
            <Button variant="secondary" onClick={() => void save(true)} disabled={busy}>
              Put the rule out of use
            </Button>
          )}
        </div>
      </section>
      {openId && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-run`}>
          <h2 className="fy-h3" id={`${id}-run`}>
            Run it
          </h2>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-month`}>
              Month
            </label>
            <select className="fy-select" id={`${id}-month`} value={month} onChange={(e) => setMonth(e.target.value)}>
              {months.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          {form.key === "usage" && (
            <div className="fy-stack fy-stack--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-bill`}>
                  The supplier's bill the file explains (its ID)
                </label>
                <input className="fy-input" id={`${id}-bill`} value={usage.bill} onChange={(e) => setUsage({ ...usage, bill: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-total`}>
                  Billed total in the file
                </label>
                <input className="fy-input" id={`${id}-total`} inputMode="decimal" value={usage.total} onChange={(e) => setUsage({ ...usage, total: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-rows`}>
                  Usage rows: code, quantity, billed
                </label>
                <textarea className="fy-textarea" id={`${id}-rows`} rows={5} value={usage.rows} onChange={(e) => setUsage({ ...usage, rows: e.target.value })} />
              </div>
            </div>
          )}
          <div className="fy-row gap-3">
            <Button variant="secondary" onClick={() => void preview()} disabled={busy || months.length === 0}>
              <Icon of={Eye} /> Preview
            </Button>
            <Button onClick={() => void propose()} disabled={busy || months.length === 0}>
              <Icon of={FilePlus} /> Propose the journal
            </Button>
          </div>
        </section>
      )}
      {report && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-report`}>
          <h2 className="fy-h3" id={`${id}-report`}>
            {report.proposed ? "Proposed" : "Preview"}: {report.from} to {report.to}
          </h2>
          {report.splits.map((s) => (
            <table className="fy-table" key={s.account_id}>
              <caption>
                {s.account_code} {s.account_name}: {s.source} with no department or value
              </caption>
              <thead>
                <tr>
                  <th scope="col">Target</th>
                  <th scope="col" className="fy-num">
                    Part
                  </th>
                </tr>
              </thead>
              <tbody>
                {s.parts.map((p) => (
                  <tr key={p.target}>
                    <th scope="row">{p.target}</th>
                    <td className="fy-num">{p.amount}</td>
                  </tr>
                ))}
                <tr>
                  <th scope="row">Unallocated (stays on the account)</th>
                  <td className="fy-num">{s.unallocated}</td>
                </tr>
              </tbody>
            </table>
          ))}
          <p className="fy-small fy-muted">
            {report.lines.length} lines: each part debited with {report.lines.filter((l) => l.values.length > 0).map((l) => valueName(l.values[0]!)).join(", ")}, what was placed credited with none. {report.note}
          </p>
          {report.journal_id && (
            <p>
              The draft journal is made: <Link to="/accounting/journals/$journalId" params={{ journalId: report.journal_id }}>open it</Link>. An approver posts it like any other.
            </p>
          )}
        </section>
      )}
    </div>
  );
}
