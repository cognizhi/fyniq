import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CalendarPlus, CircleCheck, CircleX, Clock, Eye, Pencil, Plus, Save, Send, Trash2, Undo2, UserCheck, X } from "lucide-react";
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
type Edited = components["schemas"]["JournalEdited"];
type Edit = components["schemas"]["JournalEdit"];

// Who posts, reverses and edits journals: P on "Journals" (Appendix D), the Accountant. The API decides.
const posters = ["accountant"];
// The code the API answers when no period covers the date (FY030): the year is not open yet.
const noPeriod = "STORAGE-PERIOD-01";
// The code the API answers when a journal's debits are above the self-approval threshold (FM-2): ask for approval.
const overThreshold = "JOURNAL-07";

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
  reverse_on: string; // empty: the journal does not reverse by itself (FR-GL-02)
}

let keys = 0;
const lineKey = () => `line-${++keys}`;
const newLine = (currency: string): Line => ({ key: lineKey(), account_id: "", description: "", debit: "", credit: "", currency, fx_rate: "" });

// formOf is a saved journal as the form shows it: a positive amount is a debit, a negative one a credit.
export function formOf(j: Journal): Form {
  return {
    date: j.date ?? "",
    memo: j.memo ?? "",
    reverse_on: j.reverse_on ?? "",
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
  if (f.reverse_on && f.date && f.reverse_on <= f.date) errors.reverse_on = "Date the reversal after the journal's date, such as the first day of the next month.";
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
  return { draft: { date: f.date, memo: f.memo.trim() || undefined, lines, reverse_on: f.reverse_on || undefined }, errors };
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

// firstOfNextMonth is the usual date an accrual reverses on: the first day of the month after date (YYYY-MM-DD).
export function firstOfNextMonth(date: string): string {
  const [y, m] = date.split("-").map(Number);
  if (!y || !m) return "";
  return m === 12 ? `${y + 1}-01-01` : `${y}-${String(m + 1).padStart(2, "0")}-01`;
}

// today is the date in the organisation's time zone, as YYYY-MM-DD.
function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

function BalanceStatus({ list, base }: { list: Balance[]; base: string }) {
  if (list.length === 0) return <span className="fy-small fy-muted">Enter the lines to see whether they balance.</span>;
  // A journal in several currencies balances in the base currency, once each line is converted and rounded: only
  // the server's check can say so, because the page never rounds (RND-FX, FR-UX-22).
  if (list.length > 1)
    return (
      <span className="fy-small fy-muted">
        In {list.map((b) => b.currency).join(" and ")}: select Check to see whether it balances in {base}.
      </span>
    );
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
      {list[0]?.currency !== base && <span className="fy-small fy-muted">Check it to see it in {base}: rounding can leave a difference to post to 7030.</span>}
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
            <th scope="col">Description</th>
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
              <td data-card="meta">
                <span className="fy-only-compact">
                  {sign(l.amount) > 0 ? "Debit" : "Credit"}
                  {l.description ? " · " : ""}
                </span>
                {l.description}
              </td>
              <td data-card={sign(l.amount) > 0 ? "amount" : "hide"} className="fy-num">
                {sign(l.amount) > 0 ? formatAmount(l.amount) : ""}
              </td>
              <td data-card={sign(l.amount) < 0 ? "amount" : "hide"} className="fy-num">
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
              <td data-card="hide" />
              <td className="fy-num">{formatAmount(debit)}</td>
              <td data-card="hide" className="fy-num">
                {formatAmount(credit)}
              </td>
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

// Reverse voids a posted journal with a reason. In Founder mode a void needs a second person (FM-1), so it asks for the
// reversal instead, which another founder approves.
function Reverse({ open, journal, etag, ask, onClose, onDone, onAsked }: { open: boolean; journal: Journal; etag: string; ask: boolean; onClose: () => void; onDone: (p: Posting) => void; onAsked: () => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const h = helpText(ask ? "journal.ask-reverse" : "journal.reverse");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const params = { path: { journal_id: journal.id }, header: { "If-Match": etag, "Idempotency-Key": key } };
    const body = { date: on, reason: reason.trim() };
    if (ask) {
      const out = await guarded(confirm, () => api.POST("/v1/journals/{journal_id}/reverse-approval", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onAsked();
    }
    const out = await guarded(confirm, () => api.POST("/v1/journals/{journal_id}/reverse", { params, body }));
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
      title={ask ? "Ask to reverse this journal?" : "Reverse this journal?"}
      description={
        ask
          ? "In Founder mode another founder approves a reversal first. Then a reversing entry with every line the other way round posts on the date you choose; the journal stays in the books, marked reversed."
          : "A reversing entry with every line the other way round posts on the date you choose. The journal stays in the books, marked reversed; nothing is deleted."
      }
    >
      {problem && <Refusal problem={problem} title={ask ? "The request was not made" : "The journal was not reversed"} />}
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
            <Icon of={ask ? UserCheck : Undo2} />
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
// before anything posts. A journal above the self-approval threshold, and in Founder mode a reversal, is asked for and
// approved by another person (FM-1, FM-2, SOD-3). Evidence, automatic reversal and recurring journals arrive with their
// tasks.
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
  const loaded = isNew ? { date: today(), memo: "", lines: [newLine(base), newLine(base)], reverse_on: "" } : j ? formOf(j) : null;
  const f = form ?? loaded;
  const dirty = form !== null;
  const [errors, setErrors] = useState<Errors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<Posting | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [reversing, setReversing] = useState(false);
  // An edit of a posted journal (FR-GL-12): the form prefilled with it, and why.
  const [editing, setEditing] = useState(false);
  const [reason, setReason] = useState("");
  const [reasonMissing, setReasonMissing] = useState(false);
  const [edited, setEdited] = useState<Edited | null>(null);
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []);
  const save = helpText("journal.save");
  const checkHelp = helpText("journal.check");
  const post = helpText("journal.post");
  const reverse = helpText("journal.reverse");
  const askHelp = helpText("journal.ask-approval");
  const askReverse = helpText("journal.ask-reverse");
  // In Founder mode a void needs a second person (FM-1): the API refuses a reversal and takes a request instead.
  const founder = organisation?.team_mode === "founder";
  const asked = j?.approval_asked;
  const editHelp = helpText("journal.edit");
  const reverseOnHelp = helpText("journal.reverse-on");
  const postEditHelp = helpText(founder ? "journal.ask-edit" : "journal.post-edit");
  const editingPosted = editing && j?.status === "posted" && mayPost;

  const change = (next: Form) => {
    setForm(next);
    setPreview(null);
    setEdited(null);
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

  // askPost asks another person to approve posting the draft at the version read (FM-2, SOD-3).
  const askPost = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    const etag = one.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/journals/{journal_id}/post-approval", { params: { path: { journal_id: one.data.journal.id }, header: { "If-Match": etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    setFailed("The request was not made");
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Asked for approval: the journal posts when another person approves it.");
  };

  // editBody is the edit to send, or nothing when a field needs fixing: the content as typed, and why.
  const editBody = (): Edit | null => {
    if (!f) return null;
    const { draft, errors: errs } = check(f, base);
    const why = reason.trim();
    setErrors(errs);
    setReasonMissing(!why);
    if (!draft || !why) {
      setStatus("Fix the fields marked, then try again.");
      return null;
    }
    return { date: draft.date ?? f.date, memo: draft.memo, lines: draft.lines, reverse_on: draft.reverse_on, reason: why };
  };

  const stopEditing = () => {
    setEditing(false);
    setForm(null);
    setReason("");
    setReasonMissing(false);
    setEdited(null);
    setErrors({});
  };

  // sendEdit checks the edit (a dry run that shows both entries) or posts it: the reversal of the entry the journal
  // had, on its date, and the new version's entry (FR-GL-12).
  const sendEdit = async (dryRun: boolean) => {
    const body = editBody();
    if (!body || !one.data) return;
    setBusy(true);
    setProblem(null);
    const etag = one.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/journals/{journal_id}/edit", {
        params: { path: { journal_id: one.data.journal.id }, header: dryRun ? { "If-Match": etag, Prefer: "return=preview" } : { "If-Match": etag, "Idempotency-Key": key } },
        body,
      }),
    );
    setBusy(false);
    setFailed(dryRun ? "The edit cannot post yet" : "The edit was not posted");
    if (!out.data) {
      setEdited(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setEdited(out.data);
      return setStatus("The edit balances and can post. Nothing was saved.");
    }
    stopEditing();
    await refresh();
    setStatus(`Edited. The entry it had is reversed on ${date(out.data.reversal.posting_date)}, and the new version posts on ${date(out.data.entry.posting_date)}.`);
  };

  // askEdit asks another person to approve the edit (FM-1 in Founder mode; FM-2 above the threshold).
  const askEdit = async () => {
    const body = editBody();
    if (!body || !one.data) return;
    setBusy(true);
    setProblem(null);
    const etag = one.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/journals/{journal_id}/edit-approval", { params: { path: { journal_id: one.data.journal.id }, header: { "If-Match": etag, "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    setFailed("The request was not made");
    if (!out.data) return setProblem(problemOf(out.error));
    stopEditing();
    await refresh();
    setStatus("Asked for approval: the edit posts when another person approves it.");
  };

  const title = isNew
    ? "New journal"
    : j
      ? `${editingPosted ? "Edit " : ""}${j.number ?? (j.status === "draft" ? "Draft journal" : "Journal")}${j.memo ? ` · ${j.memo}` : ""}`
      : "Journal";
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
      {!isNew && !dirty && mayPost && !asked && (
        <Button variant="primary" type="button" onClick={() => void send(false)} disabled={busy} title={post.tooltip}>
          <Icon of={Send} />
          {post.label}
        </Button>
      )}
    </>
  );

  const reverseButton = j?.status === "posted" && mayPost && !asked && !editing && (
    <>
      <Button
        type="button"
        onClick={() => {
          setEditing(true);
          setForm(null);
          setProblem(null);
          setStatus("");
        }}
        title={editHelp.tooltip}
      >
        <Icon of={Pencil} />
        {editHelp.label}…
      </Button>
      <Button type="button" onClick={() => setReversing(true)} title={founder ? askReverse.tooltip : reverse.tooltip}>
        <Icon of={founder ? UserCheck : Undo2} />
        {founder ? askReverse.label : reverse.label}…
      </Button>
    </>
  );

  const editActions = editingPosted && f && (
    <>
      <Button variant="ghost" type="button" onClick={stopEditing} disabled={busy}>
        <Icon of={X} />
        Cancel
      </Button>
      {!founder && (
        <Button type="button" onClick={() => void sendEdit(true)} disabled={busy} title={checkHelp.tooltip}>
          <Icon of={Eye} />
          Check the edit
        </Button>
      )}
      <Button variant="primary" type="button" onClick={() => void (founder ? askEdit() : sendEdit(false))} disabled={busy} title={postEditHelp.tooltip}>
        <Icon of={founder ? UserCheck : Send} />
        {postEditHelp.label}
      </Button>
    </>
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
          {j && (j.prepared_by || j.posted_by) && (
            <p className="fy-secondary">
              {[j.prepared_by && `Prepared by ${who(j.prepared_by)}`, j.posted_by && `posted by ${who(j.posted_by)}${j.posted_at ? ` on ${dateTime(j.posted_at)}` : ""}`].filter(Boolean).join(" · ")}
            </p>
          )}
        </div>
        <div className="fy-pagehead__actions fy-from-medium">
          {actions}
          {editActions}
          {reverseButton}
        </div>
      </div>
      <ReviewBanner screen="journal-editor" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The journal could not be shown" />}
      {asked && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={Clock} size={20} />
          <div className="fy-banner__text">
            <strong>{j?.status === "draft" ? "A request to post this journal waits for approval" : "A request to change this journal waits for approval"}</strong>
            <span className="fy-small">
              {j?.status === "draft" ? "Another person approves it, and it posts then. Changing the draft cancels the request." : "Another person approves the edit or the reversal, and it posts then."}
            </span>
          </div>
          <div className="fy-banner__action">
            <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/approvals/$approvalId" params={{ approvalId: asked }}>
              See the request
            </Link>
          </div>
        </div>
      )}
      {problem?.code === overThreshold && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Another person approves this journal</strong>
            <span className="fy-small">{problem.detail} The one who prepared it never approves it.</span>
          </div>
          {mayPost && (
            <div className="fy-banner__action">
              <Button size="sm" variant="primary" onClick={() => void (editingPosted ? askEdit() : askPost())} disabled={busy} title={askHelp.tooltip}>
                <Icon of={UserCheck} />
                {askHelp.label}
              </Button>
            </div>
          )}
        </div>
      )}
      {problem && problem.code !== overThreshold && (
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

      {f && (editable || editingPosted) && (
        <form id={`${id}-form`} className="fy-stack fy-stack--6" onSubmit={editingPosted ? (e) => e.preventDefault() : onSave} noValidate>
          {editingPosted && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-why`}>
              <h2 className="fy-h3" id={`${id}-why`}>
                Edit the posted journal
              </h2>
              <p className="fy-small fy-secondary">
                The entry it has is reversed on its own date, {j?.date ? date(j.date) : "the journal's date"}, and the new version posts on the date below, both at once. Both
                months must be open. {founder ? "In Founder mode another founder approves the edit first." : ""}
              </p>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-reason`}>
                  Why it is edited
                </label>
                <textarea
                  className="fy-textarea"
                  id={`${id}-reason`}
                  value={reason}
                  maxLength={500}
                  onChange={(e) => {
                    setReason(e.target.value);
                    setReasonMissing(false);
                  }}
                  aria-invalid={reasonMissing || undefined}
                  aria-describedby={reasonMissing ? `${id}-reason-error` : undefined}
                />
                {reasonMissing && (
                  <span className="fy-error" id={`${id}-reason-error`}>
                    Say why: the audit trail and the reversal keep the reason.
                  </span>
                )}
              </div>
            </section>
          )}
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
              <div className="fy-field">
                <label className="fy-row gap-2 fy-label">
                  <input
                    type="checkbox"
                    checked={Boolean(f.reverse_on)}
                    onChange={(e) => change({ ...f, reverse_on: e.target.checked ? firstOfNextMonth(f.date || today()) : "" })}
                    title={reverseOnHelp.tooltip}
                  />
                  {reverseOnHelp.label}
                </label>
                {f.reverse_on && (
                  <>
                    <label className="fy-sr" htmlFor={`${id}-reverse-on`}>
                      Reversal date
                    </label>
                    <input
                      className="fy-input"
                      id={`${id}-reverse-on`}
                      type="date"
                      value={f.reverse_on}
                      min={f.date || undefined}
                      onChange={(e) => change({ ...f, reverse_on: e.target.value })}
                      aria-invalid={errors.reverse_on ? true : undefined}
                      aria-describedby={described("reverse_on") ?? `${id}-reverse-on-help`}
                    />
                    {fieldError("reverse_on") ?? (
                      <span className="fy-help" id={`${id}-reverse-on-help`}>
                        Fyniq posts the reversal on this date by itself once the journal is posted, as for an accrual reversing on the first of the next month.
                      </span>
                    )}
                  </>
                )}
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
                    <div className="fy-field col-span-2 medium:col-span-6 expanded:col-span-3">
                      <label className="fy-label" htmlFor={`${l.key}-account`}>
                        <span className="fy-sr">Line {n}: </span>
                        Account
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
                    <div className="fy-field col-span-2 medium:col-span-6 expanded:col-span-3">
                      <label className="fy-label" htmlFor={`${l.key}-description`}>
                        <span className="fy-sr">Line {n}: </span>
                        Description
                      </label>
                      <input className="fy-input" id={`${l.key}-description`} value={l.description} maxLength={500} onChange={(e) => setLine(l.key, "description", e.target.value)} />
                    </div>
                    <div className="fy-field medium:col-span-4 expanded:col-span-2">
                      <label className="fy-label" htmlFor={`${l.key}-debit`}>
                        <span className="fy-sr">Line {n}: </span>
                        Debit
                      </label>
                      <input className="fy-input fy-num" id={`${l.key}-debit`} inputMode="decimal" value={l.debit} onChange={(e) => setLine(l.key, "debit", e.target.value)} aria-invalid={errors[`${l.key}.debit`] ? true : undefined} aria-describedby={described(`${l.key}.debit`)} />
                      {fieldError(`${l.key}.debit`)}
                    </div>
                    <div className="fy-field medium:col-span-4 expanded:col-span-2">
                      <label className="fy-label" htmlFor={`${l.key}-credit`}>
                        <span className="fy-sr">Line {n}: </span>
                        Credit
                      </label>
                      <input className="fy-input fy-num" id={`${l.key}-credit`} inputMode="decimal" value={l.credit} onChange={(e) => setLine(l.key, "credit", e.target.value)} aria-invalid={errors[`${l.key}.credit`] ? true : undefined} aria-describedby={described(`${l.key}.credit`)} />
                      {fieldError(`${l.key}.credit`)}
                    </div>
                    <div className="fy-field medium:col-span-3 expanded:col-span-1">
                      <label className="fy-label" htmlFor={`${l.key}-currency`}>
                        <span className="fy-sr">Line {n}: </span>
                        Currency
                      </label>
                      <input className="fy-input fy-mono" id={`${l.key}-currency`} value={l.currency} maxLength={3} onChange={(e) => setLine(l.key, "currency", e.target.value.toUpperCase())} aria-invalid={errors[`${l.key}.currency`] ? true : undefined} aria-describedby={described(`${l.key}.currency`)} />
                      {fieldError(`${l.key}.currency`)}
                    </div>
                    <div className="flex items-end justify-end self-end medium:col-span-1">
                      <Button variant="ghost" size="sm" type="button" aria-label={`Remove line ${n}`} onClick={() => change({ ...f, lines: f.lines.filter((x) => x.key !== l.key) })}>
                        <Icon of={Trash2} />
                      </Button>
                    </div>
                    {(l.currency.trim() || base).toUpperCase() !== base && (
                      <div className="fy-field col-span-2 medium:col-span-4 expanded:col-span-3">
                        <label className="fy-label" htmlFor={`${l.key}-rate`}>
                          <span className="fy-sr">Line {n}: </span>
                          Rate ({base} for 1 {l.currency.toUpperCase()})
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

      {j && !editable && !editingPosted && f && (
        <section className="fy-card" aria-labelledby={`${id}-read`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-read`}>
              {j.date ? date(j.date) : "No date yet"}
            </h2>
            {j.status === "draft" && <span className="fy-small fy-muted">An Accountant or a Tax agent changes drafts.</span>}
            {j.reverse_on && j.status !== "reversed" && <span className="fy-chip fy-chip--neutral">Reverses by itself on {date(j.reverse_on)}</span>}
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

      {edited && (
        <section className="fy-card" aria-labelledby={`${id}-edited`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-edited`}>
              What the edit posts, in {base}
            </h2>
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              Ready to post
            </span>
          </div>
          <p className="fy-small fy-strong px-4 pt-3 pb-2">1. The reversal of the entry it had, on {date(edited.reversal.posting_date)}</p>
          <Lines caption="What the edit reverses" accounts={byId} base={base} lines={(edited.reversal.lines ?? []).map((l) => ({ account_id: l.account_id, description: l.description, amount: l.base_amount.amount, currency: base }))} />
          <p className="fy-small fy-strong px-4 pt-4 pb-2">2. The new version, on {date(edited.entry.posting_date)}</p>
          <Lines caption="What the new version posts" accounts={byId} base={base} lines={(edited.entry.lines ?? []).map((l) => ({ account_id: l.account_id, description: l.description, amount: l.base_amount.amount, currency: base }))} />
          <p className="fy-small fy-muted px-4 py-3">A check saves nothing: both post together when you select Post the edit.</p>
        </section>
      )}

      {((editable && f) || editActions || reverseButton) && (
        <div className="fy-actionbar fy-only-compact">
          {actions}
          {editActions}
          {reverseButton}
        </div>
      )}
      {j && one.data && (
        <Reverse
          open={reversing}
          journal={j}
          etag={one.data.etag}
          ask={founder}
          onClose={() => setReversing(false)}
          onDone={async (p) => {
            setReversing(false);
            await refresh();
            setStatus(`Reversed. The reversing entry is dated ${date(p.entry.posting_date)}.`);
          }}
          onAsked={async () => {
            setReversing(false);
            await refresh();
            setStatus("Asked for approval: the reversal posts when another founder approves it.");
          }}
        />
      )}
    </div>
  );
}
