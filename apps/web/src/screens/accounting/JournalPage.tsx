import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CalendarPlus, CircleCheck, CircleX, Eye, Plus, Save, Send, Trash2, Undo2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime, timeZone } from "../../format/date";
import { isZero, minus, sign, sum, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable, type Account } from "./data";
import { preparers, StatusChip, who } from "./Journals";

type Journal = components["schemas"]["Journal"];
type Posting = components["schemas"]["JournalPosting"];
type Draft = components["schemas"]["JournalDraft"];

// Who posts and reverses journals: P on "Journals" (Appendix D), the Accountant. The API decides.
const posters = ["accountant"];
// The code the API answers when no period covers the date (FY030): the year is not open yet.
const noPeriod = "STORAGE-PERIOD-01";

export interface Line {
  key: string;
  account_id: string;
  description: string;
  debit: string;
  credit: string;
  currency: string;
  fx_rate: string;
}

export interface Form {
  date: string;
  memo: string;
  lines: Line[];
}

let keys = 0;
const lineKey = () => `line-${++keys}`;
const newLine = (currency: string): Line => ({ key: lineKey(), account_id: "", description: "", debit: "", credit: "", currency, fx_rate: "" });

// formOf is a saved journal as the form shows it: a positive amount is a debit, a negative one a credit.
export function formOf(j: Journal): Form {
  return {
    date: j.date ?? "",
    memo: j.memo ?? "",
    lines: (j.lines ?? []).map((l) => {
      const credit = l.amount.amount.startsWith("-");
      return {
        key: lineKey(),
        account_id: l.account_id,
        description: l.description ?? "",
        debit: credit ? "" : l.amount.amount,
        credit: credit ? l.amount.amount.slice(1) : "",
        currency: l.amount.currency,
        fx_rate: l.fx_rate ?? "",
      };
    }),
  };
}

const blank = (l: Line) => !l.account_id && !l.description.trim() && !l.debit.trim() && !l.credit.trim();

export type Errors = Record<string, string>; // by "date", "lines", or "<line key>.<field>"

// check turns what was typed into the draft to send, or says what to fix under each field. Blank lines
// are left out. The API checks the rest when the journal posts (ENG-24).
export function check(f: Form, base: string): { draft?: Draft; errors: Errors } {
  const errors: Errors = {};
  const lines: Draft["lines"] = [];
  if (!f.date) errors.date = "Enter the date the journal belongs to.";
  for (const l of f.lines) {
    if (blank(l)) continue;
    const debit = typed(l.debit);
    const credit = typed(l.credit);
    if (!l.account_id) errors[`${l.key}.account`] = "Choose the account.";
    if (debit === null) errors[`${l.key}.debit`] = "Enter an amount, such as 1,200.00.";
    if (credit === null) errors[`${l.key}.credit`] = "Enter an amount, such as 1,200.00.";
    let amount = "";
    if (debit !== null && credit !== null) {
      const d = debit !== "" && !isZero(debit);
      const c = credit !== "" && !isZero(credit);
      if (sign(debit || "0") < 0 || sign(credit || "0") < 0) errors[`${l.key}.debit`] = "Enter the amount without a minus sign: a credit goes under Credit.";
      else if (d && c) errors[`${l.key}.debit`] = "Enter a debit or a credit, not both.";
      else if (!d && !c) errors[`${l.key}.debit`] = "Enter a debit or a credit.";
      else amount = d ? debit : `-${credit}`;
    }
    const currency = (l.currency.trim() || base).toUpperCase();
    const rate = l.fx_rate.trim();
    if (!/^[A-Z]{3}$/.test(currency)) errors[`${l.key}.currency`] = "Enter a currency code, such as USD.";
    else if (currency !== base && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors[`${l.key}.fx_rate`] = `Enter the rate: ${base} for 1 ${currency}, such as 1.3472.`;
    lines.push({ account_id: l.account_id, amount: { amount, currency }, description: l.description.trim() || undefined, fx_rate: currency !== base ? rate : undefined });
  }
  if (lines.length === 0) errors.lines = "Add a line.";
  if (Object.keys(errors).length > 0) return { errors };
  return { draft: { date: f.date, memo: f.memo.trim() || undefined, lines }, errors };
}

export interface Balance {
  currency: string;
  debits: string;
  credits: string;
  difference: string; // debits − credits
}

// balances adds up what was typed, per currency, exactly; amounts that are not amounts yet count as 0.
export function balances(f: Form, base: string): Balance[] {
  const by = new Map<string, { d: string[]; c: string[] }>();
  for (const l of f.lines) {
    const currency = (l.currency.trim() || base).toUpperCase();
    const t = by.get(currency) ?? { d: [], c: [] };
    const d = typed(l.debit);
    const c = typed(l.credit);
    if (d) t.d.push(d);
    if (c) t.c.push(c);
    by.set(currency, t);
  }
  return [...by.entries()]
    .filter(([, t]) => t.d.length + t.c.length > 0)
    .map(([currency, t]) => {
      const debits = sum(t.d);
      const credits = sum(t.c);
      return { currency, debits, credits, difference: minus(debits, credits) };
    });
}

// today is the date in the organisation's time zone, as YYYY-MM-DD.
function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

function BalanceStatus({ list, base }: { list: Balance[]; base: string }) {
  if (list.length === 0) return <span className="fy-small fy-muted">Enter the lines to see whether they balance.</span>;
  return (
    <span className="fy-row fy-row--wrap gap-2">
      {list.map((b) =>
        isZero(b.difference) ? (
          <span key={b.currency} className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Balanced in {b.currency}: {formatAmount(b.debits)} each side
          </span>
        ) : (
          <span key={b.currency} className="fy-chip fy-chip--critical">
            <Icon of={CircleX} />
            {sign(b.difference) > 0 ? "Debits exceed credits" : "Credits exceed debits"} by {formatAmount(b.difference.replace(/^-/, ""))} {b.currency}
          </span>
        ),
      )}
      {list.some((b) => b.currency !== base) && <span className="fy-small fy-muted">Check it to see it in {base}: rounding can leave a difference to post to 7030.</span>}
    </span>
  );
}

function accountLabel(accounts: Map<string, Account>, id: string): string {
  const a = accounts.get(id);
  return a ? `${a.code} ${a.name}` : "An account not in the chart";
}

// Lines shows a journal's lines, read-only: as entered, or as posted in the base currency.
function Lines({ lines, accounts, base, caption }: { lines: { account_id: string; description?: string; amount: string; currency: string; rate?: string }[]; accounts: Map<string, Account>; base: string; caption: string }) {
  const debit = sum(lines.filter((l) => sign(l.amount) > 0).map((l) => l.amount));
  const credit = sum(lines.filter((l) => sign(l.amount) < 0).map((l) => l.amount.slice(1)));
  const foreign = lines.some((l) => l.currency !== base);
  return (
    <div className="fy-tablewrap">
      <table className="fy-table fy-table--cards" aria-label={caption}>
        <thead>
          <tr>
            <th scope="col">Account</th>
            <th scope="col" className="fy-from-medium">
              Description
            </th>
            <th scope="col" className="fy-num">
              Debit
            </th>
            <th scope="col" className="fy-num">
              Credit
            </th>
            {foreign && (
              <th scope="col" className="fy-from-medium">
                Currency · rate
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {lines.map((l, i) => (
            <tr key={i}>
              <td data-card="title">{accountLabel(accounts, l.account_id)}</td>
              <td data-card="meta" className="fy-from-medium">
                {l.description}
              </td>
              <td data-card="amount" className="fy-num">
                {sign(l.amount) > 0 ? formatAmount(l.amount) : ""}
              </td>
              <td data-card="amount" className="fy-num">
                {sign(l.amount) < 0 ? formatAmount(l.amount.slice(1)) : ""}
              </td>
              {foreign && (
                <td data-card="meta" className="fy-from-medium fy-mono">
                  {l.currency === base ? l.currency : `${l.currency} × ${l.rate ?? ""}`}
                </td>
              )}
            </tr>
          ))}
        </tbody>
        {!foreign && (
          <tfoot>
            <tr>
              <td>Total</td>
              <td className="fy-from-medium" />
              <td className="fy-num">{formatAmount(debit)}</td>
              <td className="fy-num">{formatAmount(credit)}</td>
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

function Reverse({ open, journal, etag, onClose, onDone }: { open: boolean; journal: Journal; etag: string; onClose: () => void; onDone: (p: Posting) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const h = helpText("journal.reverse");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/journals/{journal_id}/reverse", { params: { path: { journal_id: journal.id }, header: { "If-Match": etag, "Idempotency-Key": key } }, body: { date: on, reason: reason.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(out.data);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={journal.number}
      title="Reverse this journal?"
      description="A reversing entry with every line the other way round posts on the date you choose. The journal stays in the books, marked reversed; nothing is deleted."
    >
      {problem && <Refusal problem={problem} title="The journal was not reversed" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date of the reversing entry
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} min={journal.date} onChange={(e) => setOn(e.target.value)} aria-describedby={`${id}-date-help`} />
          <span className="fy-help" id={`${id}-date-help`}>
            On or after {journal.date ? date(journal.date) : "the journal's date"}, in an open period.
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is reversed
          </label>
          <textarea
            className="fy-textarea"
            id={`${id}-reason`}
            value={reason}
            maxLength={500}
            onChange={(e) => {
              setReason(e.target.value);
              setMissing(false);
            }}
            aria-invalid={missing || undefined}
            aria-describedby={missing ? `${id}-reason-error` : undefined}
          />
          {missing && (
            <span className="fy-error" id={`${id}-reason-error`}>
              Say why: the audit trail keeps the reason with the reversal.
            </span>
          )}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Undo2} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// JournalPage is a journal (docs/design/screens JournalEditor; FR-GL-01, FR-GL-12, FR-INT-14): a new one, a draft
// its preparer changes, checks and posts, or a posted journal, read-only, which an Accountant reverses with a
// reason. What was typed is added up exactly as it is entered; the server's check gives the base-currency entry
// before anything posts. Evidence, approvals above the threshold, automatic reversal and recurring journals
// arrive with their tasks.
export function JournalPage() {
  const { journalId } = useParams({ strict: false }) as { journalId?: string };
  const isNew = !journalId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const mayPost = roles.some((r) => posters.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const one = useQuery({
    queryKey: ["journal", journalId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/journals/{journal_id}", { params: { path: { journal_id: journalId ?? "" } } });
      if (r.error) throw r.error;
      return { journal: r.data, etag: etagOf(r.response) };
    },
  });
  const j = one.data?.journal;
  const editable = mayPrepare && (isNew || j?.status === "draft");
  const [form, setForm] = useState<Form | null>(null);
  const loaded = isNew ? { date: today(), memo: "", lines: [newLine(base), newLine(base)] } : j ? formOf(j) : null;
  const f = form ?? loaded;
  const dirty = form !== null;
  const [errors, setErrors] = useState<Errors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<Posting | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [reversing, setReversing] = useState(false);
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []);
  const save = helpText("journal.save");
  const checkHelp = helpText("journal.check");
  const post = helpText("journal.post");
  const reverse = helpText("journal.reverse");

  const change = (next: Form) => {
    setForm(next);
    setPreview(null);
  };
  const setLine = (key: string, field: keyof Line, value: string) => f && change({ ...f, lines: f.lines.map((l) => (l.key === key ? { ...l, [field]: value } : l)) });

  const refresh = async () => {
    await Promise.all([queries.invalidateQueries({ queryKey: ["journals"] }), queries.invalidateQueries({ queryKey: ["journal", journalId] })]);
  };

  const onSave = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { draft, errors: errs } = check(f, base);
    setErrors(errs);
    if (!draft) return setStatus("Fix the fields marked, then save again.");
    setBusy(true);
    setProblem(null);
    if (isNew) {
      const key = newKey();
      const out = await guarded(confirm, () => api.POST("/v1/journals", { params: { header: { "Idempotency-Key": key } }, body: draft }));
      setBusy(false);
      setFailed("The journal was not saved");
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["journals"] });
      setForm(null);
      return navigate({ to: "/accounting/journals/$journalId", params: { journalId: out.data.id } });
    }
    if (!one.data) return setBusy(false);
    const etag = one.data.etag;
    const out = await guarded(confirm, () => api.PUT("/v1/journals/{journal_id}", { params: { path: { journal_id: one.data.journal.id }, header: { "If-Match": etag } }, body: draft }));
    setBusy(false);
    setFailed("The journal was not saved");
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setForm(null);
    setStatus("Saved.");
  };

  const send = async (dryRun: boolean) => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    const etag = one.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/journals/{journal_id}/post", {
        params: { path: { journal_id: one.data.journal.id }, header: dryRun ? { "If-Match": etag, Prefer: "return=preview" } : { "If-Match": etag, "Idempotency-Key": key } },
      }),
    );
    setBusy(false);
    setFailed(dryRun ? "The journal cannot post yet" : "The journal was not posted");
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setPreview(out.data);
      return setStatus("It balances and can post. Nothing was saved.");
    }
    setPreview(null);
    await refresh();
    setStatus(`Posted as ${out.data.journal.number ?? "a journal"}.`);
  };

  const title = isNew ? "New journal" : j ? `${j.number ?? "Draft journal"}${j.memo ? ` · ${j.memo}` : ""}` : "Journal";
  const list = f ? balances(f, base) : [];
  const fieldError = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;
  const described = (key: string) => (errors[key] ? `${id}-${key}-error` : undefined);

  const actions = editable && f && (
    <>
      {(isNew || dirty) && (
        <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy} title={save.tooltip}>
          <Icon of={Save} />
          {isNew ? save.label : "Save the changes"}
        </Button>
      )}
      {!isNew && !dirty && (
        <Button type="button" onClick={() => void send(true)} disabled={busy} title={checkHelp.tooltip}>
          <Icon of={Eye} />
          {checkHelp.label}
        </Button>
      )}
      {!isNew && !dirty && mayPost && (
        <Button variant="primary" type="button" onClick={() => void send(false)} disabled={busy} title={post.tooltip}>
          <Icon of={Send} />
          {post.label}
        </Button>
      )}
    </>
  );

  const reverseButton = j?.status === "posted" && mayPost && (
    <Button type="button" onClick={() => setReversing(true)} title={reverse.tooltip}>
      <Icon of={Undo2} />
      {reverse.label}…
    </Button>
  );

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <Link to="/accounting/journals">Journals</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{title}</h1>
            {j && <StatusChip status={j.status} />}
          </div>
          {j && (
            <p className="fy-secondary">
              Prepared by {who(j.prepared_by)}
              {j.posted_by && ` · posted by ${who(j.posted_by)}${j.posted_at ? ` on ${dateTime(j.posted_at)}` : ""}`}
            </p>
          )}
        </div>
        <div className="fy-pagehead__actions fy-from-medium">
          {actions}
          {reverseButton}
        </div>
      </div>
      <ReviewBanner screen="journal-editor" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The journal could not be shown" />}
      {problem && (
        <>
          <Refusal problem={problem} title={failed} />
          {problem.code === noPeriod && (
            <p className="fy-small">
              <Link to="/accounting/close">
                <Icon of={CalendarPlus} /> Open the financial year in Periods and close
              </Link>
            </p>
          )}
        </>
      )}
      {!isNew && one.isPending && <p className="fy-secondary">Loading the journal…</p>}
      {isNew && !mayPrepare && <p className="fy-secondary">An Accountant or a Tax agent prepares journals.</p>}

      {f && editable && (
        <form id={`${id}-form`} className="fy-stack fy-stack--6" onSubmit={onSave} noValidate>
          <section className="fy-card" aria-labelledby={`${id}-head`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-head`}>
                Journal
              </h2>
            </div>
            <div className="fy-card__body fy-form fy-form--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-date`}>
                  Date
                </label>
                <input className="fy-input" id={`${id}-date`} type="date" value={f.date} onChange={(e) => change({ ...f, date: e.target.value })} aria-invalid={errors.date ? true : undefined} aria-describedby={described("date")} />
                {fieldError("date")}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-memo`}>
                  Description
                </label>
                <input className="fy-input" id={`${id}-memo`} value={f.memo} maxLength={1000} placeholder="What the journal is for" onChange={(e) => change({ ...f, memo: e.target.value })} />
              </div>
            </div>
          </section>

          <section className="fy-card" aria-labelledby={`${id}-lines`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-lines`}>
                Lines
              </h2>
              <span className="fy-small fy-muted fy-from-medium">Debits are positive, credits negative, once posted.</span>
            </div>
            <ol className="fy-card__body fy-stack fy-stack--3">
              {f.lines.map((l, i) => {
                const n = i + 1;
                return (
                  <li key={l.key} className="grid grid-cols-2 gap-2 border-b border-border pb-3 medium:grid-cols-12 medium:items-start">
                    <div className="fy-field col-span-2 medium:col-span-4">
                      <label className="fy-label" htmlFor={`${l.key}-account`}>
                        Line {n}: account
                      </label>
                      <select className="fy-select" id={`${l.key}-account`} value={l.account_id} onChange={(e) => setLine(l.key, "account_id", e.target.value)} aria-invalid={errors[`${l.key}.account`] ? true : undefined} aria-describedby={described(`${l.key}.account`)}>
                        <option value="">Choose an account</option>
                        {choices.map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.code} {a.name}
                          </option>
                        ))}
                        {l.account_id && !choices.some((a) => a.id === l.account_id) && <option value={l.account_id}>{accountLabel(byId, l.account_id)}</option>}
                      </select>
                      {fieldError(`${l.key}.account`)}
                    </div>
                    <div className="fy-field col-span-2 medium:col-span-3">
                      <label className="fy-label" htmlFor={`${l.key}-description`}>
                        Line {n}: description
                      </label>
                      <input className="fy-input" id={`${l.key}-description`} value={l.description} maxLength={500} onChange={(e) => setLine(l.key, "description", e.target.value)} />
                    </div>
                    <div className="fy-field medium:col-span-2">
                      <label className="fy-label" htmlFor={`${l.key}-debit`}>
                        Line {n}: debit
                      </label>
                      <input className="fy-input fy-num" id={`${l.key}-debit`} inputMode="decimal" value={l.debit} onChange={(e) => setLine(l.key, "debit", e.target.value)} aria-invalid={errors[`${l.key}.debit`] ? true : undefined} aria-describedby={described(`${l.key}.debit`)} />
                      {fieldError(`${l.key}.debit`)}
                    </div>
                    <div className="fy-field medium:col-span-2">
                      <label className="fy-label" htmlFor={`${l.key}-credit`}>
                        Line {n}: credit
                      </label>
                      <input className="fy-input fy-num" id={`${l.key}-credit`} inputMode="decimal" value={l.credit} onChange={(e) => setLine(l.key, "credit", e.target.value)} aria-invalid={errors[`${l.key}.credit`] ? true : undefined} aria-describedby={described(`${l.key}.credit`)} />
                      {fieldError(`${l.key}.credit`)}
                    </div>
                    <div className="col-span-2 flex items-end justify-end medium:col-span-1 medium:self-end">
                      <Button variant="ghost" size="sm" type="button" aria-label={`Remove line ${n}`} onClick={() => change({ ...f, lines: f.lines.filter((x) => x.key !== l.key) })}>
                        <Icon of={Trash2} />
                      </Button>
                    </div>
                    <div className="fy-field medium:col-span-2">
                      <label className="fy-label" htmlFor={`${l.key}-currency`}>
                        Line {n}: currency
                      </label>
                      <input className="fy-input fy-mono" id={`${l.key}-currency`} value={l.currency} maxLength={3} onChange={(e) => setLine(l.key, "currency", e.target.value.toUpperCase())} aria-invalid={errors[`${l.key}.currency`] ? true : undefined} aria-describedby={described(`${l.key}.currency`)} />
                      {fieldError(`${l.key}.currency`)}
                    </div>
                    {(l.currency.trim() || base).toUpperCase() !== base && (
                      <div className="fy-field medium:col-span-3">
                        <label className="fy-label" htmlFor={`${l.key}-rate`}>
                          Line {n}: rate ({base} for 1 {l.currency.toUpperCase()})
                        </label>
                        <input className="fy-input fy-num" id={`${l.key}-rate`} inputMode="decimal" value={l.fx_rate} onChange={(e) => setLine(l.key, "fx_rate", e.target.value)} aria-invalid={errors[`${l.key}.fx_rate`] ? true : undefined} aria-describedby={described(`${l.key}.fx_rate`)} />
                        {fieldError(`${l.key}.fx_rate`)}
                      </div>
                    )}
                  </li>
                );
              })}
            </ol>
            <div className="fy-card__foot fy-row fy-row--wrap fy-row--between gap-2">
              <Button variant="ghost" size="sm" type="button" onClick={() => change({ ...f, lines: [...f.lines, newLine(base)] })}>
                <Icon of={Plus} />
                Add a line
              </Button>
              <BalanceStatus list={list} base={base} />
            </div>
            {errors.lines && <p className="fy-error px-4 pb-3">{errors.lines}</p>}
          </section>
        </form>
      )}

      {j && !editable && f && (
        <section className="fy-card" aria-labelledby={`${id}-read`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-read`}>
              {j.date ? date(j.date) : "No date yet"}
            </h2>
            {j.status === "draft" && <span className="fy-small fy-muted">An Accountant or a Tax agent changes drafts.</span>}
          </div>
          <Lines
            caption="Lines as entered"
            accounts={byId}
            base={base}
            lines={(j.lines ?? []).map((l) => ({ account_id: l.account_id, description: l.description, amount: l.amount.amount, currency: l.amount.currency, rate: l.fx_rate }))}
          />
        </section>
      )}

      {preview && (
        <section className="fy-card" aria-labelledby={`${id}-preview`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-preview`}>
              What posting writes, in {base}
            </h2>
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              Ready to post
            </span>
          </div>
          <Lines caption="What posting writes" accounts={byId} base={base} lines={(preview.entry.lines ?? []).map((l) => ({ account_id: l.account_id, description: l.description, amount: l.base_amount.amount, currency: base }))} />
          <p className="fy-small fy-muted px-4 pb-3">A check saves nothing. It posts on {date(preview.entry.posting_date)}.</p>
        </section>
      )}

      {((editable && f) || reverseButton) && (
        <div className="fy-actionbar fy-only-compact">
          {actions}
          {reverseButton}
        </div>
      )}
      {j && one.data && <Reverse open={reversing} journal={j} etag={one.data.etag} onClose={() => setReversing(false)} onDone={async (p) => {
        setReversing(false);
        await refresh();
        setStatus(`Reversed. The reversing entry is dated ${date(p.entry.posting_date)}.`);
      }} />}
    </div>
  );
}
