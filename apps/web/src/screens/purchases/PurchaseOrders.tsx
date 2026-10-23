import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Lock, PencilLine, Plus, ReceiptText, UserCheck, Wallet, XCircle } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { BillTabs, preparers } from "./Bills";

export type Order = components["schemas"]["PurchaseOrder"];

// OrderStateChips are an order's states (FR-AP-01): draft, awaiting approval or rejected; approved; part billed or
// billed; closed. Each chip has an icon and a label: colour never carries the meaning alone.
export function OrderStateChips({ order }: { order: Order }) {
  const s = order.states;
  return (
    <span className="fy-row fy-row--wrap gap-2">
      {s.closed && (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Lock} />
          Closed
        </span>
      )}
      {!s.closed && order.status === "approved" && s.billing === "not_billed" && (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Approved
        </span>
      )}
      {s.billing === "part_billed" && (
        <span className="fy-chip fy-chip--info">
          <Icon of={Wallet} />
          Part billed
        </span>
      )}
      {s.billing === "billed" && (
        <span className="fy-chip fy-chip--good">
          <Icon of={ReceiptText} />
          Billed
        </span>
      )}
      {order.status === "draft" && s.approval === "draft" && (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PencilLine} />
          Draft
        </span>
      )}
      {s.approval === "awaiting_approval" && (
        <span className="fy-chip fy-chip--info">
          <Icon of={UserCheck} />
          Awaiting approval
        </span>
      )}
      {s.approval === "rejected" && (
        <span className="fy-chip fy-chip--critical">
          <Icon of={XCircle} />
          Rejected
        </span>
      )}
    </span>
  );
}

const filters: [Order["status"] | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["approved", "Approved"],
  ["closed", "Closed"],
];

// PurchaseOrders is Purchases › Bills › Orders (docs/design/screens PurchaseOrders; FR-AP-01; PRD-001 rev 0.87): the
// orders raised with suppliers, with their totals and states. An order posts nothing; bills are made from it.
export function PurchaseOrders() {
  const { session } = useSignedIn();
  const [status, setStatus] = useState<Order["status"] | "">("");
  const list = useInfiniteQuery({
    queryKey: ["purchase-orders", status],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/purchase-orders", { params: { query: { page_size: 50, cursor: pageParam, status: status || undefined } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("order.new");
  const waiting = items.filter((o) => o.states.approval === "awaiting_approval").length;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
          </nav>
          <h1 className="fy-h1">Bills</h1>
          <p className="fy-secondary">
            Purchase orders agree what is bought, and the price, before the bill arrives.
            {waiting > 0 ? (waiting === 1 ? " 1 order shown is waiting for approval." : ` ${waiting} orders shown are waiting for approval.`) : ""}
          </p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/purchases/orders/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="purchase-orders" />
      <BillTabs />
      <div className="fy-chipset" role="group" aria-label="Status">
        {filters.map(([s, label]) => (
          <button key={label} type="button" className="fy-filter" aria-pressed={status === s} onClick={() => setStatus(s)}>
            {label}
          </button>
        ))}
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The purchase orders could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the purchase orders…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={ReceiptText} size={24} />
          <h2 className="fy-h3">No purchase orders {status ? "here" : "yet"}</h2>
          <p className="fy-secondary">A purchase order lists what you buy from a supplier, how many and at what price. Once approved, its bills are made from it.</p>
          {may ? (
            <Link className="fy-btn fy-btn--primary" to="/purchases/orders/new">
              <Icon of={Plus} />
              {add.label}
            </Link>
          ) : (
            <p className="fy-small fy-secondary">A Bookkeeper, an AP specialist or an Accountant raises purchase orders.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Purchase orders">
            <thead>
              <tr>
                <th scope="col">Order</th>
                <th scope="col">Supplier</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-num">
                  Total
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((o) => (
                <tr key={o.id}>
                  <td data-card="title">
                    <Link className="fy-mono" to="/purchases/orders/$orderId" params={{ orderId: o.id }}>
                      {o.number ?? "Draft"}
                    </Link>
                    {o.reference && <span className="fy-small fy-muted block">{o.reference}</span>}
                  </td>
                  <td data-card="meta">{o.supplier?.name ?? "No supplier yet"}</td>
                  <td data-card="meta">{o.date ? date(o.date) : "No date"}</td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(o.total.amount)} {o.total.currency}
                  </td>
                  <td data-card="status">
                    <OrderStateChips order={o} />
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
      {items.length > 0 && <p className="fy-small fy-muted">An order posts nothing: its bills post, each under its own approvals.</p>}
    </div>
  );
}
