import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, Gavel } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Record = components["schemas"]["InvoiceDispute"];

// A customer's dispute of an issued invoice, or their promise to pay it (FR-AR-08, P2): an open dispute pauses the invoice's reminders until it is
// resolved, an open promise until its day has passed, and AR ageing shows the disputed amount apart. It posts nothing.
export function DisputeCard({ invoiceId, mayRecord, open }: { invoiceId: string; mayRecord: boolean; open: string }) {
  const id = useId();
  const queries = useQueryClient();
  const key = ["invoice-disputes", invoiceId];
  const [kind, setKind] = useState<"dispute" | "promise">("dispute");
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");
  const [on, setOn] = useState("");
  const [resolving, setResolving] = useState<string | null>(null);
  const [resolution, setResolution] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const records = useQuery({
    queryKey: key,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/invoices/{invoice_id}/disputes", { params: { path: { invoice_id: invoiceId } } });
      if (error) throw error;
      return data.records;
    },
  });
  const list = records.data ?? [];
  if (records.isLoading || (list.length === 0 && !mayRecord)) return null;
  const hasOpen = (k: string) => list.some((x: Record) => x.status === "open" && x.kind === k);
  // The kind the form offers: the one chosen, or the one that has no open record yet.
  const free = (["dispute", "promise"] as const).filter((k) => !hasOpen(k));
  const shown: "dispute" | "promise" = free.includes(kind) ? kind : (free[0] ?? kind);
  const record = async () => {
    setBusy(true);
    setProblem(null);
    const body = shown === "dispute" ? { kind, amount: amount.trim(), reason: reason.trim() } : { kind, promised_on: on };
    const out = await api.POST("/v1/invoices/{invoice_id}/disputes", { params: { path: { invoice_id: invoiceId }, header: { "Idempotency-Key": newKey() } }, body });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setAmount("");
    setReason("");
    setOn("");
    await queries.invalidateQueries({ queryKey: key });
  };
  const resolve = async (recordId: string) => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/invoice-disputes/{dispute_id}/resolution", {
      params: { path: { dispute_id: recordId }, header: { "Idempotency-Key": newKey() } },
      body: { resolution: resolution.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setResolving(null);
    setResolution("");
    await queries.invalidateQueries({ queryKey: key });
  };
  const h = helpText("invoice.dispute");
  const r$ = helpText("invoice.dispute-resolve");
  return (
    <section className="fy-card" aria-labelledby={`${id}-head`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-head`}>
          Dispute or promise to pay
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The record was not saved" />}
        {list.length > 0 && (
          <ul className="fy-list">
            {list.map((x: Record) => (
              <li className="fy-list__item" key={x.id}>
                <span className="fy-list__icon">
                  <Icon of={x.kind === "dispute" ? Gavel : CalendarClock} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {x.kind === "dispute" ? `Disputed ${x.amount ? formatAmount(x.amount.amount) + " " + x.amount.currency : ""}: ${x.reason ?? ""}` : `Promised to pay on ${date(x.promised_on ?? "")}`}
                  </span>
                  <span className="fy-list__meta">
                    {x.status === "open" ? "Open: its reminders are paused" : `Resolved: ${x.resolution ?? ""}`}
                  </span>
                </span>
                {mayRecord && x.status === "open" && (
                  <Button size="sm" variant="ghost" type="button" onClick={() => setResolving(x.id)} disabled={busy} title={r$.tooltip}>
                    {r$.label}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {resolving && (
          <div className="fy-row fy-row--wrap gap-2">
            <label className="sr-only" htmlFor={`${id}-res`}>
              How it ended
            </label>
            <input className="fy-input" id={`${id}-res`} value={resolution} maxLength={500} placeholder="How it ended, such as: credit note issued" onChange={(e) => setResolution(e.target.value)} />
            <Button type="button" variant="primary" onClick={() => void resolve(resolving)} disabled={busy || !resolution.trim()}>
              Save
            </Button>
            <Button type="button" variant="ghost" onClick={() => setResolving(null)} disabled={busy}>
              Cancel
            </Button>
          </div>
        )}
        {mayRecord && !(hasOpen("dispute") && hasOpen("promise")) && (
          <form
            className="fy-row fy-row--wrap gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void record();
            }}
          >
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-kind`}>
                The customer
              </label>
              <select className="fy-input" id={`${id}-kind`} value={shown} onChange={(e) => setKind(e.target.value as "dispute" | "promise")}>
                {!hasOpen("dispute") && <option value="dispute">disputes part of it</option>}
                {!hasOpen("promise") && <option value="promise">promises to pay on a day</option>}
              </select>
            </div>
            {shown === "dispute" ? (
              <>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-amount`}>
                    Amount in dispute (at most {open})
                  </label>
                  <input className="fy-input fy-num" id={`${id}-amount`} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-reason`}>
                    Why
                  </label>
                  <input className="fy-input" id={`${id}-reason`} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} />
                </div>
              </>
            ) : (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-on`}>
                  The day they promise to pay
                </label>
                <input className="fy-input" id={`${id}-on`} type="date" value={on} onChange={(e) => setOn(e.target.value)} />
              </div>
            )}
            <div className="fy-field self-end">
              <Button type="submit" variant="secondary" disabled={busy} title={h.tooltip}>
                {h.label}
              </Button>
            </div>
          </form>
        )}
      </div>
    </section>
  );
}
