import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Banknote, CircleCheck, Clock, PencilLine, Plus, ReceiptText, Search, TriangleAlert, Undo2, UserCheck, Wallet, XCircle } from "lucide-react";
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
import { PayablesSummary } from "./Payables";

export type Bill = components["schemas"]["Bill"];
type Status = Bill["status"];
type States = Bill["states"];

// Who prepares bills: C on "Bills, vendor credits" (Appendix D): the Bookkeeper, the AP specialist and the Accountant.
// The API decides; the page offers the buttons to them only.
export const preparers = ["bookkeeper", "ap_specialist", "accountant"];

const filters: [Status | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["posted", "Posted"],
  ["reversed", "Voided"],
];

// StateChips are a bill's separate states (FR-AP-02): where its approval stands, whether it is posted or voided, what
// payments settle of it, and whether it is overdue. Each chip carries its icon and its words: colour never says it alone.
// BillTabs switches between the bills and the recurring bills (FR-AP-09; PRD-001 rev 0.84).
export function BillTabs() {
  return (
    <nav className="fy-tabs" aria-label="Bill lists">
      <Link to="/purchases/bills" activeOptions={{ exact: true }}>
        Bills
      </Link>
      <Link to="/purchases/bills/recurring" activeOptions={{ exact: true }}>
        Recurring
      </Link>
    </nav>
  );
}

export function StateChips({ states, credit = false }: { states: States; credit?: boolean }) {
  return (
    <span className="fy-row fy-row--wrap gap-2">
      {states.ledger === "posted" && (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Posted
        </span>
      )}
      {states.ledger === "voided" && (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Undo2} />
          Voided
        </span>
      )}
      {states.ledger === "not_posted" && states.approval === "draft" && (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PencilLine} />
          Draft
        </span>
      )}
      {states.approval === "awaiting_approval" && (
        <span className="fy-chip fy-chip--info">
          <Icon of={UserCheck} />
          Awaiting approval
        </span>
      )}
      {states.approval === "rejected" && (
        <span className="fy-chip fy-chip--critical">
          <Icon of={XCircle} />
          Rejected
        </span>
      )}
      {states.ledger === "posted" && states.payment === "part_paid" && (
        <span className="fy-chip fy-chip--info">
          <Icon of={Wallet} />
          {credit ? "Part applied" : "Part paid"}
        </span>
      )}
      {states.ledger === "posted" && states.payment === "paid" && (
        <span className="fy-chip fy-chip--good">
          <Icon of={Banknote} />
          {credit ? "Applied" : "Paid"}
        </span>
      )}
      {states.due === "overdue" && (
        <span className="fy-chip fy-chip--serious">
          <Icon of={Clock} />
          Overdue
        </span>
      )}
    </span>
  );
}

export const total = (b: Bill) => `${formatAmount(b.total.amount)} ${b.total.currency}`;

// Bills is Purchases › Bills (docs/design/screens BillsBoard's list view; FR-AP-02): every bill, newest first, with
// its supplier, dates, total and states, narrowed by status and by text in its number. A posted bill is never edited in
// place: an edit reverses and posts it again. Above them, what the company owes its suppliers: the cash the next 30 days
// need, and in Expert mode the AP ageing (FR-AP-06). The board and the due-date calendar arrive with their task.
export function Bills() {
  const { session, organisation } = useSignedIn();
  const [status, setStatus] = useState<Status | "">("");
  const [kind, setKind] = useState<Bill["kind"]>("bill");
  const [find, setFind] = useState("");
  const findId = useId();
  const q = find.trim();
  const list = useInfiniteQuery({
    queryKey: ["bills", kind, status, q],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/bills", { params: { query: { page_size: 50, cursor: pageParam, status: status || undefined, q: q || undefined, kind } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("bill.new");
  const addCredit = helpText("credit.new");
  const base = organisation?.base_currency ?? "SGD";
  const overdue = items.filter((b) => b.states.due === "overdue").length;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
          </nav>
          <h1 className="fy-h1">Bills</h1>
          <p className="fy-secondary">What suppliers have billed the company. A posted bill is never changed in place: it is voided or edited, with a reason.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/purchases/bills/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
            <Link className="fy-btn" to="/purchases/credits/new" title={addCredit.tooltip}>
              <Icon of={Plus} />
              {addCredit.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="bills-board" />
      <BillTabs />
      <PayablesSummary />
      {overdue > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{overdue === 1 ? "1 bill shown is overdue" : `${overdue} bills shown are overdue`}</span>
            <span className="fy-small">Its due date has passed and it is not paid yet.</span>
          </div>
        </div>
      )}
      <div className="fy-row fy-row--wrap fy-row--between gap-3">
        <div className="fy-chipset" role="group" aria-label="Kind">
          {(
            [
              ["bill", "Bills"],
              ["vendor_credit", "Credit notes"],
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
            Find a bill
          </label>
          <input className="fy-input" id={findId} type="search" placeholder="Bill number" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The bills could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the bills…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={ReceiptText} size={24} />
          {status || q ? (
            <>
              <h2 className="fy-h3">No bill matches</h2>
              <p className="fy-secondary">Try another status, or another number.</p>
            </>
          ) : (
            <>
              <h2 className="fy-h3">No bills yet</h2>
              <p className="fy-secondary">A bill is what a supplier asks the company to pay: their invoice, with its lines and GST. Posting it adds it to what the company owes.</p>
              {may ? (
                <Link className="fy-btn fy-btn--primary" to="/purchases/bills/new">
                  <Icon of={Plus} />
                  {add.label}
                </Link>
              ) : (
                <p className="fy-small fy-secondary">A Bookkeeper, an AP specialist or an Accountant enters bills.</p>
              )}
            </>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Bills">
            <thead>
              <tr>
                <th scope="col">Number</th>
                <th scope="col">Supplier</th>
                <th scope="col" className="fy-from-medium">
                  Their number
                </th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-from-expanded">
                  Due
                </th>
                <th scope="col" className="fy-num">
                  Total
                </th>
                <th scope="col" className="fy-num fy-from-expanded">
                  Payable ({base})
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((b) => (
                <tr key={b.id}>
                  <td data-card="meta">
                    <Link className="fy-mono" to="/purchases/bills/$billId" params={{ billId: b.id }}>
                      {b.number || (b.status === "draft" ? "Draft" : "No number")}
                    </Link>
                  </td>
                  <td data-card="title">{b.supplier?.name || <span className="fy-muted">No supplier</span>}</td>
                  <td data-card="hide" className="fy-from-medium">
                    {b.supplier_ref || "—"}
                  </td>
                  <td data-card="meta">{b.date ? date(b.date) : "No date"}</td>
                  <td data-card="hide" className="fy-from-expanded">
                    {b.due_date ? date(b.due_date) : "—"}
                  </td>
                  <td data-card="amount" className="fy-num">
                    {total(b)}
                  </td>
                  <td data-card="hide" className="fy-num fy-from-expanded">
                    {b.payable ? formatAmount(b.payable.amount) : "—"}
                  </td>
                  <td data-card="status">
                    <StateChips states={b.states} credit={b.kind === "vendor_credit"} />
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
