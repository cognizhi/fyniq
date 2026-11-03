import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BadgeDollarSign, Building2, CircleCheck, FileDown, Handshake, Link2, ScrollText, Send, Stamp, TriangleAlert, UserRound, Wallet } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, month } from "../../format/date";
import { isZero, negate, sign, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { save } from "../reports/AccountantExports";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { originLabel, payChoiceLabel, payChoices, payChoiceWords, type DirectorAccount, type DirectorStatement, type PayChoice } from "./data";

// The roles that link a director or shareholder to the member who signs in as them: A on "Director accounts,
// settlements, share issues" (Appendix D). Another of them approves it (FM-1).
const linkers = ["owner", "controller"];

// owed is a balance as a founder reads it: what the company owes them is positive (the ledger's credit).
export const owed = (amount: string) => (isZero(amount) ? "0.00" : negate(amount));

// standing says in words where a person stands on an account, or overall.
export function standing(amount: string, name: string): string {
  if (isZero(amount)) return "Nothing owed either way";
  return sign(amount) < 0 ? `The company owes ${name}` : `${name} owes the company`;
}

const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase())
    .join("");

type ExemptInForce = components["schemas"]["ExemptStatusInForce"];

async function accounts(asAt: string): Promise<{ as_at: string; currency: string; items: DirectorAccount[]; unkeyed: string; exempt_status?: ExemptInForce }> {
  const items: DirectorAccount[] = [];
  let cursor: string | undefined;
  let head: { as_at: string; currency: string; unkeyed: string; exempt_status?: ExemptInForce } = { as_at: asAt, currency: "", unkeyed: "0.00" };
  do {
    const r = await api.GET("/v1/director-accounts", { params: { query: { as_at: asAt || undefined, page_size: 500, cursor } } });
    if (r.error) throw r.error;
    head = { as_at: r.data.as_at, currency: r.data.currency, unkeyed: r.data.unkeyed.amount, exempt_status: r.data.exempt_status };
    items.push(...(r.data.items ?? []));
    cursor = r.data.next_cursor;
  } while (cursor);
  return { ...head, items };
}

// YearEndLine says whether the person confirmed their balances at the latest financial year end (FR-EQ-12).
export function YearEndLine({ p }: { p: DirectorAccount }) {
  const y = p.year_end;
  if (!y) return null;
  if (y.status === "confirmed") {
    return (
      <p className="fy-small">
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Confirmed
        </span>{" "}
        Balances at {date(y.year_end)} confirmed by passkey{y.confirmed_at ? `, ${date(y.confirmed_at)}` : ""}.
      </p>
    );
  }
  return (
    <p className="fy-row fy-row--wrap gap-2">
      <span className="fy-chip fy-chip--warning">
        <Icon of={TriangleAlert} />
        {y.status === "changed_since" ? "Changed since confirmed" : "Not confirmed"}
      </span>
      <span className="fy-small fy-wrap">
        {y.status === "changed_since" ? `An entry dated before ${date(y.year_end)} was posted after the confirmation: ` : `Balances at ${date(y.year_end)} are not confirmed: `}
        {p.member_user_id ? "the person confirms them with a passkey in Money I put in." : "link the member who signs in as them first, then they confirm."}
      </span>
    </p>
  );
}

function PersonCard({ p, onStatement, onLink, onPay, onFees, mayLink }: { p: DirectorAccount; onStatement: () => void; onLink: () => void; onPay: () => void; onFees: () => void; mayLink: boolean }) {
  const roles = [p.director && "Director", p.shareholder && "Shareholder"].filter(Boolean).join(", ");
  const statement = helpText("director-accounts.statement");
  const link = helpText("director-accounts.link-member");
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`da-${p.contact_id}`}>
      <div className="fy-row gap-3">
        <span className="fy-avatar" aria-hidden="true">
          {initials(p.legal_name)}
        </span>
        <div className="fy-stack" style={{ gap: 0 }}>
          <h2 className="fy-h3" id={`da-${p.contact_id}`}>
            {p.legal_name}
          </h2>
          <span className="fy-small fy-muted">
            {roles}
            {p.active ? "" : ", inactive"}
          </span>
        </div>
      </div>
      <dl className="fy-dl">
        {(p.balances ?? []).map((b) => (
          <div key={b.account_id}>
            <dt>
              {b.role === "director-loan" ? "Loan and costs" : "Deferred pay and fees"}
              <span className="fy-expert-only"> · {b.code}</span>
            </dt>
            <dd className="fy-num" title={standing(b.balance.amount, p.legal_name)}>
              {formatAmount(owed(b.balance.amount))}
            </dd>
          </div>
        ))}
      </dl>
      <p className="fy-small">
        <span className="fy-strong">{standing(p.net.amount, p.legal_name)}</span>
        {!isZero(p.net.amount) && ` ${formatAmount(owed(p.net.amount).replace(/^-/, ""))} ${p.net.currency}`}
      </p>
      {p.in_debit && (
        <div className="fy-row fy-row--wrap gap-2">
          <span className="fy-chip fy-chip--warning">
            <Icon of={TriangleAlert} />
            Account in debit
          </span>
          <span className="fy-small fy-wrap">{p.debit_warning ?? "A loan to a director is restricted unless the company is an exempt private company. Ask your accountant."}</span>
        </div>
      )}
      {p.year_end && <YearEndLine p={p} />}
      {p.pay_choice && (
        <p className="fy-row fy-row--wrap fy-row--between gap-2">
          <span className="fy-small">
            <span className="fy-strong">Pay: {payChoiceLabel(p.pay_choice.choice)}</span>
            {p.pay_choice.default ? " · the default: no choice recorded" : ` · from ${month((p.pay_choice.from ?? "").slice(0, 7))}, signed decision recorded ${date(p.pay_choice.recorded_at ?? "")}`}
          </span>
          {mayLink && (
            <Button variant="ghost" size="sm" type="button" onClick={onPay} title={helpText("director-accounts.pay-choice").tooltip}>
              <Icon of={Wallet} />
              {helpText("director-accounts.pay-choice").label}
            </Button>
          )}
        </p>
      )}
      <div className="fy-row fy-row--wrap fy-row--between gap-2">
        <span className="fy-small fy-muted">{p.member_user_id ? "Signs in as a member: sees their own account" : "No member signs in as them"}</span>
        <span className="fy-row gap-2">
          {mayLink && p.director && (
            <Button variant="ghost" size="sm" type="button" onClick={onFees} title={helpText("director-accounts.fees").tooltip}>
              <Icon of={BadgeDollarSign} />
              {helpText("director-accounts.fees").label}
            </Button>
          )}
          {mayLink && (
            <Button variant="ghost" size="sm" type="button" onClick={onLink} title={link.tooltip}>
              <Icon of={Link2} />
              {link.label}
            </Button>
          )}
          <Button variant="secondary" size="sm" type="button" onClick={onStatement} title={statement.tooltip}>
            <Icon of={ScrollText} />
            {statement.label}
          </Button>
        </span>
      </div>
    </section>
  );
}

// Statement is a person's statement from one day to another, owed to them shown positive, with its PDF for a balance
// confirmation and its CSV (FR-EQ-01).
function Statement({ person, until }: { person: DirectorAccount; until: string }) {
  const id = useId();
  const [from, setFrom] = useState(`${until.slice(0, 4)}-01-01`);
  const [to, setTo] = useState(until);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState("");
  const exp = helpText("director-accounts.export");
  const q = useQuery({
    queryKey: ["director-statement", person.contact_id, from, to],
    enabled: /^\d{4}-\d{2}-\d{2}$/.test(from) && /^\d{4}-\d{2}-\d{2}$/.test(to),
    retry: false,
    queryFn: async (): Promise<DirectorStatement> => {
      const r = await api.GET("/v1/director-accounts/{contact_id}/statement", { params: { path: { contact_id: person.contact_id }, query: { from, to } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const download = async (format: "pdf" | "csv") => {
    setBusy(format);
    setProblem(null);
    const r = await api.GET("/v1/director-accounts/{contact_id}/statement/export", { params: { path: { contact_id: person.contact_id }, query: { from, to, format } } });
    setBusy("");
    if (r.error) return setProblem(problemOf(r.error));
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
  };
  const s = q.data;
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head" style={{ flexWrap: "wrap" }}>
        <h2 className="fy-h3" id={`${id}-title`}>
          Statement · {person.legal_name}
        </h2>
        <span className="fy-row gap-2">
          <Button variant="ghost" size="sm" type="button" disabled={!!busy || !s} onClick={() => download("pdf")} title={exp.tooltip}>
            <Icon of={FileDown} />
            {busy === "pdf" ? "Making the PDF…" : "PDF for confirmation"}
          </Button>
          <Button variant="ghost" size="sm" type="button" disabled={!!busy || !s} onClick={() => download("csv")}>
            <Icon of={FileDown} />
            CSV
          </Button>
        </span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <input className="fy-input" type="date" id={`${id}-from`} value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              To
            </label>
            <input className="fy-input" type="date" id={`${id}-to`} value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
        </div>
        {problem && <Refusal problem={problem} title="The statement was not exported" />}
        {q.isError && <Refusal problem={problemOf(q.error)} title="The statement could not be worked out" />}
        {q.isPending && q.fetchStatus !== "idle" && <p className="fy-secondary">Working out the statement…</p>}
      </div>
      {s && (
        <>
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-statement">
              <caption className="fy-sr">
                What the company owes {s.legal_name}, {date(s.from)} to {date(s.to)}, by where it came from
              </caption>
              <thead>
                <tr>
                  <th scope="col">Owed to {s.legal_name.split(" ")[0]} ({s.currency})</th>
                  {(s.accounts ?? []).map((a) => (
                    <th key={a.account_id} scope="col" className="fy-num">
                      {a.name}
                      <span className="fy-expert-only"> · {a.code}</span>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>Opening, {date(s.from)}</td>
                  {(s.accounts ?? []).map((a) => (
                    <td key={a.account_id} className="fy-num">
                      {formatAmount(owed(a.opening.amount))}
                    </td>
                  ))}
                </tr>
                {[...new Set((s.accounts ?? []).flatMap((a) => (a.by_origin ?? []).map((o) => o.origin)))].map((origin) => (
                  <tr key={origin}>
                    <td className="fy-sub">{originLabel(origin)}</td>
                    {(s.accounts ?? []).map((a) => {
                      const t = (a.by_origin ?? []).find((o) => o.origin === origin);
                      return (
                        <td key={a.account_id} className="fy-num">
                          {t ? formatAmount(owed(t.amount.amount)) : "—"}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td>Closing, {date(s.to)}</td>
                  {(s.accounts ?? []).map((a) => (
                    <td key={a.account_id} className="fy-num">
                      {formatAmount(owed(a.closing.amount))}
                    </td>
                  ))}
                </tr>
              </tfoot>
            </table>
          </div>
          {(s.movements ?? []).length > 0 && (
            <details className="fy-card__body">
              <summary className="fy-small">Every movement ({(s.movements ?? []).length})</summary>
              <ul className="fy-list">
                {(s.movements ?? []).map((m, i) => (
                  <li key={`${m.entry_id}-${i}`} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {date(m.date)} · {originLabel(m.origin)}
                        {m.reversal ? " (reversed)" : ""}
                      </span>
                      <span className="fy-list__meta whitespace-normal">{[m.number, m.description || m.memo].filter(Boolean).join(" · ")}</span>
                    </span>
                    <span className="fy-num">{formatAmount(owed(m.amount.amount))}</span>
                  </li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}
    </section>
  );
}

// LinkMember asks for a director or shareholder to be linked to the member who signs in as them; another person who
// holds the approval right approves it, never the member (FR-EQ-01, FM-1).
function LinkMember({ person, onClose, onAsked }: { person: DirectorAccount | null; onClose: () => void; onAsked: (what: string, approval: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [member, setMember] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const members = useQuery({
    queryKey: ["members"],
    enabled: !!person,
    queryFn: async () => {
      const r = await api.GET("/v1/members");
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  if (!person) return null;
  const close = () => {
    setMember("");
    setProblem(null);
    onClose();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const contact = await api.GET("/v1/contacts/{contact_id}", { params: { path: { contact_id: person.contact_id } } });
    if (contact.error) {
      setBusy(false);
      return setProblem(problemOf(contact.error));
    }
    const etag = etagOf(contact.response);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.PUT("/v1/contacts/{contact_id}/member", {
        params: { path: { contact_id: person.contact_id }, header: { "If-Match": etag, "Idempotency-Key": key } },
        body: member ? { member_user_id: member } : {},
      }),
    );
    setBusy(false);
    if (out.error) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["approvals"] });
    onAsked(`Asked for ${person.legal_name}'s link. Another person who holds the approval right approves it.`, out.data.id);
    close();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && close()} title={`Who signs in as ${person.legal_name}`} description="They see their own account in Money I put in, and never approve their own settlement. Another person approves the link.">
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {problem && <Refusal problem={problem} title="The link was not asked for" />}
        {members.isError && <Refusal problem={problemOf(members.error)} title="The members could not be read" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-member`}>
            Member
          </label>
          <select className="fy-select" id={`${id}-member`} value={member} onChange={(e) => setMember(e.target.value)}>
            <option value="">{person.member_user_id ? "Nobody: unlink them" : "Choose a member"}</option>
            {(members.data ?? []).map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.display_name}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy || (!member && !person.member_user_id)}>
            <Icon of={Handshake} />
            {busy ? "Asking…" : "Ask for approval"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// PayChoiceDialog drafts a person's pay choice from a month on, takes the signed decision as its evidence, and records
// it (FR-EQ-04): append-only, superseding the one before from its month.
function PayChoiceDialog({ person, onClose, onDone }: { person: DirectorAccount | null; onClose: () => void; onDone: (said: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [from, setFrom] = useState("");
  const [choice, setChoice] = useState<PayChoice | "">("");
  const [note, setNote] = useState("");
  const [draft, setDraft] = useState<{ id: string; etag: string; decisions: number } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  if (!person) return null;
  const close = () => {
    setDraft(null);
    setProblem(null);
    onClose();
  };
  const reread = async (choiceId: string) => {
    const r = await api.GET("/v1/director-pay-choices/{choice_id}", { params: { path: { choice_id: choiceId } } });
    if (r.error) return setProblem(problemOf(r.error));
    setDraft({ id: choiceId, etag: etagOf(r.response), decisions: r.data.decisions });
  };
  const save = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!/^\d{4}-\d{2}$/.test(from)) errs.from = "Choose the month it applies from.";
    if (!choice) errs.choice = "Choose the pay choice.";
    setErrors(errs);
    if (!choice || Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-pay-choices", {
        params: { header: { "Idempotency-Key": key } },
        body: { contact_id: person.contact_id, from: `${from}-01`, choice, ...(note.trim() ? { note: note.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setDraft({ id: out.data.id, etag: etagOf(out.response), decisions: out.data.decisions });
  };
  const record = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-pay-choices/{choice_id}/record", { params: { path: { choice_id: draft.id }, header: { "If-Match": draft.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["director-accounts"] });
    onDone(`Recorded ${person.legal_name}'s pay choice: ${payChoiceLabel(out.data.choice)} from ${month(out.data.from.slice(0, 7))}.`);
    close();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && close()} title={`${person.legal_name}'s pay choice`} description="From a month on, with the signed decision. It supersedes the choice before it from that month; with none recorded, it is no salary.">
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The pay choice was not saved" />}
        {!draft ? (
          <form className="fy-stack fy-stack--3" onSubmit={save} noValidate>
            <div className="fy-form fy-form--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-from`}>
                  From the month
                </label>
                <input className="fy-input" type="month" id={`${id}-from`} value={from} onChange={(e) => setFrom(e.target.value)} aria-invalid={errors.from ? true : undefined} aria-describedby={errors.from ? `${id}-from-error` : undefined} />
                {errors.from && (
                  <span className="fy-error" id={`${id}-from-error`}>
                    {errors.from}
                  </span>
                )}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-choice`}>
                  Pay choice
                </label>
                <select className="fy-select" id={`${id}-choice`} value={choice} onChange={(e) => setChoice(e.target.value as PayChoice)} aria-invalid={errors.choice ? true : undefined} aria-describedby={errors.choice ? `${id}-choice-error` : undefined}>
                  <option value="">Choose a pay choice</option>
                  {payChoices.map((c) => (
                    <option key={c} value={c}>
                      {payChoiceWords[c]}
                    </option>
                  ))}
                </select>
                {errors.choice && (
                  <span className="fy-error" id={`${id}-choice-error`}>
                    {errors.choice}
                  </span>
                )}
              </div>
            </div>
            {choice === "salary_deferred" && (
              <p className="fy-small fy-wrap">
                <Icon of={TriangleAlert} /> CPF is still due at the end of each month, and the salary is taxed, even though it is not paid. Ask your accountant first.
              </p>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-note`}>
                Note (optional)
              </label>
              <textarea className="fy-textarea" rows={2} maxLength={1000} id={`${id}-note`} value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={close}>
                Cancel
              </Button>
              <Button variant="primary" type="submit" disabled={busy}>
                {busy ? "Saving…" : "Next: attach the signed decision"}
              </Button>
            </div>
          </form>
        ) : (
          <>
            <EvidencePanel record={{ kind: "pay-choice", id: draft.id, etag: draft.etag }} draft mayAttach onChanged={() => void reread(draft.id)} />
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={close}>
                Later
              </Button>
              <Button variant="primary" type="button" disabled={busy || draft.decisions === 0} onClick={record}>
                <Icon of={Stamp} />
                {busy ? "Recording…" : "Record the pay choice"}
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

// FeesDialog lists a director's fees and records a new one (FR-EQ-04): drafted with the shareholders' resolution
// attached, then sent for another person's approval, which posts it Dr 6050 · Cr 2120 to the director; never the
// director's own approval.
function FeesDialog({ person, base, onClose, onAsked }: { person: DirectorAccount | null; base: string; onClose: () => void; onAsked: (text: string, approval: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState({ entitled_on: "", period_from: "", period_to: "", amount: "", memo: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [draft, setDraft] = useState<{ id: string; etag: string; resolutions: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const fees = useQuery({
    queryKey: ["director-fees", person?.contact_id],
    enabled: !!person,
    queryFn: async () => {
      const r = await api.GET("/v1/director-fees", { params: { query: { contact_id: person?.contact_id, page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  if (!person) return null;
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  const reread = async (feeId: string) => {
    const r = await api.GET("/v1/director-fees/{fee_id}", { params: { path: { fee_id: feeId } } });
    if (r.error) return setProblem(problemOf(r.error));
    setDraft({ id: feeId, etag: etagOf(r.response), resolutions: r.data.resolutions });
  };
  const save = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    const day = /^\d{4}-\d{2}-\d{2}$/;
    if (!day.test(form.entitled_on)) errs.entitled_on = "Enter the day of the general meeting that voted it.";
    if (!day.test(form.period_from) || !day.test(form.period_to)) errs.period = "Enter the first and last day of the period it is for.";
    else if (form.period_to < form.period_from) errs.period = "The period ends before it starts.";
    const amount = typed(form.amount);
    if (!amount || amount.startsWith("-") || isZero(amount)) errs.amount = `Enter the fee in ${base}, more than zero.`;
    setErrors(errs);
    if (Object.keys(errs).length > 0 || !amount) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-fees", {
        params: { header: { "Idempotency-Key": key } },
        body: {
          contact_id: person.contact_id,
          entitled_on: form.entitled_on,
          period_from: form.period_from,
          period_to: form.period_to,
          amount: { amount, currency: base },
          ...(form.memo.trim() ? { memo: form.memo.trim() } : {}),
        },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setDraft({ id: out.data.id, etag: etagOf(out.response), resolutions: out.data.resolutions });
  };
  const ask = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/director-fees/{fee_id}/post-approval", { params: { path: { fee_id: draft.id }, header: { "If-Match": draft.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["director-fees"] });
    await queries.invalidateQueries({ queryKey: ["approvals"] });
    onAsked(`Asked for approval of ${person.legal_name}'s fee. Another person approves it, never ${person.legal_name}.`, out.data.id);
    onClose();
  };
  const error = (k: string) =>
    errors[k] && (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`${person.legal_name}'s directors' fees`} description="A fee the shareholders voted posts to 6050 Directors' fees and is owed to the director on 2120, once another person approves it.">
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The fee was not saved" />}
        {fees.isError && <Refusal problem={problemOf(fees.error)} title="The fees could not be listed" />}
        {(fees.data ?? []).length > 0 && (
          <ul className="fy-list" aria-label="Fees">
            {(fees.data ?? []).map((f) => (
              <li key={f.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {f.number ?? "Draft"} · {f.memo || `Fee for ${date(f.period_from)} to ${date(f.period_to)}`}
                  </span>
                  <span className="fy-list__meta">
                    Voted {date(f.entitled_on)} · {f.status === "posted" ? "Posted" : f.status === "waiting" ? "Waiting for approval" : "Draft"}
                  </span>
                </span>
                <span className="fy-num">{formatAmount(f.amount.amount)}</span>
              </li>
            ))}
          </ul>
        )}
        {!draft ? (
          <form className="fy-stack fy-stack--3" onSubmit={save} noValidate>
            <div className="fy-form fy-form--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-entitled`}>
                  Voted on
                </label>
                <input className="fy-input" type="date" id={`${id}-entitled`} value={form.entitled_on} onChange={set("entitled_on")} aria-invalid={errors.entitled_on ? true : undefined} aria-describedby={errors.entitled_on ? `${id}-entitled_on-error` : undefined} />
                {error("entitled_on")}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-amount`}>
                  Fee ({base})
                </label>
                <input className="fy-input fy-num" inputMode="decimal" id={`${id}-amount`} value={form.amount} onChange={set("amount")} aria-invalid={errors.amount ? true : undefined} aria-describedby={errors.amount ? `${id}-amount-error` : undefined} />
                {error("amount")}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-from`}>
                  For the period from
                </label>
                <input className="fy-input" type="date" id={`${id}-from`} value={form.period_from} onChange={set("period_from")} aria-invalid={errors.period ? true : undefined} aria-describedby={errors.period ? `${id}-period-error` : undefined} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-to`}>
                  To
                </label>
                <input className="fy-input" type="date" id={`${id}-to`} value={form.period_to} onChange={set("period_to")} aria-invalid={errors.period ? true : undefined} aria-describedby={errors.period ? `${id}-period-error` : undefined} />
                {error("period")}
              </div>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-memo`}>
                Memo (optional)
              </label>
              <input className="fy-input" id={`${id}-memo`} maxLength={500} value={form.memo} onChange={set("memo")} />
            </div>
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={onClose}>
                Close
              </Button>
              <Button variant="primary" type="submit" disabled={busy}>
                {busy ? "Saving…" : "Next: attach the resolution"}
              </Button>
            </div>
          </form>
        ) : (
          <>
            <EvidencePanel record={{ kind: "director-fee", id: draft.id, etag: draft.etag }} draft mayAttach onChanged={() => void reread(draft.id)} />
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={onClose}>
                Later
              </Button>
              <Button variant="primary" type="button" disabled={busy || draft.resolutions === 0} onClick={ask}>
                <Icon of={Send} />
                {busy ? "Asking…" : "Ask for approval"}
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

// ExemptDialog records the organisation's exempt-private-company status from a day on, with the ACRA business profile
// as its evidence (FR-EQ-11): append-only, superseding the one before from its day.
function ExemptDialog({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: (said: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [from, setFrom] = useState("");
  const [exempt, setExempt] = useState<"yes" | "no" | "">("");
  const [note, setNote] = useState("");
  const [draft, setDraft] = useState<{ id: string; etag: string; evidence: number } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  if (!open) return null;
  const close = () => {
    setDraft(null);
    setProblem(null);
    onClose();
  };
  const reread = async (statusId: string) => {
    const r = await api.GET("/v1/exempt-statuses/{status_id}", { params: { path: { status_id: statusId } } });
    if (r.error) return setProblem(problemOf(r.error));
    setDraft({ id: statusId, etag: etagOf(r.response), evidence: r.data.evidence });
  };
  const save = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!/^\d{4}-\d{2}-\d{2}$/.test(from)) errs.from = "Enter the day it applies from.";
    if (!exempt) errs.exempt = "Say whether the company is exempt from that day.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/exempt-statuses", { params: { header: { "Idempotency-Key": key } }, body: { from, exempt: exempt === "yes", ...(note.trim() ? { note: note.trim() } : {}) } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setDraft({ id: out.data.id, etag: etagOf(out.response), evidence: out.data.evidence });
  };
  const record = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/exempt-statuses/{status_id}/record", { params: { path: { status_id: draft.id }, header: { "If-Match": draft.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["director-accounts"] });
    onDone(`Recorded the company as ${out.data.exempt ? "an exempt private company" : "not an exempt private company"} from ${date(out.data.from)}.`);
    close();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && close()} title="Exempt private company" description="An exempt private company has no corporate shareholder and at most 20 members. Record the status from the day it applies, with the ACRA business profile.">
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The status was not saved" />}
        {!draft ? (
          <form className="fy-stack fy-stack--3" onSubmit={save} noValidate>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-from`}>
                From
              </label>
              <input className="fy-input" type="date" id={`${id}-from`} value={from} onChange={(e) => setFrom(e.target.value)} aria-invalid={errors.from ? true : undefined} aria-describedby={errors.from ? `${id}-from-error` : undefined} />
              {errors.from && (
                <span className="fy-error" id={`${id}-from-error`}>
                  {errors.from}
                </span>
              )}
            </div>
            <fieldset className="fy-field" aria-describedby={errors.exempt ? `${id}-exempt-error` : undefined}>
              <legend className="fy-label">Exempt private company from that day</legend>
              <label className="fy-check">
                <input type="radio" name={`${id}-exempt`} checked={exempt === "yes"} onChange={() => setExempt("yes")} />
                <span>Yes, exempt</span>
              </label>
              <label className="fy-check">
                <input type="radio" name={`${id}-exempt`} checked={exempt === "no"} onChange={() => setExempt("no")} />
                <span>No, not exempt (for example, a corporate investor joined)</span>
              </label>
              {errors.exempt && (
                <span className="fy-error" id={`${id}-exempt-error`}>
                  {errors.exempt}
                </span>
              )}
            </fieldset>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-note`}>
                Note (optional)
              </label>
              <input className="fy-input" id={`${id}-note`} maxLength={1000} value={note} onChange={(e) => setNote(e.target.value)} />
            </div>
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={close}>
                Cancel
              </Button>
              <Button variant="primary" type="submit" disabled={busy}>
                {busy ? "Saving…" : "Next: attach the business profile"}
              </Button>
            </div>
          </form>
        ) : (
          <>
            <EvidencePanel record={{ kind: "exempt-status", id: draft.id, etag: draft.etag }} draft mayAttach onChanged={() => void reread(draft.id)} />
            <div className="fy-row justify-end gap-2">
              <Button variant="ghost" type="button" onClick={close}>
                Later
              </Button>
              <Button variant="primary" type="button" disabled={busy || draft.evidence === 0} onClick={record}>
                <Icon of={Stamp} />
                {busy ? "Recording…" : "Record the status"}
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

// exemptLine says the exempt-private-company status in force, in words.
function exemptLine(s: { status: string; from?: string } | undefined): string {
  switch (s?.status) {
    case "exempt":
      return `Exempt private company since ${date(s.from ?? "")}: a loan to a director is allowed, and disclosed.`;
    case "not_exempt":
      return `Not an exempt private company since ${date(s.from ?? "")}: section 162 of the Companies Act restricts loans to directors.`;
    default:
      return "No exempt-private-company status is recorded, so a director's account in debit is treated as for a company that is not exempt.";
  }
}

// DirectorAccounts is Accounting › Director accounts (docs/design/screens DirectorAccounts; FR-EQ-01, FR-EQ-11,
// FR-EQ-14): what the company owes each director and shareholder on 2500 and 2120, and what they owe it, from the
// balance service; each person's statement by where the amounts came from; and the member who signs in as each.
export function DirectorAccounts() {
  const { session } = useSignedIn();
  const [asAt, setAsAt] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const [linking, setLinking] = useState<DirectorAccount | null>(null);
  const [paying, setPaying] = useState<DirectorAccount | null>(null);
  const [feeing, setFeeing] = useState<DirectorAccount | null>(null);
  const [exempting, setExempting] = useState(false);
  const [status, setStatus] = useState<{ text: string; approval?: string } | null>(null);
  const q = useQuery({ queryKey: ["director-accounts", asAt], queryFn: () => accounts(asAt) });
  const mayLink = (session.roles ?? []).some((r) => linkers.includes(r));
  const people = q.data?.items ?? [];
  const selected = people.find((p) => p.contact_id === shown) ?? null;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <span>Director accounts</span>
          </nav>
          <h1 className="fy-h1">Director accounts</h1>
          <p className="fy-secondary">What the company owes its directors and shareholders, and what they owe it, by where each amount came from.</p>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor="da-as-at">
            As at
          </label>
          <input className="fy-input" type="date" id="da-as-at" value={asAt || q.data?.as_at || ""} onChange={(e) => setAsAt(e.target.value)} />
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
      {q.isError && <Refusal problem={problemOf(q.error)} title="The director accounts could not be shown" />}
      {q.data && (
        <p className="fy-row fy-row--wrap fy-row--between gap-2">
          <span className="fy-small fy-wrap">
            <Icon of={Building2} /> {exemptLine(q.data.exempt_status)}
          </span>
          {mayLink && (
            <Button variant="ghost" size="sm" type="button" onClick={() => setExempting(true)} title={helpText("director-accounts.exempt").tooltip}>
              {helpText("director-accounts.exempt").label}
            </Button>
          )}
        </p>
      )}
      {q.isPending && <p className="fy-secondary">Loading the director accounts…</p>}
      {q.data && !isZero(q.data.unkeyed) && (
        <div className="fy-banner fy-banner--warning" role="alert">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-small">
              {formatAmount(owed(q.data.unkeyed))} {q.data.currency} on 2500 and 2120 names no director or shareholder. Reverse those journals and post them again with
              the person each amount belongs to.
            </span>
          </div>
        </div>
      )}
      {q.data && people.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={UserRound} size={24} />
          <h2 className="fy-h3">No directors or shareholders yet</h2>
          <p className="fy-secondary">
            Mark the founders' contacts as directors or shareholders in <Link to="/contacts">Contacts</Link>. Each then has an account here, and every amount on 2500 and 2120
            names whose it is.
          </p>
        </div>
      )}
      {people.length > 0 && (
        <div className="fy-grid fy-grid--2 fy-stagger">
          {people.map((p) => (
            <PersonCard key={p.contact_id} p={p} mayLink={mayLink} onStatement={() => setShown(p.contact_id)} onLink={() => setLinking(p)} onPay={() => setPaying(p)} onFees={() => setFeeing(p)} />
          ))}
        </div>
      )}
      {selected && q.data && <Statement key={selected.contact_id} person={selected} until={q.data.as_at} />}
      <LinkMember person={linking} onClose={() => setLinking(null)} onAsked={(text, approval) => setStatus({ text, approval })} />
      <PayChoiceDialog key={paying?.contact_id ?? "none"} person={paying} onClose={() => setPaying(null)} onDone={(text) => setStatus({ text })} />
      <ExemptDialog open={exempting} onClose={() => setExempting(false)} onDone={(text) => setStatus({ text })} />
      <FeesDialog
        key={feeing?.contact_id ?? "no-fees"}
        person={feeing}
        base={q.data?.currency ?? "SGD"}
        onClose={() => setFeeing(null)}
        onAsked={(text, approval) => setStatus({ text, approval })}
      />
    </div>
  );
}
