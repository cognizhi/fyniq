import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";
import { MyOnboarding } from "./MyOnboarding";

type Row = components["schemas"]["RequestRow"];

const iso = (d: Date): string => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

// MyHomePanels are what a person sees first (nm-my-home, NM-ESS-01): the actions they take most, their annual leave left, the next public holiday they observe and where their
// open requests are. Each figure is the API's own, read where it is kept; one the person's company has switched off or the person's roles do not give shows nothing rather than
// a wrong zero. The next pay day and the payslip wait for payroll, and Claims waits for the Fyniq link.
export function MyHomePanels() {
  const [left, setLeft] = useState<string | null>(null);
  const [holiday, setHoliday] = useState<{ date: string; label: string } | null>(null);
  const [open, setOpen] = useState<Row[] | null>(null);
  useEffect(() => {
    const now = new Date();
    const year = now.getFullYear();
    api.GET("/v1/me/leave/{year}", { params: { path: { year } } }).then(
      async ({ data }) => {
        if (!data) return;
        setLeft((data.types ?? []).find((x) => x.key === "annual")?.balance.left ?? null);
        for (let m = 0; m < 14; m++) {
          const month = new Date(now.getFullYear(), now.getMonth() + m, 1);
          const { data: cal } = await api.GET("/v1/leave-calendar", { params: { query: { month: iso(month).slice(0, 7) } } });
          const mine = (cal?.people ?? []).find((p) => p.person_id === data.person_id);
          const next = (mine?.entries ?? []).filter((e) => e.kind === "holiday" && e.date >= iso(now)).sort((a, b) => a.date.localeCompare(b.date))[0];
          if (next) return setHoliday({ date: next.date, label: next.label });
        }
      },
      () => undefined,
    );
    api.GET("/v1/me/requests").then(
      ({ data }) => setOpen((data?.requests ?? []).filter((r) => r.step === "received" || r.step === "with_hr")),
      () => undefined,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const tiles = [
    { href: "/me/leave/new", label: "mytiles.apply" },
    { href: "/me/letters/new", label: "mytiles.letter" },
    { href: "/me/details", label: "mytiles.details" },
    { href: "/me/ask/hr", label: "mytiles.ask" },
    { href: "/me/leave", label: "mytiles.leave" },
    { href: "/me/requests", label: "mytiles.requests" },
  ];
  return (
    <>
      <section className="fy-stack fy-stack--2" aria-labelledby="mytiles-h">
        <h2 className="fy-h2" id="mytiles-h">{t("mytiles.title")}</h2>
        <ul className="fy-row" style={{ gap: "var(--space-3)", flexWrap: "wrap" }}>
          {tiles.map((x) => (
            <li key={x.href}>
              <a className="fy-card" href={x.href}><span className="fy-strong">{t(x.label)}</span></a>
            </li>
          ))}
        </ul>
      </section>
      <MyOnboarding />
      <section className="fy-stack fy-stack--2" aria-labelledby="glance-h">
        <h2 className="fy-h2" id="glance-h">{t("glance.title")}</h2>
        <dl className="fy-stack fy-stack--1">
          {left !== null && <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("glance.left")}</dt><dd>{t("glance.days", { days: left })}</dd></div>}
          {holiday && <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("glance.holiday")}</dt><dd>{t("glance.holidayOn", { name: holiday.label, date: holiday.date })}</dd></div>}
          {open !== null && <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("glance.open")}</dt><dd>{t("glance.count", { n: String(open.length) })}</dd></div>}
        </dl>
        {open !== null && open.length > 0 && (
          <ul className="fy-stack fy-stack--1">
            {open.slice(0, 3).map((r) => (
              <li key={r.id}>{t(`steps.kind.${r.kind}`, { title: r.kind === "details_change" ? t(`steps.change.${r.title}`) : r.title })}: {t(`steps.step.${r.step}`)}, {t("steps.due", { due: r.due_on })}</li>
            ))}
          </ul>
        )}
      </section>
    </>
  );
}
