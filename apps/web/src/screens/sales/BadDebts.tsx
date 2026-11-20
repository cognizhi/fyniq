import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, ClockAlert, Hourglass, ReceiptText } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Relief = components["schemas"]["BadDebtRelief"];

const words: Record<Relief["status"], [string, string, typeof CircleCheck]> = {
  not_yet: ["Not yet", "fy-chip--neutral", Hourglass],
  eligible: ["Claim now", "fy-chip--good", CircleCheck],
  expired: ["Out of time", "fy-chip--serious", ClockAlert],
};

// Who claims the relief: C on GST return, filing (the Accountant, the Finance controller and the Tax agent; Appendix D).
const claimers = ["accountant", "controller", "tax_agent"];

const today = () => new Date().toISOString().slice(0, 10);

async function queue() {
  const { data, error } = await api.GET("/v1/tax/bad-debt-relief", {});
  if (error) throw error;
  return data;
}

// BadDebts is the queue of invoices written off as a bad debt that bore GST, with when a claim may be made and until when (FR-AR-11). It shows what the server answers: the dates are
// the sg-tax-filings pack's, never worked out here (FR-UX-22). The claim itself goes in the GST return.
export function BadDebts() {
  const { session } = useSignedIn();
  const mayClaim = (session.roles ?? []).some((r) => claimers.includes(r));
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["bad-debt-relief"], queryFn: queue });
  const items = q.data?.items ?? [];
  const [claiming, setClaiming] = useState("");
  const [form, setForm] = useState({ date: "", f5_period_end: "", note: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState("");
  const open = (id: string) => {
    setClaiming(id);
    setProblem(null);
    setStatus("");
    setForm({ date: q.data?.today ?? today(), f5_period_end: "", note: "" });
  };
  const claim = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/tax/bad-debt-relief/claims", {
        params: { header: { "Idempotency-Key": newKey() } },
        body: { write_off_id: claiming, date: form.date, ...(form.f5_period_end ? { f5_period_end: form.f5_period_end } : {}), ...(form.note.trim() ? { note: form.note.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus(`Claimed ${formatAmount(out.data.claim.gst.amount)} of GST on ${date(out.data.claim.date)}; it posted to the GST control account.`);
    setClaiming("");
    await queries.invalidateQueries({ queryKey: ["bad-debt-relief"] });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Sales</span>
          <h1 className="fy-h1" title={helpText("sales.bad-debts").tooltip}>
            Bad debts and GST relief
          </h1>
          <p className="fy-secondary">Invoices written off as a bad debt that bore GST, with when the GST may be claimed back from IRAS and until when.</p>
        </div>
      </div>
      {status && (
        <p className="fy-secondary" role="status">
          {status}
        </p>
      )}
      {problem && <Refusal problem={problem} title="The claim was not made" />}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The queue could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the queue…</p>}
      {q.data && items.length === 0 && <p className="fy-secondary">No bad debt that bore GST has been written off.</p>}
      {items.length > 0 && (
        <table className="fy-table">
          <caption className="fy-sr-only">Bad debts that may carry GST relief</caption>
          <thead>
            <tr>
              <th scope="col">Invoice</th>
              <th scope="col">Supplied</th>
              <th scope="col">Written off</th>
              <th scope="col">GST in it</th>
              <th scope="col">Claim from</th>
              <th scope="col">Claim by</th>
              <th scope="col">Status</th>
              {mayClaim && <th scope="col">Relief</th>}
            </tr>
          </thead>
          <tbody>
            {items.map((x) => {
              const [label, tone, icon] = words[x.status];
              return (
                <tr key={x.write_off_id}>
                  <td>{x.invoice_number}</td>
                  <td>{date(x.supply_date)}</td>
                  <td className="fy-num">
                    {x.written_off.currency} {formatAmount(x.written_off.amount)} on {date(x.written_off_on)}
                  </td>
                  <td className="fy-num">
                    {x.gst.currency} {formatAmount(x.gst.amount)}
                  </td>
                  <td>{date(x.eligible_from)}</td>
                  <td>{date(x.claim_by)}</td>
                  <td>
                    <span className={`fy-chip ${tone}`}>
                      <Icon of={icon} />
                      {label}
                    </span>
                  </td>
                  {mayClaim && (
                    <td>
                      {x.claim ? (
                        <span className="fy-chip fy-chip--good">
                          <Icon of={ReceiptText} />
                          Claimed {formatAmount(x.claim.gst.amount)} on {date(x.claim.date)}
                        </span>
                      ) : (
                        x.status === "eligible" && (
                          <Button type="button" variant="secondary" onClick={() => open(x.write_off_id)}>
                            Claim relief for {x.invoice_number}
                          </Button>
                        )
                      )}
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {claiming && (
        <form
          className="fy-card fy-stack"
          aria-label="Claim GST bad debt relief"
          onSubmit={(e) => {
            e.preventDefault();
            void claim();
          }}
        >
          <h2 className="fy-h2">Claim GST bad debt relief</h2>
          <p className="fy-secondary">
            Posts at once: the GST control account is debited and the bad-debt account credited for the invoice&apos;s GST in the part written off. You declare the conditions that are not dates: reasonable
            steps to recover the debt, a price at no more than open market value. Say how below, and attach files to the claim as evidence.
          </p>
          <label className="fy-field">
            <span>Claim date</span>
            <input className="fy-input" type="date" value={form.date} max={q.data?.today} onChange={(e) => setForm({ ...form, date: e.target.value })} required />
          </label>
          <label className="fy-field">
            <span>GST return period ends</span>
            <input className="fy-input" type="date" value={form.f5_period_end} onChange={(e) => setForm({ ...form, f5_period_end: e.target.value })} />
          </label>
          <label className="fy-field">
            <span>Steps taken to recover the debt, or the debtor&apos;s insolvency</span>
            <textarea className="fy-input" maxLength={1000} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
          </label>
          <div className="fy-actions">
            <Button type="submit" disabled={busy}>
              Claim relief
            </Button>
            <Button type="button" variant="secondary" onClick={() => setClaiming("")}>
              Cancel
            </Button>
          </div>
        </form>
      )}
      <p className="fy-small fy-muted">
        Dates come from {q.data?.items[0]?.pack ?? "the sg-tax-filings pack"}. The amount of a claim is worked out on the server, to the cent, half away from zero, and is the accountant&apos;s to confirm.
      </p>
    </div>
  );
}
