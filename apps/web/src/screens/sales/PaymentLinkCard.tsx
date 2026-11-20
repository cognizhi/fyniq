import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, ExternalLink, Link2, X } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Link = components["schemas"]["InvoicePaymentLink"];

const words: Record<string, string> = { live: "Live", paid: "Paid", expired: "Expired", cancelled: "Cancelled" };

// A payment link on an issued invoice (FR-AR-13, FR-PSP-02): the provider's page where the customer pays what is open, asked for
// by a person with a right on Invoices, with the charges the provider made under it. The amount is the invoice's open balance
// read by the server; this card sends none. A paid link is settled by Fyniq's own reconciliation of the provider's line, never here.
export function PaymentLinkCard({ invoiceId, mayAsk }: { invoiceId: string; mayAsk: boolean }) {
  const id = useId();
  const queries = useQueryClient();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [reason, setReason] = useState("");
  const [copied, setCopied] = useState(false);
  const key = ["invoice-payment-link", invoiceId];
  const link = useQuery({
    queryKey: key,
    queryFn: async (): Promise<Link | null> => {
      const { data, response, error } = await api.GET("/v1/invoices/{invoice_id}/payment-link", { params: { path: { invoice_id: invoiceId } } });
      if (response.status === 404) return null;
      if (!data) throw problemOf(error);
      return data;
    },
  });
  const l = link.data ?? null;
  const live = l?.status === "live";
  if (link.isLoading || (!l && !mayAsk)) return null;
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/invoices/{invoice_id}/payment-link", {
      params: { path: { invoice_id: invoiceId }, header: { "Idempotency-Key": newKey() } },
      body: {},
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    queries.setQueryData(key, out.data); // the server's answer (FR-UX-22)
  };
  const cancel = async () => {
    if (!reason.trim()) return setProblem({ ...problemOf(undefined), detail: "Say why the link is cancelled." });
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/invoices/{invoice_id}/payment-link/cancellation", {
      params: { path: { invoice_id: invoiceId }, header: { "Idempotency-Key": newKey() } },
      body: { reason: reason.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    queries.setQueryData(key, out.data); // the server's answer (FR-UX-22)
    setCancelling(false);
    setReason("");
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(l?.url ?? "");
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  const ask$ = helpText("invoice.payment-link");
  const cancel$ = helpText("invoice.payment-link-cancel");
  return (
    <section className="fy-card" aria-labelledby={`${id}-link`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-link`}>
          Payment link
        </h2>
        {l && <span className={`fy-chip fy-chip--${live ? "info" : l.status === "paid" ? "success" : "neutral"}`}>{words[l.status] ?? l.status}</span>}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The payment link was not made" />}
        {!l && (
          <>
            <p className="fy-secondary">Let the customer pay what is open from a page of your payment provider. When they pay, Fyniq matches the provider's line to this invoice by itself.</p>
            <div>
              <Button variant="primary" type="button" onClick={() => void ask()} disabled={busy} title={ask$.tooltip}>
                <Icon of={Link2} />
                {ask$.label}
              </Button>
            </div>
          </>
        )}
        {l && (
          <>
            <p className="fy-small">
              For {formatAmount(l.amount.amount)} {l.amount.currency}, reference {l.reference}
              {l.expires_at ? `, works until ${date(l.expires_at)}` : ""}.
            </p>
            {live && l.url && (
              <div className="fy-row fy-row--wrap gap-2">
                <input className="fy-input" aria-label="The payment link" readOnly value={l.url} onFocus={(e) => e.currentTarget.select()} />
                <Button size="sm" variant="secondary" type="button" onClick={() => void copy()}>
                  <Icon of={Copy} />
                  {copied ? "Copied" : "Copy"}
                </Button>
                <a className="fy-button fy-button--sm fy-button--secondary" href={l.url} target="_blank" rel="noopener noreferrer">
                  <Icon of={ExternalLink} />
                  Open
                </a>
              </div>
            )}
            {l.closed_reason && <p className="fy-small fy-muted">Closed: {l.closed_reason}</p>}
            {l.payments.length > 0 && (
              <ul className="fy-small">
                {l.payments.map((p) => (
                  <li key={p.provider_payment_id}>
                    {formatAmount(p.amount.amount)} {p.amount.currency} paid on {date(p.paid_at)} (the provider's charge {p.provider_payment_id})
                  </li>
                ))}
              </ul>
            )}
            {live && mayAsk && (
              <div>
                <Button size="sm" variant="ghost" type="button" onClick={() => setCancelling(true)} disabled={busy} title={cancel$.tooltip}>
                  <Icon of={X} />
                  {cancel$.label}
                </Button>
              </div>
            )}
            {!live && mayAsk && l.status !== "paid" && (
              <div>
                <Button variant="secondary" type="button" onClick={() => void ask()} disabled={busy} title={ask$.tooltip}>
                  <Icon of={Link2} />
                  {ask$.label}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
      {cancelling && (
        <Dialog
          open
          onOpenChange={(o) => !o && setCancelling(false)}
          dismissible={!busy}
          title="Cancel the payment link"
          description="Fyniq closes the link here so a new one can be made. The provider's page stays payable until it expires, and a payment made on it waits for a person in Banking."
          footer={
            <>
              <Button variant="ghost" onClick={() => setCancelling(false)} disabled={busy}>
                Keep it
              </Button>
              <Button variant="primary" onClick={() => void cancel()} disabled={busy}>
                Cancel the link
              </Button>
            </>
          }
        >
          {problem && <Refusal problem={problem} title="The link was not cancelled" />}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-why`}>
              Why
            </label>
            <input className="fy-input" id={`${id}-why`} value={reason} maxLength={500} placeholder="Such as: the customer asked for a new link" onChange={(e) => setReason(e.target.value)} />
          </div>
        </Dialog>
      )}
    </section>
  );
}
