import { useQuery } from "@tanstack/react-query";
import { CircleCheck, ClockAlert, Hourglass } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Relief = components["schemas"]["BadDebtRelief"];

const words: Record<Relief["status"], [string, string, typeof CircleCheck]> = {
  not_yet: ["Not yet", "fy-chip--neutral", Hourglass],
  eligible: ["Claim now", "fy-chip--good", CircleCheck],
  expired: ["Out of time", "fy-chip--serious", ClockAlert],
};

async function queue() {
  const { data, error } = await api.GET("/v1/tax/bad-debt-relief", {});
  if (error) throw error;
  return data;
}

// BadDebts is the queue of invoices written off as a bad debt that bore GST, with when a claim may be made and until when (FR-AR-11). It shows what the server answers: the dates are
// the sg-tax-filings pack's, never worked out here (FR-UX-22). The claim itself goes in the GST return.
export function BadDebts() {
  const q = useQuery({ queryKey: ["bad-debt-relief"], queryFn: queue });
  const items = q.data?.items ?? [];
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
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <p className="fy-small fy-muted">Dates come from {q.data?.items[0]?.pack ?? "the sg-tax-filings pack"}. The claim is made in the GST return; recording it here follows.</p>
    </div>
  );
}
