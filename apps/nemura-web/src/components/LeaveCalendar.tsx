import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Calendar = components["schemas"]["LeaveCalendar"];
type State = { status: "loading" } | { status: "failed" } | { status: "ready"; cal: Calendar };

const pad = (n: number): string => String(n).padStart(2, "0");
const monthOf = (d: Date): string => `${d.getFullYear()}-${pad(d.getMonth() + 1)}`;

// LeaveCalendar is the month at a glance (NM-LV-05): a row for each person the API puts in the caller's view and a column for each day, with a letter for away, leave,
// a request waiting, sick leave and a public holiday as that person observes it. The API decides what each viewer may see: where the type is not theirs it says Away.
export function LeaveCalendar({ start }: { start?: string }) {
  const [month, setMonth] = useState(start ?? monthOf(new Date()));
  const [state, set] = useState<State>({ status: "loading" });
  useEffect(() => {
    let live = true;
    set({ status: "loading" });
    api.GET("/v1/leave-calendar", { params: { query: { month } } }).then(
      ({ data }) => live && set(data ? { status: "ready", cal: data } : { status: "failed" }),
      () => live && set({ status: "failed" }),
    );
    return () => {
      live = false;
    };
  }, [month]);
  const [y, m] = month.split("-").map(Number) as [number, number];
  const shift = (by: number) => setMonth(monthOf(new Date(y, m - 1 + by, 1)));
  const last = new Date(y, m, 0).getDate();
  const days = Array.from({ length: last }, (_, i) => i + 1);
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="calendar-h">
      <h2 className="fy-h2" id="calendar-h">{t("calendar.title")}</h2>
      <div className="fy-row" style={{ gap: "var(--space-3)" }}>
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => shift(-1)}>{t("calendar.previous")}</button>
        <span className="fy-strong" aria-live="polite">{t("calendar.month")} {month}</span>
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => shift(1)}>{t("calendar.next")}</button>
      </div>
      <p className="fy-muted">{t("calendar.key")}</p>
      {state.status === "loading" && <p role="status" className="fy-secondary">{t("calendar.loading")}</p>}
      {state.status === "failed" && <p role="alert" className="fy-secondary">{t("calendar.failed")}</p>}
      {state.status === "ready" && (state.cal.people ?? []).length === 0 && <p className="fy-secondary">{t("calendar.none")}</p>}
      {state.status === "ready" && (state.cal.people ?? []).length > 0 && (
        <div style={{ overflowX: "auto" }}>
          <table className="fy-table">
            <thead>
              <tr>
                <th scope="col">{t("calendar.person")}</th>
                {days.map((d) => (
                  <th key={d} scope="col" className="fy-num">{d}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(state.cal.people ?? []).map((p) => {
                const by = new Map((p.entries ?? []).map((e) => [e.date, e]));
                return (
                  <tr key={p.person_id}>
                    <th scope="row">{p.name}</th>
                    {days.map((d) => {
                      const date = `${month}-${pad(d)}`;
                      const e = by.get(date);
                      const dow = new Date(y, m - 1, d).getDay() || 7;
                      const works = (p.weekdays ?? []).length === 0 || (p.weekdays ?? []).includes(dow);
                      if (e) return <td key={d} title={e.label} aria-label={t("calendar.cell", { name: p.name, date, label: e.label })}>{t(`calendar.kind.${e.kind}`)}</td>;
                      return works ? <td key={d} /> : <td key={d} aria-label={t("calendar.cell", { name: p.name, date, label: t("calendar.restDay") })}>·</td>;
                    })}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
