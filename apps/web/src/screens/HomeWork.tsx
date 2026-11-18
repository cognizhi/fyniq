import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { LucideIcon } from "lucide-react";
import { Banknote, ChevronRight, ClockAlert, FilePen, FileSearch, FileText, Landmark, Mail, PlugZap, ReceiptText, ScrollText, Send, ShieldAlert, UserCog, UserPlus, Wallet } from "lucide-react";
import { api } from "../api/client";
import { formatAmount } from "../format/amount";
import { date } from "../format/date";
import { isZero } from "../format/decimal";
import { helpText } from "../i18n";
import { nav } from "../registry";
import { useSignedIn } from "../session";
import { mayOpen } from "../shell/navigation";
import { Icon } from "../ui";

// A tally is how many items a read found, and whether there were more than one page holds; null when the person's
// roles may not read them, so the row is left out rather than shown as nothing to do (the API decides who sees what).
export interface Tally {
  n: number;
  more: boolean;
}

type Page<T> = { data?: { items?: T[] | null; next_cursor?: string }; error?: unknown };

export async function tally<T>(read: () => Promise<Page<T>>, keep: (x: T) => boolean = () => true): Promise<Tally | null> {
  const { data, error } = await read();
  if (error || !data) return null;
  return { n: (data.items ?? []).filter(keep).length, more: !!data.next_cursor };
}

// Work is one row of "Your work": what §7.1's table says a role's Home shows, counted from the list it opens.
interface Work {
  id: string;
  roles: string[];
  icon: LucideIcon;
  title: (n: string, one: boolean) => string;
  meta: string;
  to: string;
  tone?: "serious" | "critical";
  read: () => Promise<Tally | null>;
}

const max = { page_size: 500 } as const;

// work is every row, in the order §7.1 lists each role's Home. Overdue comes from the API's own due state, and an
// amount left to apply from its decimal string: nothing is worked out here (FR-UX-22).
export const work: Work[] = [
  {
    id: "bank-lines",
    roles: ["bookkeeper"],
    icon: Landmark,
    title: (n, one) => `${n} bank ${one ? "line" : "lines"} to categorise`,
    meta: "Statement lines not yet categorised, matched or transferred",
    to: "/banking/categorise",
    read: () => tally(() => api.GET("/v1/bank-lines", { params: { query: { status: "unreconciled", ...max } } })),
  },
  {
    id: "draft-invoices",
    roles: ["bookkeeper", "ar_specialist"],
    icon: FilePen,
    title: (n, one) => `${n} draft ${one ? "invoice" : "invoices"} to finish`,
    meta: "Saved but not issued yet",
    to: "/sales/invoices",
    read: () => tally(() => api.GET("/v1/invoices", { params: { query: { kind: "invoice", status: "draft", ...max } } })),
  },
  {
    id: "draft-bills",
    roles: ["bookkeeper", "ap_specialist"],
    icon: FilePen,
    title: (n, one) => `${n} draft ${one ? "bill" : "bills"} to finish`,
    meta: "Saved but not posted yet",
    to: "/purchases/bills",
    read: () => tally(() => api.GET("/v1/bills", { params: { query: { kind: "bill", status: "draft", ...max } } })),
  },
  {
    id: "overdue-invoices",
    roles: ["ar_specialist"],
    icon: ClockAlert,
    tone: "serious",
    title: (n, one) => `${n} ${one ? "invoice" : "invoices"} overdue`,
    meta: "Issued, past their due date and not paid in full",
    to: "/sales/invoices",
    read: () => tally(() => api.GET("/v1/invoices", { params: { query: { kind: "invoice", status: "posted", ...max } } }), (i) => i.states.due === "overdue"),
  },
  {
    id: "receipts-to-apply",
    roles: ["ar_specialist"],
    icon: Wallet,
    title: (n, one) => `${n} ${one ? "receipt" : "receipts"} to apply`,
    meta: "Money received with an amount not yet set against an invoice; the customer statements show each one",
    to: "/sales/statements",
    read: () => tally(() => api.GET("/v1/customer-receipts", { params: { query: { status: "posted", ...max } } }), (r) => !isZero(r.unapplied.amount)),
  },
  {
    id: "quotes-waiting",
    roles: ["ar_specialist"],
    icon: Send,
    title: (n, one) => `${n} ${one ? "quote" : "quotes"} waiting for an answer`,
    meta: "Sent to the customer, not accepted or declined yet",
    to: "/sales/quotes",
    read: () => tally(() => api.GET("/v1/quotes", { params: { query: { kind: "quote", status: "sent", ...max } } })),
  },
  {
    id: "overdue-bills",
    roles: ["ap_specialist"],
    icon: ClockAlert,
    tone: "serious",
    title: (n, one) => `${n} ${one ? "bill" : "bills"} overdue`,
    meta: "Posted, past their due date and not paid in full",
    to: "/purchases/bills",
    read: () => tally(() => api.GET("/v1/bills", { params: { query: { kind: "bill", status: "posted", ...max } } }), (b) => b.states.due === "overdue"),
  },
  {
    id: "bills-to-pay",
    roles: ["ap_specialist"],
    icon: FileText,
    title: (n, one) => `${n} ${one ? "bill" : "bills"} to pay`,
    meta: "Posted and not yet in a payment batch or paid",
    to: "/purchases/bills",
    read: () => tally(() => api.GET("/v1/bills", { params: { query: { kind: "bill", status: "posted", ...max } } }), (b) => b.states.payment === "unpaid" || b.states.payment === "part_paid"),
  },
  {
    id: "batches-to-prepare",
    roles: ["ap_specialist"],
    icon: Banknote,
    title: (n, one) => `${n} payment ${one ? "batch" : "batches"} being prepared`,
    meta: "Drafts to finish and send for approval",
    to: "/purchases/payment-batches",
    read: () => tally(() => api.GET("/v1/payment-batches", { params: { query: { status: "draft" } } })),
  },
  {
    id: "claims-to-pay",
    roles: ["ap_specialist"],
    icon: ReceiptText,
    title: (n, one) => `${n} approved ${one ? "claim" : "claims"} to pay`,
    meta: "Expense claims approved and not paid yet",
    to: "/purchases/claims",
    read: () => tally(() => api.GET("/v1/expense-claims", { params: { query: { whose: "everyone", stage: "approved", ...max } } })),
  },
  {
    id: "bank-detail-changes",
    roles: ["ap_specialist"],
    icon: ShieldAlert,
    title: (n, one) => `${n} bank-detail ${one ? "change" : "changes"} waiting`,
    meta: "A payee's or claimant's new bank account, waiting for a second person's approval",
    to: "/inbox",
    read: () =>
      tally(
        () => api.GET("/v1/approvals", { params: { query: { status: "pending", ...max } } }),
        (a) => a.object_type === "payee_bank_change" || a.object_type === "claimant_bank_change",
      ),
  },
  {
    id: "batches-to-release",
    roles: ["payment_releaser"],
    icon: Banknote,
    title: (n, one) => `${n} ${one ? "batch" : "batches"} waiting for release`,
    meta: "Approved payment batches to release to the bank",
    to: "/purchases/payment-batches",
    read: () => tally(() => api.GET("/v1/payment-batches", { params: { query: { status: "approved" } } })),
  },
  {
    id: "payments-rejected",
    roles: ["payment_releaser"],
    icon: ShieldAlert,
    tone: "critical",
    title: (n, one) => `${n} ${one ? "payment" : "payments"} the bank rejected`,
    meta: "In released batches: look at each one and pay it again or cancel it",
    to: "/purchases/payment-batches",
    read: async () => {
      const t = await Promise.all(
        (["released", "paid"] as const).map(async (status) => {
          const { data, error } = await api.GET("/v1/payment-batches", { params: { query: { status } } });
          return error || !data ? null : { n: (data.items ?? []).flatMap((b) => b.payments).filter((p) => p.rejected).length, more: !!data.next_cursor };
        }),
      );
      return t.some((x) => x === null) ? null : { n: t.reduce((s, x) => s + x!.n, 0), more: t.some((x) => x!.more) };
    },
  },
  {
    id: "join-requests",
    roles: ["administrator"],
    icon: UserPlus,
    title: (n, one) => `${n} ${one ? "person" : "people"} asking to join`,
    meta: "Signed in with the company's email domain: let each one in, or refuse",
    to: "/settings/members",
    read: () => tally(() => api.GET("/v1/members/join-requests", { params: { query: max } }), (r) => r.status === "pending"),
  },
  {
    id: "invitations-open",
    roles: ["administrator"],
    icon: Mail,
    title: (n, one) => `${n} ${one ? "invitation" : "invitations"} waiting to be accepted`,
    meta: "Sent and not withdrawn: resend or withdraw one that is no longer needed",
    to: "/settings/members",
    read: () => tally(() => api.GET("/v1/members/invitations", { params: { query: max } }), (i) => !i.withdrawn_at),
  },
  {
    id: "role-changes",
    roles: ["administrator"],
    icon: UserCog,
    title: (n, one) => `${n} role ${one ? "change" : "changes"} waiting for a second approver`,
    meta: "Members' roles and invitations another person must approve (SOD-6)",
    to: "/inbox",
    read: () => tally(() => api.GET("/v1/approvals", { params: { query: { status: "pending", ...max } } }), (a) => roleObjects.has(a.object_type)),
  },
  {
    id: "integrations-failing",
    roles: ["administrator"],
    icon: PlugZap,
    tone: "critical",
    title: (n, one) => `${n} ${one ? "integration" : "integrations"} failing`,
    meta: "Their last test or sync failed: open each one to see why",
    to: "/settings/integrations",
    read: () => tally(() => api.GET("/v1/organisation/connectors", { params: { query: { page_size: 200 } } }), (c) => c.health === "failing"),
  },
  {
    id: "secretary-findings",
    roles: ["company_secretary"],
    icon: ScrollText,
    tone: "serious",
    title: (n, one) => `${n} corporate ${one ? "item" : "items"} to see to`,
    meta: "From the company secretary's last run: filings, resolutions and register updates due or to review",
    to: "/compliance/secretary",
    read: async () => {
      const { data, error } = await api.GET("/v1/compliance/secretary", {});
      return error || !data ? null : { n: (data.findings ?? []).filter((f) => toSee.has(f.status)).length, more: false };
    },
  },
  {
    id: "register-approvals",
    roles: ["company_secretary"],
    icon: FileText,
    title: (n, one) => `${n} register ${one ? "update" : "updates"} waiting for approval`,
    meta: "Holder records, share classes and equity transactions another person must approve",
    to: "/inbox",
    read: () => tally(() => api.GET("/v1/approvals", { params: { query: { status: "pending", ...max } } }), (a) => registerObjects.has(a.object_type)),
  },
  {
    id: "data-requests",
    roles: ["dpo"],
    icon: FileSearch,
    tone: "serious",
    title: (n, one) => `${n} data ${one ? "request" : "requests"} to answer`,
    meta: "Access and correction requests received and not yet answered, each with its due date",
    to: "/compliance/data-protection",
    read: () => tally(() => api.GET("/v1/compliance/personal-data-requests", {}), (r) => openRequest.has(r.status)),
  },
  {
    id: "breaches-open",
    roles: ["dpo"],
    icon: ShieldAlert,
    tone: "critical",
    title: (n, one) => `${n} ${one ? "breach" : "breaches"} still open`,
    meta: "Recorded and not yet closed: assess each, and notify the PDPC and the people affected when it is notifiable",
    to: "/compliance/data-protection",
    read: () => tally(() => api.GET("/v1/compliance/breaches", {}), (b) => b.status !== "closed"),
  },
];

// The approvals an Administrator's and a Company secretary's rows count, by the object each changes.
const roleObjects = new Set(["membership", "member_invitation"]);
const registerObjects = new Set(["holder_record", "share_class", "equity_transaction"]);
// The company secretary's findings that need a person, and the data requests not yet answered.
const toSee = new Set(["due", "review", "blocked"]);
const openRequest = new Set(["received", "acknowledged", "extended"]);

// rowsFor are the rows a person's roles call for, each once.
export function rowsFor(roles: readonly string[]): Work[] {
  return work.filter((w) => w.roles.some((r) => roles.includes(r)));
}

// The roles that land on a page of their own instead of Home (§7.1, landingOf), whose "Home shows" is drawn there.
export const landingRoles = { settings: ["administrator"], compliance: ["company_secretary", "dpo"] } as const;

// YourWork is the per-role part of Home (§7.1, FR-UX-40): for the Bookkeeper, the AR and AP specialists and the payment
// releaser, the work waiting in their areas, each row opening the list it counts, and the releaser's bank balances.
// The Administrator, the Company secretary and the DPO land on Settings or Compliance instead, which draw their rows
// there (only: the roles that land on the page). A person with several roles sees each row once; a full-Home role
// (Owner, Controller, Accountant, Tax agent) gets these figures from the pulse, the tiles and the Inbox instead, so none
// of its rows show for it alone.
export function YourWork({ only }: { only?: readonly string[] } = {}) {
  const { session } = useSignedIn();
  const roles = (session.roles ?? []).filter((r) => !only || only.includes(r));
  const rows = rowsFor(roles);
  const h = helpText("home.your-work");
  if (rows.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby="your-work-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="your-work-title" title={h.tooltip}>
          {h.label}
        </h2>
      </div>
      <ul className="fy-list" aria-labelledby="your-work-title">
        {rows.map((w) => (
          <WorkRow key={w.id} work={w} />
        ))}
      </ul>
      {roles.includes("payment_releaser") && <Balances />}
    </section>
  );
}

function WorkRow({ work: w }: { work: Work }) {
  const t = useQuery({ queryKey: ["home-work", w.id], queryFn: w.read, retry: false, staleTime: 30_000 });
  if (!t.data) return null;
  const n = t.data.more ? `${t.data.n} or more` : t.data.n === 0 ? "No" : String(t.data.n);
  const waiting = t.data.n > 0;
  const tone = waiting && w.tone ? `fy-list__icon fy-list__icon--${w.tone === "critical" ? "critical" : "warning"}` : waiting ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon";
  return (
    <li>
      <Link className="fy-list__item" to={w.to}>
        <span className={tone}>
          <Icon of={w.icon} size={20} />
        </span>
        <span className="fy-list__main">
          <span className="fy-list__title">{w.title(n, t.data.n === 1 && !t.data.more)}</span>
          <span className="fy-list__meta whitespace-normal">{w.meta}</span>
        </span>
        <Icon of={ChevronRight} />
      </Link>
    </li>
  );
}

// Balances is the payment releaser's bank balances (§7.1): each money account's balance as the cash position reports
// it, in its own currency.
function Balances() {
  const cash = useQuery({
    queryKey: ["cash-position", "home"],
    retry: false,
    staleTime: 30_000,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/cash-position", {});
      return error || !data ? null : data;
    },
  });
  const p = cash.data;
  if (!p || p.accounts.length === 0) return null;
  return (
    <div className="fy-card__body fy-stack fy-stack--2">
      <h3 className="fy-h3" id="home-balances">
        Bank balances on {date(p.date)}
      </h3>
      <dl className="fy-dl" aria-labelledby="home-balances">
        {p.accounts.map((a) => (
          <div key={a.money_account_id}>
            <dt>{a.name}</dt>
            <dd className="fy-num">
              {formatAmount(a.balance.amount)} {a.balance.currency}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

const soon = new Set(["overdue", "due_soon"]);

// Deadlines is Home's upcoming statutory deadlines (docs/design/screens Home: CPF by the 14th, GST F5, ECI, AIS): the
// first five the calendar has open, those overdue or due soon first, each with its ⏰ chip, for whoever may read the
// calendar (FR-CMP-01). It opens the calendar for a person whose areas hold it.
export function Deadlines() {
  const { session } = useSignedIn();
  const h = helpText("home.deadlines");
  const cal = useQuery({
    queryKey: ["compliance-calendar", "home"],
    retry: false,
    staleTime: 60_000,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/compliance/calendar", {});
      return error || !data ? null : data;
    },
  });
  const items = (cal.data?.deadlines ?? [])
    .filter((d) => d.due_on && (d.status === "open" || soon.has(d.status)))
    .sort((a, b) => Number(soon.has(b.status)) - Number(soon.has(a.status)) || (a.due_on ?? "").localeCompare(b.due_on ?? ""))
    .slice(0, 5);
  if (!cal.data || items.length === 0) return null;
  const linked = mayOpen(nav, session, "/compliance/calendar");
  return (
    <section className="fy-card" aria-labelledby="deadlines-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="deadlines-title" title={h.tooltip}>
          {h.label}
        </h2>
        {linked && (
          <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/compliance/calendar">
            The statutory calendar
          </Link>
        )}
      </div>
      <ul className="fy-list" aria-labelledby="deadlines-title">
        {items.map((d) => (
          <li key={d.key} className="fy-list__item">
            <span className={soon.has(d.status) ? "fy-list__icon fy-list__icon--warning" : "fy-list__icon"}>
              <Icon of={ClockAlert} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">{d.title}</span>
              <span className="fy-list__meta">Due {date(d.due_on)}</span>
            </span>
            {d.status === "overdue" ? (
              <span className="fy-chip fy-chip--serious">
                <Icon of={ClockAlert} />
                Overdue
              </span>
            ) : d.status === "due_soon" ? (
              <span className="fy-chip fy-chip--warning">
                <Icon of={ClockAlert} />
                Due soon
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

// AuditorAccess is the Auditor's engagement (§7.1): their access is read-only and time-boxed, so it says until when, and
// opens what an auditor reads: the reports, the journals, the evidence and the audit trail. It reads the person's own
// membership, as Settings › Members shows it.
export function AuditorAccess() {
  const { session } = useSignedIn();
  const auditor = (session.roles ?? []).includes("auditor");
  const me = useQuery({
    queryKey: ["members", "auditor-access"],
    enabled: auditor && !!session.user_id,
    retry: false,
    staleTime: 60_000,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/members", { params: { query: max } });
      return error || !data ? null : ((data.items ?? []).find((m) => m.user_id === session.user_id) ?? null);
    },
  });
  if (!auditor) return null;
  const ends = me.data?.roles?.find((r) => r.role === "auditor")?.expires_at;
  const h = helpText("home.auditor-access");
  return (
    <section className="fy-card" aria-labelledby="auditor-access-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="auditor-access-title" title={h.tooltip}>
          {h.label}
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--2">
        <p>{ends ? `Read-only, until ${date(ends)}. Nothing you open here can be changed.` : "Read-only: nothing you open here can be changed."}</p>
        <ul className="fy-list" aria-labelledby="auditor-access-title">
          {auditorPages.map((p) => (
            <li key={p.to}>
              <Link className="fy-list__item" to={p.to}>
                <span className="fy-list__icon">
                  <Icon of={p.icon} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title">{p.title}</span>
                  <span className="fy-list__meta whitespace-normal">{p.meta}</span>
                </span>
                <Icon of={ChevronRight} />
              </Link>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

const auditorPages: { to: string; icon: LucideIcon; title: string; meta: string }[] = [
  { to: "/accounting/journals", icon: FileText, title: "Journals", meta: "Every journal, with its lines, evidence and history" },
  { to: "/compliance/evidence", icon: FileSearch, title: "Evidence", meta: "The files kept for each record, searchable and exportable" },
  { to: "/compliance/audit-trail", icon: ScrollText, title: "Audit trail", meta: "Who changed what, when and why, with each change's before and after" },
];
