import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Copy, FlaskConical, Lock, PencilLine, Plus, Save, Send, Trash2, Upload } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, month } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, listPeriods, postable, type Account } from "../accounting/data";
import { DimensionFields, useDimensions, valueCodes } from "../accounting/lines";
import { PlanningTabs } from "./tabs";

type View = components["schemas"]["BudgetView"];
type Line = { key: string; account_id: string; dimension_value_ids: string[]; amounts: string[] };

const statusWords: Record<View["status"], { label: string; icon: typeof Lock; tone: string }> = {
  draft: { label: "Draft", icon: PencilLine, tone: "fy-chip--neutral" },
  awaiting_approval: { label: "Awaiting approval", icon: Send, tone: "fy-chip--warning" },
  recorded: { label: "Recorded, locked", icon: Lock, tone: "fy-chip--good" },
  superseded: { label: "Superseded", icon: Lock, tone: "fy-chip--neutral" },
};

let keys = 0;
const lineKey = () => `budget-line-${++keys}`;

// years are the financial years the books have, each by its first day, from the periods (and the next one to open).
function useYears(): { first: string; label: string }[] {
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods, staleTime: 30_000 });
  return useMemo(() => {
    const byYear = new Map<number, string>();
    for (const p of periods.data?.items ?? []) {
      if (p.financial_year === undefined) continue;
      const was = byYear.get(p.financial_year);
      if (!was || p.starts_on < was) byYear.set(p.financial_year, p.starts_on);
    }
    const out = [...byYear.values()].sort().map((first) => ({ first, label: `The year from ${date(first)}` }));
    const next = periods.data?.next_year?.starts_on;
    if (next && !out.some((y) => y.first === next)) out.push({ first: next, label: `The year from ${date(next)}` });
    return out;
  }, [periods.data]);
}

// Budgets is Planning › Budgets (docs/design/screens Budgets; FR-PLN-01, FR-UX-07; p1-planning design D6, D12): a
// financial year's budget in versions, by income or expense account and dimension values, a month a column. A draft is
// edited here and saved; another person who holds A approves it, which records and locks it, and the year's budget is
// its latest recorded version. A new version starts empty, is copied from another with growth, or is imported from
// XLSX. The totals are the server's. Nothing here posts. Expert only.
export function Budgets() {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [selected, setSelected] = useState<string | null>(null);
  const [lines, setLines] = useState<Line[]>([]);
  const [name, setName] = useState("");
  const [dialog, setDialog] = useState<"new" | "copy" | "import" | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [message, setMessage] = useState("");
  const dims = useDimensions();
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts, staleTime: 30_000 });
  const choices = useMemo(() => postable(accounts.data ?? []).filter((a) => a.type === "revenue" || a.type === "expense"), [accounts.data]);
  const byId = useMemo(() => new Map((accounts.data ?? []).map((a) => [a.id, a])), [accounts.data]);
  const list = useQuery({
    queryKey: ["budgets"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/budgets", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data;
    },
  });
  const one = useQuery({
    queryKey: ["budget", selected],
    enabled: selected !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/budgets/{budget_id}", { params: { path: { budget_id: selected! } } });
      if (r.error) throw r.error;
      return { view: r.data as View, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  useEffect(() => {
    if (!selected && list.data?.items[0]) setSelected(list.data.items[0].id);
  }, [list.data, selected]);
  const v = one.data?.view;
  useEffect(() => {
    if (!v) return;
    setName(v.budget.name);
    setLines(v.budget.lines.map((l) => ({ key: lineKey(), account_id: l.account_id, dimension_value_ids: l.dimension_value_ids ?? [], amounts: [...(l.amounts ?? [])] })));
  }, [v]);
  const draft = v?.status === "draft";
  const months = v?.months ?? [];
  const after = async (newId: string, words: string) => {
    setSelected(newId);
    setMessage(words);
    await queries.invalidateQueries({ queryKey: ["budgets"] });
    await queries.invalidateQueries({ queryKey: ["budget", newId] });
  };
  const save = async () => {
    if (!v || !one.data) return;
    setBusy(true);
    setProblem(null);
    const r = await api.PUT("/v1/budgets/{budget_id}", {
      params: { path: { budget_id: v.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } },
      body: {
        name: name.trim(),
        kind: v.budget.kind,
        financial_year: v.budget.financial_year,
        revises: v.budget.revises,
        lines: lines
          .filter((l) => l.account_id)
          .map((l) => ({ account_id: l.account_id, dimension_value_ids: l.dimension_value_ids, amounts: l.amounts.map((a) => a.trim() || "0") })),
      },
    });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    await after(r.data.id, `Saved ${r.data.budget.name}.`);
  };
  const ask = async () => {
    if (!v || !one.data) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const r = await guarded(confirm, () =>
      api.POST("/v1/budgets/{budget_id}/approval", { params: { path: { budget_id: v.id }, header: { "If-Match": one.data!.etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    await after(v.id, "Asked for approval: another person who approves budgets decides it in their Inbox.");
  };
  const status = v ? statusWords[v.status] : null;
  const sandbox = helpText("planning.sandbox");
  const action = (key: string) => helpText(key);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Planning</span>
          <h1 className="fy-h1">{v ? v.budget.name : "Budgets"}</h1>
          <div className="fy-row fy-row--wrap gap-2">
            {status && (
              <span className={`fy-chip ${status.tone}`}>
                <Icon of={status.icon} />
                {status.label} · version {v!.version}
              </span>
            )}
            {v && <span className="fy-small fy-muted">The year from {date(v.budget.financial_year)}, {v.budget.kind}</span>}
            <span className="fy-badge fy-badge--quiet" title={sandbox.tooltip}>
              <Icon of={FlaskConical} />
              {sandbox.label}
            </span>
          </div>
        </div>
        <div className="fy-pagehead__actions">
          <Button variant="ghost" onClick={() => setDialog("new")} title={action("budget.new").tooltip}>
            <Icon of={Plus} />
            {action("budget.new").label}
          </Button>
          {v && (
            <Button variant="ghost" onClick={() => setDialog("copy")} title={action("budget.copy").tooltip}>
              <Icon of={Copy} />
              {action("budget.copy").label}
            </Button>
          )}
          <Button variant="secondary" onClick={() => setDialog("import")} title={action("budget.import").tooltip}>
            <Icon of={Upload} />
            {action("budget.import").label}
          </Button>
          {draft && (
            <>
              <Button variant="secondary" onClick={() => void save()} disabled={busy} title={action("budget.save").tooltip}>
                <Icon of={Save} />
                {action("budget.save").label}
              </Button>
              <Button variant="primary" onClick={() => void ask()} disabled={busy} title={action("budget.ask-approval").tooltip}>
                <Icon of={Send} />
                {action("budget.ask-approval").label}
              </Button>
            </>
          )}
        </div>
      </div>
      <PlanningTabs current="/planning/budgets" />
      {(list.data?.items.length ?? 0) > 0 && (
        <div className="fy-field max-w-md">
          <label className="fy-label" htmlFor={`${id}-version`}>
            Version
          </label>
          <select className="fy-select" id={`${id}-version`} value={selected ?? ""} onChange={(e) => setSelected(e.target.value)}>
            {list.data!.items.map((b) => (
              <option key={b.id} value={b.id}>
                {b.budget.name}: {statusWords[b.status].label} ({date(b.budget.financial_year)})
              </option>
            ))}
          </select>
        </div>
      )}
      {message && (
        <p className="fy-small" role="status">
          {message}
        </p>
      )}
      {v?.approval_id && (
        <p className="fy-small">
          <Link to="/approvals/$approvalId" params={{ approvalId: v.approval_id }}>
            See the approval request
          </Link>
        </p>
      )}
      {problem && <Refusal problem={problem} title="The budget was not saved" />}
      {list.data && list.data.items.length === 0 && (
        <p className="fy-secondary">No budget yet. Start one with New budget, or import it from a spreadsheet.</p>
      )}

      {v && (
        <section className="fy-card" aria-labelledby={`${id}-grid`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-grid`}>
              Lines
            </h2>
            {draft && (
              <div className="fy-field">
                <label className="fy-label fy-sr" htmlFor={`${id}-name`}>
                  Name
                </label>
                <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} aria-label="Name" />
              </div>
            )}
          </div>
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--compact fy-statement">
              <caption className="fy-sr">
                {v.budget.name}, by account and month, in {v.currency}
              </caption>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  {months.map((m) => (
                    <th key={m} scope="col" className="fy-num">
                      {month(m)}
                    </th>
                  ))}
                  <th scope="col" className="fy-num">
                    Year
                  </th>
                  {draft && <th scope="col">Remove</th>}
                </tr>
              </thead>
              <tbody>
                {lines.map((l, i) => (
                  <tr key={l.key}>
                    <th scope="row">
                      {draft ? (
                        <BudgetLineHead id={`${id}-${l.key}`} line={l} n={i + 1} choices={choices} byId={byId} onChange={(x) => setLines(lines.map((y) => (y.key === l.key ? x : y)))} />
                      ) : (
                        <>
                          <span className="fy-acct">{byId.get(l.account_id)?.code}</span>
                          {byId.get(l.account_id)?.name ?? "An account not in the chart"}
                          {l.dimension_value_ids.length > 0 && (
                            <span className="fy-small fy-muted block">{valueCodes(l.dimension_value_ids, dims.dimensions, dims.byId)}</span>
                          )}
                        </>
                      )}
                    </th>
                    {months.map((m, k) => (
                      <td key={m} className="fy-num">
                        {draft ? (
                          <input
                            className="fy-input fy-num"
                            inputMode="decimal"
                            aria-label={`Line ${i + 1}, ${month(m)}`}
                            value={l.amounts[k] ?? ""}
                            onChange={(e) => {
                              const amounts = [...l.amounts];
                              amounts[k] = e.target.value;
                              setLines(lines.map((y) => (y.key === l.key ? { ...l, amounts } : y)));
                            }}
                          />
                        ) : (
                          formatAmount(l.amounts[k] ?? "0")
                        )}
                      </td>
                    ))}
                    <td className="fy-num fy-strong">{v.line_totals[i] ? formatAmount(v.line_totals[i]!) : "—"}</td>
                    {draft && (
                      <td>
                        <Button variant="ghost" size="sm" onClick={() => setLines(lines.filter((y) => y.key !== l.key))} aria-label={`Remove line ${i + 1}`}>
                          <Icon of={Trash2} />
                        </Button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
              <tfoot>
                {(
                  [
                    ["Income", v.totals.income, v.totals.income_year],
                    ["Costs", v.totals.costs, v.totals.costs_year],
                    ["Net", v.totals.net, v.totals.net_year],
                  ] as const
                ).map(([label, row, year]) => (
                  <tr key={label}>
                    <th scope="row">{label}</th>
                    {months.map((m, k) => (
                      <td key={m} className="fy-num">
                        {formatAmount(row[k] ?? "0")}
                      </td>
                    ))}
                    <td className="fy-num fy-strong">{formatAmount(year)}</td>
                    {draft && <td />}
                  </tr>
                ))}
              </tfoot>
            </table>
          </div>
          {draft && (
            <div className="fy-card__body fy-stack fy-stack--2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setLines([...lines, { key: lineKey(), account_id: "", dimension_value_ids: [], amounts: months.map(() => "") }])}
                title={action("budget.add-line").tooltip}
              >
                <Icon of={Plus} />
                {action("budget.add-line").label}
              </Button>
              <span className="fy-small fy-muted">The totals are worked out when you save. Amounts are signed as the P&L shows them: income and costs above zero.</span>
            </div>
          )}
        </section>
      )}

      {dialog === "new" && <NewBudget onClose={() => setDialog(null)} onDone={(x) => void after(x, "Started a new budget: add its lines and save.")} />}
      {dialog === "copy" && v && <CopyBudget from={v} onClose={() => setDialog(null)} onDone={(x) => void after(x, "Copied into a new draft.")} />}
      {dialog === "import" && <ImportBudget onClose={() => setDialog(null)} onDone={(x) => void after(x, "Imported into a new draft.")} />}
    </div>
  );
}

// BudgetLineHead is a draft line's account and dimension values.
function BudgetLineHead({ id, line, n, choices, byId, onChange }: { id: string; line: Line; n: number; choices: Account[]; byId: Map<string, Account>; onChange: (l: Line) => void }) {
  return (
    <div className="fy-stack fy-stack--2 min-w-[14rem]">
      <label className="fy-sr" htmlFor={`${id}-account`}>
        Line {n}: account
      </label>
      <select className="fy-select" id={`${id}-account`} value={line.account_id} onChange={(e) => onChange({ ...line, account_id: e.target.value })}>
        <option value="">Choose an account</option>
        {choices.map((a) => (
          <option key={a.id} value={a.id}>
            {a.code} {a.name}
          </option>
        ))}
      </select>
      <DimensionFields ids={line.dimension_value_ids} keyPrefix={id} n={n} account={byId.get(line.account_id)} set={(ids) => onChange({ ...line, dimension_value_ids: ids })} className="fy-field" />
    </div>
  );
}

// YearField chooses a financial year by its first day.
function YearField({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  const years = useYears();
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        Financial year
      </label>
      <select className="fy-select" id={id} value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">Choose a year</option>
        {years.map((y) => (
          <option key={y.first} value={y.first}>
            {y.label}
          </option>
        ))}
      </select>
    </div>
  );
}

// NewBudget starts an empty original budget for a year, its lines added after.
function NewBudget({ onClose, onDone }: { onClose: () => void; onDone: (id: string) => void }) {
  const id = useId();
  const [name, setName] = useState("");
  const [year, setYear] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const send = async () => {
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/budgets", { params: { header: { "Idempotency-Key": newKey() } }, body: { name: name.trim(), kind: "original", financial_year: year, lines: [] } });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    onDone(r.data.id);
    onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="New budget"
      description="An original budget for a financial year, its lines added after."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            Start it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The budget was not started" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-name`}>
          Name
        </label>
        <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
      </div>
      <YearField id={`${id}-year`} value={year} onChange={setYear} />
    </Dialog>
  );
}

// CopyBudget copies a version into a new draft with growth: for the same year as a revised version, or another year.
function CopyBudget({ from, onClose, onDone }: { from: View; onClose: () => void; onDone: (id: string) => void }) {
  const id = useId();
  const [name, setName] = useState(`${from.budget.name} (copy)`);
  const [kind, setKind] = useState<"original" | "revised">("original");
  const [year, setYear] = useState(from.budget.financial_year);
  const [growth, setGrowth] = useState("0");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const send = async () => {
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/budgets:copy", {
      params: { header: { "Idempotency-Key": newKey() } },
      body: { from: from.id, name: name.trim(), kind, financial_year: year, growth: growth.trim() },
    });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    onDone(r.data.id);
    onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Copy ${from.budget.name}`}
      description="Each amount × (1 + growth), rounded once to the cent. A revised version revises a recorded budget of the same year."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            Copy it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The budget was not copied" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-name`}>
          Name
        </label>
        <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-kind`}>
          Kind
        </label>
        <select className="fy-select" id={`${id}-kind`} value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
          <option value="original">Original: another year's first budget</option>
          <option value="revised">Revised: a new version of this year's</option>
        </select>
      </div>
      <YearField id={`${id}-year`} value={year} onChange={setYear} />
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-growth`}>
          Growth
        </label>
        <input className="fy-input fy-num" id={`${id}-growth`} inputMode="decimal" value={growth} onChange={(e) => setGrowth(e.target.value)} aria-describedby={`${id}-growth-hint`} />
        <span className="fy-help" id={`${id}-growth-hint`}>
          A fraction: 0.05 for 5% more, -0.1 for 10% less
        </span>
      </div>
    </Dialog>
  );
}

// ImportBudget reads a budget from an XLSX workbook into a new draft; every problem comes back with its row and column.
function ImportBudget({ onClose, onDone }: { onClose: () => void; onDone: (id: string) => void }) {
  const id = useId();
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [year, setYear] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const send = async () => {
    if (!file) return;
    setBusy(true);
    setProblem(null);
    // The generated body names the form's fields; the request sends them as multipart/form-data with the file itself.
    const body = { file: file.name, name: name.trim(), kind: "original" as const, financial_year: year };
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", file, file.name);
      form.append("name", name.trim());
      form.append("kind", "original");
      form.append("financial_year", year);
      return form;
    };
    const r = await api.POST("/v1/budgets:import", { params: { header: { "Idempotency-Key": newKey() } }, body, bodySerializer });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    onDone(r.data.id);
    onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Import a budget"
      description="The first sheet of an .xlsx workbook: a header row of Account, a column per dimension by its name and a column per month (YYYY-MM), then a row per line."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy || !file}>
            Import it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The budget was not imported" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-file`}>
          Workbook
        </label>
        <input className="fy-input" id={`${id}-file`} type="file" accept=".xlsx" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-name`}>
          Name
        </label>
        <input className="fy-input" id={`${id}-name`} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
      </div>
      <YearField id={`${id}-year`} value={year} onChange={setYear} />
    </Dialog>
  );
}
