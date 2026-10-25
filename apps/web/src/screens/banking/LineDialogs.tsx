import { useQuery } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { negate, sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { checkCategorisation, send, settles, type Line, type PartForm, type Request } from "./lineActions";

type Contact = components["schemas"]["Contact"];

// usePostingAccounts are the accounts a line's part can post to: active posting accounts, not a group, a money account
// or a control account (receivables, payables), which the API refuses.
export function usePostingAccounts() {
  return useQuery({
    queryKey: ["accounts", "posting"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/accounts", {});
      if (error) throw error;
      return (data.items ?? []).filter((a) => a.active && !a.group && !a.money_account && !a.control);
    },
  });
}

export function useContacts() {
  return useQuery({
    queryKey: ["contacts", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
}

// useGSTCodes are the GST codes on a line's day: sales codes for money in, purchase codes for money out.
export function useGSTCodes(line: Line) {
  const incoming = sign(line.amount.amount) > 0;
  return useQuery({
    queryKey: [incoming ? "sales-gst-codes" : "purchase-gst-codes", line.booking_date],
    queryFn: async () => {
      const out = incoming
        ? await api.GET("/v1/sales/gst-codes", { params: { query: { on: line.booking_date } } })
        : await api.GET("/v1/purchases/gst-codes", { params: { query: { on: line.booking_date } } });
      if (out.error) throw out.error;
      return (out.data.codes ?? []).map((c) => ({ code: c.code, description: c.description }));
    },
  });
}

const blank = (key: string): PartForm => ({ key, account_id: "", amount: "", gst_code: "", description: "" });

// CategoriseDialog codes a line to accounts and posts it (FR-BNK-05, FR-BNK-06): Change is one part, the whole line, with
// its account, contact and GST code; Split (Expert) is several parts, each with its amount, at most one taking the rest.
export function CategoriseDialog({ line, etag, split, onClose, onDone }: { line: Line; etag: string; split: boolean; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const accounts = usePostingAccounts();
  const contacts = useContacts();
  const codes = useGSTCodes(line);
  const [parts, setParts] = useState<PartForm[]>(split ? [blank("p1"), blank("p2")] : [blank("p1")]);
  const [contact, setContact] = useState("");
  const [reference, setReference] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText(split ? "bank-line.split" : "bank-line.change");
  const set = (key: string, patch: Partial<PartForm>) => setParts(parts.map((p) => (p.key === key ? { ...p, ...patch } : p)));
  const go = async () => {
    const { body, errors: e } = checkCategorisation(parts, contact, reference, line.amount.currency);
    setErrors(e);
    if (!body) return;
    setBusy(true);
    setProblem(null);
    const out = await send(line, etag, { action: "categorise", body });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const aria = (k: string) => (errors[k] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${k}-error` } : {});
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={h.label}
      description={`${formatAmount(line.amount.amount)} ${line.amount.currency} on ${date(line.booking_date)}. ${h.tooltip}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Post it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The line was not categorised" />}
      {errors.parts && <p className="fy-error">{errors.parts}</p>}
      {parts.map((p, n) => (
        <fieldset key={p.key} className="fy-field">
          {split && <legend className="fy-label">Part {n + 1}</legend>}
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-field fy-grow">
              <label className="fy-label" htmlFor={`${id}-${p.key}-account`}>
                {split ? `Part ${n + 1} account` : "Category"}
              </label>
              <select className="fy-select" id={`${id}-${p.key}-account`} value={p.account_id} onChange={(e) => set(p.key, { account_id: e.target.value })} {...aria(`${p.key}.account`)}>
                <option value="">Choose an account</option>
                {(accounts.data ?? []).map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.code} {a.name}
                  </option>
                ))}
              </select>
              {err(`${p.key}.account`)}
            </div>
            {split && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-${p.key}-amount`}>
                  Part {n + 1} amount
                </label>
                <input className="fy-input fy-num max-w-40" id={`${id}-${p.key}-amount`} inputMode="decimal" value={p.amount} placeholder="The rest" onChange={(e) => set(p.key, { amount: e.target.value })} {...aria(`${p.key}.amount`)} />
                {err(`${p.key}.amount`)}
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-${p.key}-gst`}>
                {split ? `Part ${n + 1} GST code` : "GST code"}
              </label>
              <select className="fy-select" id={`${id}-${p.key}-gst`} value={p.gst_code} onChange={(e) => set(p.key, { gst_code: e.target.value })}>
                <option value="">The account's default</option>
                {(codes.data ?? []).map((c) => (
                  <option key={c.code} value={c.code} title={c.description}>
                    {c.code}
                  </option>
                ))}
              </select>
            </div>
            {split && parts.length > 1 && (
              <button type="button" className="fy-iconbtn" aria-label={`Remove part ${n + 1}`} onClick={() => setParts(parts.filter((x) => x.key !== p.key))}>
                <Icon of={Trash2} />
              </button>
            )}
          </div>
        </fieldset>
      ))}
      {split && (
        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setParts([...parts, blank(`p${parts.length + 1}-${newKey().slice(0, 6)}`)])}>
          <Icon of={Plus} />
          Add a part
        </button>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-contact`}>
          Contact (optional)
        </label>
        <select className="fy-select" id={`${id}-contact`} value={contact} onChange={(e) => setContact(e.target.value)}>
          <option value="">None</option>
          {(contacts.data ?? []).map((c: Contact) => (
            <option key={c.id} value={c.id}>
              {c.legal_name}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reference`}>
          Reference (optional)
        </label>
        <input className="fy-input" id={`${id}-reference`} value={reference} maxLength={140} onChange={(e) => setReference(e.target.value)} />
      </div>
    </Dialog>
  );
}

// TransferDialog marks a line as one side of a transfer between money accounts (FR-BNK-09): with the other side's line
// when it is waiting, matched on both sides; or, before it arrives, in transit to the other account.
export function TransferDialog({ line, accounts, waiting, onClose, onDone }: { line: Line; accounts: components["schemas"]["MoneyAccount"][]; waiting: Line[]; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const out = sign(line.amount.amount) < 0;
  const others = accounts.filter((a) => a.id !== line.money_account_id && !a.closed_on);
  const [other, setOther] = useState("");
  const [otherLine, setOtherLine] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText("bank-line.transfer");
  const mirrors = waiting.filter((l) => l.money_account_id === other && sign(l.amount.amount) === (out ? 1 : -1) && (l.amount.currency !== line.amount.currency || l.amount.amount === negate(line.amount.amount)));
  const go = async () => {
    if (!other) return setMissing(true);
    const size = out ? negate(line.amount.amount) : line.amount.amount;
    const target = accounts.find((a) => a.id === other);
    const body: components["schemas"]["BankTransferDraft"] = out
      ? otherLine
        ? { from_line_id: line.id, to_line_id: otherLine }
        : { from_line_id: line.id, to_money_account_id: other, received: target?.currency === line.amount.currency ? { amount: size, currency: line.amount.currency } : undefined }
      : otherLine
        ? { from_line_id: otherLine, to_line_id: line.id }
        : { to_line_id: line.id, from_money_account_id: other, paid: target?.currency === line.amount.currency ? { amount: size, currency: line.amount.currency } : undefined };
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/bank-transfers", { params: { header: { "Idempotency-Key": newKey() } }, body });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={h.label}
      description={h.tooltip}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Record the transfer
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The transfer was not recorded" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-other`}>
          {out ? "Moved to" : "Moved from"}
        </label>
        <select
          className="fy-select"
          id={`${id}-other`}
          value={other}
          onChange={(e) => {
            setOther(e.target.value);
            setOtherLine("");
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-other-error` : undefined}
        >
          <option value="">Choose the other account</option>
          {others.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name} ({a.currency})
            </option>
          ))}
        </select>
        {missing && (
          <span className="fy-error" id={`${id}-other-error`}>
            Choose the account the money moved {out ? "to" : "from"}.
          </span>
        )}
      </div>
      {other && (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-line`}>
            Its line on that account
          </label>
          <select className="fy-select" id={`${id}-line`} value={otherLine} onChange={(e) => setOtherLine(e.target.value)}>
            <option value="">Not arrived yet: in transit</option>
            {mirrors.map((l) => (
              <option key={l.id} value={l.id}>
                {date(l.booking_date)} · {formatAmount(l.amount.amount)} {l.amount.currency} · {l.description ?? ""}
              </option>
            ))}
          </select>
          <span className="fy-small fy-muted">Across currencies, each side posts at its own currency's rate for the day; a fee the provider reports goes to bank charges.</span>
        </div>
      )}
    </Dialog>
  );
}

// MatchDialog matches a line to a contact's open invoices (money in) or bills (money out), several at once (FR-BNK-07,
// FR-BNK-08): each settles what is open of it, or less; "in full" lets a small difference clear within the tolerance.
export function MatchDialog({ line, etag, onClose, onDone }: { line: Line; etag: string; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const incoming = sign(line.amount.amount) > 0;
  const contacts = useContacts();
  const [contact, setContact] = useState("");
  const [chosen, setChosen] = useState<Record<string, { amount: string; full: boolean }>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const h = helpText("bank-line.match");
  const docs = useQuery({
    queryKey: [incoming ? "open-invoices" : "open-bills", contact],
    enabled: Boolean(contact),
    queryFn: async () => {
      if (incoming) {
        const { data, error } = await api.GET("/v1/invoices", { params: { query: { status: "posted", page_size: 200 } } });
        if (error) throw error;
        return (data.items ?? [])
          .filter((i) => i.customer?.id === contact && i.open && sign(i.open.amount) > 0)
          .map((i) => ({ id: i.id, number: i.number ?? "", date: i.date ?? "", open: i.open! }));
      }
      const { data, error } = await api.GET("/v1/bills", { params: { query: { status: "posted", page_size: 200 } } });
      if (error) throw error;
      return (data.items ?? [])
        .filter((b) => b.supplier?.id === contact && b.open && sign(b.open.amount) > 0)
        .map((b) => ({ id: b.id, number: b.number ?? b.supplier_ref ?? "", date: b.date ?? "", open: b.open! }));
    },
  });
  const people = (contacts.data ?? []).filter((c) => (incoming ? c.customer : c.vendor));
  const go = async () => {
    const picked = Object.entries(chosen);
    if (!contact || picked.length === 0) return setMissing(true);
    const all = docs.data ?? [];
    const currency = all.find((d) => d.id === picked[0]![0])?.open.currency ?? line.amount.currency;
    const r: Request = incoming
      ? {
          action: "match-invoices",
          body: {
            customer_id: contact,
            currency: currency === line.amount.currency ? undefined : currency,
            invoices: picked.map(([doc, v]) => ({ invoice_id: doc, amount: { amount: v.amount, currency }, in_full: v.full || undefined })),
          },
        }
      : {
          action: "match-bills",
          body: {
            supplier_id: contact,
            currency: currency === line.amount.currency ? undefined : currency,
            bills: picked.map(([doc, v]) => ({ bill_id: doc, amount: { amount: v.amount, currency }, in_full: v.full || undefined })),
          },
        };
    setBusy(true);
    setProblem(null);
    const out = await send(line, etag, r);
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={h.label}
      description={`${formatAmount(line.amount.amount)} ${line.amount.currency} on ${date(line.booking_date)}. ${h.tooltip}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Match and post
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The line was not matched" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-contact`}>
          {incoming ? "Customer" : "Supplier"}
        </label>
        <select
          className="fy-select"
          id={`${id}-contact`}
          value={contact}
          onChange={(e) => {
            setContact(e.target.value);
            setChosen({});
          }}
        >
          <option value="">Choose who {incoming ? "paid" : "was paid"}</option>
          {people.map((c) => (
            <option key={c.id} value={c.id}>
              {c.legal_name}
            </option>
          ))}
        </select>
      </div>
      {missing && <p className="fy-error">Choose {incoming ? "the customer and at least one invoice" : "the supplier and at least one bill"}.</p>}
      {docs.data && docs.data.length === 0 && <p className="fy-secondary">Nothing of theirs is open.</p>}
      {docs.data && docs.data.length > 0 && (
        <table className="fy-table" aria-label={incoming ? "Open invoices" : "Open bills"}>
          <thead>
            <tr>
              <th scope="col">Settle</th>
              <th scope="col">Document</th>
              <th scope="col" className="fy-num">
                Open
              </th>
              <th scope="col" className="fy-num">
                Amount
              </th>
              <th scope="col">In full</th>
            </tr>
          </thead>
          <tbody>
            {docs.data.map((d) => {
              const c = chosen[d.id];
              return (
                <tr key={d.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Settle ${d.number}`}
                      checked={Boolean(c)}
                      onChange={(e) => {
                        const next = { ...chosen };
                        if (e.target.checked) next[d.id] = { amount: settles(line, d.open).amount, full: false };
                        else delete next[d.id];
                        setChosen(next);
                      }}
                    />
                  </td>
                  <td>
                    <span className="fy-mono">{d.number}</span>
                    {d.date && <span className="fy-small fy-muted block">{date(d.date)}</span>}
                  </td>
                  <td className="fy-num">
                    {formatAmount(d.open.amount)} {d.open.currency}
                  </td>
                  <td className="fy-num">
                    {c && (
                      <input
                        className="fy-input fy-num max-w-40"
                        aria-label={`Amount for ${d.number}`}
                        inputMode="decimal"
                        value={c.amount}
                        onChange={(e) => setChosen({ ...chosen, [d.id]: { ...c, amount: e.target.value } })}
                      />
                    )}
                  </td>
                  <td>{c && <input type="checkbox" aria-label={`Settle ${d.number} in full`} checked={c.full} onChange={(e) => setChosen({ ...chosen, [d.id]: { ...c, full: e.target.checked } })} />}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <p className="fy-small fy-muted">What the line pays beyond the documents is kept as a credit with them; a small shortfall settled in full clears within the organisation's tolerance.</p>
    </Dialog>
  );
}
