import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, Clock, FilePlus2, Hourglass, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { issuers, SalesTabs } from "./Invoices";
import { SubscriptionContracts } from "./SubscriptionContracts";

type Invoice = components["schemas"]["PaymentProviderInvoice"];
type Subscription = components["schemas"]["PaymentProviderSubscription"];

const money = (m: components["schemas"]["Money"]) => `${m.currency} ${formatAmount(m.amount)}`;

// stateOf is a mirrored invoice's standing in words, with the icon its tone needs: colour never says it alone (FR-VIZ-05).
function stateOf(i: Invoice): { label: string; chip: string; icon: typeof Clock } {
  switch (i.state) {
    case "posted":
      return { label: "Issued", chip: "good", icon: CircleCheck };
    case "drafted":
      return { label: "Draft made, waiting to be issued", chip: "info", icon: Hourglass };
    case "parked":
      return { label: "Waiting for a person", chip: "warning", icon: TriangleAlert };
    default:
      return { label: "Read, no invoice yet", chip: "neutral", icon: Clock };
  }
}

// Subscriptions is what a billing provider bills (Model B, FR-PSP-03, FR-PSP-07), mirrored read-only: each subscription with what it bills each interval, and each
// invoice with the provider's collection state (the attempts so far, the next retry, the outcome) beside the AR document made of it. Fyniq never retries or charges.
export function Subscriptions() {
  const id = useId();
  const queries = useQueryClient();
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => issuers.includes(r));
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState("");
  const one = useQuery({
    queryKey: ["provider-invoices"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/payment-providers/invoices");
      if (error) throw error;
      return data;
    },
  });
  const post = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/payment-providers/invoices/post");
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setSaid(`${out.data.posted} issued, ${out.data.parked} set aside for a person, ${out.data.waiting} waiting.`);
    await queries.invalidateQueries({ queryKey: ["provider-invoices"] });
  };
  const invoices = one.data?.invoices ?? [];
  const subs = one.data?.subscriptions ?? [];
  const parked = invoices.filter((i) => i.state === "parked");
  const toFix = invoices.filter((i) => i.state === "posted" && (i.status === "void" || i.status === "uncollectible"));
  const drafted = invoices.some((i) => i.state === "drafted");
  const h = helpText("psp.post-invoices");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
          </nav>
          <h1 className="fy-h1">{helpText("sales.subscriptions").label}</h1>
          <p className="fy-secondary">What your billing provider bills your customers, mirrored read-only: Fyniq never retries a payment or charges anyone, and its own reminders do not run on these invoices.</p>
        </div>
        {may && drafted && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" type="button" onClick={() => void post()} disabled={busy} title={h.tooltip}>
              <Icon of={FilePlus2} />
              {h.label}
            </Button>
          </div>
        )}
      </div>
      <SalesTabs />
      {said && (
        <p className="fy-banner fy-banner--info" role="status">
          {said}
        </p>
      )}
      {problem && <Refusal problem={problem} title="The invoices were not issued" />}
      {one.isError && <Refusal problem={problemOf(one.error)} title="The provider's invoices could not be read" />}
      <SubscriptionContracts may={may} />
      {one.isPending && <p className="fy-secondary">Loading what the provider bills…</p>}
      {parked.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="alert">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{parked.length === 1 ? "An invoice waits" : `${parked.length} invoices wait`} for a person</span>
            <span className="fy-small">Fyniq did not make or issue them, and nothing was posted: {parked[0]?.parked_reason}</span>
          </div>
        </div>
      )}
      {toFix.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="alert">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">The provider voided or wrote off {toFix.length === 1 ? "an invoice" : `${toFix.length} invoices`} you issued</span>
            <span className="fy-small">Void it or write it off under Invoices: {toFix.map((i) => i.number || i.provider_invoice_id).join(", ")}.</span>
          </div>
        </div>
      )}
      {one.data && subs.length === 0 && invoices.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Clock} size={24} />
          <h2 className="fy-h3">No provider is billing customers yet</h2>
          <p className="fy-secondary">Name a revenue account on a Stripe provider account under Banking, then its subscriptions and invoices appear here.</p>
          <Link className="fy-btn fy-btn--primary" to="/banking/payouts">
            Set up a provider account
          </Link>
        </div>
      )}
      {subs.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-subs`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-subs`}>
              Subscriptions
            </h2>
            <span className="fy-chip fy-chip--neutral">Read-only mirror</span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table" aria-label="Subscriptions">
              <thead>
                <tr>
                  <th scope="col">Customer</th>
                  <th scope="col">Bills</th>
                  <th scope="col">Status</th>
                  <th scope="col">Period ends</th>
                </tr>
              </thead>
              <tbody>
                {subs.map((s: Subscription) => (
                  <tr key={s.id}>
                    <th scope="row">{s.customer}</th>
                    <td>
                      {money(s.amount)} {s.interval}
                    </td>
                    <td>
                      {s.status}
                      {s.cancel_at_period_end ? ", ends with the period" : ""}
                    </td>
                    <td>{s.current_period_end ? date(s.current_period_end) : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {invoices.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-inv`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-inv`}>
              Invoices
            </h2>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table" aria-label="Provider invoices">
              <thead>
                <tr>
                  <th scope="col">Invoice</th>
                  <th scope="col">Customer</th>
                  <th scope="col" className="fy-num">
                    Total
                  </th>
                  <th scope="col">Provider says</th>
                  <th scope="col">In Fyniq</th>
                </tr>
              </thead>
              <tbody>
                {invoices.map((i: Invoice) => {
                  const st = stateOf(i);
                  return (
                    <tr key={i.id}>
                      <th scope="row">
                        {i.document_id ? <Link to="/sales/invoices/$invoiceId" params={{ invoiceId: i.document_id }}>{i.number || i.provider_invoice_id}</Link> : i.number || i.provider_invoice_id}
                      </th>
                      <td>{i.customer}</td>
                      <td className="fy-num">{money(i.total)}</td>
                      <td>
                        {i.status}
                        {i.status === "open" && i.attempt_count > 0 ? `, ${i.attempt_count} ${i.attempt_count === 1 ? "attempt" : "attempts"} so far` : ""}
                        {i.next_attempt_on ? `, next on ${date(i.next_attempt_on)}` : ""}
                      </td>
                      <td>
                        <span className={`fy-chip fy-chip--${st.chip}`}>
                          <Icon of={st.icon} /> {st.label}
                        </span>
                        {i.state === "parked" && (
                          <span className="fy-small fy-muted">
                            {" "}
                            <Icon of={CircleX} /> {i.parked_reason}
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
