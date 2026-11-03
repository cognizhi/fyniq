import { CircleCheck, CircleHelp, CircleX, ClockAlert, TriangleAlert, type LucideIcon } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";

export type Checks = components["schemas"]["ChecksOut"];
export type Check = components["schemas"]["CheckOut"];
export type CheckItem = components["schemas"]["CheckItem"];

// readChecks reads the checks the person sees, with books health (GET /v1/checks; FR-CTL-01, FR-CTL-02).
export async function readChecks(): Promise<Checks> {
  const { data, error } = await api.GET("/v1/checks");
  if (error) throw error;
  return data;
}

// A status as a chip: icon and words, never colour alone (FR-UX-09; MASTER.md "status chips"). ✖ blocks the close; a
// check whose run failed counts as ✖ (rev 0.162); one not assessed is outlined, as it does not change the score.
export const statuses: Record<Check["status"], { label: string; icon: LucideIcon; tone: string }> = {
  error: { label: "Blocks the close", icon: CircleX, tone: "fy-chip--critical" },
  failed: { label: "Could not run", icon: CircleX, tone: "fy-chip--critical" },
  warning: { label: "To review", icon: TriangleAlert, tone: "fy-chip--warning" },
  due: { label: "Due", icon: ClockAlert, tone: "fy-chip--serious" },
  not_assessed: { label: "Not assessed", icon: CircleHelp, tone: "fy-chip--warning fy-chip--outline" },
  ok: { label: "OK", icon: CircleCheck, tone: "fy-chip--good" },
};

// The score in words (FR-CTL-02).
export const scores: Record<Checks["health"]["score"], { label: string; icon: LucideIcon; tone: string }> = {
  problems: { label: "Problems", icon: CircleX, tone: "fy-chip--critical" },
  review: { label: "Review", icon: TriangleAlert, tone: "fy-chip--warning" },
  ok: { label: "OK", icon: CircleCheck, tone: "fy-chip--good" },
};

// needsAction is a check that found something, or could not say.
export const needsAction = (c: Check) => c.status !== "ok";

// itemLink is where an item opens (FR-UX-04): its record's page, or none for one with no page yet.
export function itemLink(item: CheckItem): { to: string; params?: Record<string, string> } | null {
  switch (item.kind) {
    case "period":
      return item.id ? { to: "/accounting/close/$periodId", params: { periodId: item.id } } : null;
    case "close": // the close's own finding for a month: "<period>/<its key>"
      return item.id ? { to: "/accounting/close/$periodId", params: { periodId: item.id.split("/")[0] ?? item.id } } : null;
    case "account":
    case "role":
      return { to: "/accounting/chart-of-accounts" };
    case "opening_balances":
      return { to: "/accounting/opening-balances" };
    case "review_log":
      return { to: "/review-log" };
    case "member":
      return { to: "/settings/members" };
    case "access_review":
      return { to: "/settings/access-review" };
    case "statutory_pack":
      return { to: "/settings/statutory-packs" };
    case "connector":
      return item.id ? { to: "/settings/integrations/$connector", params: { connector: item.id } } : null;
    case "connector_secret": // "<connector>/<its secret's name>"
      return item.id ? { to: "/settings/integrations/$connector", params: { connector: item.id.split("/")[0] ?? item.id } } : null;
    default:
      return null;
  }
}
