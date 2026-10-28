import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleX, Hourglass, Landmark, PencilLine, Send, TriangleAlert, Undo2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { Reveal } from "../contacts/BankDetails";

type Change = components["schemas"]["ClaimantBankChange"];
type Check = components["schemas"]["BillCheck"];

// Who keeps a bank account for reimbursements: whoever claims (Appendix D's "Expense claims", C own). The API decides.
export const claimants = ["employee"];

// bankLine names a person's account as they read it: the bank's code and the last four digits; never the number.
function bankLine(c: Change): string {
  return `${c.account.bic} · ••••${c.account.account_number_last4}`;
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

// MyChangeEditor prepares a change of the person's own account, attaches the screenshot of their banking app and asks for
// approval (FR-AP-22, FR-DOC-06): a Singapore bank's account in SGD. Asking freezes payments to the person until
// someone in finance, never the person, approves it.
function MyChangeEditor({ change, name, onClose, onDone }: { change: Change | "new"; name: string; onClose: () => void; onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const formId = useId();
  const editing = change === "new" ? null : change;
  const [saved, setSaved] = useState<{ change: Change; etag: string } | null>(editing ? { change: editing, etag: `"${editing.version}-draft"` } : null);
  const [bic, setBic] = useState(saved?.change.account.bic ?? "");
  const [holder, setHolder] = useState(saved?.change.account.account_name ?? name);
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const ask = helpText("my-bank.ask");
  const id = (k: string) => `${formId}-${k}`;
  const errors = (saved?.change.checks ?? []).filter((c) => c.severity === "error");
  const reload = async (changeId: string, etag: string) => {
    const fresh = await api.GET("/v1/me/bank-changes/{change_id}", { params: { path: { change_id: changeId } } });
    if (fresh.data) setSaved({ change: fresh.data, etag: etagOf(fresh.response) || etag });
  };

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body = { bic: bic.trim().toUpperCase(), account_name: holder, account_number: number || undefined };
    const out = saved
      ? await api.PUT("/v1/me/bank-changes/{change_id}", { params: { path: { change_id: saved.change.id }, header: { "If-Match": saved.etag } }, body })
      : await api.POST("/v1/me/bank-changes", { params: { header: { "Idempotency-Key": newKey() } }, body: { ...body, account_number: number } });
    setBusy(false);
    setFailed("Your bank account was not saved");
    if (!out.data) return setProblem(problemOf(out.error));
    setSaved({ change: out.data, etag: etagOf(out.response) || `"${out.data.version}-draft"` });
    // The form shows what was saved, as the API kept it (the code in capitals, the name trimmed); never the number.
    setBic(out.data.account.bic);
    setHolder(out.data.account.account_name);
    setNumber("");
    await queries.invalidateQueries({ queryKey: ["my-bank-details"] });
  };

  const request = async () => {
    if (!saved) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/me/bank-changes/{change_id}/approval", { params: { path: { change_id: saved.change.id }, header: { "If-Match": saved.etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    setFailed("Approval was not asked for");
    if (!out.data) {
      await reload(saved.change.id, saved.etag);
      return setProblem(problemOf(out.error));
    }
    await queries.invalidateQueries({ queryKey: ["my-bank-details"] });
    onDone("Asked for approval of your bank account. Payments to you wait until someone in finance approves it, or you withdraw it.");
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      wide
      eyebrow="Profile and security"
      title={saved ? "Change of your bank account" : "Change my bank account"}
      description="Where your expense claims are paid: a Singapore bank's account in SGD. It takes effect when someone in finance approves it; until then, payments to you wait."
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
        {problem && <Refusal problem={problem} title={failed} />}
        <form className="fy-form fy-form--2" id={formId} onSubmit={(e) => void save(e)} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("bic")}>
              Bank's SWIFT/BIC code
            </label>
            <input className="fy-input fy-mono" id={id("bic")} value={bic} onChange={(e) => setBic(e.target.value)} maxLength={11} autoComplete="off" aria-describedby={id("bic-help")} />
            <span className="fy-help" id={id("bic-help")}>
              A bank in Singapore, such as DBSSSGSG, OCBCSGSG or UOVBSGSG. Your banking app shows it.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("name")}>
              Name on the account
            </label>
            <input className="fy-input" id={id("name")} value={holder} onChange={(e) => setHolder(e.target.value)} maxLength={140} />
          </div>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={id("number")}>
              Account number
            </label>
            <input
              className="fy-input fy-mono"
              id={id("number")}
              value={number}
              onChange={(e) => setNumber(e.target.value)}
              maxLength={40}
              autoComplete="off"
              spellCheck={false}
              inputMode="numeric"
              aria-describedby={id("number-help")}
            />
            <span className="fy-help" id={id("number-help")}>
              {saved ? `Saved as ••••${saved.change.account.account_number_last4}. Leave it empty to keep it.` : "As your bank shows it; spaces and hyphens are dropped. It is sealed when saved and shown only by its last four digits."}
            </span>
          </div>
        </form>
        {saved && (
          <>
            <Checks list={saved.change.checks} />
            <EvidencePanel record={{ kind: "my-bank-change", id: saved.change.id, etag: saved.etag }} draft mayAttach onChanged={() => void reload(saved.change.id, saved.etag)} />
          </>
        )}
      </div>
    </Dialog>
  );
}

// Withdraw takes back a change waiting for approval, which lifts the freeze on payments to the person; a person reaches
// their change's request only this way, never through the approvals.
function Withdraw({ change, onDone }: { change: Change; onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("my-bank.withdraw");
  const withdraw = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/me/bank-changes/{change_id}/withdraw", {
      params: { path: { change_id: change.id }, header: { "If-Match": `"${change.version}-draft"`, "Idempotency-Key": newKey() } },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["my-bank-details"] });
    onDone("Withdrawn. Your bank account stays as it was, and payments to you are no longer held.");
  };
  return (
    <span className="fy-stack fy-stack--1">
      <Button variant="ghost" size="sm" onClick={() => void withdraw()} disabled={busy} title={h.tooltip}>
        <Icon of={Undo2} />
        {h.label}
      </Button>
      {problem && <Refusal problem={problem} title="The change was not withdrawn" />}
    </span>
  );
}

// MyBankAccount is the bank account card of Profile and security (docs/design/screens ProfilePreferences; FR-AP-22): the
// account in force, masked, shown on request; a change waiting, which can be withdrawn; a draft to continue; and Change my
// bank account. Only those who claim see it.
export function MyBankAccount() {
  const { session } = useSignedIn();
  const [editing, setEditing] = useState<Change | "new" | null>(null);
  const [status, setStatus] = useState("");
  const may = (session.roles ?? []).some((r) => claimants.includes(r));
  const card = helpText("profile.bank-account");
  const change = helpText("my-bank.change");
  const q = useQuery({
    queryKey: ["my-bank-details"],
    enabled: may,
    queryFn: async () => {
      const r = await api.GET("/v1/me/bank-details");
      if (r.error) throw r.error;
      return r.data;
    },
  });
  if (!may) return null;
  const details = q.data;
  const waiting = details?.changes.find((c) => c.status === "waiting");
  const draft = details?.changes.find((c) => c.status === "draft");

  return (
    <section className="fy-card" aria-labelledby="bank-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="bank-title" title={card.tooltip}>
          {card.label}
        </h2>
        {waiting && (
          <span className="fy-chip fy-chip--serious">
            <Icon of={Hourglass} />
            Change waits for approval
          </span>
        )}
      </div>
      <div className="fy-stack fy-stack--3 p-4">
        <p className="fy-small fy-muted">Where your expense claims are paid. Only you enter it; someone in finance approves each change, and until then payments to you wait.</p>
        <p className="fy-small fy-secondary" role="status">
          {status}
        </p>
        {q.isError && <Refusal problem={problemOf(q.error)} title="Your bank account could not be shown" />}
        {q.isPending && <p className="fy-small fy-secondary">Loading your bank account…</p>}
        {details && !details.in_force && !waiting && !draft && <p className="fy-small">No bank account yet. Add yours so your claims can be paid by bank transfer.</p>}
        {details && (details.in_force || waiting || draft) && (
          <ul className="fy-list rounded-md border border-border" aria-label="Your bank account">
            {details.in_force && (
              <li className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Landmark} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{bankLine(details.in_force)}</span>
                  <span className="fy-list__meta whitespace-normal">
                    In force{details.in_force.approved_at ? ` since ${date(details.in_force.approved_at)}` : ""} · {details.in_force.account.account_name}
                  </span>
                </span>
                <Reveal changeId={details.in_force.id} label="Your account number" via="mine" />
              </li>
            )}
            {waiting && (
              <li className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Hourglass} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{bankLine(waiting)}</span>
                  <span className="fy-list__meta whitespace-normal">Waits for finance to approve it</span>
                </span>
                <Withdraw change={waiting} onDone={setStatus} />
              </li>
            )}
            {draft && (
              <li className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={PencilLine} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{bankLine(draft)}</span>
                  <span className="fy-list__meta whitespace-normal">Draft: attach the screenshot and ask for approval</span>
                </span>
                <Button variant="ghost" size="sm" onClick={() => setEditing(draft)}>
                  Continue
                </Button>
              </li>
            )}
          </ul>
        )}
      </div>
      <div className="fy-card__foot">
        <Button variant="secondary" size="sm" onClick={() => setEditing("new")} disabled={!details || Boolean(waiting)} title={change.tooltip}>
          <Icon of={PencilLine} />
          {change.label}
        </Button>
      </div>
      {editing !== null && (
        <MyChangeEditor
          change={editing}
          name={details?.in_force?.account.account_name ?? ""}
          onClose={() => setEditing(null)}
          onDone={(what) => {
            setEditing(null);
            setStatus(what);
          }}
        />
      )}
    </section>
  );
}
