import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, Clock, ClockAlert, PencilLine, Plus, Send, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { isZero, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, type Account } from "../accounting/data";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { accounts, owed } from "./DirectorAccounts";
import { directorRoles } from "./data";

// Settle a director's balance (FR-EQ-05, FR-EQ-07, FM-9; p1-founders-money task 4.4, design D10): an Owner or the
// Controller chooses how what the company owes a director or shareholder on 2500 or 2120 is settled, drafts it with its
// details, sees its ledger impact, attaches the agreement and asks another person to approve it, never the person
// settled. Converting to shares comes with the register of share issues, and to a SAFE or note with the equity register.

type Settlement = components["schemas"]["DirectorSettlement"];
type Route = Settlement["route"];

// The roles that draft a settlement: A on "Director accounts, settlements, share issues" (Appendix D).
const drafters = ["owner", "controller"];

// The routes, in the PRD's order, with their plain consequences: general information, not advice.
const routes: { route: Route | "safe"; title: string; words: string; later?: string }[] = [
  { route: "repay", title: "Repay in cash", words: "The next payment batch pays it into their approved bank account, and the balance goes down. Not income for them." },
  { route: "keep", title: "Keep as a loan", words: "Nothing posts. The terms go in a loan note or board minute, and the balance is confirmed at each year end." },
  { route: "convert", title: "Convert to shares", words: "The balance becomes share capital on the date ACRA records the allotment. Needs the shareholders' approval, a board resolution, a conversion agreement and their balance confirmation." },
  { route: "waive", title: "Waive", words: "They give up the amount. It posts to capital contribution or other income, with a reason." },
  { route: "safe", title: "Convert to a SAFE or note", words: "The amount turns into shares at the next priced round.", later: "Comes with the equity register" },
];

const routeTitle = (r: Route) => routes.find((x) => x.route === r)?.title ?? r;

// The account a waiver may credit: 3300 Capital contribution, or an income account; never a director's account, a
// money account, share capital or GST control (FR-EQ-07).
const creditable = (a: Account) =>
  a.active && !a.group && !a.money_account && !directorRoles.includes(a.role ?? "") && a.role !== "share-capital" && a.role !== "gst-control" && (a.type === "equity" || a.type === "revenue");

type Conversion = components["schemas"]["SettlementConversion"];

type Form = {
  route: Route;
  account_id: string;
  amount: string;
  interest_rate: string;
  repay_on: string;
  subordinated: boolean;
  credit_account_id: string;
  capacity: "shareholder" | "other";
  reason: string;
  conversion: { class_account_id: string; price: string; price_basis: NonNullable<Conversion["price_basis"]> | ""; capacity: Conversion["capacity"]; measurement: NonNullable<Conversion["measurement"]>; fair_value_price: string; difference_account_id: string; note: string };
};

const blankConversion: Form["conversion"] = { class_account_id: "", price: "", price_basis: "", capacity: "lender", measurement: "carrying", fair_value_price: "", difference_account_id: "", note: "" };

const blank = (route: Route, account: string): Form => ({ route, account_id: account, amount: "", interest_rate: "0", repay_on: "", subordinated: false, credit_account_id: "", capacity: "shareholder", reason: "", conversion: blankConversion });

// The price bases and capacities of a conversion, in the PRD's words (FR-EQ-06).
const priceBases: Record<NonNullable<Conversion["price_basis"]>, string> = { latest_priced_round: "The latest priced round", valuation: "An independent valuation", unanimous: "A price every shareholder approves" };
const capacities: Record<Conversion["capacity"], string> = { lender: "A lender", employee: "An employee", shareholder: "A shareholder" };

function formOf(s: Settlement): Form {
  return {
    route: s.route,
    account_id: s.account_id,
    amount: s.amount.amount,
    interest_rate: s.terms?.interest_rate ?? "0",
    repay_on: s.terms?.repay_on ?? "",
    subordinated: !!s.terms?.subordinated,
    credit_account_id: s.waiver?.credit_account_id ?? "",
    capacity: s.waiver?.capacity ?? "shareholder",
    reason: s.waiver?.reason ?? "",
    conversion: s.conversion
      ? { ...blankConversion, ...s.conversion, price_basis: s.conversion.price_basis ?? "", measurement: s.conversion.measurement ?? "carrying", fair_value_price: s.conversion.fair_value_price ?? "", difference_account_id: s.conversion.difference_account_id ?? "", note: s.conversion.note ?? "" }
      : blankConversion,
  };
}

function StatusChip({ s }: { s: Settlement }) {
  if (s.status === "effective")
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Effective
      </span>
    );
  if (s.status === "decided" && s.route === "convert")
    return (
      <span className="fy-chip fy-chip--serious">
        <Icon of={ClockAlert} />
        Approved, waiting for ACRA
      </span>
    );
  if (s.status === "decided")
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Decided
      </span>
    );
  if (s.status === "waiting")
    return (
      <span className="fy-chip fy-chip--serious">
        <Icon of={ClockAlert} />
        Awaiting approval
      </span>
    );
  return (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={PencilLine} />
      Draft
    </span>
  );
}

// Impact is a settlement's ledger impact: the waiver's lines in Expert mode, and what it means in plain words.
function Impact({ s, codes, name }: { s: Settlement; codes: Map<string, Account>; name: string }) {
  const amount = `${formatAmount(s.amount.amount)} ${s.amount.currency}`;
  const plain =
    s.route === "keep"
      ? `Nothing posts: ${amount} stays owed to ${name} as a loan on these terms.`
      : s.route === "repay"
        ? `Once approved, the next payment batch pays ${amount} to ${name}, and what the company owes them goes down by it.`
        : s.route === "convert"
          ? `When ACRA records the allotment, ${s.shares ? `${formatAmount(s.shares.converted.amount)} ${s.shares.converted.currency}` : amount} moves from what the company owes ${name} to their shareholding${s.shares && !isZero(s.shares.remainder.amount) ? `, and ${formatAmount(s.shares.remainder.amount)} ${s.shares.remainder.currency} stays owed` : ""}.`
          : `${amount} stops being owed to ${name} and goes to ${codes.get(s.waiver?.credit_account_id ?? "")?.name ?? "the account chosen"}.`;
  return (
    <section className="fy-card" aria-labelledby="ds-impact">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="ds-impact">
          {s.status === "decided" ? "What it posted" : "What it posts"}
        </h2>
        {s.route === "waive" && <span className="fy-small fy-muted">On the day it is approved</span>}
        {s.route === "convert" && s.status !== "effective" && <span className="fy-small fy-muted">On the day ACRA registers it, never backdated</span>}
      </div>
      <div className="fy-card__body fy-stack fy-stack--2">
        <p className="fy-small">{plain}</p>
        {s.ledger_impact.length > 0 && (
          <table className="fy-table fy-table--compact fy-ledger fy-expert-only">
            <caption className="sr-only">Ledger impact</caption>
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Debit
                </th>
                <th scope="col" className="fy-num">
                  Credit
                </th>
              </tr>
            </thead>
            <tbody>
              {s.ledger_impact.map((l) => {
                const a = codes.get(l.account_id);
                const credit = l.amount.amount.startsWith("-");
                return (
                  <tr key={l.line_no}>
                    <td>
                      <span className="fy-acct">{a?.code}</span>
                      {a?.name}
                    </td>
                    <td className="fy-num">{credit ? "" : formatAmount(l.amount.amount)}</td>
                    <td className="fy-num">{credit ? formatAmount(l.amount.amount.slice(1)) : ""}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}

// The ACRA steps of a conversion (FR-EQ-09), in their order and plain words.
const stepWords: Record<NonNullable<Settlement["acra"]>["steps"][number]["kind"], string> = {
  resolved: "Shareholders resolved",
  approval_lodged: "Approval lodged with ACRA",
  agm_due: "Next AGM due by",
  allotment_lodged: "Return of allotment lodged",
};

// AcraSteps shows an approved conversion's ACRA steps and what they raise (⏰ due, ⚠ warning), and records the next one,
// with ACRA's acknowledgement attached through the evidence panel (FR-EQ-09; design D12).
function AcraSteps({ s, onDone, onRead }: { s: Settlement; onDone: (text: string) => void; onRead: () => void }) {
  const id = useId();
  const confirm = useConfirm();
  const [form, setForm] = useState<{ kind: keyof typeof stepWords; on: string; reference: string }>({ kind: "approval_lodged", on: "", reference: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const acra = s.acra;
  if (!acra) return null;
  const record = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-settlements/{settlement_id}/acra-steps", {
        params: { path: { settlement_id: s.id }, header: { "Idempotency-Key": newKey() } },
        body: { kind: form.kind, on: form.on, ...(form.reference.trim() ? { reference: form.reference.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Recorded: ${stepWords[form.kind].toLowerCase()} on ${date(form.on)}.`);
    setForm({ ...form, on: "", reference: "" });
    onRead();
  };
  const latest = (kind: keyof typeof stepWords) => acra.steps.filter((x) => x.kind === kind).at(-1);
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <h3 className="fy-h3" id={`${id}-title`} title={helpText("director-settlement.acra-step").tooltip}>
        ACRA steps
      </h3>
      {acra.flags.map((f) => (
        <p key={f.code} className="fy-row fy-row--wrap gap-2">
          <span className={f.kind === "due" ? "fy-chip fy-chip--serious" : "fy-chip fy-chip--warning"}>
            <Icon of={f.kind === "due" ? ClockAlert : TriangleAlert} />
            {f.kind === "due" ? "Due" : "Review"}
          </span>
          <span className="fy-small fy-wrap">
            {f.message[0]?.toUpperCase() + f.message.slice(1)}. {f.fix}.
          </span>
        </p>
      ))}
      <dl className="fy-dl">
        {(Object.keys(stepWords) as (keyof typeof stepWords)[]).map((k) => {
          const x = latest(k);
          const shown = k === "agm_due" ? acra.agm_due_on : x?.on;
          return (
            <div key={k}>
              <dt>{stepWords[k]}</dt>
              <dd>{shown ? `${date(shown)}${x?.reference ? ` · ${x.reference}` : ""}` : "Not yet"}</dd>
            </div>
          );
        })}
      </dl>
      {problem && <Refusal problem={problem} title="The step was not recorded" />}
      <form className="fy-form fy-form--2" onSubmit={(e) => void record(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            Step
          </label>
          <select className="fy-input" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as keyof typeof stepWords })}>
            {(Object.keys(stepWords) as (keyof typeof stepWords)[]).map((k) => (
              <option key={k} value={k}>
                {stepWords[k]}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            On
          </label>
          <input className="fy-input" type="date" id={`${id}-on`} value={form.on} onChange={(e) => setForm({ ...form, on: e.target.value })} />
        </div>
        <div className="fy-field fy-span-2">
          <label className="fy-label" htmlFor={`${id}-ref`}>
            ACRA reference (optional)
          </label>
          <input className="fy-input" id={`${id}-ref`} maxLength={200} value={form.reference} onChange={(e) => setForm({ ...form, reference: e.target.value })} />
        </div>
        <Button variant="secondary" type="submit" disabled={busy || !form.on}>
          {busy ? "Recording…" : "Record the step"}
        </Button>
      </form>
    </section>
  );
}

// EffectiveForm records an approved conversion as registered by ACRA (FR-EQ-06, FR-EQ-09): the lodgement reference, the
// resolutions and the day ACRA's register shows the allotment, which it posts on, never backdated.
function EffectiveForm({ s, etag, name, onDone, onRead }: { s: Settlement; etag: string; name: string; onDone: (text: string) => void; onRead: () => void }) {
  const id = useId();
  const confirm = useConfirm();
  const [form, setForm] = useState({ lodgement_ref: "", resolution_refs: "", effective_on: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const record = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-settlements/{settlement_id}/effective", {
        params: { path: { settlement_id: s.id }, header: { "If-Match": etag, "Idempotency-Key": key } },
        body: { lodgement_ref: form.lodgement_ref.trim(), resolution_refs: form.resolution_refs.trim(), effective_on: form.effective_on },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`${s.number ?? "The conversion"} is effective: ${name}'s shares are in the register.`);
    onRead();
  };
  return (
    <form className="fy-stack fy-stack--3" onSubmit={(e) => void record(e)} noValidate aria-labelledby={`${id}-title`}>
      <h3 className="fy-h3" id={`${id}-title`} title={helpText("director-settlement.effective").tooltip}>
        When ACRA registers it
      </h3>
      <p className="fy-small">
        Approved, it posts nothing until ACRA's register of members shows the allotment.
        {s.lapses_on && ` The shareholders' approval lapses on ${date(s.lapses_on)}, the date by which the next AGM must be held; after it a new approval is needed.`}
      </p>
      {problem && <Refusal problem={problem} title="The conversion was not recorded" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-ref`}>
          ACRA lodgement reference
        </label>
        <input className="fy-input" id={`${id}-ref`} maxLength={100} value={form.lodgement_ref} onChange={(e) => setForm({ ...form, lodgement_ref: e.target.value })} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-res`}>
          Resolutions
        </label>
        <input className="fy-input" id={`${id}-res`} maxLength={500} value={form.resolution_refs} onChange={(e) => setForm({ ...form, resolution_refs: e.target.value })} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-on`}>
          Registered on
        </label>
        <input className="fy-input" type="date" id={`${id}-on`} value={form.effective_on} onChange={(e) => setForm({ ...form, effective_on: e.target.value })} />
        <span className="fy-help">The day ACRA's register shows the allotment. It posts on that day and is never backdated.</span>
      </div>
      <Button variant="primary" type="submit" disabled={busy || !form.lodgement_ref.trim() || !form.resolution_refs.trim() || !form.effective_on}>
        {busy ? "Recording…" : "Record as registered"}
      </Button>
    </form>
  );
}

export function DirectorSettlement() {
  const { contactId } = useParams({ strict: false }) as { contactId: string };
  const id = useId();
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const people = useQuery({ queryKey: ["director-accounts", ""], queryFn: () => accounts("") });
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const list = useQuery({
    queryKey: ["director-settlements", contactId],
    queryFn: async () => {
      const r = await api.GET("/v1/director-settlements", { params: { query: { contact_id: contactId, page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const [open, setOpen] = useState<{ s: Settlement; etag: string } | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState<{ text: string; approval?: string } | null>(null);
  const [key, setKey] = useState(newKey);

  const person = people.data?.items.find((p) => p.contact_id === contactId);
  const base = people.data?.currency ?? "SGD";
  const codes = new Map((chart.data ?? []).map((a) => [a.id, a]));
  const balances = (person?.balances ?? []).filter((b) => !isZero(b.balance.amount) && b.balance.amount.startsWith("-"));
  const name = person?.legal_name ?? "the person";
  // A route chosen before the balances arrive still lands on the first account the company owes the person on.
  const f = form ? { ...form, account_id: form.account_id || (balances[0]?.account_id ?? "") } : blank(list.data?.default_route ?? "keep", balances[0]?.account_id ?? "");
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm({ ...f, [k]: v });
  const setConv = <K extends keyof Form["conversion"]>(k: K, v: Form["conversion"][K]) => setForm({ ...f, conversion: { ...f.conversion, [k]: v } });
  const editable = mayDraft && (!open || open.s.status === "draft");

  const read = async (settlementId: string) => {
    const r = await api.GET("/v1/director-settlements/{settlement_id}", { params: { path: { settlement_id: settlementId } } });
    if (r.error) return setProblem(problemOf(r.error));
    setOpen({ s: r.data, etag: etagOf(r.response) });
    setForm(formOf(r.data));
  };
  const start = () => {
    setOpen(null);
    setForm(null);
    setErrors({});
    setProblem(null);
    setKey(newKey());
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    const amount = typed(f.amount);
    if (!f.account_id) errs.account = "Choose the account the company owes them on.";
    if (!amount || amount.startsWith("-") || isZero(amount)) errs.amount = `Enter the amount in ${base}, more than zero.`;
    if (f.route === "keep" && (typed(f.interest_rate) === null || f.interest_rate.startsWith("-"))) errs.interest_rate = "Enter the interest a year as a percentage, such as 0.";
    if (f.route === "waive" && !f.credit_account_id) errs.credit = "Choose the account the waiver credits.";
    if (f.route === "waive" && !f.reason.trim()) errs.reason = "Say why they waive it.";
    if (f.route === "convert" && !f.conversion.class_account_id) errs.class = "Choose the class of shares allotted.";
    if (f.route === "convert" && (!typed(f.conversion.price) || isZero(typed(f.conversion.price) ?? "0"))) errs.price = "Enter the price per share, such as 1.20.";
    setErrors(errs);
    if (Object.keys(errs).length > 0 || !amount) return;
    const body = {
      contact_id: contactId,
      account_id: f.account_id,
      amount: { amount, currency: base },
      route: f.route,
      ...(f.route === "keep" ? { terms: { interest_rate: typed(f.interest_rate) ?? "0", ...(f.repay_on ? { repay_on: f.repay_on } : {}), subordinated: f.subordinated } } : {}),
      ...(f.route === "waive" ? { waiver: { credit_account_id: f.credit_account_id, capacity: f.capacity, reason: f.reason.trim() } } : {}),
      ...(f.route === "convert"
        ? {
            conversion: {
              class_account_id: f.conversion.class_account_id,
              price: typed(f.conversion.price) ?? f.conversion.price,
              ...(f.conversion.price_basis ? { price_basis: f.conversion.price_basis } : {}),
              capacity: f.conversion.capacity,
              measurement: f.conversion.measurement,
              ...(f.conversion.measurement === "fair_value"
                ? { fair_value_price: typed(f.conversion.fair_value_price) ?? f.conversion.fair_value_price, difference_account_id: f.conversion.difference_account_id, note: f.conversion.note.trim() }
                : {}),
            },
          }
        : {}),
    };
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      open
        ? api.PUT("/v1/director-settlements/{settlement_id}", { params: { path: { settlement_id: open.s.id }, header: { "If-Match": open.etag } }, body })
        : api.POST("/v1/director-settlements", { params: { header: { "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setOpen({ s: out.data, etag: etagOf(out.response) });
    await queries.invalidateQueries({ queryKey: ["director-settlements", contactId] });
  };

  const ask = async () => {
    if (!open) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-settlements/{settlement_id}/approval", { params: { path: { settlement_id: open.s.id }, header: { "If-Match": open.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["director-settlements", contactId] });
    await queries.invalidateQueries({ queryKey: ["approvals"] });
    setStatus({ text: `Asked for approval. Another person approves it, never ${name}.`, approval: out.data.id });
    await read(open.s.id);
  };

  const step = !open ? 1 : open.s.status === "draft" ? (open.s.agreements > 0 ? 3 : 2) : open.s.status === "waiting" ? 3 : 4;
  const field = (k: string) => (errors[k] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${k}-error` } : {});
  const error = (k: string) =>
    errors[k] && (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    );
  const h = helpText("accounting.director-settlement");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <Link to="/accounting/directors">Director accounts</Link>
            <span aria-hidden="true">›</span>
            <span>Settle</span>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1" title={h.tooltip}>
              Settle {name}'s balance
            </h1>
            {open && <StatusChip s={open.s} />}
          </div>
          {person && (
            <p className="fy-secondary">
              {balances.length === 0
                ? `The company owes ${name} nothing today.`
                : balances.map((b) => `${formatAmount(owed(b.balance.amount))} ${base} owed on ${b.role === "director-loan" ? "the loan from director" : "deferred pay and fees"}`).join(" · ")}
            </p>
          )}
        </div>
      </div>

      {status && (
        <p className="fy-small" role="status">
          {status.text}{" "}
          {status.approval && (
            <Link to="/approvals/$approvalId" params={{ approvalId: status.approval }}>
              Open the request
            </Link>
          )}
        </p>
      )}
      {people.isError && <Refusal problem={problemOf(people.error)} title="The director accounts could not be shown" />}
      {list.isError && <Refusal problem={problemOf(list.error)} title="The settlements could not be listed" />}
      {problem && <Refusal problem={problem} title="The settlement was not saved" />}

      <ol className="fy-steps fy-steps--wrap" aria-label="Settlement progress">
        {["Route", "Details", "Evidence and approval", f.route === "convert" ? "Effective" : "Decided"].map((label, i) => (
          <li key={label} className={i + 1 < step ? "fy-step fy-step--done" : "fy-step"} aria-current={i + 1 === step ? "step" : undefined}>
            <span className="fy-step__n">{i + 1}</span>
            {label}
          </li>
        ))}
      </ol>

      <div className="fy-split">
        <div className="fy-stack fy-stack--6">
          <form className="fy-stack fy-stack--6" onSubmit={save} noValidate>
            <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-route`}>
              <h2 className="fy-h3" id={`${id}-route`}>
                How the balance is settled
              </h2>
              <p className="fy-small fy-muted">General information about each route, not advice. Your accountant confirms what suits the company.</p>
              <div className="fy-stack fy-stack--2" role="radiogroup" aria-labelledby={`${id}-route`}>
                {routes.map((r) => {
                  const disabled = !!r.later || !editable;
                  return (
                    <button
                      key={r.route}
                      type="button"
                      className="fy-choice"
                      role="radio"
                      aria-checked={f.route === r.route}
                      aria-disabled={disabled || undefined}
                      disabled={disabled}
                      onClick={() => r.route !== "safe" && set("route", r.route)}
                    >
                      <span className="fy-radio" />
                      <span className="fy-stack fy-grow gap-1">
                        <span className="fy-row fy-row--wrap gap-2">
                          <span className="fy-strong">{r.title}</span>
                          {r.later && (
                            <span className="fy-chip fy-chip--neutral">
                              <Icon of={Clock} />
                              {r.later}
                            </span>
                          )}
                        </span>
                        <span className="fy-small fy-secondary">{r.words}</span>
                      </span>
                    </button>
                  );
                })}
              </div>
            </section>

            <section className="fy-card" aria-labelledby={`${id}-details`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-details`}>
                  {routeTitle(f.route)}: details
                </h2>
              </div>
              <fieldset className="fy-card__body fy-form fy-form--2" disabled={!editable}>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-account`}>
                    Owed on
                  </label>
                  <select className="fy-input" id={`${id}-account`} value={f.account_id} onChange={(e) => set("account_id", e.target.value)} {...field("account")}>
                    <option value="">Choose</option>
                    {(person?.balances ?? []).map((b) => (
                      <option key={b.account_id} value={b.account_id}>
                        {b.code} {b.role === "director-loan" ? "Loan from director / shareholder" : "Deferred remuneration payable"} · {formatAmount(owed(b.balance.amount))} owed
                      </option>
                    ))}
                  </select>
                  {error("account")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-amount`}>
                    Amount ({base})
                  </label>
                  <input className="fy-input fy-num" inputMode="decimal" id={`${id}-amount`} value={f.amount} onChange={(e) => set("amount", e.target.value)} {...field("amount")} />
                  {error("amount")}
                </div>
                {f.route === "keep" && (
                  <>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-rate`}>
                        Interest a year (%)
                      </label>
                      <input className="fy-input fy-num" inputMode="decimal" id={`${id}-rate`} value={f.interest_rate} onChange={(e) => set("interest_rate", e.target.value)} {...field("interest_rate")} />
                      {error("interest_rate")}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-repay-on`}>
                        Repaid on (optional)
                      </label>
                      <input className="fy-input" type="date" id={`${id}-repay-on`} value={f.repay_on} onChange={(e) => set("repay_on", e.target.value)} />
                      <span className="fy-help">Left empty, it is repayable on demand.</span>
                    </div>
                    <label className="fy-row gap-2">
                      <input type="checkbox" checked={f.subordinated} onChange={(e) => set("subordinated", e.target.checked)} />
                      <span className="fy-small">Subordinated to the company's other creditors</span>
                    </label>
                  </>
                )}
                {f.route === "repay" && (
                  <p className="fy-small fy-wrap">
                    {open?.s.repay_to
                      ? `Paid into ${open.s.repay_to.bic} ••••${open.s.repay_to.account_number_last4} (${open.s.repay_to.account_name}) by the next payment batch.`
                      : `Paid by the next payment batch into ${name}'s approved bank account: their own if a member is linked to them, else their contact's bank details.`}
                  </p>
                )}
                {f.route === "waive" && (
                  <>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-capacity`}>
                        They waive it as
                      </label>
                      <select className="fy-input" id={`${id}-capacity`} value={f.capacity} onChange={(e) => set("capacity", e.target.value as Form["capacity"])}>
                        <option value="shareholder">A shareholder: a capital contribution</option>
                        <option value="other">Otherwise: income of the company</option>
                      </select>
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-credit`}>
                        Credit to
                      </label>
                      <select className="fy-input" id={`${id}-credit`} value={f.credit_account_id} onChange={(e) => set("credit_account_id", e.target.value)} {...field("credit")}>
                        <option value="">Choose</option>
                        {(chart.data ?? []).filter(creditable).map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.code} {a.name}
                          </option>
                        ))}
                      </select>
                      {error("credit")}
                    </div>
                    <div className="fy-field fy-span-2">
                      <label className="fy-label" htmlFor={`${id}-reason`}>
                        Why they waive it
                      </label>
                      <input className="fy-input" id={`${id}-reason`} maxLength={1000} value={f.reason} onChange={(e) => set("reason", e.target.value)} {...field("reason")} />
                      {error("reason")}
                    </div>
                  </>
                )}
                {f.route === "convert" && (
                  <>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-class`}>
                        Class of shares
                      </label>
                      <select className="fy-input" id={`${id}-class`} value={f.conversion.class_account_id} onChange={(e) => setConv("class_account_id", e.target.value)} {...field("class")}>
                        <option value="">Choose</option>
                        {(chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group).map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.code} {a.name}
                          </option>
                        ))}
                      </select>
                      {error("class")}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-price`}>
                        Price per share ({base})
                      </label>
                      <input className="fy-input fy-num" inputMode="decimal" id={`${id}-price`} value={f.conversion.price} onChange={(e) => setConv("price", e.target.value)} {...field("price")} />
                      {error("price")}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-basis`}>
                        Price basis
                      </label>
                      <select className="fy-input" id={`${id}-basis`} value={f.conversion.price_basis} onChange={(e) => setConv("price_basis", e.target.value as Form["conversion"]["price_basis"])}>
                        <option value="">The organisation's default</option>
                        {Object.entries(priceBases).map(([k, v]) => (
                          <option key={k} value={k}>
                            {v}
                          </option>
                        ))}
                      </select>
                      <span className="fy-help">A price away from the latest round or a valuation moves value between shareholders.</span>
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-as`}>
                        They convert as
                      </label>
                      <select className="fy-input" id={`${id}-as`} value={f.conversion.capacity} onChange={(e) => setConv("capacity", e.target.value as Form["conversion"]["capacity"])}>
                        {Object.entries(capacities).map(([k, v]) => (
                          <option key={k} value={k}>
                            {v}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div className="fy-field fy-expert-only">
                      <label className="fy-label" htmlFor={`${id}-measure`}>
                        Measurement basis
                      </label>
                      <select className="fy-input" id={`${id}-measure`} value={f.conversion.measurement} onChange={(e) => setConv("measurement", e.target.value as Form["conversion"]["measurement"])}>
                        <option value="carrying">The carrying amount (the default)</option>
                        <option value="fair_value">The shares' fair value, the difference in profit or loss</option>
                      </select>
                    </div>
                    {f.conversion.measurement === "fair_value" && (
                      <>
                        <div className="fy-field">
                          <label className="fy-label" htmlFor={`${id}-fair`}>
                            Fair value per share ({base})
                          </label>
                          <input className="fy-input fy-num" inputMode="decimal" id={`${id}-fair`} value={f.conversion.fair_value_price} onChange={(e) => setConv("fair_value_price", e.target.value)} />
                        </div>
                        <div className="fy-field">
                          <label className="fy-label" htmlFor={`${id}-difference`}>
                            Difference to
                          </label>
                          <select className="fy-input" id={`${id}-difference`} value={f.conversion.difference_account_id} onChange={(e) => setConv("difference_account_id", e.target.value)}>
                            <option value="">Choose</option>
                            {(chart.data ?? []).filter((a) => a.active && !a.group && !a.money_account && (a.type === "revenue" || a.type === "expense")).map((a) => (
                              <option key={a.id} value={a.id}>
                                {a.code} {a.name}
                              </option>
                            ))}
                          </select>
                        </div>
                        <div className="fy-field fy-span-2">
                          <label className="fy-label" htmlFor={`${id}-note`}>
                            The accountant's note
                          </label>
                          <input className="fy-input" id={`${id}-note`} maxLength={1000} value={f.conversion.note} onChange={(e) => setConv("note", e.target.value)} />
                        </div>
                      </>
                    )}
                  </>
                )}
              </fieldset>
              {editable && (
                <div className="fy-card__foot fy-row justify-end gap-2">
                  {open && (
                    <Button variant="ghost" type="button" onClick={start}>
                      <Icon of={Plus} />
                      New settlement
                    </Button>
                  )}
                  <Button variant="primary" type="submit" disabled={busy || balances.length === 0}>
                    {busy ? "Saving…" : open ? "Save changes" : "Save the draft"}
                  </Button>
                </div>
              )}
            </section>
          </form>

          {open && open.s.checks.length > 0 && (
            <ul className="fy-stack fy-stack--1" aria-label="What stops it">
              {open.s.checks.map((c, i) => (
                <li key={i} className="fy-small fy-error">
                  {c.message}. {c.fix}.
                </li>
              ))}
            </ul>
          )}
          {open?.s.shares && (
            <section className="fy-card fy-card--pad fy-row fy-row--wrap fy-row--between gap-3" aria-label="Shares allotted">
              <span className="fy-stack" style={{ gap: 0 }}>
                <span className="fy-small fy-muted">Shares to allot</span>
                <span className="fy-num fy-strong">{Number.isSafeInteger(Number(open.s.shares.shares)) ? Number(open.s.shares.shares).toLocaleString("en-SG") : open.s.shares.shares}</span>
              </span>
              <span className="fy-small fy-muted">
                Rounded down to whole shares<span className="fy-expert-only"> (RND-SHARE)</span>; {formatAmount(open.s.shares.remainder.amount)} {open.s.shares.remainder.currency} stays owed
              </span>
            </section>
          )}
          {open && <Impact s={open.s} codes={codes} name={name} />}
          {open?.s.repaid && (
            <p className="fy-small">
              Repaid by payment batches so far: {formatAmount(open.s.repaid.amount)} {open.s.repaid.currency} of {formatAmount(open.s.amount.amount)} {open.s.amount.currency}.
            </p>
          )}
        </div>

        <aside className="fy-stack fy-stack--6" aria-label="Evidence and approvals">
          {open ? (
            <>
              <EvidencePanel record={{ kind: "director-settlement", id: open.s.id, etag: open.etag }} draft={open.s.status === "draft"} mayAttach={mayDraft && open.s.status === "draft"} onChanged={() => void read(open.s.id)} />
              <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-approvals`}>
                <h2 className="fy-h3" id={`${id}-approvals`}>
                  Approval
                </h2>
                <p className="fy-small">
                  {name} benefits, so they never approve it. Another person who holds the approval right on director accounts approves it.
                  {open.s.accountant_review && " Every other person who could approve also benefits from a settlement of their own, so the accountant reviews it as well."}
                </p>
                {open.s.status === "draft" && mayDraft && (
                  <Button variant="primary" type="button" disabled={busy || open.s.agreements === 0} onClick={() => void ask()} title={helpText("director-settlement.ask").tooltip}>
                    <Icon of={Send} />
                    {busy ? "Asking…" : "Ask for approval"}
                  </Button>
                )}
                {open.s.status === "waiting" && open.s.approval_asked && (
                  <Link to="/approvals/$approvalId" params={{ approvalId: open.s.approval_asked }}>
                    Open the request
                  </Link>
                )}
                {open.s.status === "decided" && open.s.route !== "convert" && <p className="fy-small">Decided as {open.s.number}.</p>}
                {open.s.status === "decided" && open.s.route === "convert" && mayDraft && (
                  <>
                    <AcraSteps s={open.s} onDone={(text) => setStatus({ text })} onRead={() => void read(open.s.id)} />
                    <EffectiveForm s={open.s} etag={open.etag} name={name} onDone={(text) => setStatus({ text })} onRead={() => void read(open.s.id)} />
                  </>
                )}
                {open.s.status === "effective" && open.s.share_issue && (
                  <p className="fy-small">
                    {open.s.number} is effective from {date(open.s.share_issue.effective_on)}: {open.s.share_issue.shares} shares in the register, ACRA's reference {open.s.share_issue.lodgement_ref}.
                  </p>
                )}
              </section>
            </>
          ) : (
            <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-before`}>
              <h2 className="fy-h3" id={`${id}-before`}>
                Evidence and approval
              </h2>
              <p className="fy-small">Save the draft first. Then attach the loan note, board minute or deed, and ask another person to approve it.</p>
            </section>
          )}

          <section className="fy-card" aria-labelledby={`${id}-list`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-list`}>
                {name}'s settlements
              </h2>
            </div>
            {(list.data?.items ?? []).length === 0 ? (
              <p className="fy-card__body fy-small fy-muted">None yet.</p>
            ) : (
              <ul className="fy-list">
                {(list.data?.items ?? []).map((s) => (
                  <li key={s.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <Button variant="ghost" size="sm" type="button" className="justify-start" onClick={() => void read(s.id)} aria-current={open?.s.id === s.id || undefined}>
                        {s.number ?? "Draft"} · {routeTitle(s.route)}
                      </Button>
                      <span className="fy-list__meta">
                        {formatAmount(s.amount.amount)} {s.amount.currency} · {date(s.date)}
                      </span>
                    </span>
                    <StatusChip s={s} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </aside>
      </div>
    </div>
  );
}
