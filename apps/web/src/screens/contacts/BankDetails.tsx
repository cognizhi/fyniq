import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, Eye, EyeOff, Hourglass, Landmark, PencilLine, Plus, Send, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";

type Contact = components["schemas"]["Contact"];
type Change = components["schemas"]["PayeeBankChange"];
type Account = components["schemas"]["PayeeAccount"];
type Check = components["schemas"]["BillCheck"];

// Who reads vendors' bank details (Appendix D's "Vendor bank details"): the Owner and the Controller approve (A), the
// Accountant and the AP specialist prepare and ask (C), the Auditor and the Tax agent see them masked (M). The API decides.
export const bankPreparers = ["accountant", "ap_specialist"];
export const bankRevealers = ["owner", "controller", ...bankPreparers];
export const bankReaders = [...bankRevealers, "auditor", "tax_agent"];

const kinds = { singapore: "Singapore bank", overseas: "Bank outside Singapore" } as const;

// accountLine names an account as people read it: the bank, the last four characters and the name; never the number.
export function accountLine(a: Account): string {
  return `${a.bic} · ••••${a.account_number_last4}${a.number_kind === "iban" ? " (IBAN)" : ""} · ${a.account_name}`;
}

function ChangeStatus({ c }: { c: Change }) {
  if (c.status === "draft")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={PencilLine} />
        Draft
      </span>
    );
  if (c.status === "waiting")
    return (
      <span className="fy-chip fy-chip--serious">
        <Icon of={Hourglass} />
        Waiting for approval
      </span>
    );
  return c.in_force ? (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      In force
    </span>
  ) : (
    <span className="fy-chip fy-chip--neutral">Replaced</span>
  );
}

// Reveal shows one account's full number on request: the API asks for step-up, audits the reveal, and answers a masked
// view masked (FR-CT-03; PRD-001 rev 0.152). The number stays on screen until hidden, and is never kept.
export function Reveal({ changeId, label }: { changeId: string; label: string }) {
  const confirm = useConfirm();
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const help = helpText("bank-details.reveal");
  const show = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/bank-changes/{change_id}/reveal", { params: { path: { change_id: changeId } } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setNumber(out.data.account_number);
  };
  return (
    <span className="fy-stack fy-stack--1">
      <span className="fy-row fy-row--wrap gap-2">
        {number ? (
          <>
            <span className="fy-mono" aria-label={`${label}: ${number.split("").join(" ")}`}>
              {number}
            </span>
            <Button variant="ghost" size="sm" onClick={() => setNumber("")}>
              <Icon of={EyeOff} />
              Hide
            </Button>
          </>
        ) : (
          <Button variant="ghost" size="sm" onClick={() => void show()} disabled={busy} title={help.tooltip} aria-label={`${help.label}: ${label}`}>
            <Icon of={Eye} />
            {help.label}
          </Button>
        )}
      </span>
      {problem && <Refusal problem={problem} title="The number was not shown" />}
    </span>
  );
}

function Checks({ list }: { list: Check[] }) {
  if (list.length === 0) return null;
  return (
    <ul className="fy-stack fy-stack--2" aria-label="Checks">
      {list.map((c, i) => (
        <li key={i} className={`fy-banner ${c.severity === "error" ? "fy-banner--critical" : "fy-banner--warning"}`}>
          <Icon of={c.severity === "error" ? CircleX : TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{c.message}</span>
            {c.fix && <span className="fy-small">{c.fix}</span>}
          </div>
        </li>
      ))}
    </ul>
  );
}

interface Draft {
  currency: string;
  kind: "singapore" | "overseas";
  bic: string;
  bank_country: string;
  account_name: string;
  account_number: string;
  callback_note: string;
}

// ChangeEditor prepares a change of a vendor's bank account in one currency, attaches the vendor's letter and asks for
// approval (docs/design/screens BankDetailsChange; FR-CT-03, FR-DOC-06, SOD-1). Asking freezes payments to the vendor
// and tells every founder; another person approves it.
function ChangeEditor({ contact, change, onClose, onDone }: { contact: Contact; change: Change | "new"; onClose: () => void; onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const formId = useId();
  const editing = change === "new" ? null : change;
  const [saved, setSaved] = useState<{ change: Change; etag: string } | null>(editing ? { change: editing, etag: `"${editing.version}-draft"` } : null);
  const start = saved?.change.account;
  const [d, setD] = useState<Draft>({
    currency: start?.currency ?? contact.currency ?? "SGD",
    kind: start?.kind ?? "singapore",
    bic: start?.bic ?? "",
    bank_country: start?.bank_country ?? "SG",
    account_name: start?.account_name ?? contact.legal_name,
    account_number: "",
    callback_note: saved?.change.callback_note ?? "",
  });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const ask = helpText("bank-details.ask");
  const set = (k: keyof Draft, v: string) => setD({ ...d, [k]: v, ...(k === "kind" && v === "singapore" ? { bank_country: "SG" } : {}) });
  const id = (k: string) => `${formId}-${k}`;
  const errors = (saved?.change.checks ?? []).filter((c) => c.severity === "error");

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body = { ...d, bic: d.bic.trim().toUpperCase(), bank_country: d.bank_country.trim().toUpperCase(), currency: d.currency.trim().toUpperCase(), account_number: d.account_number || undefined, callback_note: d.callback_note || undefined };
    const out = saved
      ? await api.PUT("/v1/bank-changes/{change_id}", { params: { path: { change_id: saved.change.id }, header: { "If-Match": saved.etag } }, body })
      : await api.POST("/v1/bank-changes", { params: { header: { "Idempotency-Key": newKey() } }, body: { ...body, contact_id: contact.id, account_number: d.account_number } });
    setBusy(false);
    setFailed("The change was not saved");
    if (!out.data) return setProblem(problemOf(out.error));
    setSaved({ change: out.data, etag: etagOf(out.response) || `"${out.data.version}-draft"` });
    // The form shows what was saved, as the API kept it (the code in capitals, the name trimmed); never the number.
    const kept = out.data.account;
    setD({ ...d, currency: kept.currency, kind: kept.kind, bic: kept.bic, bank_country: kept.bank_country, account_name: kept.account_name, account_number: "" });
    await queries.invalidateQueries({ queryKey: ["bank-details", contact.id] });
  };

  const request = async () => {
    if (!saved) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/bank-changes/{change_id}/approval", { params: { path: { change_id: saved.change.id }, header: { "If-Match": saved.etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    setFailed("Approval was not asked for");
    if (!out.data) {
      const fresh = await api.GET("/v1/bank-changes/{change_id}", { params: { path: { change_id: saved.change.id } } });
      if (fresh.data) setSaved({ change: fresh.data, etag: etagOf(fresh.response) || saved.etag });
      return setProblem(problemOf(out.error));
    }
    await queries.invalidateQueries({ queryKey: ["bank-details", contact.id] });
    onDone(`Asked for approval of ${contact.legal_name}'s ${d.currency} account. Payments to ${contact.legal_name} are frozen until another person approves, rejects or you withdraw it.`);
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      wide
      eyebrow={contact.legal_name}
      title={saved ? "Change of bank details" : "Change bank details"}
      description="A change takes effect only when another person approves it. Until then, payments to this vendor are frozen and every founder is told."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Close
          </Button>
          <Button variant="secondary" type="submit" form={formId} disabled={busy}>
            {saved ? "Save the draft" : "Save as a draft"}
          </Button>
          {saved && (
            <Button variant="primary" onClick={() => void request()} disabled={busy || errors.length > 0} title={ask.tooltip}>
              <Icon of={Send} />
              {ask.label}
            </Button>
          )}
        </>
      }
    >
      <div className="fy-stack fy-stack--5">
        <div className="fy-banner fy-banner--warning" role="note">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Check the change with the vendor before you ask</span>
            <span className="fy-small">Call them on a number you already had, never one from the letter or email that asked for the change. Fraud by changed bank details starts with a convincing letter.</span>
          </div>
        </div>
        {problem && <Refusal problem={problem} title={failed} />}
        <form className="fy-form fy-form--2" id={formId} onSubmit={(e) => void save(e)} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("currency")}>
              Currency
            </label>
            <input className="fy-input" id={id("currency")} value={d.currency} onChange={(e) => set("currency", e.target.value)} maxLength={3} disabled={saved !== null} aria-describedby={id("currency-help")} />
            <span className="fy-help" id={id("currency-help")}>
              One account per currency. Another currency is another change.
            </span>
          </div>
          <fieldset className="fy-field m-0 min-w-0 border-0 p-0">
            <legend className="fy-label">Bank</legend>
            <div className="fy-row fy-row--wrap gap-3">
              {(Object.keys(kinds) as (keyof typeof kinds)[]).map((k) => (
                <label key={k} className="fy-row gap-2">
                  <input type="radio" name={id("kind")} checked={d.kind === k} onChange={() => set("kind", k)} />
                  {kinds[k]}
                </label>
              ))}
            </div>
          </fieldset>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("bic")}>
              SWIFT/BIC code
            </label>
            <input className="fy-input fy-mono" id={id("bic")} value={d.bic} onChange={(e) => set("bic", e.target.value)} maxLength={11} autoComplete="off" aria-describedby={id("bic-help")} />
            <span className="fy-help" id={id("bic-help")}>
              8 or 11 characters, such as DBSSSGSG. Its 5th and 6th letters are the bank's country.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("country")}>
              Bank's country
            </label>
            <input className="fy-input" id={id("country")} value={d.bank_country} onChange={(e) => set("bank_country", e.target.value)} maxLength={2} disabled={d.kind === "singapore"} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("name")}>
              Name on the account
            </label>
            <input className="fy-input" id={id("name")} value={d.account_name} onChange={(e) => set("account_name", e.target.value)} maxLength={140} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("number")}>
              {d.kind === "overseas" ? "Account number or IBAN" : "Account number"}
            </label>
            <input
              className="fy-input fy-mono"
              id={id("number")}
              value={d.account_number}
              onChange={(e) => set("account_number", e.target.value)}
              maxLength={40}
              autoComplete="off"
              spellCheck={false}
              inputMode={d.kind === "singapore" ? "numeric" : "text"}
              aria-describedby={id("number-help")}
            />
            <span className="fy-help" id={id("number-help")}>
              {saved ? `Saved as ••••${saved.change.account.account_number_last4}. Leave it empty to keep it.` : "As the vendor's letter prints it; spaces and hyphens are dropped. It is sealed when saved and shown only by its last four characters."}
            </span>
          </div>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={id("callback")}>
              Call-back note
            </label>
            <textarea className="fy-textarea" id={id("callback")} rows={2} maxLength={500} value={d.callback_note} onChange={(e) => set("callback_note", e.target.value)} aria-describedby={id("callback-help")} />
            <span className="fy-help" id={id("callback-help")}>
              Recommended: who you called, when, and the number you used.
            </span>
          </div>
        </form>
        {saved && (
          <>
            <Checks list={saved.change.checks} />
            <EvidencePanel
              record={{ kind: "bank-change", id: saved.change.id, etag: saved.etag }}
              draft
              mayAttach={(session.roles ?? []).some((r) => bankPreparers.includes(r))}
              onChanged={async () => {
                const fresh = await api.GET("/v1/bank-changes/{change_id}", { params: { path: { change_id: saved.change.id } } });
                if (fresh.data) setSaved({ change: fresh.data, etag: etagOf(fresh.response) || saved.etag });
              }}
            />
          </>
        )}
      </div>
    </Dialog>
  );
}

// BankDetails is a vendor's bank details (docs/design/screens ContactRecord, its Bank details tab; FR-CT-03): the account in
// force per currency, masked, revealed on request; every change, with the one waiting and the freeze it puts on payments.
export function BankDetails({ contact, onClose, onChanged }: { contact: Contact | null; onClose: () => void; onChanged: (what: string) => void }) {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const may = roles.some((r) => bankPreparers.includes(r));
  const reveal = roles.some((r) => bankRevealers.includes(r));
  const change = helpText("bank-details.change");
  const [editing, setEditing] = useState<Change | "new" | null>(null);
  const q = useQuery({
    queryKey: ["bank-details", contact?.id],
    enabled: contact !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/contacts/{contact_id}/bank-details", { params: { path: { contact_id: contact?.id ?? "" } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  if (!contact) return null;
  const details = q.data;

  return (
    <>
      <Dialog
        open={editing === null}
        onOpenChange={(o) => !o && onClose()}
        wide
        eyebrow="Contacts"
        title={`${contact.legal_name}'s bank details`}
        description="Where payments to this vendor go: one account per currency, each changed only with a second person's approval."
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              Close
            </Button>
            {may && (
              <Button variant="primary" onClick={() => setEditing("new")} title={change.tooltip}>
                <Icon of={Plus} />
                {change.label}
              </Button>
            )}
          </>
        }
      >
        {q.isError && <Refusal problem={problemOf(q.error)} title="The bank details could not be shown" />}
        {q.isPending && <p className="fy-secondary">Loading the bank details…</p>}
        {details && (
          <div className="fy-stack fy-stack--5">
            {details.payments_frozen && (
              <div className="fy-banner fy-banner--warning" role="status">
                <Icon of={TriangleAlert} size={20} />
                <div className="fy-banner__text">
                  <span className="fy-strong">Payments to {contact.legal_name} are frozen</span>
                  <span className="fy-small">A change of their bank details waits for approval. No payment date can be planned until it is approved, rejected or withdrawn.</span>
                </div>
              </div>
            )}
            <section aria-labelledby="bank-in-force">
              <h3 className="fy-h3" id="bank-in-force">
                In force
              </h3>
              {details.in_force.length === 0 ? (
                <p className="fy-secondary">No account is approved yet. {may ? "Prepare a change with the vendor's letter, and ask another person to approve it." : ""}</p>
              ) : (
                <ul className="fy-list">
                  {details.in_force.map((c) => (
                    <li key={c.id} className="fy-list__item">
                      <span className="fy-avatar fy-avatar--sm" aria-hidden="true">
                        <Icon of={Landmark} />
                      </span>
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">
                          {c.account.currency} · {kinds[c.account.kind]}
                        </span>
                        <span className="fy-list__meta whitespace-normal">
                          {accountLine(c.account)}
                          {c.approved_at ? ` · approved ${dateTime(c.approved_at)}` : ""}
                        </span>
                      </span>
                      {reveal && <Reveal changeId={c.id} label={`${c.account.currency} account number`} />}
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <section aria-labelledby="bank-changes">
              <h3 className="fy-h3" id="bank-changes">
                Changes
              </h3>
              {details.changes.length === 0 ? (
                <p className="fy-secondary">No change yet.</p>
              ) : (
                <ul className="fy-list">
                  {details.changes.map((c) => (
                    <li key={c.id} className="fy-list__item">
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">{c.account.currency} account</span>
                        <span className="fy-list__meta whitespace-normal">{accountLine(c.account)}</span>
                      </span>
                      <ChangeStatus c={c} />
                      {c.status === "draft" && may && (
                        <Button variant="ghost" size="sm" onClick={() => setEditing(c)}>
                          Continue
                        </Button>
                      )}
                      {c.status === "waiting" && c.approval_id && (
                        <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/approvals/$approvalId" params={{ approvalId: c.approval_id }}>
                          Open the approval
                        </Link>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        )}
      </Dialog>
      {editing !== null && (
        <ChangeEditor
          contact={contact}
          change={editing}
          onClose={() => setEditing(null)}
          onDone={(what) => {
            setEditing(null);
            onChanged(what);
          }}
        />
      )}
    </>
  );
}

// CompareNumbers lets the approver of a change compare the new number, and the one it replaces, with the vendor's letter
// (FR-CT-03; PRD-001 rev 0.152): each revealed on request, stepped up and audited.
export function CompareNumbers({ changeId, envelope }: { changeId: string; envelope: string }) {
  const { session } = useSignedIn();
  if (!(session.roles ?? []).some((r) => bankRevealers.includes(r))) return null;
  let replaces = "";
  try {
    replaces = String(JSON.parse(envelope)?.dependencies?.replaces ?? "");
  } catch {
    replaces = "";
  }
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="compare-numbers">
      <h2 className="fy-h3" id="compare-numbers">
        Compare with the vendor's letter
      </h2>
      <p className="fy-small fy-secondary">Each number shows after you confirm it's you, and the audit trail records that you looked.</p>
      <div className="fy-row fy-row--wrap gap-4">
        <span className="fy-stack fy-stack--1">
          <span className="fy-small fy-strong">The new account</span>
          <Reveal changeId={changeId} label="The new account number" />
        </span>
        {replaces && (
          <span className="fy-stack fy-stack--1">
            <span className="fy-small fy-strong">The account it replaces</span>
            <Reveal changeId={replaces} label="The account number it replaces" />
          </span>
        )}
      </div>
    </section>
  );
}
