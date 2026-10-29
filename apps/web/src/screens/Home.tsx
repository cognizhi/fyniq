import { useQuery } from "@tanstack/react-query";
import { Link, Navigate } from "@tanstack/react-router";
import { ChevronRight, Circle, CircleCheck, ClipboardCheck, ClockAlert, Stamp, TriangleAlert } from "lucide-react";
import { api } from "../api/client";
import { useSignedIn } from "../session";
import { Icon } from "../ui";
import { Dashboard } from "./Dashboard";

// Each read answers null when the person's roles may not read it: the step it feeds is left out
// rather than shown as not done (the API decides who sees what).
async function count<T>(read: () => Promise<{ data?: { items?: T[] | null }; error?: unknown }>, keep: (x: T) => boolean = () => true): Promise<number | null> {
  const { data, error } = await read();
  return error || !data ? null : (data.items ?? []).filter(keep).length;
}

function useCounts() {
  const opts = { retry: false, staleTime: 30_000 };
  return {
    // The automatic reversals that wait for a month that is closed or a year not opened (FR-GL-02; rev 0.58).
    reversals: useQuery({ ...opts, queryKey: ["journals", "waiting-reversals", "home"], queryFn: () => count(() => api.GET("/v1/journals/waiting-reversals", { params: { query: { page_size: 100 } } })) }),
    toApprove: useQuery({ ...opts, queryKey: ["approvals", "home"], queryFn: () => count(() => api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 500 } } }), (a) => a.you_may_approve) }),
    review: useQuery({
      ...opts,
      queryKey: ["review-log", "home"],
      queryFn: async () => {
        const { data, error } = await api.GET("/v1/review-log", { params: { query: { status: "unreviewed", page_size: 200 } } });
        return error || !data ? null : { mine: (data.items ?? []).filter((e) => e.you_may_review).length, overdue: data.overdue };
      },
    }),
    profile: useQuery({
      ...opts,
      queryKey: ["profile", "home"],
      queryFn: async () => {
        const { data, error } = await api.GET("/v1/organisation/profile");
        return error || !data ? null : data;
      },
    }),
    opening: useQuery({
      ...opts,
      queryKey: ["opening-balances", "home"],
      queryFn: async () => {
        const { data, error } = await api.GET("/v1/opening-balances");
        return error || !data ? null : data.status;
      },
    }),
    periods: useQuery({ ...opts, queryKey: ["periods", "home"], queryFn: () => count(() => api.GET("/v1/periods", { params: { query: { page_size: 1 } } })) }),
    accounts: useQuery({ ...opts, queryKey: ["accounts", "home"], queryFn: () => count(() => api.GET("/v1/accounts", { params: { query: { page_size: 500 } } })) }),
    money: useQuery({ ...opts, queryKey: ["accounts", "home-money"], queryFn: () => count(() => api.GET("/v1/accounts", { params: { query: { page_size: 500 } } }), (a) => a.money_account && a.active) }),
    contacts: useQuery({ ...opts, queryKey: ["contacts", "home"], queryFn: () => count(() => api.GET("/v1/contacts", { params: { query: { page_size: 1 } } })) }),
    members: useQuery({ ...opts, queryKey: ["members", "home"], queryFn: () => count(() => api.GET("/v1/members", { params: { query: { page_size: 10 } } })) }),
    invites: useQuery({ ...opts, queryKey: ["invitations", "home"], queryFn: () => count(() => api.GET("/v1/members/invitations", { params: { query: { page_size: 10 } } })) }),
    passkeys: useQuery({ ...opts, queryKey: ["passkeys", "home"], queryFn: () => count(() => api.GET("/v1/session/passkeys", { params: { query: { page_size: 50 } } }), (k) => !k.revoked_at) }),
  };
}

interface Step {
  id: string;
  title: string;
  why: string;
  done: boolean;
  to: string;
}

// Waiting is what waits for the person now: approvals, the weekly review, and automatic reversals that cannot post
// until a month is opened (FR-APR-01, FM-3, FR-GL-02).
function Waiting({ toApprove, review, reversals }: { toApprove: number | null; review: { mine: number; overdue: number } | null; reversals: number | null }) {
  if (toApprove === null && review === null && !reversals) return null;
  return (
    <section className="fy-card" aria-labelledby="waiting-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="waiting-title">
          Waiting for you
        </h2>
      </div>
      <ul className="fy-list" aria-labelledby="waiting-title">
        {toApprove !== null && (
          <li>
            <Link className="fy-list__item" to="/inbox">
              <span className={toApprove ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon"}>
                <Icon of={Stamp} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">{toApprove === 0 ? "Nothing to approve" : toApprove === 1 ? "1 request to approve" : `${toApprove} requests to approve`}</span>
                <span className="fy-list__meta">Changes another person asked for, waiting for your approval</span>
              </span>
              <Icon of={ChevronRight} />
            </Link>
          </li>
        )}
        {review !== null && (
          <li>
            <Link className="fy-list__item" to="/review-log">
              <span className={review.mine ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon"}>
                <Icon of={ClipboardCheck} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">{review.mine === 0 ? "Nothing to review" : review.mine === 1 ? "1 high-risk action to review" : `${review.mine} high-risk actions to review`}</span>
                <span className="fy-list__meta">The weekly review of another founder's actions</span>
              </span>
              {review.overdue > 0 && (
                <span className="fy-chip fy-chip--serious">
                  <Icon of={ClockAlert} />
                  {review.overdue} overdue
                </span>
              )}
              <Icon of={ChevronRight} />
            </Link>
          </li>
        )}
        {reversals ? (
          <li>
            <Link className="fy-list__item" to="/accounting/journals">
              <span className="fy-list__icon fy-list__icon--warning">
                <Icon of={TriangleAlert} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">{reversals === 1 ? "1 automatic reversal is waiting" : `${reversals >= 100 ? "100 or more" : reversals} automatic reversals are waiting`}</span>
                <span className="fy-list__meta">Each is dated in a month that is closed or a year not opened yet: it posts once that month is open</span>
              </span>
              <Icon of={ChevronRight} />
            </Link>
          </li>
        ) : null}
      </ul>
    </section>
  );
}

// SetUp is the guided set-up checklist (FR-UX-10, docs/design/screens guided-setup): the steps the
// API can tell are done, with progress. Opening balances, bank feeds, employees and the workbook
// import join it with their modules.
function SetUp({ steps }: { steps: Step[] }) {
  if (steps.length === 0) return null;
  const done = steps.filter((s) => s.done).length;
  return (
    <section className="fy-card" aria-labelledby="setup-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="setup-title">
          Set up the company
        </h2>
        <span className="fy-badge fy-badge--quiet">
          {done} of {steps.length}
        </span>
      </div>
      <div className="fy-card__body">
        <div className="fy-progress fy-progress--good" role="progressbar" aria-label="Set-up progress" aria-valuemin={0} aria-valuemax={steps.length} aria-valuenow={done}>
          <span className={["w-0", "w-1/8", "w-1/4", "w-3/8", "w-1/2", "w-5/8", "w-3/4", "w-7/8", "w-full"][Math.round((done / steps.length) * 8)]} />
        </div>
      </div>
      <ol className="fy-list" aria-labelledby="setup-title">
        {steps.map((s) => (
          <li key={s.id}>
            <Link className="fy-list__item" to={s.to}>
              <span className={s.done ? "fy-list__icon fy-list__icon--good" : "fy-list__icon"}>
                <Icon of={s.done ? CircleCheck : Circle} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{s.title}</span>
                <span className="fy-list__meta whitespace-normal">{s.why}</span>
              </span>
              <span className={s.done ? "fy-chip fy-chip--good" : "fy-chip fy-chip--neutral"}>{s.done ? "Done" : "To do"}</span>
            </Link>
          </li>
        ))}
      </ol>
    </section>
  );
}

// Home is the first signed-in page (docs/design/screens Home): the money's pulse, tiles and charts
// (FR-KPI-01), what waits for the person, and while the company is new, the set-up checklist.
// Deadlines arrive with their modules.
// personalRoles are the roles whose rights are a person's own items only (FR-UX-39): someone who holds no other lands
// on My claims, with nothing of the company's finances.
export const personalRoles = ["employee"];

export function personalOnly(roles: readonly string[] | null | undefined): boolean {
  return (roles ?? []).length > 0 && (roles ?? []).every((r) => personalRoles.includes(r));
}

export function Home() {
  const { session } = useSignedIn();
  return personalOnly(session.roles) ? <Navigate to="/me/claims" replace /> : <Workspace />;
}

function Workspace() {
  const { session, organisation } = useSignedIn();
  const c = useCounts();
  const steps: Step[] = [];
  const add = (id: string, known: unknown, done: boolean, title: string, why: string, to: string) => {
    if (known !== null && known !== undefined) steps.push({ id, done, title, why, to });
  };
  // An Owner approves but does not keep the books (Appendix D): a founder set up as Owner alone gives
  // themselves the roles they work in first, such as Controller and Accountant (FR-ADM-01, rev 0.35).
  const roles = session.roles ?? [];
  add("roles", c.members.data, roles.some((r) => r !== "owner"), "Give yourself the roles you work in", "The Owner approves; the Controller, Accountant or Bookkeeper keeps the books. Settings › Members and roles", "/settings/members");
  const p = c.profile.data;
  add("profile", p, Boolean(p?.legal_name && p.uen), "Enter the company's legal name and UEN", "Documents print them; Settings › Organisation", "/settings/organisation");
  add("year", p, Boolean(p?.books_start), "Set when the books start", "The first day Fyniq keeps the books, and the financial year; Settings › Organisation", "/settings/organisation");
  add("periods", c.periods.data, (c.periods.data ?? 0) > 0, "Open the first financial year", "Its monthly periods: nothing posts on a date until its year is open", "/accounting/close");
  add("chart", c.accounts.data, (c.accounts.data ?? 0) > 0, "Load the chart of accounts", "Start from the standard Singapore start-up chart", "/accounting/chart-of-accounts");
  add("bank", c.money.data, (c.money.data ?? 0) > 0, "Add the bank accounts", "Each bank, card or cash account as a money account in the chart", "/accounting/chart-of-accounts");
  add("opening", c.opening.data, c.opening.data === "posted" || c.opening.data === "reversed", "Enter the opening balances", "Each account's balance on the day the books start", "/accounting/opening-balances");
  add("contacts", c.contacts.data, (c.contacts.data ?? 0) > 0, "Add customers and vendors", "Who you invoice, and whose bills you pay", "/contacts");
  add("founders", c.members.data, (c.members.data ?? 0) > 1 || (c.invites.data ?? 0) > 0, "Invite the other founders", "Before the second one joins, your role changes are made at once", "/settings/members");
  add("passkey", c.passkeys.data, (c.passkeys.data ?? 0) > 0, "Add a passkey", "Confirming it's you, and signing approvals, then takes a touch", "/settings/profile");
  const allDone = steps.length > 0 && steps.every((s) => s.done);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{organisation ? organisation.name : "Fyniq"}</h1>
          <p className="fy-secondary">
            {organisation ? `Books in ${organisation.base_currency}. ${organisation.team_mode === "standard" ? "Standard" : "Founder"} mode.` : "Intelligence behind every number."}
          </p>
        </div>
      </div>
      <div role="status" aria-live="polite" className="api">
        {!organisation && <p>Signed in. You are not a member of an organisation yet.</p>}
        {session.super_admin && <p className="fy-small fy-secondary">You are a Super admin of this installation.</p>}
      </div>
      {organisation && (
        <>
          <Dashboard />
          <Waiting toApprove={c.toApprove.data ?? null} review={c.review.data ?? null} reversals={c.reversals.data ?? null} />
          {!allDone && <SetUp steps={steps} />}
        </>
      )}
    </div>
  );
}
