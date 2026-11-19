import { useEffect, useState } from "react";
import { api } from "../api/client";
import { t } from "../i18n";

type Counts = { requests: number; away: number; certificates: number; checks: number; flex: number; claims: number };

const today = (): string => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};

// PeopleTiles are the leave tiles of the people team's home (nm-people-home): what waits and who is away today, each a link to the page that deals with it. A figure the
// caller's roles do not give shows nothing rather than a wrong zero.
export function PeopleTiles() {
  const [c, set] = useState<Partial<Counts>>({});
  useEffect(() => {
    const day = today();
    const put = (patch: Partial<Counts>) => set((was) => ({ ...was, ...patch }));
    api.GET("/v1/leave-requests", { params: { query: { state: "pending" } } }).then(({ data }) => data && put({ requests: (data.requests ?? []).length }), () => undefined);
    api.GET("/v1/leave-calendar", { params: { query: { month: day.slice(0, 7) } } }).then(
      ({ data }) => data && put({ away: (data.people ?? []).filter((p) => (p.entries ?? []).some((e) => e.date === day && e.kind !== "holiday")).length }),
      () => undefined,
    );
    api.GET("/v1/sick-leave", { params: { query: { unverified: true } } }).then(({ data }) => data && put({ certificates: (data.notifications ?? []).length }), () => undefined);
    api.GET("/v1/checks").then(({ data }) => data && put({ checks: (data.findings ?? []).length }), () => undefined);
    api.GET("/v1/flexible-work-requests").then(({ data }) => data && put({ flex: (data.requests ?? []).filter((r) => r.state === "received").length }), () => undefined);
    api.GET("/v1/government-paid-leave").then(({ data }) => data && put({ claims: (data.blocks ?? []).filter((b) => (b.claims ?? []).length === 0).length }), () => undefined);
  }, []);
  const tiles: { key: keyof Counts; label: string; href: string }[] = [
    { key: "requests", label: "tiles.requests", href: "/people/leave" },
    { key: "away", label: "tiles.away", href: "/people/leave" },
    { key: "certificates", label: "tiles.certificates", href: "/people/leave" },
    { key: "flex", label: "tiles.flex", href: "/people/leave" },
    { key: "claims", label: "tiles.claims", href: "/people/leave/government-paid" },
    { key: "checks", label: "tiles.checks", href: "/settings/policy" },
  ];
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="tiles-h">
      <h2 className="fy-h2" id="tiles-h">{t("tiles.title")}</h2>
      <ul className="fy-row" style={{ gap: "var(--space-3)", flexWrap: "wrap" }}>
        {tiles.map((x) => (
          <li key={x.key}>
            <a className="fy-card" href={x.href}>
              <span className="fy-strong">{c[x.key] === undefined ? "–" : String(c[x.key])}</span> <span>{t(x.label)}</span>
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}
