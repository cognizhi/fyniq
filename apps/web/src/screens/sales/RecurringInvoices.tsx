import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, Repeat, Send } from "lucide-react";
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
import { preparers, SalesTabs } from "./Invoices";

export type RecurringInvoice = components["schemas"]["RecurringInvoice"];
type Schedule = components["schemas"]["InvoiceSchedule"];

const weekday = (on: string) => new Date(`${on}T00:00:00Z`).toLocaleDateString("en-SG", { weekday: "long", timeZone: "UTC" });

// repeatsOf is a recurring invoice's schedule in words: weekly or fortnightly on the start's weekday, or monthly to
// yearly on a day of the month (FR-AR-07).
export function repeatsOf(s: Schedule): string {
  if (s.every_weeks) return `${s.every_weeks === 2 ? "Every two weeks" : "Weekly"} on ${weekday(s.starts_on)}`;
  return repeats({ every_months: s.every_months ?? 1, day_of_month: s.day_of_month ?? 1, starts_on: s.starts_on, ends_on: s.ends_on });
}


// ModeChip says what the scheduler does with each occurrence: makes a draft, or issues and sends it once approved.
export function ModeChip({ r }: { r: RecurringInvoice }) {
  if (r.mode === "draft") return <span className="fy-small fy-muted">Makes drafts</span>;
  return (
    <span className={`fy-chip fy-chip--${r.issues_and_sends ? "good" : "warning"}`}>
      <Icon of={Send} />
      {r.issues_and_sends ? "Issues and sends" : "Issue and send, awaiting approval"}
    </span>
  );
}

// RecurringInvoices is Sales › Recurring (docs/design/screens Recurring; FR-AR-07): each recurring invoice by name, with
// its schedule, next occurrence, what it makes and its status.
export function RecurringInvoices() {
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("recurring-invoice.new");
  const list = useInfiniteQuery({
    queryKey: ["recurring-invoices"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/recurring-invoices", { params: { query: { page_size: 50, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
          </nav>
          <h1 className="fy-h1">Recurring invoices</h1>
          <p className="fy-secondary">Invoices that repeat, weekly to yearly. Each occurrence's draft is made on its date; changing one saves a new version, and invoices already made do not change.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/sales/recurring/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="recurring-documents" />
      <SalesTabs />
      {list.isError && <Refusal problem={problemOf(list.error)} title="The recurring invoices could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the recurring invoices…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Repeat} size={24} />
          <h2 className="fy-h3">No recurring invoices yet</h2>
          <p className="fy-secondary">A recurring invoice bills a customer the same lines on a schedule, such as a monthly subscription.</p>
          {may ? (
            <Link className="fy-btn fy-btn--primary" to="/sales/recurring/new">
              <Icon of={Plus} />
              {add.label}
            </Link>
          ) : (
            <p className="fy-small fy-secondary">A Bookkeeper, an AR specialist or an Accountant sets up recurring invoices.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Recurring invoices">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Repeats</th>
                <th scope="col">Next</th>
                <th scope="col" className="fy-from-medium">
                  Makes
                </th>
                <th scope="col" className="fy-num">
                  Before GST
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id}>
                  <td data-card="title">
                    <Link to="/sales/recurring/$recurringInvoiceId" params={{ recurringInvoiceId: r.id }}>
                      {r.name}
                    </Link>
                    <span className="fy-small fy-muted block">{r.customer?.name ?? "No customer"}</span>
                  </td>
                  <td data-card="meta">{repeatsOf(r.schedule)}</td>
                  <td data-card="meta">{r.next_on ? date(r.next_on) : "—"}</td>
                  <td data-card="hide" className="fy-from-medium">
                    <ModeChip r={r} />
                  </td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(r.net.amount)} {r.net.currency}
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
    </div>
  );
}
