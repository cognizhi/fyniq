import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, CircleX, Copy, Eye, FileUp, Plus, TriangleAlert, Upload } from "lucide-react";
import { useId, useRef, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { makers } from "./BankAccounts";

type Format = "csv" | "xlsx" | "ofx" | "camt053" | "mt940" | "mt940_sap";
type Result = components["schemas"]["BankImport"];
type Spec = components["schemas"]["StatementMappingSpec"];

// formatOf guesses a file's format from its name; a person can change it. A CSV or XLSX needs its account and mapping.
export function formatOf(name: string): Format {
  const ext = name.toLowerCase().split(".").pop() ?? "";
  if (ext === "xlsx") return "xlsx";
  if (ext === "ofx" || ext === "qfx") return "ofx";
  if (ext === "xml") return "camt053";
  if (ext === "sta" || ext === "940" || ext === "mt940") return "mt940";
  return "csv";
}

export const tabular = (f: Format) => f === "csv" || f === "xlsx";

interface Item {
  key: string;
  file: File;
  format: Format;
  account: string;
  mapping: string;
  preview?: Result;
  result?: Result;
  problem?: ApiProblem;
}

// The date layouts a mapping offers, in the words people use, with the Go layout the reader takes.
export const layouts: [string, string][] = [
  ["DD/MM/YYYY", "02/01/2006"],
  ["D/M/YYYY", "2/1/2006"],
  ["YYYY-MM-DD", "2006-01-02"],
  ["DD-MM-YYYY", "02-01-2006"],
  ["DD MMM YYYY", "02 Jan 2006"],
  ["MM/DD/YYYY", "01/02/2006"],
];

// StatementImport is Banking › Import statements (docs/design/screens StatementImport; FR-BNK-02, FR-BNK-04, FR-BNK-16,
// FR-DOC-08; p1-bank-import-reconcile task 10.2): several files at once, each with its account, format and mapping; a
// preview of what each adds, drops as held already, keeps as a possible duplicate and whether its chain continues; then
// the import, with a result per file. Each file is checked and scanned by the server before anything is read.
export function StatementImport() {
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const search = useSearch({ strict: false }) as { account?: string };
  const queries = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const id = useId();
  const [items, setItems] = useState<Item[]>([]);
  const [busy, setBusy] = useState(false);
  const [mapping, setMapping] = useState(false);
  const accounts = useQuery({
    queryKey: ["money-accounts", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return (data.items ?? []).filter((a) => !a.closed_on);
    },
  });
  const mappings = useQuery({
    queryKey: ["statement-mappings"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/statement-mappings", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const h = helpText("banking.import");

  const add = (files: FileList | null) => {
    if (!files) return;
    const more = [...files].map((file) => ({ key: newKey(), file, format: formatOf(file.name), account: search.account ?? "", mapping: "" }));
    setItems([...items, ...more]);
    if (input.current) input.current.value = "";
  };
  const change = (key: string, patch: Partial<Item>) => setItems((all) => all.map((i) => (i.key === key ? { ...i, ...patch, preview: undefined, problem: undefined } : i)));
  const missing = (i: Item) => (tabular(i.format) ? (!i.account ? "Choose its account." : !i.mapping ? "Choose its column mapping." : "") : "");
  const send = async (i: Item, preview: boolean) => {
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", i.file, i.file.name);
      form.append("format", i.format);
      if (i.account) form.append("money_account_id", i.account);
      if (tabular(i.format) && i.mapping) form.append("mapping_id", i.mapping);
      return form;
    };
    const body = { file: i.file.name, format: i.format, money_account_id: i.account || undefined, mapping_id: tabular(i.format) ? i.mapping || undefined : undefined };
    const header: { "Idempotency-Key": string; Prefer?: string } = { "Idempotency-Key": preview ? newKey() : i.key };
    if (preview) header.Prefer = "return=preview";
    return api.POST("/v1/bank-imports", { params: { header }, body, bodySerializer });
  };
  const run = async (preview: boolean) => {
    setBusy(true);
    const next: Item[] = [];
    for (const i of items) {
      if (i.result || missing(i)) {
        next.push(i);
        continue;
      }
      const out = await send(i, preview);
      next.push(out.data ? (preview ? { ...i, preview: out.data, problem: undefined } : { ...i, result: out.data, problem: undefined }) : { ...i, problem: problemOf(out.error) });
    }
    setItems(next);
    setBusy(false);
    if (!preview) for (const key of [["money-accounts"], ["bank-lines"], ["bank-statements"], ["cash-position"]]) void queries.invalidateQueries({ queryKey: key });
  };
  const ready = items.some((i) => !i.result && !missing(i));

  if (!may)
    return (
      <div className="fy-page fy-enter">
        <h1 className="fy-h1">Import statements</h1>
        <p className="fy-secondary">A Bookkeeper or an Accountant imports statements.</p>
      </div>
    );
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/banking">Banking</Link>
        <Icon of={ChevronRight} />
        <span>Import statements</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Import statements</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        <div className="fy-pagehead__actions">
          <button type="button" className="fy-btn fy-btn--ghost" onClick={() => setMapping(true)}>
            <Icon of={Plus} />
            New column mapping
          </button>
        </div>
      </div>
      <ReviewBanner screen="statement-import" />
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-files`}>
          Statement files
        </label>
        <input ref={input} className="fy-input" id={`${id}-files`} type="file" multiple accept=".csv,.xlsx,.ofx,.qfx,.xml,.sta,.940,.mt940,.txt" onChange={(e) => add(e.target.files)} />
        <span className="fy-small fy-muted">CSV, XLSX, OFX or QFX, camt.053 or MT940, up to 10 MB each. Each file is checked and scanned for malware first.</span>
      </div>
      {accounts.isError && <Refusal problem={problemOf(accounts.error)} title="The money accounts could not be listed" />}
      {items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={FileUp} size={24} />
          <h2 className="fy-h3">Choose the bank's statement files</h2>
          <p className="fy-secondary">Several at once, for one account or several. Nothing is added until you import after the preview.</p>
        </div>
      )}
      {items.map((i) => (
        <FileCard
          key={i.key}
          item={i}
          accounts={accounts.data ?? []}
          mappings={mappings.data ?? []}
          missing={missing(i)}
          onChange={(patch) => change(i.key, patch)}
          onRemove={() => setItems(items.filter((x) => x.key !== i.key))}
        />
      ))}
      {items.length > 0 && (
        <div className="fy-row fy-row--wrap gap-2">
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void run(true)} disabled={busy || !ready}>
            <Icon of={Eye} />
            Preview
          </button>
          <button type="button" className="fy-btn fy-btn--primary" onClick={() => void run(false)} disabled={busy || !ready}>
            <Icon of={Upload} />
            Import
          </button>
        </div>
      )}
      {mapping && (
        <MappingDialog
          onClose={() => setMapping(false)}
          onDone={() => {
            setMapping(false);
            void queries.invalidateQueries({ queryKey: ["statement-mappings"] });
          }}
        />
      )}
    </div>
  );
}

function FileCard({
  item: i,
  accounts,
  mappings,
  missing,
  onChange,
  onRemove,
}: {
  item: Item;
  accounts: components["schemas"]["MoneyAccount"][];
  mappings: components["schemas"]["StatementMapping"][];
  missing: string;
  onChange: (patch: Partial<Item>) => void;
  onRemove: () => void;
}) {
  const id = useId();
  const shown = i.result ?? i.preview;
  return (
    <section className="fy-card" aria-labelledby={`${id}-name`}>
      <div className="fy-card__head">
        <h2 className="fy-h3 fy-mono fy-grow" id={`${id}-name`}>
          {i.file.name}
        </h2>
        {i.result ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Imported
          </span>
        ) : (
          <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={onRemove}>
            Remove
          </button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {!i.result && (
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-format`}>
                Format
              </label>
              <select className="fy-select" id={`${id}-format`} value={i.format} onChange={(e) => onChange({ format: e.target.value as Format })}>
                <option value="csv">CSV</option>
                <option value="xlsx">Excel (XLSX)</option>
                <option value="ofx">OFX or QFX</option>
                <option value="camt053">camt.053</option>
                <option value="mt940">MT940</option>
                <option value="mt940_sap">MT940 (SAP)</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-account`}>
                Account
              </label>
              <select className="fy-select" id={`${id}-account`} value={i.account} onChange={(e) => onChange({ account: e.target.value })}>
                <option value="">{tabular(i.format) ? "Choose an account" : "The one the file names"}</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.currency})
                  </option>
                ))}
              </select>
            </div>
            {tabular(i.format) && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-mapping`}>
                  Column mapping
                </label>
                <select className="fy-select" id={`${id}-mapping`} value={i.mapping} onChange={(e) => onChange({ mapping: e.target.value })}>
                  <option value="">Choose a mapping</option>
                  {mappings
                    .filter((m) => m.format === i.format)
                    .map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name}
                      </option>
                    ))}
                </select>
              </div>
            )}
          </div>
        )}
        {missing && !i.result && (
          <p className="fy-small fy-row gap-2">
            <Icon of={TriangleAlert} />
            {missing}
          </p>
        )}
        {i.problem && <Refusal problem={i.problem} title={`${i.file.name} was not read`} />}
        {shown && <Outcome result={shown} imported={Boolean(i.result)} />}
      </div>
    </section>
  );
}

// Outcome is what a file's statements add, drop and keep for a person, and whether each chain continues (FR-BNK-04,
// FR-BNK-16); once imported, also what the system then reconciled by itself (FR-BNK-14).
function Outcome({ result, imported }: { result: Result; imported: boolean }) {
  return (
    <div className="fy-stack fy-stack--3">
      {result.statements.map((s, n) => (
        <div key={`${s.money_account_id}-${n}`} className="fy-stack fy-stack--2">
          <p className="fy-strong">
            {date(s.from)} to {date(s.to)}
            {s.opening && s.closing && (
              <span className="fy-secondary fy-num">
                {" "}
                · {formatAmount(s.opening.amount)} to {formatAmount(s.closing.amount)} {s.closing.currency}
              </span>
            )}
          </p>
          <ul className="fy-row fy-row--wrap gap-2" aria-label="What it does">
            <li className="fy-chip fy-chip--info">
              <Icon of={Plus} />
              {s.added} {imported ? "added" : "to add"}
            </li>
            <li className="fy-chip fy-chip--neutral">
              <Icon of={CircleX} />
              {s.dropped} held already, dropped
            </li>
            {s.flagged > 0 && (
              <li className="fy-chip fy-chip--warning">
                <Icon of={Copy} />
                {s.flagged} possible {s.flagged === 1 ? "duplicate" : "duplicates"}, kept for you
              </li>
            )}
            {s.gap ? (
              <li className="fy-chip fy-chip--warning">
                <Icon of={TriangleAlert} />
                Gap: the opening is not the previous closing
              </li>
            ) : (
              <li className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Continues the chain
              </li>
            )}
          </ul>
        </div>
      ))}
      {imported && result.auto_reconciled && (
        <p className="fy-small">
          {result.auto_reconciled.reconciled === 1 ? "1 line was" : `${result.auto_reconciled.reconciled} lines were`} reconciled by Fyniq itself; the rest wait in
          the queue.
        </p>
      )}
    </div>
  );
}

// MappingDialog saves a column mapping for a bank's CSV or XLSX export, for next time: the date column and its layout,
// a signed amount or money in and out columns, and the optional reference, counterparty, description and balance.
export function MappingDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [f, setF] = useState({ name: "", format: "csv" as "csv" | "xlsx", layout: "02/01/2006", date: "", signed: true, amount: "", in: "", out: "", reference: "", counterparty: "", description: "", balance: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value });
  const send = async () => {
    const e: Record<string, string> = {};
    if (!f.name.trim()) e.name = "Name the mapping, such as the bank's name.";
    if (!f.date.trim()) e.date = "Enter the column that holds the booking date.";
    if (f.signed && !f.amount.trim()) e.amount = "Enter the column that holds the signed amount.";
    if (!f.signed && (!f.in.trim() || !f.out.trim())) e.in = "Enter both the money-in and the money-out columns.";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    const spec: Spec = {
      date_formats: [f.layout],
      number: {},
      date: f.date.trim(),
      amount: f.signed ? f.amount.trim() : undefined,
      in: f.signed ? undefined : f.in.trim(),
      out: f.signed ? undefined : f.out.trim(),
      reference: f.reference.trim() || undefined,
      counterparty: f.counterparty.trim() || undefined,
      description: f.description.trim() ? [f.description.trim()] : undefined,
      balance: f.balance.trim() || undefined,
    };
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/statement-mappings", { params: { header: { "Idempotency-Key": newKey() } }, body: { name: f.name.trim(), format: f.format, spec } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  const text = (k: keyof typeof f, label: string, hint?: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input
        className="fy-input"
        id={`${id}-${k}`}
        value={String(f[k])}
        maxLength={100}
        onChange={set(k)}
        aria-invalid={errors[k] ? true : undefined}
        aria-describedby={errors[k] ? `${id}-${k}-error` : undefined}
      />
      {errors[k] && (
        <span className="fy-error" id={`${id}-${k}-error`}>
          {errors[k]}
        </span>
      )}
      {hint && <span className="fy-small fy-muted">{hint}</span>}
    </div>
  );
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="New column mapping"
      description="Which column of the bank's export holds what. Enter each column's heading as the file writes it."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            Save the mapping
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The mapping was not saved" />}
      {text("name", "Name")}
      <div className="fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-format`}>
            For files in
          </label>
          <select className="fy-select" id={`${id}-format`} value={f.format} onChange={set("format")}>
            <option value="csv">CSV</option>
            <option value="xlsx">Excel (XLSX)</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-layout`}>
            Dates written as
          </label>
          <select className="fy-select" id={`${id}-layout`} value={f.layout} onChange={set("layout")}>
            {layouts.map(([words, layout]) => (
              <option key={layout} value={layout}>
                {words}
              </option>
            ))}
          </select>
        </div>
      </div>
      {text("date", "Booking date column")}
      <fieldset className="fy-field">
        <legend className="fy-label">Amounts</legend>
        <div className="fy-row fy-row--wrap gap-3">
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-signed`} checked={f.signed} onChange={() => setF({ ...f, signed: true })} />
            One signed amount column
          </label>
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-signed`} checked={!f.signed} onChange={() => setF({ ...f, signed: false })} />
            Money in and money out columns
          </label>
        </div>
      </fieldset>
      {f.signed ? (
        text("amount", "Amount column", "Money in positive, money out negative.")
      ) : (
        <>
          {text("in", "Money-in column")}
          {text("out", "Money-out column")}
        </>
      )}
      {text("reference", "Reference column (optional)")}
      {text("counterparty", "Counterparty column (optional)")}
      {text("description", "Description column (optional)")}
      {text("balance", "Balance column (optional)", "With a running balance, an overlap with a statement held already is proved, and its lines dropped.")}
    </Dialog>
  );
}
