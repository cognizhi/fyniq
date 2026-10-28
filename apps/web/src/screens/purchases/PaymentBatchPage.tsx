import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Ban, CircleCheck, CircleX, Download, Hourglass, Mail, MailX, Send, Trash2, TriangleAlert, Undo2, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { BatchStatus, batchName, batchPreparers, batchReleasers, type Batch } from "./PaymentBatches";

type Payment = Batch["payments"][number];
type Check = components["schemas"]["BillCheck"];

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

// PaymentState is a payment's state in words: before release, ready, held by a change of its payee's account, or blocked
// by its items' checks; after, paid, waiting for the bank's statement line, or rejected; and a supplier's remittance.
function PaymentState({ p, released }: { p: Payment; released: boolean }) {
  if (released) {
    return (
      <span className="fy-row fy-row--wrap gap-2">
        {p.rejected ? (
          <span className="fy-chip fy-chip--critical">
            <Icon of={Undo2} />
            Rejected
          </span>
        ) : p.paid ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Paid
          </span>
        ) : (
          <span className="fy-chip fy-chip--serious">
            <Icon of={Hourglass} />
            Waiting for the bank
          </span>
        )}
        {p.remittance === "sent" && (
          <span className="fy-chip fy-chip--good">
            <Icon of={Mail} />
            Remittance sent
          </span>
        )}
        {p.remittance === "no_email" && (
          <span className="fy-chip fy-chip--warning">
            <Icon of={MailX} />
            No remittance email
          </span>
        )}
      </span>
    );
  }
  if (p.payments_frozen)
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={Ban} />
        Held: bank details changing
      </span>
    );
  if (p.items.some((i) => i.checks.length > 0))
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={CircleX} />
        Blocked
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Ready
    </span>
  );
}

// CancelBatch cancels an approved batch with a reason: its bills and claims are free again.
function CancelBatch({ open, batch, etag, onClose, onDone }: { open: boolean; batch: Batch; etag: string; onClose: () => void; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("batch.cancel");
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/payment-batches/{batch_id}/cancel", {
      params: { path: { batch_id: batch.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      body: { reason: reason.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Cancelled ${batchName(batch)}: its bills and claims are free for another batch.`);
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={batchName(batch)} title={h.label} description="It pays nothing. Its bills and claims are free for another batch, or to pay by hand.">
      {problem && <Refusal problem={problem} title="The batch was not cancelled" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is cancelled
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
              Say why: the audit trail keeps the reason.
            </span>
          )}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Keep it
          </Button>
          <Button variant="destructive" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Ban} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// save keeps a file the browser downloads: the payment list, from its base64.
function save(name: string, type: string, base64: string) {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  if (typeof URL.createObjectURL !== "function") return;
  const url = URL.createObjectURL(new Blob([bytes], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

// PaymentBatchPage is one payment batch (docs/design/screens PaymentBatch; FR-BNK-12, FR-AP-22, G4-055, SOD-1, SOD-2): its
// payments by payee and what holds any, then asking for approval, approving again after a payee's change, releasing it,
// its payment list, and whether each payment is paid and its supplier sent remittance advice.
export function PaymentBatchPage() {
  const { batchId } = useParams({ strict: false }) as { batchId: string };
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const roles = session.roles ?? [];
  const prepares = roles.some((r) => batchPreparers.includes(r));
  const releases = roles.some((r) => batchReleasers.includes(r));
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [cancelling, setCancelling] = useState(false);
  const [file, setFile] = useState<{ name: string; sha256: string } | null>(null);
  const ask = helpText("batch.ask");
  const again = helpText("batch.reapprove");
  const release = helpText("batch.release");
  const download = helpText("batch.download");
  const cancel = helpText("batch.cancel");
  const q = useQuery({
    queryKey: ["payment-batch", batchId],
    queryFn: async () => {
      const r = await api.GET("/v1/payment-batches/{batch_id}", { params: { path: { batch_id: batchId } } });
      if (r.error) throw r.error;
      return { batch: r.data, etag: etagOf(r.response) };
    },
  });
  const refresh = () => queries.invalidateQueries({ queryKey: ["payment-batch", batchId] });
  const b = q.data?.batch;
  const etag = q.data?.etag ?? "";
  const released = b?.status === "released" || b?.status === "paid";
  const errors = (b?.checks ?? []).filter((c) => c.severity === "error");

  const run = async (what: string, call: () => Promise<{ data?: unknown; error?: unknown }>, done: string) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    setFailed(what);
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus(done);
    await refresh();
  };
  const header = () => ({ "If-Match": etag, "Idempotency-Key": newKey() });
  const remove = async (p: Payment, docId: string) => {
    if (!b) return;
    const items = b.payments.flatMap((x) => x.items).filter((i) => i.document_id !== docId).map((i) => ({ kind: i.kind, document_id: i.document_id }));
    await run(
      "The item was not removed",
      () => api.PUT("/v1/payment-batches/{batch_id}", { params: { path: { batch_id: b.id }, header: { "If-Match": etag } }, body: { money_account_id: b.money_account_id, payment_date: b.payment_date, method: b.method, notes: b.notes, items } }),
      `Removed it from the batch: ${p.payee.name} is not paid it in this batch.`,
    );
  };
  const getFile = async () => {
    if (!b) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.GET("/v1/payment-batches/{batch_id}/file", { params: { path: { batch_id: b.id } } }));
    setBusy(false);
    setFailed("The payment list was not downloaded");
    if (!out.data) return setProblem(problemOf(out.error));
    save(out.data.file_name, out.data.media_type, out.data.content_base64);
    setFile({ name: out.data.file_name, sha256: out.data.sha256 });
    setStatus(`Downloaded ${out.data.file_name}. Key each payment into your bank's bulk payment screen, check the count and total, then delete the file.`);
  };

  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The payment batch could not be shown" />;
  if (!b) return <p className="fy-secondary">Loading the payment batch…</p>;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
            <Link to="/purchases/payment-batches">Payment batches</Link>
          </nav>
          <h1 className="fy-h1">{batchName(b)}</h1>
          <p className="fy-secondary">
            Pays {date(b.payment_date)} from {b.money_account_name ?? "its bank account"} by {b.method.toUpperCase()}
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <BatchStatus batch={b} />
          {b.status === "draft" && prepares && (
            <Button
              variant="primary"
              onClick={() => void run("Approval was not asked for", () => api.POST("/v1/payment-batches/{batch_id}/approval", { params: { path: { batch_id: b.id }, header: header() } }), "Asked for approval: another person approves it before it is released.")}
              disabled={busy || errors.length > 0}
              title={ask.tooltip}
            >
              <Icon of={UserCheck} />
              {ask.label}
            </Button>
          )}
          {b.status === "waiting" && b.approval_id && (
            <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: b.approval_id }}>
              Open the approval
            </Link>
          )}
          {b.status === "approved" && b.reapproval_needed && prepares && (
            <Button
              variant="primary"
              onClick={() => void run("Approval was not asked for again", () => api.POST("/v1/payment-batches/{batch_id}/reapproval", { params: { path: { batch_id: b.id }, header: header() } }), "Asked for its approval again: the approver sees the accounts and total it pays now.")}
              disabled={busy || errors.length > 0}
              title={again.tooltip}
            >
              <Icon of={UserCheck} />
              {again.label}
            </Button>
          )}
          {b.status === "approved" && !b.reapproval_needed && releases && (
            <Button
              variant="primary"
              onClick={() => void run("The batch was not released", () => api.POST("/v1/payment-batches/{batch_id}/release", { params: { path: { batch_id: b.id }, header: header() } }), "Released: its payments are posted. Download the payment list and pay it in the bank.")}
              disabled={busy || errors.length > 0}
              title={release.tooltip}
            >
              <Icon of={Send} />
              {release.label}
            </Button>
          )}
          {released && releases && (
            <Button variant="secondary" onClick={() => void getFile()} disabled={busy} title={download.tooltip}>
              <Icon of={Download} />
              {download.label}
            </Button>
          )}
          {b.status === "approved" && prepares && (
            <Button variant="ghost" onClick={() => setCancelling(true)} disabled={busy} title={cancel.tooltip}>
              <Icon of={Ban} />
              {cancel.label}
            </Button>
          )}
        </div>
      </div>
      <ReviewBanner screen="payment-batch" />
      <p className="fy-small fy-secondary" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title={failed} />}
      {b.status === "approved" && b.reapproval_needed && (
        <div className="fy-banner fy-banner--critical" role="note">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Release is blocked: the batch changed after it was approved</span>
            <span className="fy-small">A payee's bank account, or what is open on an item, changed after the approval. A payment approved before a bank-detail change always needs approving again. Ask for its approval again, or cancel it.</span>
          </div>
        </div>
      )}
      <Checks list={b.checks} />

      <section className="fy-card" aria-labelledby="payments-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="payments-title">
            Payments in this batch
          </h2>
          <span className="fy-small fy-muted">SGD</span>
        </div>
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-labelledby="payments-title">
            <thead>
              <tr>
                <th scope="col">Payee</th>
                <th scope="col">Pays</th>
                <th scope="col">Into</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {b.payments.map((p) => (
                <tr key={`${p.payee.kind}-${p.payee.id}`}>
                  <td data-card="title">
                    {p.payee.name}
                    <span className="fy-small fy-muted block">{p.payee.kind === "person" ? "Claimant" : "Supplier"}</span>
                  </td>
                  <td data-card="meta">
                    <ul className="fy-stack fy-stack--1">
                      {p.items.map((i) => (
                        <li key={i.document_id} className="fy-row fy-row--wrap gap-2">
                          <span className="fy-mono">{i.number || "Unnumbered"}</span>
                          <span className="fy-num">{formatAmount(i.open.amount)}</span>
                          {i.checks.map((c, k) => (
                            <span key={k} className="fy-small fy-error">
                              {c.message}
                            </span>
                          ))}
                          {b.status === "draft" && prepares && (
                            <Button variant="ghost" size="sm" onClick={() => void remove(p, i.document_id)} disabled={busy} aria-label={`Remove ${i.number || "the item"} from the batch`}>
                              <Icon of={Trash2} />
                            </Button>
                          )}
                        </li>
                      ))}
                    </ul>
                  </td>
                  <td data-card="meta">{p.account ? `${p.account.bic} · ••••${p.account.account_number_last4}` : released ? "As the payment list says" : "No approved account"}</td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(p.amount.amount)}
                  </td>
                  <td data-card="status">
                    <PaymentState p={p} released={released} />
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <th scope="row" colSpan={3}>
                  {released ? "Paid in this batch" : "To pay"}
                </th>
                <td className="fy-num fy-strong">{formatAmount(b.total.amount)}</td>
                <td />
              </tr>
            </tfoot>
          </table>
        </div>
      </section>

      {b.release && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="release-title">
          <h2 className="fy-h3" id="release-title">
            Payment list
          </h2>
          <p className="fy-small">
            Released by {b.release.released_by.name ?? "a member"} on {dateTime(b.release.released_at)}. Key each payment into your bank's bulk payment screen; the batch is paid when the bank statement shows each one.
          </p>
          <p className="fy-small fy-secondary">
            {b.release.file_name} · SHA-256 <span className="fy-mono break-all">{b.release.sha256}</span>
          </p>
          {file && file.sha256 !== b.release.sha256 && <p className="fy-error">The downloaded file does not match the one released: do not use it.</p>}
        </section>
      )}

      {cancelling && (
        <CancelBatch
          open
          batch={b}
          etag={etag}
          onClose={() => setCancelling(false)}
          onDone={async (what) => {
            setCancelling(false);
            setStatus(what);
            await refresh();
          }}
        />
      )}
    </div>
  );
}
