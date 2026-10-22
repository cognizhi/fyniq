import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, Repeat } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { CheckChip } from "../accounting/Journals";
import { RecurringStatusChip, repeats } from "../accounting/RecurringJournals";
import { BillTabs, preparers } from "./Bills";

export type RecurringBill = components["schemas"]["RecurringBill"];

// RecurringBills is Purchases › Bills › Recurring (FR-AP-09, FR-AUT-01; PRD-001 rev 0.84): the bills that repeat, such
// as the office rent. Fyniq makes the draft bill of each occurrence on its date; a person adds the supplier's number
// and posts it under the usual approvals: nothing recurring posts by itself (the owner's decision of 22 Oct 2026).
export function RecurringBills() {
  const { session } = useSignedIn();
  const list = useInfiniteQuery({
    queryKey: ["recurring-bills"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/recurring-bills", { params: { query: { page_size: 50, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("recurring-bill.new");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
          </nav>
          <h1 className="fy-h1">Bills</h1>
          <p className="fy-secondary">Recurring bills: Fyniq makes each one's draft on its date, and a person adds the supplier's number and posts it.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/purchases/bills/recurring/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="bills-board" />
      <BillTabs />
      {list.isError && <Refusal problem={problemOf(list.error)} title="The recurring bills could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the recurring bills…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Repeat} size={24} />
          <h2 className="fy-h3">No recurring bills yet</h2>
          <p className="fy-secondary">A recurring bill repeats, such as the monthly office rent. Fyniq makes its draft on each date; you add the supplier's number and post it.</p>
          {may ? (
            <Link className="fy-btn fy-btn--primary" to="/purchases/bills/recurring/new">
              <Icon of={Plus} />
              {add.label}
            </Link>
          ) : (
            <p className="fy-small fy-secondary">A Bookkeeper, an AP specialist or an Accountant sets up recurring bills.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Recurring bills">
            <thead>
              <tr>
                <th scope="col">Recurring bill</th>
                <th scope="col">Repeats</th>
                <th scope="col">Next draft</th>
                <th scope="col" className="fy-num">
                  Total
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id}>
                  <td data-card="title">
                    <Link to="/purchases/bills/recurring/$recurringBillId" params={{ recurringBillId: r.id }}>
                      {r.name}
                    </Link>
                    {r.supplier?.name && <span className="fy-small fy-muted block">{r.supplier.name}</span>}
                  </td>
                  <td data-card="meta">
                    {repeats(r.schedule)}
                    <span className="fy-small fy-muted block">{r.schedule.ends_on ? `Until ${date(r.schedule.ends_on)}` : "No end date"}</span>
                  </td>
                  <td data-card="meta">{r.next_on ? date(r.next_on) : "None"}</td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(r.total.amount)} {r.total.currency}
                  </td>
                  <td data-card="status">
                    <span className="fy-row fy-row--wrap gap-2">
                      <RecurringStatusChip status={r.status} />
                      <CheckChip checks={r.checks.filter((c) => c.severity !== "ok")} />
                    </span>
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
      {items.length > 0 && <p className="fy-small fy-muted">The total is each draft's, with GST. Each draft waits for a person to post it, under the usual approvals.</p>}
    </div>
  );
}
