import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Banknote, CircleCheck, Clock, Eye, FileText, MailCheck, MailX, PencilLine, Plus, Search, Send, TriangleAlert, Undo2, UserCheck, Wallet, XCircle } from "lucide-react";
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

export type Invoice = components["schemas"]["Invoice"];
type Status = Invoice["status"];
type States = Invoice["states"];

// Who prepares invoices: C on "Invoices, credit notes" (Appendix D): the Bookkeeper, the AR specialist and the
// Accountant. The API decides; the page offers the buttons to them only.
export const preparers = ["bookkeeper", "ar_specialist", "accountant"];
// Who issues, emails, voids and edits them: P on "Invoices, credit notes", the Accountant and the AR specialist.
export const issuers = ["accountant", "ar_specialist"];

const filters: [Status | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["posted", "Issued"],
  ["reversed", "Voided"],
];

// SalesTabs switches between the invoices, the quotes and orders (FR-AR-01), the recurring invoices (FR-AR-07) and the
// statements and reminders (FR-AR-08), as each arrives.
export function SalesTabs() {
  return (
    <nav className="fy-tabs" aria-label="Sales lists">
      <Link to="/sales/invoices" activeOptions={{ exact: true }}>
        Invoices
      </Link>
    </nav>
  );
}

// StateChips are an invoice's separate states (FR-AR-03): where its approval stands, whether it is issued or voided,
// its email's delivery, what receipts and credits settle of it, and whether it is overdue. Each chip carries its icon and
// its words: colour never says it alone.
export function StateChips({ states, credit = false }: { states: States; credit?: boolean }) {
  const settlement: Record<States["settlement"], [string, typeof Wallet, string] | null> = {
    open: null,
    part_paid: ["Part paid", Wallet, "info"],
    paid: ["Paid", Banknote, "good"],
    written_off: ["Written off", Undo2, "neutral"],
    part_applied: ["Part applied", Wallet, "info"],
    applied: ["Applied", Banknote, "good"],
  };
  const delivery: Record<States["delivery"], [string, typeof Send, string] | null> = {
    not_sent: null,
    sent: ["Sent", Send, "neutral"],
    delivered: ["Delivered", MailCheck, "good"],
    viewed: ["Viewed", Eye, "good"],
    bounced: ["Bounced", MailX, "critical"],
  };
  const s = states.ledger === "posted" ? settlement[states.settlement] : null;
  const d = states.ledger === "posted" ? delivery[states.delivery] : null;
  return (
    <span className="fy-row fy-row--wrap gap-2">
      {states.ledger === "posted" && (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Issued
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
      {!credit && d && (
        <span className={`fy-chip fy-chip--${d[2]}`}>
          <Icon of={d[1]} />
          {d[0]}
        </span>
      )}
      {s && (
        <span className={`fy-chip fy-chip--${s[2]}`}>
          <Icon of={s[1]} />
          {s[0]}
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

export const total = (i: Invoice) => (i.total ? `${formatAmount(i.total.amount)} ${i.total.currency}` : "—");

// Invoices is Sales › Invoices (docs/design/screens InvoiceRecord's list; FR-AR-03): every invoice, or credit note, newest
// first, with its customer, dates, total and states, narrowed by status and by text in its number. An issued invoice is
// never changed in place: it is voided or edited, with a reason.
export function Invoices() {
  const { session } = useSignedIn();
  const [status, setStatus] = useState<Status | "">("");
  const [kind, setKind] = useState<Invoice["kind"]>("invoice");
  const [find, setFind] = useState("");
  const findId = useId();
  const q = find.trim();
  const list = useInfiniteQuery({
    queryKey: ["invoices", kind, status, q],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/invoices", { params: { query: { page_size: 50, cursor: pageParam, status: status || undefined, q: q || undefined, kind } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const may = (session.roles ?? []).some((r) => preparers.includes(r));
  const add = helpText("invoice.new");
  const addCredit = helpText("credit-note.new");
  const overdue = items.filter((i) => i.states.due === "overdue").length;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
          </nav>
          <h1 className="fy-h1">Invoices</h1>
          <p className="fy-secondary">What the company bills its customers. An issued invoice is never changed in place: it is voided or edited, with a reason.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/sales/invoices/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
            <Link className="fy-btn" to="/sales/credit-notes/new" title={addCredit.tooltip}>
              <Icon of={Plus} />
              {addCredit.label}
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="invoice-record" />
      <SalesTabs />
      {overdue > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{overdue === 1 ? "1 invoice shown is overdue" : `${overdue} invoices shown are overdue`}</span>
            <span className="fy-small">Its due date has passed and the customer has not paid it all yet.</span>
          </div>
        </div>
      )}
      <div className="fy-row fy-row--wrap fy-row--between gap-3">
        <div className="fy-chipset" role="group" aria-label="Kind">
          {(
            [
              ["invoice", "Invoices"],
              ["credit_note", "Credit notes"],
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
            Find an invoice
          </label>
          <input className="fy-input" id={findId} type="search" placeholder="Invoice number" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The invoices could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the invoices…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={FileText} size={24} />
          {status || q ? (
            <>
              <h2 className="fy-h3">No invoice matches</h2>
              <p className="fy-secondary">Try another status, or another number.</p>
            </>
          ) : (
            <>
              <h2 className="fy-h3">{kind === "credit_note" ? "No credit notes yet" : "No invoices yet"}</h2>
              <p className="fy-secondary">An invoice is what a customer is asked to pay: its lines, GST and due date. Issuing it adds it to what customers owe the company.</p>
              {may ? (
                <Link className="fy-btn fy-btn--primary" to="/sales/invoices/new">
                  <Icon of={Plus} />
                  {add.label}
                </Link>
              ) : (
                <p className="fy-small fy-secondary">A Bookkeeper, an AR specialist or an Accountant prepares invoices.</p>
              )}
            </>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label={kind === "credit_note" ? "Credit notes" : "Invoices"}>
            <thead>
              <tr>
                <th scope="col">Number</th>
                <th scope="col">Customer</th>
                <th scope="col" className="fy-from-medium">
                  Reference
                </th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-from-expanded">
                  Due
                </th>
                <th scope="col" className="fy-num">
                  Total
                </th>
                <th scope="col" className="fy-num fy-from-expanded">
                  Open
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((i) => (
                <tr key={i.id}>
                  <td data-card="meta">
                    <Link className="fy-mono" to="/sales/invoices/$invoiceId" params={{ invoiceId: i.id }}>
                      {i.number || (i.status === "draft" ? "Draft" : "No number")}
                    </Link>
                  </td>
                  <td data-card="title">{i.customer?.name || <span className="fy-muted">No customer</span>}</td>
                  <td data-card="hide" className="fy-from-medium">
                    {i.reference || "—"}
                  </td>
                  <td data-card="meta">{i.date ? date(i.date) : "No date"}</td>
                  <td data-card="hide" className="fy-from-expanded">
                    {i.due_date ? date(i.due_date) : "—"}
                  </td>
                  <td data-card="amount" className="fy-num">
                    {total(i)}
                  </td>
                  <td data-card="hide" className="fy-num fy-from-expanded">
                    {i.open ? formatAmount(i.open.amount) : "—"}
                  </td>
                  <td data-card="status">
                    <StateChips states={i.states} credit={i.kind === "credit_note"} />
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
