import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, CircleCheck, Landmark, Lock, Pencil, Plus, Search } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Account = components["schemas"]["Account"];
type AccountType = Account["type"];

const types: [AccountType, string][] = [
  ["asset", "Assets"],
  ["liability", "Liabilities"],
  ["equity", "Equity"],
  ["revenue", "Revenue"],
  ["expense", "Expenses"],
];
const functions = ["COGS", "R&D", "S&M", "G&A", "Other"] as const;
const indent = ["", "ps-4", "ps-8", "ps-12", "ps-16"]; // a group's accounts sit under it

// Who adds and changes accounts: C on "Chart of accounts, dimensions" (Appendix D). The API decides;
// the page offers the buttons to these roles only.
const creators = ["accountant", "tax_agent"];

async function listAccounts(): Promise<Account[]> {
  const out: Account[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/accounts", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

// ordered puts each account under its group, depth first, by code, and says how deep it sits.
export function ordered(accounts: Account[]): { a: Account; depth: number }[] {
  const byParent = new Map<string, Account[]>();
  const ids = new Set(accounts.map((a) => a.id));
  for (const a of accounts) {
    const p = a.parent_id && ids.has(a.parent_id) ? a.parent_id : "";
    byParent.set(p, [...(byParent.get(p) ?? []), a]);
  }
  const out: { a: Account; depth: number }[] = [];
  const walk = (p: string, depth: number) => {
    for (const a of (byParent.get(p) ?? []).sort((x, y) => x.code.localeCompare(y.code, "en", { numeric: true }))) {
      out.push({ a, depth });
      walk(a.id, depth + 1);
    }
  };
  walk("", 0);
  return out;
}

function Flags({ a }: { a: Account }) {
  return (
    <span className="fy-row fy-row--wrap gap-1">
      {a.role && (
        <span className="fy-tag" title={`System account (${a.role}): its code, type and role never change, and it stays active.`}>
          <Icon of={Lock} />
          System
        </span>
      )}
      {a.group && <span className="fy-tag">Group</span>}
      {a.money_account && (
        <span className="fy-tag">
          <Icon of={Landmark} />
          Money{a.currency ? ` ${a.currency}` : ""}
        </span>
      )}
      {a.control && <span className="fy-tag">Control</span>}
      {a.payroll_cost && <span className="fy-tag">Payroll cost</span>}
      {!a.active && <span className="fy-chip fy-chip--neutral">Inactive</span>}
    </span>
  );
}

interface Draft {
  code: string;
  name: string;
  type: AccountType;
  parent_id: string;
  group: boolean;
  report_group: string;
  cash_flow_line: string;
  function: string;
  default_gst_code: string;
  money_account: boolean;
  currency: string;
  guidance: string;
  active: boolean;
}

const blank: Draft = { code: "", name: "", type: "expense", parent_id: "", group: false, report_group: "", cash_flow_line: "", function: "", default_gst_code: "", money_account: false, currency: "", guidance: "", active: true };

function draftOf(a: Account): Draft {
  return {
    code: a.code,
    name: a.name,
    type: a.type,
    parent_id: a.parent_id ?? "",
    group: a.group,
    report_group: a.report_group ?? "",
    cash_flow_line: a.cash_flow_line ?? "",
    function: a.function ?? "",
    default_gst_code: a.default_gst_code ?? "",
    money_account: a.money_account,
    currency: a.currency ?? "",
    guidance: a.guidance ?? "",
    active: a.active,
  };
}

function AccountForm({ editing, accounts, onClose, onSaved }: { editing: Account | "new" | null; accounts: Account[]; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const isNew = editing === "new";
  const current = editing && editing !== "new" ? editing : null;
  const one = useQuery({
    queryKey: ["account", current?.id],
    enabled: current !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/accounts/{account_id}", { params: { path: { account_id: current?.id ?? "" } } });
      if (r.error) throw r.error;
      return { account: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [errors, setErrors] = useState<Partial<Record<keyof Draft, string>>>({});
  const base = isNew ? blank : one.data ? draftOf(one.data.account) : null;
  const d = draft ?? base;
  const set = (k: keyof Draft, v: string | boolean) => d && setDraft({ ...d, [k]: v });
  const groups = accounts.filter((a) => a.group && d && a.type === d.type && a.id !== current?.id);

  const close = () => {
    setDraft(null);
    setProblem(null);
    setErrors({});
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!d) return;
    const errs: typeof errors = {};
    if (isNew && !/^[A-Za-z0-9.-]{1,20}$/.test(d.code.trim())) errs.code = "Give a code of 1 to 20 letters, digits, dots or hyphens, such as 6115.";
    if (!d.name.trim()) errs.name = "Give the account a name.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const text = (v: string) => v.trim() || undefined;
    if (isNew) {
      const key = newKey();
      const body = {
        code: d.code.trim(),
        name: d.name.trim(),
        type: d.type,
        group: d.group,
        money_account: d.money_account,
        parent_id: text(d.parent_id),
        report_group: text(d.report_group),
        cash_flow_line: text(d.cash_flow_line),
        function: text(d.function),
        default_gst_code: text(d.default_gst_code),
        currency: d.money_account ? text(d.currency) : undefined,
        guidance: text(d.guidance),
      };
      const out = await guarded(confirm, () => api.POST("/v1/accounts", { params: { header: { "Idempotency-Key": key } }, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["accounts"] });
      onSaved(`${d.code.trim()} ${d.name.trim()} added.`);
      return close();
    }
    if (!one.data || !base) return setBusy(false);
    // Only the fields changed go: a field left out keeps its value.
    const changes: Record<string, string | boolean> = {};
    for (const k of ["name", "parent_id", "group", "report_group", "cash_flow_line", "function", "default_gst_code", "guidance", "active", "money_account", "currency"] as const) {
      if (d[k] !== base[k]) changes[k] = typeof d[k] === "string" ? (d[k] as string).trim() : d[k];
    }
    if (Object.keys(changes).length === 0) {
      setBusy(false);
      return close();
    }
    const etag = one.data.etag;
    const out = await guarded(confirm, () => api.PATCH("/v1/accounts/{account_id}", { params: { path: { account_id: one.data.account.id }, header: { "If-Match": etag } }, body: changes }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all(["accounts", "account"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onSaved(`${d.code} ${d.name.trim()} saved.`);
    close();
  };

  const field = (k: keyof Draft, label: string, help?: string, full?: boolean) => (
    <div className={full ? "fy-field fy-field--full" : "fy-field"}>
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input
        className="fy-input"
        id={`${id}-${k}`}
        value={String(d?.[k] ?? "")}
        onChange={(e) => set(k, e.target.value)}
        aria-invalid={errors[k] ? true : undefined}
        aria-describedby={[help && `${id}-${k}-help`, errors[k] && `${id}-${k}-error`].filter(Boolean).join(" ") || undefined}
      />
      {help && (
        <span className="fy-help" id={`${id}-${k}-help`}>
          {help}
        </span>
      )}
      {errors[k] && (
        <span className="fy-error" id={`${id}-${k}-error`}>
          {errors[k]}
        </span>
      )}
    </div>
  );

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Chart of accounts"
      title={isNew ? "New account" : current ? `${current.code} ${current.name}` : "Account"}
      description={isNew ? "Its code and type never change once it is added." : current?.role ? "A system account: its code, type and role never change, and it stays active." : "Its code and type never change."}
    >
      {one.isError && <Refusal problem={problemOf(one.error)} title="The account could not be read" />}
      {problem && <Refusal problem={problem} title={isNew ? "The account was not added" : "The account was not saved"} />}
      {d && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-form fy-form--2">
            {isNew ? field("code", "Code", "1 to 20 letters, digits, dots or hyphens; it never changes.") : null}
            {field("name", "Name")}
            {isNew && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-type`}>
                  Type
                </label>
                <select className="fy-select" id={`${id}-type`} value={d.type} onChange={(e) => setDraft({ ...d, type: e.target.value as AccountType, parent_id: "" })}>
                  {types.map(([t, label]) => (
                    <option key={t} value={t}>
                      {label.replace(/s$/, "").replace(/ie$/, "y")}
                    </option>
                  ))}
                </select>
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-parent`}>
                Under group
              </label>
              <select className="fy-select" id={`${id}-parent`} value={d.parent_id} onChange={(e) => set("parent_id", e.target.value)}>
                <option value="">None: at the top</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.code} {g.name}
                  </option>
                ))}
              </select>
            </div>
            {field("report_group", "Report group", "Where the statements show it, such as Premises & office.")}
            {(d.type === "asset" || d.type === "liability" || d.type === "equity") && field("cash_flow_line", "Cash-flow line")}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-function`}>
                Function
              </label>
              <select className="fy-select" id={`${id}-function`} value={d.function} onChange={(e) => set("function", e.target.value)}>
                <option value="">None</option>
                {functions.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>
            </div>
            {field("default_gst_code", "Default GST code", "The GST code a line on it starts with, such as TX.")}
            {d.money_account && field("currency", "Currency", "A bank account's currency, such as SGD.")}
            {field("guidance", "Guidance", "Shown when someone picks the account.", true)}
          </div>
          <div className="fy-row fy-row--wrap gap-4">
            <label className="fy-row gap-2">
              <input type="checkbox" checked={d.group} onChange={(e) => set("group", e.target.checked)} />
              A group, with accounts under it (posting goes to the accounts, not the group)
            </label>
            <label className="fy-row gap-2">
              <input type="checkbox" checked={d.money_account} onChange={(e) => set("money_account", e.target.checked)} />
              A money account (bank, card or cash)
            </label>
            {!isNew && !current?.role && (
              <label className="fy-row gap-2">
                <input type="checkbox" checked={d.active} onChange={(e) => set("active", e.target.checked)} />
                Active: takes new postings
              </label>
            )}
          </div>
          {current && (current.control || current.payroll_cost) && <p className="fy-small fy-secondary">The control and payroll-cost flags change only with an approval, which arrives with the approval engine.</p>}
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {isNew ? "Add the account" : "Save the account"}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

function LoadStandard({ open, onClose, onLoaded }: { open: boolean; onClose: () => void; onLoaded: (n: number) => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("chart.load-standard");
  const run = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/accounts:load-standard-chart", { params: { header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["accounts"] });
    onLoaded(out.data.accounts);
    onClose();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Load the standard Singapore start-up chart?"
      description="86 accounts with their report groups, cash-flow lines, functions, GST codes, flags and guidance. You can rename, regroup and add accounts afterwards. It loads only into a chart with no accounts."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={run} disabled={busy}>
            <Icon of={BookOpen} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The chart was not loaded" />}
    </Dialog>
  );
}

// ChartOfAccounts is Accounting › Chart of accounts (docs/design/screens ChartOfAccounts; FR-COA-01…05):
// every account by type, groups with their accounts under them, each with its report group, function,
// default GST code and flags; system accounts locked. An Accountant or Tax agent adds and changes
// accounts; an empty chart offers the standard Singapore start-up chart.
export function ChartOfAccounts() {
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const [find, setFind] = useState("");
  const [inactive, setInactive] = useState(false);
  const [editing, setEditing] = useState<Account | "new" | null>(null);
  const [loading, setLoading] = useState(false);
  const [status, setStatus] = useState("");
  const findId = useId();
  const may = (session.roles ?? []).some((r) => creators.includes(r));
  const add = helpText("chart.add");
  const load = helpText("chart.load-standard");
  const all = q.data ?? [];
  const needle = find.trim().toLowerCase();
  const rows = ordered(all).filter(({ a }) => (inactive || a.active) && (!needle || a.code.toLowerCase().includes(needle) || a.name.toLowerCase().includes(needle)));

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Accounting</span>
          <h1 className="fy-h1">Chart of accounts</h1>
          <p className="fy-secondary">{q.data ? `${all.length} accounts, ${all.filter((a) => a.active).length} active.` : "The accounts the books post to."}</p>
        </div>
        {may && all.length > 0 && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setEditing("new")} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="chart-of-accounts" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The chart could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the chart…</p>}
      {q.data && all.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={BookOpen} size={24} />
          <h2 className="fy-h3">No accounts yet</h2>
          <p className="fy-secondary">Start from the standard Singapore start-up chart: 86 accounts set up for a company like yours, which you can rename, regroup and add to.</p>
          {may ? (
            <div className="fy-row fy-row--wrap justify-center gap-2">
              <Button variant="primary" onClick={() => setLoading(true)} title={load.tooltip}>
                <Icon of={BookOpen} />
                {load.label}
              </Button>
              <Button onClick={() => setEditing("new")}>
                <Icon of={Plus} />
                {add.label}
              </Button>
            </div>
          ) : (
            <p className="fy-small fy-secondary">An Accountant or a Tax agent sets up the chart.</p>
          )}
        </div>
      )}
      {all.length > 0 && (
        <>
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-inputgroup max-w-80 flex-1">
              <span className="fy-inputgroup__addon">
                <Icon of={Search} />
              </span>
              <label className="fy-sr" htmlFor={findId}>
                Find an account
              </label>
              <input className="fy-input" id={findId} type="search" placeholder="Code or name" value={find} onChange={(e) => setFind(e.target.value)} />
            </div>
            <label className="fy-row gap-2 fy-small">
              <input type="checkbox" checked={inactive} onChange={(e) => setInactive(e.target.checked)} />
              Show inactive accounts
            </label>
          </div>
          {types.map(([t, label]) => {
            const mine = rows.filter(({ a }) => a.type === t);
            if (mine.length === 0) return null;
            const tid = `type-${t}`;
            return (
              <section key={t} className="fy-card" aria-labelledby={tid}>
                <div className="fy-card__head">
                  <h2 className="fy-h3" id={tid}>
                    {label}
                  </h2>
                  <span className="fy-badge fy-badge--quiet">{mine.length}</span>
                </div>
                <div className="fy-tablewrap">
                  <table className="fy-table fy-table--compact table-fixed" aria-labelledby={tid}>
                    {/* The same widths in every type's table, so the columns line up down the page. */}
                    <colgroup>
                      <col className="w-16 medium:w-24" />
                      <col />
                      <col className="fy-from-medium w-56" />
                      <col className="fy-from-expanded w-24" />
                      <col className="fy-from-expanded w-20" />
                      <col className="w-20 medium:w-44" />
                    </colgroup>
                    <thead>
                      <tr>
                        <th scope="col">Code</th>
                        <th scope="col">Name</th>
                        <th scope="col" className="fy-from-medium">
                          Report group
                        </th>
                        <th scope="col" className="fy-from-expanded">
                          Function
                        </th>
                        <th scope="col" className="fy-from-expanded">
                          GST
                        </th>
                        <th scope="col">
                          <span className="fy-sr">Flags and actions</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {mine.map(({ a, depth }) => (
                        <tr key={a.id}>
                          <td className="fy-mono">{a.code}</td>
                          <td>
                            <span className={`${a.group ? "fy-strong " : ""}${indent[Math.min(depth, indent.length - 1)]}`} title={a.guidance}>
                              {a.name}
                            </span>
                          </td>
                          <td className="fy-from-medium fy-secondary">{a.report_group}</td>
                          <td className="fy-from-expanded fy-secondary">{a.function}</td>
                          <td className="fy-from-expanded fy-mono">{a.default_gst_code}</td>
                          <td>
                            <span className="fy-row fy-row--wrap justify-end gap-2">
                              <Flags a={a} />
                              {may && (
                                <Button variant="ghost" size="sm" onClick={() => setEditing(a)} aria-label={`Change ${a.code} ${a.name}`}>
                                  <Icon of={Pencil} />
                                </Button>
                              )}
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            );
          })}
          {rows.length === 0 && (
            <p className="fy-secondary fy-row gap-2">
              <Icon of={CircleCheck} />
              No account matches "{find}".
            </p>
          )}
        </>
      )}
      <AccountForm editing={editing} accounts={all} onClose={() => setEditing(null)} onSaved={setStatus} />
      <LoadStandard open={loading} onClose={() => setLoading(false)} onLoaded={(n) => setStatus(`The standard chart is loaded: ${n} accounts.`)} />
    </div>
  );
}
