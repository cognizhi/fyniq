import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, Clock, Download, FileText, XCircle } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type PublicDocument = components["schemas"]["PublicDocument"];

// tokenOf is the link's token, from the address's fragment (#…): the fragment never reaches a server, and the token
// goes to the API only in the Fyniq-Link header (the threat model docs/security/threat-models/document-links.md).
export function tokenOf(hash: string): string {
  const t = hash.replace(/^#/, "").trim();
  return /^[A-Za-z0-9_-]{20,64}$/.test(t) ? t : "";
}

const kinds: Record<PublicDocument["kind"], string> = { invoice: "Invoice", quote: "Quote", sales_order: "Sales order" };

const money = (m: components["schemas"]["Money"]) => `${m.currency} ${formatAmount(m.amount)}`;

// Answer is the customer's acceptance or decline of a quote through its link: a name to accept, and a comment, kept
// with the quote as a record of the decision (FR-AR-16).
function Answer({ doc, token, onDone }: { doc: PublicDocument; token: string; onDone: (message: string) => void }) {
  const id = useId();
  const [name, setName] = useState("");
  const [comment, setComment] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (accept: boolean, e?: FormEvent) => {
    e?.preventDefault();
    if (accept && !name.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const params = { header: { "Fyniq-Link": token, "Idempotency-Key": newKey() } };
    const body = { name: name.trim() || undefined, comment: comment.trim() || undefined };
    const out = accept ? await api.POST("/v1/public/document/accept", { params, body }) : await api.POST("/v1/public/document/decline", { params, body });
    setBusy(false);
    if (out.error) return setProblem(problemOf(out.error));
    onDone(accept ? `Accepted. ${doc.supplier} has your acceptance.` : `Declined. ${doc.supplier} has your answer.`);
  };
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-head`}>
      <h2 className="fy-h3" id={`${id}-head`}>
        Your answer
      </h2>
      {problem && <Refusal problem={problem} title="Your answer was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(true, e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Your full name
          </label>
          <input
            className="fy-input"
            id={`${id}-name`}
            value={name}
            maxLength={200}
            autoComplete="name"
            onChange={(e) => {
              setName(e.target.value);
              setMissing(false);
            }}
            aria-invalid={missing || undefined}
            aria-describedby={missing ? `${id}-name-error` : undefined}
          />
          {missing && (
            <span className="fy-error" id={`${id}-name-error`}>
              Enter your name to accept the quote.
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-comment`}>
            Comment (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-comment`} value={comment} maxLength={2000} onChange={(e) => setComment(e.target.value)} aria-describedby={`${id}-comment-help`} />
          <span className="fy-help" id={`${id}-comment-help`}>
            Your name, today's date and your comment are kept with the quote as a record of your decision.
          </span>
        </div>
        <div className="fy-row fy-row--wrap gap-2">
          <Button variant="primary" type="submit" disabled={busy}>
            <Icon of={CircleCheck} />
            Accept the quote
          </Button>
          <Button type="button" onClick={() => void run(false)} disabled={busy}>
            <Icon of={XCircle} />
            Decline
          </Button>
        </div>
      </form>
    </section>
  );
}

// DocumentView is what a customer sees through the secure link an email carried (docs/design/screens
// CustomerDocumentView; FR-AR-06, FR-AR-16): the quote or invoice as its PDF shows it, the PDF to download, and for a
// sent quote still valid, accepting or declining it. No sign-in: the link's token, from the address's fragment, is the
// key, and the page shows nothing beyond the document.
export function DocumentView() {
  const queries = useQueryClient();
  const token = tokenOf(typeof window === "undefined" ? "" : window.location.hash);
  const [status, setStatus] = useState("");
  const [downloading, setDownloading] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const one = useQuery({
    queryKey: ["public-document", token],
    enabled: Boolean(token),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/public/document", { params: { header: { "Fyniq-Link": token } } });
      if (error) throw error;
      return data;
    },
  });
  const doc = one.data;
  const download = async () => {
    if (!doc) return;
    setDownloading(true);
    setProblem(null);
    const { data, error } = await api.GET("/v1/public/document/pdf", { params: { header: { "Fyniq-Link": token } } });
    setDownloading(false);
    if (error || !data) return setProblem(problemOf(error));
    const bytes = Uint8Array.from(atob(data.pdf), (c) => c.charCodeAt(0));
    const url = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${doc.number}.pdf`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (!token) {
    return (
      <main className="shell fy-stack" id="main">
        <h1 className="fy-h1">This link is incomplete</h1>
        <p className="fy-lead">Open the link from the email again, as it was sent: the part after # is its key.</p>
      </main>
    );
  }
  return (
    <main className="shell fy-stack fy-stack--4" id="main">
      {one.isPending && <p className="fy-secondary">Opening the document…</p>}
      {one.isError && (
        <>
          <h1 className="fy-h1">This link does not open a document</h1>
          <Refusal problem={problemOf(one.error)} title="The link is not valid, has ended or was revoked" />
          <p className="fy-secondary">Ask the sender for a new link.</p>
        </>
      )}
      {doc && (
        <>
          <header className="fy-stack fy-stack--2">
            <p className="fy-secondary">
              {doc.supplier} · for {doc.customer}
            </p>
            <h1 className="fy-h1">
              {doc.kind === "invoice" && doc.tax_invoice ? "Tax invoice" : kinds[doc.kind]} {doc.number}
            </h1>
            <p className="fy-lead">
              {doc.kind === "invoice" && doc.amount_due ? `Amount due ${money(doc.amount_due)}` : `Total ${money(doc.total)}`}
              {doc.due_date ? ` · due ${date(doc.due_date)}` : ""}
              {doc.valid_until ? ` · valid until ${date(doc.valid_until)}` : ""}
            </p>
            <div className="fy-row fy-row--wrap gap-2">
              <Button type="button" onClick={() => void download()} disabled={downloading} title={helpText("public.download").tooltip}>
                <Icon of={Download} />
                {helpText("public.download").label}
              </Button>
            </div>
          </header>
          {status && (
            <p className="fy-banner fy-banner--info" role="status">
              {status}
            </p>
          )}
          {problem && <Refusal problem={problem} title="The PDF could not be downloaded" />}
          {doc.answer && (
            <div className="fy-banner fy-banner--info" role="note">
              <Icon of={doc.answer.outcome === "accepted" ? CircleCheck : doc.answer.outcome === "declined" ? XCircle : Clock} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">
                  {doc.answer.outcome === "accepted" ? "Accepted" : doc.answer.outcome === "declined" ? "Declined" : "Expired"} on {date(doc.answer.date)}
                  {doc.answer.name ? ` by ${doc.answer.name}` : ""}
                </span>
              </div>
            </div>
          )}
          <section className="fy-card" aria-label={`${kinds[doc.kind]} ${doc.number}`}>
            <div className="fy-card__body fy-stack fy-stack--3">
              <p className="fy-small">
                Dated {date(doc.date)}
                {doc.reference ? ` · your reference ${doc.reference}` : ""}
              </p>
              <table className="fy-table fy-table--compact" aria-label="Lines">
                <thead>
                  <tr>
                    <th scope="col">Description</th>
                    <th scope="col" className="fy-num">
                      Qty
                    </th>
                    <th scope="col" className="fy-num">
                      Unit price
                    </th>
                    <th scope="col">GST</th>
                    <th scope="col" className="fy-num">
                      Amount ({doc.total.currency})
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {doc.lines.map((l, i) => (
                    <tr key={i}>
                      <td>{l.description}</td>
                      <td className="fy-num">{l.quantity ?? ""}</td>
                      <td className="fy-num">{l.unit_price ? formatAmount(l.unit_price) : ""}</td>
                      <td>{l.gst_rate ?? ""}</td>
                      <td className="fy-num">{formatAmount(l.amount.amount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="fy-stack fy-stack--1 self-end">
                <div className="fy-row fy-row--between gap-6">
                  <span className="fy-secondary">Before GST</span>
                  <span className="fy-num">{money(doc.subtotal)}</span>
                </div>
                <div className="fy-row fy-row--between gap-6">
                  <span className="fy-secondary">GST</span>
                  <span className="fy-num">{money(doc.gst)}</span>
                </div>
                <div className="fy-row fy-row--between gap-6">
                  <span className="fy-strong">Total</span>
                  <span className="fy-num fy-strong">{money(doc.total)}</span>
                </div>
                {doc.amount_due && (
                  <div className="fy-row fy-row--between gap-6">
                    <span className="fy-strong">Amount due</span>
                    <span className="fy-num fy-strong">{money(doc.amount_due)}</span>
                  </div>
                )}
              </div>
            </div>
          </section>
          {doc.can_answer && (
            <Answer
              doc={doc}
              token={token}
              onDone={(message) => {
                setStatus(message);
                void queries.invalidateQueries({ queryKey: ["public-document", token] });
              }}
            />
          )}
          <p className="fy-small fy-muted">
            <Icon of={FileText} /> This link works until {date(doc.link_expires_at.slice(0, 10))} at the latest. Keep the PDF for your records.
          </p>
        </>
      )}
    </main>
  );
}
