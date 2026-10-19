import type { Session } from "../api/client";
import type { Mode, NavFeature } from "../registry";

// navFor cuts the navigation to the person (FR-UX-40, §7.1): an item shows when one of the person's
// roles in the organisation may see it, the organisation has switched its feature on (FR-ORG-06),
// and the experience mode shows it (Basic hides Expert-only items, experience/modes "Basic and
// Expert navigation"). This is presentation only: the API refuses whatever a role may not do, so a
// hidden page called directly still answers 403.
export function navFor<T extends NavFeature>(items: readonly T[], session: Pick<Session, "roles" | "features">, mode: Mode): T[] {
  const roles = new Set(session.roles ?? []);
  return items.filter(
    (n) =>
      (n.roles === "all" || n.roles.some((r) => roles.has(r))) &&
      (!n.feature || session.features?.[n.feature] === true) &&
      (mode === "expert" || n.mode === "basic"),
  );
}

// The compact bottom bar holds at most five items: four of the person's, then More (MASTER.md §5.1).
export function bottomBar<T extends NavFeature>(visible: readonly T[]): { items: T[]; more: boolean } {
  const items = visible.filter((n) => n.bottom).slice(0, 4);
  return { items, more: visible.length > items.length };
}
