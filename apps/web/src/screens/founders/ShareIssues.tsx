import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";
import { accounts } from "./DirectorAccounts";

// The register of share issues (FR-EQ-08; p1-founders-money task 5.4, design D11): every allotment, earliest effective
// date first, with its holder, class, shares, price, amount, consideration, resolutions and ACRA reference. Share
// capital moves only with it or the opening balances (INV-13). Shares issued before the books start, such as the
// incorporation subscription, are recorded here by an Owner or the Controller; conversions add theirs when ACRA
// registers them. Transfers, cash issues and the cap table come with the equity register.

type ShareIssue = components["schemas"]["ShareIssue"];

// The roles that record an issue before the books start: A on "Director accounts, settlements, share issues".
const recorders = ["owner", "controller"];

const considerations: Record<ShareIssue["consideration"], string> = {
  cash: "Cash",
  debt_capitalised: "Debt capitalised",
  services: "Services",
  safe_or_note: "A SAFE or note converted",
};

// RecordBeforeBooks records shares issued before the books start: no entry posts, as the opening balances hold their
// share capital.
function RecordBeforeBooks({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const h = helpText("share-issues.record-before-books");
  const [form, setForm] = useState({ holder_id: "", class_account_id: "", shares: "", price: "", consideration: "cash" as ShareIssue["consideration"], resolution_refs: "", lodgement_ref: "", effective_on: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const people = useQuery({ queryKey: ["director-accounts", ""], queryFn: () => accounts(""), enabled: open });
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts, enabled: open });
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  const record = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/share-issues", {
        params: { header: { "Idempotency-Key": key } },
        body: {
          holder_id: form.holder_id,
          class_account_id: form.class_account_id,
          shares: form.shares.replace(/,/g, "").trim(),
          price: form.price.trim(),
          consideration: form.consideration,
          resolution_refs: form.resolution_refs.trim(),
          ...(form.lodgement_ref.trim() ? { lodgement_ref: form.lodgement_ref.trim() } : {}),
          effective_on: form.effective_on,
        },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["share-issues"] });
    onDone(`Recorded ${out.data.shares} shares of ${out.data.holder_name}, issued on ${date(out.data.effective_on)}: no entry posts.`);
    onClose();
  };
  const ready = form.holder_id && form.class_account_id && form.shares && form.price && form.resolution_refs.trim() && form.effective_on;
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow="Register of share issues"
      title={h.label}
      description="Shares issued before the books start, such as the incorporation subscription. No entry posts: the opening balances hold their share capital."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy || !ready} title={h.tooltip}>
            <Icon of={Plus} />
            {busy ? "Recording…" : "Record the issue"}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The issue was not recorded" />}
        <form className="fy-form fy-form--2" id={`${id}-form`} onSubmit={(e) => void record(e)} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-holder`}>
              Holder
            </label>
            <select className="fy-input" id={`${id}-holder`} value={form.holder_id} onChange={set("holder_id")}>
              <option value="">Choose</option>
              {(people.data?.items ?? []).map((p) => (
                <option key={p.contact_id} value={p.contact_id}>
                  {p.legal_name}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-class`}>
              Class
            </label>
            <select className="fy-input" id={`${id}-class`} value={form.class_account_id} onChange={set("class_account_id")}>
              <option value="">Choose</option>
              {(chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group).map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-shares`}>
              Shares
            </label>
            <input className="fy-input fy-num" inputMode="numeric" id={`${id}-shares`} value={form.shares} onChange={set("shares")} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-price`}>
              Price per share
            </label>
            <input className="fy-input fy-num" inputMode="decimal" id={`${id}-price`} value={form.price} onChange={set("price")} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-consideration`}>
              Paid with
            </label>
            <select className="fy-input" id={`${id}-consideration`} value={form.consideration} onChange={set("consideration")}>
              {Object.entries(considerations).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-on`}>
              Issued on
            </label>
            <input className="fy-input" type="date" id={`${id}-on`} value={form.effective_on} onChange={set("effective_on")} />
            <span className="fy-help">Before the books start.</span>
          </div>
          <div className="fy-field fy-span-2">
            <label className="fy-label" htmlFor={`${id}-res`}>
              Resolutions
            </label>
            <input className="fy-input" id={`${id}-res`} maxLength={500} value={form.resolution_refs} onChange={set("resolution_refs")} />
          </div>
          <div className="fy-field fy-span-2">
            <label className="fy-label" htmlFor={`${id}-ref`}>
              ACRA reference (optional)
            </label>
            <input className="fy-input" id={`${id}-ref`} maxLength={100} value={form.lodgement_ref} onChange={set("lodgement_ref")} />
          </div>
        </form>
      </div>
    </Dialog>
  );
}

// ShareIssueRegister is the register itself, a section of the equity transactions page and of its own: every allotment,
// and the shares issued before the books start, which an Owner or the Controller records.
export function ShareIssueRegister({ titled = false }: { titled?: boolean }) {
  const { session } = useSignedIn();
  const mayRecord = (session.roles ?? []).some((r) => recorders.includes(r));
  const [recording, setRecording] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const q = useQuery({
    queryKey: ["share-issues"],
    queryFn: async () => {
      const r = await api.GET("/v1/share-issues");
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={titled ? "share-issues-title" : undefined}>
      {(titled || mayRecord) && (
        <div className="fy-row fy-row--wrap fy-row--between gap-3">
          {titled ? (
            <h2 className="fy-h3" id="share-issues-title" title={helpText("equity.share-issues").tooltip}>
              Register of share issues
            </h2>
          ) : (
            <span />
          )}
          {mayRecord && (
            <Button variant="secondary" onClick={() => setRecording(true)} title={helpText("share-issues.record-before-books").tooltip}>
              <Icon of={Plus} />
              {helpText("share-issues.record-before-books").label}
            </Button>
          )}
        </div>
      )}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The register could not be shown" />}
      {q.data && q.data.length === 0 && <p className="fy-small fy-muted">No shares are recorded yet. Record the shares issued before the books start, such as the incorporation subscription.</p>}
      {q.data && q.data.length > 0 && (
        <table className="fy-table" aria-label="Register of share issues">
          <thead>
            <tr>
              <th scope="col">Effective</th>
              <th scope="col">Holder</th>
              <th scope="col">Class</th>
              <th scope="col" className="fy-num">
                Shares
              </th>
              <th scope="col" className="fy-num">
                Price
              </th>
              <th scope="col" className="fy-num">
                Amount
              </th>
              <th scope="col">Paid with</th>
              <th scope="col">Resolutions and ACRA</th>
            </tr>
          </thead>
          <tbody>
            {q.data.map((s) => (
              <tr key={s.id}>
                <td data-card="meta">{date(s.effective_on)}</td>
                <td data-card="title">{s.holder_name}</td>
                <td data-card="meta">
                  <span className="fy-mono">{s.class_code}</span>
                </td>
                <td data-card="amount" className="fy-num">
                  {Number.isSafeInteger(Number(s.shares)) ? Number(s.shares).toLocaleString("en-SG") : s.shares}
                </td>
                <td className="fy-num">{s.price}</td>
                <td data-card="amount" className="fy-num">
                  {formatAmount(s.amount.amount)}
                </td>
                <td data-card="meta">
                  {considerations[s.consideration]}
                  {s.before_books && <span className="fy-small fy-muted block">Before the books start: no entry</span>}
                </td>
                <td data-card="meta" className="fy-small fy-wrap">
                  {s.resolution_refs}
                  {s.lodgement_ref && <span className="block fy-mono">{s.lodgement_ref}</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <RecordBeforeBooks open={recording} onClose={() => setRecording(false)} onDone={setStatus} />
    </section>
  );
}

export function ShareIssues() {
  const h = helpText("equity.share-issues");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting/directors">Director accounts</Link>
            <span aria-hidden="true">›</span>
            <span>Register of share issues</span>
          </nav>
          <h1 className="fy-h1" title={h.tooltip}>
            Register of share issues
          </h1>
          <p className="fy-secondary">Every allotment of shares. Share capital changes only with this register or the opening balances.</p>
        </div>
      </div>
      <ShareIssueRegister />
    </div>
  );
}
