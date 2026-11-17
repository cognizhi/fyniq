import type { Session } from "../api/client";
import type { Mode, NavFeature } from "../registry";

// navFor cuts the navigation to the person (FR-UX-40, §7.1): an item shows when one of the person's
// roles in the organisation may see it, the organisation has switched its feature on (FR-ORG-06),
// and the experience mode shows it (Basic hides Expert-only items, experience/modes "Basic and
// Expert navigation"). A Super admin also has the role "super_admin" here, for the installation's pages. This is
// presentation only: the API refuses whatever a role may not do, so a hidden page called directly still answers 403.
export function navFor<T extends NavFeature>(items: readonly T[], session: Pick<Session, "roles" | "features"> & { super_admin?: boolean }, mode: Mode): T[] {
  const roles = new Set<string>(session.roles ?? []);
  if (session.super_admin) roles.add("super_admin");
  // A person whose rights are their own items only has the personal workspace: its items and Settings, where their
  // profile is; no Home, Inbox or finance area (FR-UX-39).
  const personal = personalOnly(session.roles);
  return items.filter(
    (n) =>
      (!personal || n.group === "personal" || n.group === "footer") &&
      (n.roles === "all" || n.roles.some((r) => roles.has(r))) &&
      (!n.feature || session.features?.[n.feature] === true) &&
      (mode === "expert" || n.mode === "basic"),
  );
}

// personalRoles are the roles whose rights are a person's own items only (FR-UX-39): someone who holds no other lands
// on My claims, with nothing of the company's finances.
export const personalRoles = ["employee"];

export function personalOnly(roles: readonly string[] | null | undefined): boolean {
  return (roles ?? []).length > 0 && (roles ?? []).every((r) => personalRoles.includes(r));
}

// The pages a person with personal rights only may open (FR-UX-39): their own items, their settings, an approval
// they were asked for, their search and help. A link to any other page shows that it is not part of their access.
const personalPaths = ["/me", "/settings", "/approvals", "/search", "/help"];

// areaOf is the navigation area a path is in (FR-UX-40): the main item whose route starts the path, or whose route's
// first part does, the longest match winning, so /purchases/claims is Expense claims and /purchases/orders Purchases.
// Home and the Inbox are everyone's, so a path no area holds (Settings, a person's own pages) has none.
export function areaOf<T extends NavFeature>(items: readonly T[], path: string): T | undefined {
  let best: T | undefined;
  let length = 0;
  const under = (prefix: string) => path === prefix || path.startsWith(`${prefix}/`);
  for (const n of items) {
    if (n.group !== "main" || n.roles === "all") continue;
    for (const prefix of [n.route, `/${n.route.split("/")[1]}`]) {
      if (under(prefix) && prefix.length > length) {
        best = n;
        length = prefix.length;
      }
    }
  }
  return best;
}

// mayOpen says whether a path is part of the person's access as the navigation shows it: inside an area their roles
// hold, or no area's (FR-UX-39, FR-UX-40). Presentation only: the API refuses what a role may not read either way.
export function mayOpen<T extends NavFeature>(items: readonly T[], session: Pick<Session, "roles"> & { super_admin?: boolean }, path: string): boolean {
  if (personalOnly(session.roles)) return path === "/" || personalPaths.some((p) => path === p || path.startsWith(`${p}/`));
  const area = areaOf(items, path);
  if (!area || area.roles === "all") return true;
  return area.roles.some((r) => (session.roles ?? []).includes(r) || (r === "super_admin" && !!session.super_admin));
}

// homeRoles land on Home (§7.1, "Finance · Home"); a person with none of them lands where their role's work is.
const homeRoles = ["approver", "bookkeeper", "ar_specialist", "ap_specialist", "payment_releaser", "payroll_administrator", "accountant", "controller", "owner", "tax_agent"];
const landings: [string, string][] = [
  ["administrator", "/settings"],
  ["company_secretary", "/compliance"],
  ["dpo", "/compliance"],
  ["board_viewer", "/reports"],
  ["auditor", "/reports"],
];

// landingOf is where a person lands instead of Home (§7.1's table): My claims with personal rights only, otherwise
// their role's page when no role of theirs lands on Home; undefined for Home. With several roles, Home wins.
export function landingOf(roles: readonly string[] | null | undefined): string | undefined {
  const rs = roles ?? [];
  if (personalOnly(rs)) return "/me/claims";
  if (rs.some((r) => homeRoles.includes(r))) return undefined;
  return landings.find(([r]) => rs.includes(r))?.[1];
}

// The compact bottom bar holds at most five items: four of the person's, then More (MASTER.md §5.1), which is always
// there, since its sheet also holds the account (appearance, About and sign out) the compact header has no room for.
export function bottomBar<T extends NavFeature>(visible: readonly T[]): T[] {
  return visible.filter((n) => n.bottom).slice(0, 4);
}
