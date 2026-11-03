import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FileDown, Handshake, Link2, ScrollText, TriangleAlert, UserRound } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { isZero, negate, sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { save } from "../reports/AccountantExports";
import { originLabel, type DirectorAccount, type DirectorStatement } from "./data";

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

async function accounts(asAt: string): Promise<{ as_at: string; currency: string; items: DirectorAccount[]; unkeyed: string }> {
  const items: DirectorAccount[] = [];
  let cursor: string | undefined;
  let head = { as_at: asAt, currency: "", unkeyed: "0.00" };
  do {
    const r = await api.GET("/v1/director-accounts", { params: { query: { as_at: asAt || undefined, page_size: 500, cursor } } });
    if (r.error) throw r.error;
    head = { as_at: r.data.as_at, currency: r.data.currency, unkeyed: r.data.unkeyed.amount };
    items.push(...(r.data.items ?? []));
    cursor = r.data.next_cursor;
  } while (cursor);
  return { ...head, items };
}

function PersonCard({ p, onStatement, onLink, mayLink }: { p: DirectorAccount; onStatement: () => void; onLink: () => void; mayLink: boolean }) {
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
          <span className="fy-small fy-wrap">A loan to a director is restricted unless the company is an exempt private company. Ask your accountant.</span>
        </div>
      )}
      <div className="fy-row fy-row--wrap fy-row--between gap-2">
        <span className="fy-small fy-muted">{p.member_user_id ? "Signs in as a member: sees their own account" : "No member signs in as them"}</span>
        <span className="fy-row gap-2">
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

// DirectorAccounts is Accounting › Director accounts (docs/design/screens DirectorAccounts; FR-EQ-01, FR-EQ-11,
// FR-EQ-14): what the company owes each director and shareholder on 2500 and 2120, and what they owe it, from the
// balance service; each person's statement by where the amounts came from; and the member who signs in as each.
export function DirectorAccounts() {
  const { session } = useSignedIn();
  const [asAt, setAsAt] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const [linking, setLinking] = useState<DirectorAccount | null>(null);
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
            <PersonCard key={p.contact_id} p={p} mayLink={mayLink} onStatement={() => setShown(p.contact_id)} onLink={() => setLinking(p)} />
          ))}
        </div>
      )}
      {selected && q.data && <Statement key={selected.contact_id} person={selected} until={q.data.as_at} />}
      <LinkMember person={linking} onClose={() => setLinking(null)} onAsked={(text, approval) => setStatus({ text, approval })} />
    </div>
  );
}
