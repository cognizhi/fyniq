import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CirclePause, CircleStop, Plus, Repeat } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { CheckChip, debits, JournalTabs, preparers } from "./Journals";

export type Recurring = components["schemas"]["RecurringJournal"];
export type Schedule = components["schemas"]["RecurringSchedule"];

// ordinal is a day of the month as it is read: 1st, 2nd, 3rd, 4th, … 21st, 22nd, 23rd, 31st.
export function ordinal(n: number): string {
  const tens = n % 100;
  if (tens >= 11 && tens <= 13) return `${n}th`;
  return `${n}${({ 1: "st", 2: "nd", 3: "rd" } as Record<number, string>)[n % 10] ?? "th"}`;
}

const every: Record<number, string> = { 1: "Monthly", 3: "Every 3 months", 6: "Every 6 months", 12: "Yearly" };

// repeats says when a recurring journal recurs: "Monthly on the 31st (the last day of a shorter month)".
export function repeats(s: Schedule): string {
  const day = s.day_of_month >= 29 ? `on the ${ordinal(s.day_of_month)} (the last day of a shorter month)` : `on the ${ordinal(s.day_of_month)}`;
  return `${every[s.every_months] ?? `Every ${s.every_months} months`} ${day}`;
}

// RecurringStatusChip is whether a recurring journal makes its drafts: active, paused or ended.
export function RecurringStatusChip({ status }: { status: Recurring["status"] }) {
  if (status === "paused")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={CirclePause} />
        Paused
      </span>
    );
  if (status === "ended")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={CircleStop} />
        Ended
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Active
    </span>
  );
}

// RecurringJournals is Accounting › Journals › Recurring (docs/design/screens Journals; FR-GL-02, FR-AUT-01): the
// journals that repeat. Fyniq makes the draft of each occurrence on its date, and a person posts it under the usual
// approvals: nothing recurring posts by itself (the founders' decision of 21 Oct 2026, PRD-001 rev 0.58).
export function RecurringJournals() {
  const { session, organisation } = useSignedIn();
  const list = useInfiniteQuery({
    queryKey: ["recurring-journals"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/recurring-journals", { params: { query: { page_size: 50, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("recurring.new");
  const base = organisation?.base_currency ?? "SGD";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Journals</h1>
          <p className="fy-secondary">Recurring journals: Fyniq makes each one's draft on its date, and a person checks and posts it.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/accounting/journals/recurring/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="journals" />
      <JournalTabs />
      {list.isError && <Refusal problem={problemOf(list.error)} title="The recurring journals could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the recurring journals…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Repeat} size={24} />
          <h2 className="fy-h3">No recurring journals yet</h2>
          <p className="fy-secondary">A recurring journal repeats, such as a monthly rent accrual. Fyniq makes its draft on each date; you check it and post it.</p>
          {may ? (
            <Link className="fy-btn fy-btn--primary" to="/accounting/journals/recurring/new">
              <Icon of={Plus} />
              {add.label}
            </Link>
          ) : (
            <p className="fy-small fy-secondary">An Accountant or a Tax agent sets up recurring journals.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Recurring journals">
            <thead>
              <tr>
                <th scope="col">Recurring journal</th>
                <th scope="col">Repeats</th>
                <th scope="col">Next draft</th>
                <th scope="col" className="fy-num">
                  Total ({base})
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id}>
                  <td data-card="title">
                    <Link to="/accounting/journals/recurring/$recurringId" params={{ recurringId: r.id }}>
                      {r.name}
                    </Link>
                    {r.memo && <span className="fy-small fy-muted block">{r.memo}</span>}
                  </td>
                  <td data-card="meta">
                    {repeats(r.schedule)}
                    <span className="fy-small fy-muted block">{r.schedule.ends_on ? `Until ${date(r.schedule.ends_on)}` : "No end date"}</span>
                  </td>
                  <td data-card="meta">{r.next_on ? date(r.next_on) : "None"}</td>
                  <td data-card="amount" className="fy-num">
                    {debits(r.debits)}
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
      {items.length > 0 && <p className="fy-small fy-muted">The total is each draft's debits. Each draft waits for a person to post it, under the usual approvals.</p>}
    </div>
  );
}
