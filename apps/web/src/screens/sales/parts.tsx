import { CircleX, Mail, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "../accounting/Journals";

type Check = components["schemas"]["InvoiceCheck"];
type Money = components["schemas"]["Money"];
type GST = components["schemas"]["InvoiceGST"];
type Deposit = components["schemas"]["InvoiceDeposit"];
export type Send = components["schemas"]["DocumentSendOut"];

// Checks lists what stops a draft going out (✖), issued or sent as verb says, and what it goes out with (⚠).
export function Checks({ checks, verb = "issued" }: { checks: Check[]; verb?: "issued" | "sent" }) {
  const id = useId();
  if (checks.length === 0) return null;
  const errors = checks.filter((c) => c.severity === "error").length;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {errors > 0 ? (errors === 1 ? `1 thing to fix before it can be ${verb}` : `${errors} things to fix before it can be ${verb}`) : `To check before it is ${verb}`}
        </h2>
      </div>
      <ul className="fy-list">
        {checks.map((c, i) => (
          <li key={`${c.code}-${c.field ?? ""}-${i}`} className="fy-list__item">
            <span className={`fy-list__icon ${c.severity === "error" ? "fy-list__icon--critical" : "fy-list__icon--warning"}`}>
              <Icon of={c.severity === "error" ? CircleX : TriangleAlert} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">
                {c.message} <span className="fy-small fy-muted fy-mono">{c.code}</span>
              </span>
              {c.fix && <span className="fy-list__meta whitespace-normal">{c.fix}.</span>}
            </span>
            <span className="fy-sr">{c.severity === "error" ? `Stops it being ${verb}` : "Review it"}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// Totals are a sales document's net, its GST by code, its total, and for an invoice the deposits it deducts and what is
// left to pay, as the server worked them out: never computed here (FR-UX-22).
export function Totals({ doc }: { doc: { currency: string; net?: Money; gst_charged: boolean; gst_subtotals?: GST[] | null; total?: Money; deposits?: Deposit[] | null; balance?: Money } }) {
  const id = useId();
  const cur = doc.currency;
  const row = (label: string, m?: Money, strong = false) =>
    m && (
      <div className="fy-row fy-row--between">
        <span className={strong ? "fy-strong" : "fy-secondary"}>{label}</span>
        <span className={`fy-num${strong ? " fy-strong" : ""}`}>
          {formatAmount(m.amount)} {m.currency}
        </span>
      </div>
    );
  const deposits = doc.deposits ?? [];
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={id}>
      <h2 className="fy-h3" id={id}>
        Totals
      </h2>
      {row("Before GST", doc.net)}
      {doc.gst_charged
        ? (doc.gst_subtotals ?? []).map((g) => (
            <div key={g.gst_code} className="fy-row fy-row--between">
              <span className="fy-secondary">
                GST {g.gst_code} on {formatAmount(g.net.amount)}
              </span>
              <span className="fy-num">
                {formatAmount(g.gst.amount)} {cur}
              </span>
            </div>
          ))
        : doc.net && <p className="fy-small fy-muted">No GST: the organisation is not GST-registered on its date.</p>}
      {row("Total", doc.total, true)}
      {deposits.map((d) => (
        <div key={d.deposit_id} className="fy-row fy-row--between">
          <span className="fy-secondary">Less deposit {d.number ?? ""}</span>
          <span className="fy-num">
            −{formatAmount(d.amount.amount)} {cur}
          </span>
        </div>
      ))}
      {deposits.length > 0 && row("Left to pay", doc.balance, true)}
    </section>
  );
}

const deliveryWords: Record<Send["delivery"], string> = { sent: "Sent", delivered: "Delivered", viewed: "Viewed", bounced: "Bounced" };

// Sends are each email of a document (FR-AR-06): to whom, when, by whom, and its delivery.
export function Sends({ sends }: { sends: Send[] }) {
  const id = useId();
  if (sends.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Emails
        </h2>
      </div>
      <ul className="fy-list">
        {sends.map((s) => (
          <li key={s.id} className="fy-list__item">
            <span className="fy-list__icon">
              <Icon of={Mail} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">To {s.to}</span>
              <span className="fy-list__meta whitespace-normal">
                {dateTime(s.sent_at)}
                {s.sent_by ? ` · by ${who(s.sent_by)}` : ""} · version {s.version}
              </span>
            </span>
            <span className={`fy-chip fy-chip--${s.delivery === "bounced" ? "critical" : s.delivery === "sent" ? "neutral" : "good"}`}>{deliveryWords[s.delivery]}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// EmailDialog emails an issued invoice or a sent quote to the customer's billing email or another address (FR-AR-06).
export function EmailDialog({ open, kind, id: docId, number, etag, onClose, onDone }: { open: boolean; kind: "invoice" | "quote"; id: string; number?: string; etag: string; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [to, setTo] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText(`${kind}.email`);
  const noun = kind === "invoice" ? "invoice" : "quote";
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const header = { "If-Match": etag, "Idempotency-Key": newKey() };
    const body = to.trim() ? { to: to.trim() } : undefined;
    const out =
      kind === "invoice"
        ? await guarded(confirm, () => api.POST("/v1/invoices/{invoice_id}/email", { params: { path: { invoice_id: docId }, header }, body }))
        : await guarded(confirm, () => api.POST("/v1/quotes/{quote_id}/email", { params: { path: { quote_id: docId }, header }, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Emailed to ${out.data.sends?.[0]?.to ?? "the customer"}, with its PDF and a secure link.`);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={number}
      title={`Email this ${noun}?`}
      description={`It goes from the organisation's invoices sender, with the PDF of this version and a secure link the customer opens without signing in. The PDF is kept with the ${noun}.`}
    >
      {problem && <Refusal problem={problem} title={`The ${noun} was not emailed`} />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            Send to (optional)
          </label>
          <input className="fy-input" id={`${id}-to`} type="email" value={to} maxLength={254} onChange={(e) => setTo(e.target.value)} aria-describedby={`${id}-to-help`} />
          <span className="fy-help" id={`${id}-to-help`}>
            Leave it blank for the customer's billing email.
          </span>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Mail} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
