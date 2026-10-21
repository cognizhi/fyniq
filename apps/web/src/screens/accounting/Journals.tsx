import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookOpen, CircleCheck, CircleX, Lock, PencilLine, Plus, Search, TriangleAlert, Undo2 } from "lucide-react";
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

type Summary = components["schemas"]["JournalSummary"];
type Status = Summary["status"];
export type Check = components["schemas"]["JournalCheck"];
export type Person = components["schemas"]["Person"];

// Who prepares journals: C on "Journals" (Appendix D): the Accountant and the Tax agent. The API decides; the page
// offers the button to them only.
export const preparers = ["accountant", "tax_agent"];

const filters: [Status | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["posted", "Posted"],
  ["reversed", "Reversed"],
];

export function StatusChip({ status }: { status: Status }) {
  if (status === "draft")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={PencilLine} />
        Draft
      </span>
    );
  if (status === "reversed")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={Undo2} />
        Reversed
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Posted
    </span>
  );
}

// CheckChip is a journal's first status, as the workbook showed it (ENG-20, ENG-21): the first error (✖) that stops it
// posting, a warning (⚠), or that a draft is ready or a posted journal locked. Nothing more for a posted journal that is
// merely OK: its status says so.
export function CheckChip({ checks }: { checks: Check[] | null | undefined }) {
  const first = checks?.[0];
  if (!first) return null;
  const errors = checks.filter((c) => c.severity === "error").length;
  if (first.severity === "error")
    return (
      <span className="fy-chip fy-chip--critical" title={first.detail}>
        <Icon of={CircleX} />
        {first.title}
        {errors > 1 ? ` (+${errors - 1})` : ""}
      </span>
    );
  if (first.severity === "warning")
    return (
      <span className="fy-chip fy-chip--warning" title={first.detail}>
        <Icon of={TriangleAlert} />
        {first.title}
      </span>
    );
  if (first.code === "JOURNAL-LOCKED")
    return (
      <span className="fy-chip fy-chip--neutral" title={first.detail}>
        <Icon of={Lock} />
        {first.title}
      </span>
    );
  if (first.code === "JOURNAL-OK")
    return (
      <span className="fy-chip fy-chip--good" title={first.detail}>
        <Icon of={CircleCheck} />
        Ready to post
      </span>
    );
  return null;
}

// who names a person: a member by name, the AI as such, a service by its name.
export function who(p: Person | undefined | null): string {
  if (!p) return "—";
  if (p.kind === "ai") return "AI assistant";
  return p.name || (p.kind === "user" ? "A former member" : p.id);
}

// Debit totals per currency, as the API sends them: "1,200.00 SGD", one per currency.
export function debits(d: Summary["debits"]): string {
  return d.length === 0 ? "—" : d.map((m) => `${formatAmount(m.amount)}${d.length > 1 ? ` ${m.currency}` : ""}`).join(" · ");
}

// JournalTabs switches between the journals and the recurring journals (docs/design/screens Journals; FR-GL-02).
export function JournalTabs() {
  return (
    <nav className="fy-tabs" aria-label="Journal lists">
      <Link to="/accounting/journals" activeOptions={{ exact: true }}>
        Journals
      </Link>
      <Link to="/accounting/journals/recurring" activeOptions={{ exact: true }}>
        Recurring
      </Link>
    </nav>
  );
}

// WaitingReversals warns of the posted journals whose automatic reversal waits for a month that is closed or a year
// not opened yet (FR-GL-02; rev 0.59): they post within the hour once that month is open.
function WaitingReversals() {
  const waiting = useQuery({
    queryKey: ["journals", "waiting-reversals"],
    retry: false,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/journals/waiting-reversals", { params: { query: { page_size: 5 } } });
      if (error) throw error;
      return data;
    },
  });
  const items = waiting.data?.items ?? [];
  if (items.length === 0) return null;
  const more = Boolean(waiting.data?.next_cursor);
  const h = helpText("journal.waiting");
  return (
    <div className="fy-banner fy-banner--warning" role="status">
      <Icon of={TriangleAlert} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong">{items.length === 1 && !more ? "1 automatic reversal is waiting" : `${items.length}${more ? " or more" : ""} automatic reversals are waiting`}</span>
        <span className="fy-small">{h.tooltip}</span>
        <ul className="fy-small">
          {items.map((j) => (
            <li key={j.id}>
              <Link to="/accounting/journals/$journalId" params={{ journalId: j.id }}>
                {j.number || j.memo || "A journal"}
              </Link>
              {j.reverse_on ? `: reverses on ${date(j.reverse_on)}` : ""}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

// Journals is Accounting › Journals (docs/design/screens Journals; FR-GL-01, FR-GL-12): every manual journal,
// newest first, narrowed by status and by text in its description or number. Posted journals are never edited;
// they are reversed. The recurring journals are the other tab (FR-GL-02), and automatic reversals that wait are
// warned of at the top. The awaiting-approval filter and the locked-period banner arrive with their tasks.
export function Journals() {
  const { session, organisation } = useSignedIn();
  const [status, setStatus] = useState<Status | "">("");
  const [find, setFind] = useState("");
  const findId = useId();
  const q = find.trim();
  const list = useInfiniteQuery({
    queryKey: ["journals", status, q],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/journals", { params: { query: { page_size: 50, cursor: pageParam, status: status || undefined, q: q || undefined } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("journal.new");
  const base = organisation?.base_currency ?? "SGD";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Journals</h1>
          <p className="fy-secondary">Manual journals. A posted journal is never edited: it is reversed, with a reason.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/accounting/journals/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="journals" />
      <JournalTabs />
      <WaitingReversals />
      <div className="fy-row fy-row--wrap fy-row--between gap-3">
        <div className="fy-chipset" role="group" aria-label="Status">
          {filters.map(([s, label]) => (
            <button key={label} type="button" className="fy-filter" aria-pressed={status === s} onClick={() => setStatus(s)}>
              {label}
            </button>
          ))}
        </div>
        <div className="fy-inputgroup max-w-80 flex-1">
          <span className="fy-inputgroup__addon">
            <Icon of={Search} />
          </span>
          <label className="fy-sr" htmlFor={findId}>
            Find a journal
          </label>
          <input className="fy-input" id={findId} type="search" placeholder="Number or description" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The journals could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the journals…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={BookOpen} size={24} />
          {status || q ? (
            <>
              <h2 className="fy-h3">No journal matches</h2>
              <p className="fy-secondary">Try another status, or fewer words.</p>
            </>
          ) : (
            <>
              <h2 className="fy-h3">No journals yet</h2>
              <p className="fy-secondary">A journal moves amounts between accounts: an accrual, a correction, a reclassification. Its debits equal its credits, and it posts on its date.</p>
              {may ? (
                <Link className="fy-btn fy-btn--primary" to="/accounting/journals/new">
                  <Icon of={Plus} />
                  {add.label}
                </Link>
              ) : (
                <p className="fy-small fy-secondary">An Accountant or a Tax agent prepares journals.</p>
              )}
            </>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Journals">
            <thead>
              <tr>
                <th scope="col">Number</th>
                <th scope="col">Date</th>
                <th scope="col">Description</th>
                <th scope="col" className="fy-num">
                  Total ({base})
                </th>
                <th scope="col" className="fy-from-expanded">
                  Prepared by
                </th>
                <th scope="col" className="fy-from-expanded">
                  Posted by
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((j) => (
                <tr key={j.id}>
                  <td data-card="meta">
                    <Link className="fy-mono" to="/accounting/journals/$journalId" params={{ journalId: j.id }}>
                      {j.number || (j.status === "draft" ? "Draft" : "No number")}
                    </Link>
                  </td>
                  <td data-card="meta">{j.date ? date(j.date) : "No date"}</td>
                  <td data-card="title">{j.memo || <span className="fy-muted">No description</span>}</td>
                  <td data-card="amount" className="fy-num">
                    {debits(j.debits)}
                  </td>
                  <td data-card="hide" className="fy-from-expanded">
                    {who(j.prepared_by)}
                  </td>
                  <td data-card="hide" className="fy-from-expanded">
                    {who(j.posted_by)}
                  </td>
                  <td data-card="status">
                    <span className="fy-row fy-row--wrap gap-2">
                      <StatusChip status={j.status} />
                      <CheckChip checks={j.checks} />
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
      {items.length > 0 && <p className="fy-small fy-muted">The total is each journal's debits, as entered.</p>}
    </div>
  );
}
