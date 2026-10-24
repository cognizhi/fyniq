import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Ban, CircleCheck, Clock, Eye, FileCheck, FileText, MailCheck, MailX, PencilLine, Plus, Search, Send, XCircle } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { preparers, SalesTabs } from "./Invoices";

export type Quote = components["schemas"]["Quote"];
type Status = Quote["status"];

// statusChip is a quote's status (FR-AR-01), with its icon and words: colour never says it alone.
const statuses: Record<Status, [string, typeof Send, string]> = {
  draft: ["Draft", PencilLine, "neutral"],
  sent: ["Sent", Send, "info"],
  accepted: ["Accepted", CircleCheck, "good"],
  declined: ["Declined", XCircle, "critical"],
  expired: ["Expired", Clock, "neutral"],
  invoiced: ["Invoiced", FileCheck, "good"],
  cancelled: ["Cancelled", Ban, "neutral"],
};

const deliveries: Record<Quote["delivery"], [string, typeof Send, string] | null> = {
  not_sent: null,
  sent: ["Emailed", Send, "neutral"],
  delivered: ["Delivered", MailCheck, "good"],
  viewed: ["Viewed", Eye, "good"],
  bounced: ["Bounced", MailX, "critical"],
};

export function QuoteChips({ quote }: { quote: Quote }) {
  const [label, icon, tone] = statuses[quote.status];
  const d = quote.status === "draft" ? null : deliveries[quote.delivery];
  return (
    <span className="fy-row fy-row--wrap gap-2">
      <span className={`fy-chip fy-chip--${tone}`}>
        <Icon of={icon} />
        {label}
      </span>
      {d && (
        <span className={`fy-chip fy-chip--${d[2]}`}>
          <Icon of={d[1]} />
          {d[0]}
        </span>
      )}
    </span>
  );
}

export const quoteTotal = (q: Quote) => (q.total ? `${formatAmount(q.total.amount)} ${q.total.currency}` : "—");

const filters: [Quote["status"] | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["sent", "Sent"],
  ["cancelled", "Cancelled"],
];

// Quotes is Sales › Quotes and orders (docs/design/screens Quotes; FR-AR-01, FR-AR-02): the quotes, or the sales orders,
// newest first, each with its customer, validity date, total, status and email; above them, what the open ones come to
// in the pipeline. An accepted one is invoiced from its page.
export function Quotes() {
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const [kind, setKind] = useState<Quote["kind"]>("quote");
  const [status, setStatus] = useState<Quote["status"] | "">("");
  const [find, setFind] = useState("");
  const findId = useId();
  const q = find.trim();
  const list = useInfiniteQuery({
    queryKey: ["quotes", kind, status, q],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const st = status === "draft" || status === "sent" || status === "cancelled" ? status : undefined;
      const { data, error } = await api.GET("/v1/quotes", { params: { query: { page_size: 50, cursor: pageParam, kind, status: st, q: q || undefined } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const pipeline = useQuery({
    queryKey: ["sales-pipeline"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/quotes/pipeline", {});
      if (error) throw error;
      return data;
    },
  });
  const items = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("quote.new");
  const addOrder = helpText("sales-order.new");
  const ready = items.filter((x) => x.status === "accepted").length;
  const p = pipeline.data;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
          </nav>
          <h1 className="fy-h1">Quotes and orders</h1>
          <p className="fy-secondary">What the company offers its customers, and the orders they place. An accepted one becomes an invoice.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/sales/quotes/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
            <Link className="fy-btn" to="/sales/orders/new" title={addOrder.tooltip}>
              <Icon of={Plus} />
              {addOrder.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="quotes-list" />
      <SalesTabs />
      {p && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="Pipeline">
          <h2 className="fy-h3">{helpText("sales.pipeline").label}</h2>
          <p className="fy-small">
            {formatAmount(p.total.amount)} {p.total.currency} open: {p.quotes.count === 1 ? "1 quote" : `${p.quotes.count} quotes`} ({formatAmount(p.quotes.total.amount)}) and{" "}
            {p.orders.count === 1 ? "1 sales order" : `${p.orders.count} sales orders`} ({formatAmount(p.orders.total.amount)}), in {base} at each one's rate.
          </p>
        </section>
      )}
      {ready > 0 && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={FileCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{ready === 1 ? "1 accepted quote shown is ready to invoice" : `${ready} accepted quotes shown are ready to invoice`}</span>
            <span className="fy-small">Open it and make its invoice.</span>
          </div>
        </div>
      )}
      <div className="fy-row fy-row--wrap fy-row--between gap-3">
        <div className="fy-chipset" role="group" aria-label="Kind">
          {(
            [
              ["quote", "Quotes"],
              ["sales_order", "Sales orders"],
            ] as const
          ).map(([k, label]) => (
            <button key={k} type="button" className="fy-filter" aria-pressed={kind === k} onClick={() => setKind(k)}>
              {label}
            </button>
          ))}
        </div>
        <div className="fy-chipset" role="group" aria-label="Status">
          {filters.map(([s, label]) => (
            <button key={label} type="button" className="fy-filter" aria-pressed={status === s} onClick={() => setStatus(s)}>
              {label}
            </button>
          ))}
        </div>
        <div className="fy-inputgroup max-w-80 min-w-48 flex-1">
          <span className="fy-inputgroup__addon">
            <Icon of={Search} />
          </span>
          <label className="fy-sr" htmlFor={findId}>
            Find a quote
          </label>
          <input className="fy-input" id={findId} type="search" placeholder="Quote number" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The quotes could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the quotes…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={FileText} size={24} />
          {status || q ? (
            <>
              <h2 className="fy-h3">No quote matches</h2>
              <p className="fy-secondary">Try another status, or another number.</p>
            </>
          ) : (
            <>
              <h2 className="fy-h3">{kind === "sales_order" ? "No sales orders yet" : "No quotes yet"}</h2>
              <p className="fy-secondary">A quote offers a customer lines at a price until a date. Sending it numbers it; once they accept it, it becomes an invoice.</p>
              {may ? (
                <Link className="fy-btn fy-btn--primary" to="/sales/quotes/new">
                  <Icon of={Plus} />
                  {add.label}
                </Link>
              ) : (
                <p className="fy-small fy-secondary">A Bookkeeper, an AR specialist or an Accountant prepares quotes.</p>
              )}
            </>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label={kind === "sales_order" ? "Sales orders" : "Quotes"}>
            <thead>
              <tr>
                <th scope="col">Number</th>
                <th scope="col">Customer</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-from-medium">
                  Valid until
                </th>
                <th scope="col" className="fy-num">
                  Total
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((x) => (
                <tr key={x.id}>
                  <td data-card="meta">
                    <Link className="fy-mono" to="/sales/quotes/$quoteId" params={{ quoteId: x.id }}>
                      {x.number || "Draft"}
                    </Link>
                  </td>
                  <td data-card="title">{x.customer?.name || <span className="fy-muted">No customer</span>}</td>
                  <td data-card="meta">{x.date ? date(x.date) : "No date"}</td>
                  <td data-card="hide" className="fy-from-medium">
                    {x.valid_until ? date(x.valid_until) : "—"}
                  </td>
                  <td data-card="amount" className="fy-num">
                    {quoteTotal(x)}
                  </td>
                  <td data-card="status">
                    <QuoteChips quote={x} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && (
        <div>
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Show more
          </button>
        </div>
      )}
    </div>
  );
}
