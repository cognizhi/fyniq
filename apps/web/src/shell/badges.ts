import { useQueries } from "@tanstack/react-query";
import { api, type Session } from "../api/client";
import { tally, type Tally } from "../tally";
import { useStatusPhrases } from "./status";

// The badges of the sidebar (FR-UX-40, experience/navigation "Navigation items and badges"): the Inbox's waiting items, the
// overdue invoices of Sales, the bills due in Purchases, the bank lines to categorise in Banking and the statutory deadlines due in Compliance. Each is counted from
// the list it opens, by the API, which decides who may read it: a read the person's roles refuse gives no badge, and
// nothing is worked out here (FR-UX-22). A badge says what it counts in words for a screen reader, never a bare number,
// and a change is announced through the shell's one polite status region (FR-UX-21).

export interface Badge {
  n: number;
  more: boolean;
  said: string; // "3 approvals waiting"
}

interface Source {
  nav: string; // the navigation item's ID
  noun: [one: string, many: string, what: string];
  read: () => Promise<Tally | null>;
}

const max = { page_size: 500 } as const;

const sources: Source[] = [
  {
    nav: "nav.inbox",
    noun: ["approval", "approvals", "waiting"],
    read: () => tally(() => api.GET("/v1/approvals", { params: { query: { status: "pending", ...max } } }), (a) => a.you_may_approve),
  },
  {
    nav: "nav.sales",
    noun: ["invoice", "invoices", "overdue"],
    read: () => tally(() => api.GET("/v1/invoices", { params: { query: { kind: "invoice", status: "posted", ...max } } }), (i) => i.states.due === "overdue"),
  },
  {
    nav: "nav.purchases",
    noun: ["bill", "bills", "due"],
    read: () => tally(() => api.GET("/v1/bill-reminders", { params: { query: { page_size: 100 } } })),
  },
  {
    nav: "nav.compliance",
    noun: ["deadline", "deadlines", "due"],
    // The calendar's deadlines overdue or due soon: the API works out each one's status.
    read: async () => {
      const { data, error } = await api.GET("/v1/compliance/calendar", {});
      if (error || !data) return null;
      return { n: (data.deadlines ?? []).filter((d) => d.status === "overdue" || d.status === "due_soon").length, more: false };
    },
  },
  {
    nav: "nav.banking",
    noun: ["bank line", "bank lines", "to categorise"],
    read: () => tally(() => api.GET("/v1/bank-lines", { params: { query: { status: "unreconciled", ...max } } })),
  },
];

export function badgeOf(s: Source, t: Tally | null | undefined): Badge | null {
  if (!t) return null;
  const [one, many, what] = s.noun;
  return { n: t.n, more: t.more, said: `${t.n}${t.more ? "+" : ""} ${t.n === 1 && !t.more ? one : many} ${what}` };
}

const every = 60_000;

// useBadges reads each badge the person's navigation holds, again every minute and when the window is focused, and
// announces a change through the status region. It returns the badges by navigation item.
export function useBadges(visibleNav: readonly string[], session: Pick<Session, "user_id">): Record<string, Badge> {
  // The Inbox is for everyone; the others only for those whose navigation holds the area, and whose roles the API lets read.
  const queries = useQueries({
    queries: sources.map((s) => ({
      queryKey: ["nav-badge", s.nav],
      queryFn: s.read,
      enabled: visibleNav.includes(s.nav) && !!session.user_id,
      retry: false,
      refetchInterval: every,
    })),
  });
  const out: Record<string, Badge> = {};
  const phrases = sources.map((s, i) => {
    const b = badgeOf(s, queries[i]!.data);
    if (b) out[s.nav] = b;
    // One phrase per badge, said when its count changes; a count that falls to nothing says so.
    return b === null ? null : b.n === 0 ? `No ${s.noun[1]} ${s.noun[2]}` : b.said;
  });
  useStatusPhrases(phrases);
  return out;
}
