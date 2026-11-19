import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type View = components["schemas"]["LeaveView"];
type Kind = { status: "loading" } | { status: "failed"; detail: string } | { status: "ready"; view: View };

const days = [1, 2, 3, 4, 5, 6, 7];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("leave.failed");

// MyLeave is nm-my-leave: a person's balance for a leave year, each type with the steps of its calculation and where the days come from (NM-LV-02). Without a
// person it shows the signed-in person's own; with one it shows theirs, and the people team may work out a year's annual leave, carry unused days forward and set
// the working pattern (NM-LV-13, NM-TIM-01).
export function MyLeave({ roles, personId }: { roles: readonly string[]; personId?: string }) {
  const [year, setYear] = useState(new Date().getFullYear());
  const [state, set] = useState<Kind>({ status: "loading" });
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const load = async () => {
    try {
      const params = { path: { year } };
      const res = personId ? await api.GET("/v1/people/{id}/leave/{year}", { params: { path: { id: personId, year } } }) : await api.GET("/v1/me/leave/{year}", { params });
      set(res.data ? { status: "ready", view: res.data } : { status: "failed", detail: problem(res.error) });
    } catch {
      set({ status: "failed", detail: t("leave.failed") });
    }
  };
  useEffect(() => {
    set({ status: "loading" });
    setNote(null);
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [year, personId]);
  const manage = Boolean(personId) && team(roles) && roles.some((r) => ["hr_officer", "hr_admin"].includes(r));
  const grant = async () => {
    const { data, error } = await api.POST("/v1/people/{id}/leave/{year}/annual-grant", { params: { path: { id: personId ?? "", year } } });
    setNote(data ? { ok: true, text: t("leave.granted", { written: data.written }) } : { ok: false, text: problem(error) });
    await load();
  };
  const carry = async () => {
    const { data, error } = await api.POST("/v1/people/{id}/leave/{year}/carry-forward", { params: { path: { id: personId ?? "", year } } });
    setNote(data ? { ok: true, text: t("leave.carried", { carried: data.carried }) } : { ok: false, text: problem(error) });
    await load();
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-leave" />
        <h1 className="fy-h1" title={helpText("leave.page").tooltip}>{t(personId ? "leave.personTitle" : "leave.title")}</h1>
        <p className="fy-secondary">{t("leave.lead")}</p>
        <div className="fy-row" style={{ gap: "var(--space-3)" }}>
          <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setYear(year - 1)}>{t("leave.previous")}</button>
          <span className="fy-strong" aria-live="polite">{t("leave.year")} {year}</span>
          <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setYear(year + 1)}>{t("leave.next")}</button>
        </div>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("leave.loading")}</p>}
        {state.status === "failed" && <p role="alert" className="fy-secondary">{state.detail}</p>}
        {state.status === "ready" && <Balances view={state.view} />}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {manage && state.status === "ready" && (
          <section className="fy-stack fy-stack--2" aria-labelledby="manage-h">
            <h2 className="fy-h2" id="manage-h">{t("leave.manage")}</h2>
            <div className="fy-row" style={{ gap: "var(--space-3)" }}>
              <button className="fy-btn fy-btn--primary" type="button" onClick={grant}>{t("leave.grant")}</button>
              <button className="fy-btn fy-btn--secondary" type="button" onClick={carry}>{t("leave.carry")}</button>
            </div>
            <PatternForm id={personId ?? ""} saved={async (m) => { setNote(m); await load(); }} />
          </section>
        )}
        {!personId && <a href="/me/leave/new">{t("leave.apply")}</a>}
        {personId && <a href={`/people/${personId}`}>{t("leave.back")}</a>}
      </main>
    </Shell>
  );
}

function Balances({ view }: { view: View }) {
  const p = view.working_pattern;
  return (
    <>
      <section className="fy-stack fy-stack--1" aria-labelledby="pattern-h">
        <h2 className="fy-h2" id="pattern-h">{t("leave.pattern")}</h2>
        <p className="fy-secondary">
          {p ? t("leave.patternFrom", { from: p.effective_from, days: (p.weekdays ?? []).map((d) => t(`leave.day.${d}`)).join(", "), hours: p.hours_per_week }) : t("leave.noPattern")}
        </p>
      </section>
      {(view.types ?? []).length === 0 && <p className="fy-secondary">{t("leave.noTypes")}</p>}
      {(view.types ?? []).map((ty) => (
        <section key={ty.key} className="fy-stack fy-stack--2" aria-labelledby={`type-${ty.key}`}>
          <h2 className="fy-h2" id={`type-${ty.key}`}>{ty.name}</h2>
          <table className="fy-table" aria-label={t("leave.balanceOf", { type: ty.name })}>
            <thead>
              <tr>
                <th scope="col">{t("leave.entitled")}</th>
                <th scope="col">{t("leave.taken")}</th>
                <th scope="col">{t("leave.booked")}</th>
                <th scope="col">{t("leave.expired")}</th>
                <th scope="col">{t("leave.encashed")}</th>
                <th scope="col">{t("leave.left")}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td className="fy-num">{ty.balance.entitled}</td>
                <td className="fy-num">{ty.balance.taken}</td>
                <td className="fy-num">{ty.balance.booked}</td>
                <td className="fy-num">{ty.balance.expired}</td>
                <td className="fy-num">{ty.balance.encashed}</td>
                <td className="fy-num fy-strong">{ty.balance.left}</td>
              </tr>
            </tbody>
          </table>
          <p className="fy-muted">{t("leave.units")}</p>
          {ty.carried_until && <p className="fy-secondary">{t("leave.carriedUntil", { date: ty.carried_until })}</p>}
          {(ty.basis ?? []).length > 0 && (
            <>
              <h3 className="fy-h3">{t("leave.working")}</h3>
              <ol className="fy-stack fy-stack--1">
                {(ty.basis ?? []).map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ol>
            </>
          )}
          {(ty.sources ?? []).length > 0 && <p className="fy-muted">{t("leave.sources", { sources: (ty.sources ?? []).join(", ") })}</p>}
        </section>
      ))}
    </>
  );
}

function PatternForm({ id, saved }: { id: string; saved: (m: { ok: boolean; text: string }) => unknown }) {
  const [from, setFrom] = useState("");
  const [hours, setHours] = useState("44.0");
  const [picked, setPicked] = useState<number[]>([1, 2, 3, 4, 5]);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.PUT("/v1/people/{id}/working-pattern", { params: { path: { id } }, body: { effective_from: from, weekdays: picked, hours_per_week: hours } });
    await saved(error ? { ok: false, text: problem(error) } : { ok: true, text: t("leave.patternSaved") });
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={submit} aria-labelledby="pattern-form-h">
      <h3 className="fy-h3" id="pattern-form-h">{t("leave.patternTitle")}</h3>
      <label className="fy-field">
        <span>{t("leave.patternStart")}</span>
        <input className="fy-input" type="date" required value={from} onChange={(e) => setFrom(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("leave.patternHours")}</span>
        <input className="fy-input" inputMode="decimal" required value={hours} onChange={(e) => setHours(e.target.value)} />
      </label>
      <fieldset className="fy-row" style={{ gap: "var(--space-3)", flexWrap: "wrap" }}>
        <legend>{t("leave.patternDays")}</legend>
        {days.map((d) => (
          <label key={d} className="fy-row" style={{ gap: "var(--space-1)" }}>
            <input type="checkbox" checked={picked.includes(d)} onChange={(e) => setPicked(e.target.checked ? [...picked, d].sort() : picked.filter((x) => x !== d))} />
            <span>{t(`leave.day.${d}`)}</span>
          </label>
        ))}
      </fieldset>
      <button className="fy-btn fy-btn--primary" type="submit">{t("leave.patternSave")}</button>
    </form>
  );
}
