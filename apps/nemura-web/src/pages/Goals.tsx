import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Goal = components["schemas"]["Goal"];
type Measure = components["schemas"]["Measure"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("goals.failed");
const blank = { title: "", due: "", krTitle: "", unit: "", target: "", parent: "" };

// Goals is nm-goals (NM-PRF-01, NM-PRF-02): the company's and the teams' goals, the person's own goals with their key results and the role KPIs set for them. A key result moves by an update the person gives, which keeps
// who gave it. Progress is the API's own, exact and rounded down; the status says whether it keeps up with the period. Nemura shows no company financial figure.
export function Goals({ roles }: { roles: readonly string[] }) {
  const [company, setCompany] = useState<Goal[]>([]);
  const [teams, setTeams] = useState<Goal[]>([]);
  const [mine, setMine] = useState<Goal[]>([]);
  const [kpis, setKpis] = useState<Measure[]>([]);
  const [off, setOff] = useState(false);
  const [note, setNote] = useState<Note>(null);
  const [f, setF] = useState(blank);
  const [value, setValue] = useState<Record<string, string>>({});
  const load = () => {
    api.GET("/v1/me/goals").then(({ data, error }) => {
      setOff(!data && !!error);
      setCompany(data?.company ?? []);
      setTeams(data?.team ?? []);
      setMine(data?.mine ?? []);
      setKpis(data?.kpis ?? []);
    }, () => setMine([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) load();
    return !error;
  };
  const update = async (m: Measure) => done((await api.POST("/v1/measures/{id}/updates", { params: { path: { id: m.id } }, body: { value: value[m.id] ?? "" } })).error, t("goals.updated"));
  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    const measures = f.krTitle ? [{ title: f.krTitle, unit: f.unit || undefined, target: f.target }] : undefined;
    if (done((await api.POST("/v1/goals", { body: { scope: "personal", title: f.title, due_on: f.due, parent_id: f.parent || undefined, measures } })).error, t("goals.added"))) setF(blank);
  };
  const measureLine = (m: Measure, canUpdate: boolean) => (
    <li key={m.id} className="fy-stack fy-stack--1">
      <span>{t("goals.measure", { title: m.title, current: m.current, target: m.target, unit: m.unit ?? "", pct: String(m.progress) })} <span className="fy-chip">{t(`goals.status.${m.status}`)}</span></span>
      <progress max={100} value={m.progress} aria-label={t("goals.progress", { title: m.title })} />
      {canUpdate && (
        <span className="fy-row" style={{ gap: "var(--space-2)" }}>
          <label className="fy-field"><span className="fy-small">{t("goals.value", { title: m.title })}</span><input className="fy-input" inputMode="decimal" pattern="-?[0-9]{1,12}(\.[0-9]{1,4})?" value={value[m.id] ?? ""} onChange={(e) => setValue({ ...value, [m.id]: e.target.value })} /></label>
          <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={() => update(m)}>{t("goals.update", { title: m.title })}</button>
        </span>
      )}
    </li>
  );
  const card = (g: Goal, own: boolean) => (
    <li key={g.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
      <h3 className="fy-h3">{g.title}</h3>
      <span className="fy-secondary">{t("goals.line", { progress: String(g.progress), due: g.due_on, state: t(`goals.state.${g.state}`) })}{g.team ? ` ${g.team}` : ""}</span>
      <ul className="fy-stack fy-stack--2">{g.measures.map((m) => measureLine(m, own && g.state === "open"))}</ul>
      {own && g.state === "open" && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => done((await api.POST("/v1/goals/{id}/closing", { params: { path: { id: g.id } }, body: { state: "done" } })).error, t("goals.closed"))}>{t("goals.markDone", { title: g.title })}</button>}
    </li>
  );
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-goals" />
        <h1 className="fy-h1" title={helpText("goals.page").tooltip}>{t("goals.title")}</h1>
        <p className="fy-secondary">{t("goals.lead")}</p>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {off && <p className="fy-secondary">{t("goals.off")}</p>}
        {!off && (
          <>
            {company.length > 0 && (<section className="fy-stack fy-stack--2" aria-labelledby="co-h"><h2 className="fy-h2" id="co-h">{t("goals.company")}</h2><ul className="fy-stack fy-stack--2">{company.map((g) => card(g, false))}</ul></section>)}
            {teams.length > 0 && (<section className="fy-stack fy-stack--2" aria-labelledby="te-h"><h2 className="fy-h2" id="te-h">{t("goals.team")}</h2><ul className="fy-stack fy-stack--2">{teams.map((g) => card(g, false))}</ul></section>)}
            <section className="fy-stack fy-stack--2" aria-labelledby="my-h">
              <h2 className="fy-h2" id="my-h">{t("goals.mine")}</h2>
              {mine.length === 0 && <p className="fy-secondary">{t("goals.none")}</p>}
              <ul className="fy-stack fy-stack--2">{mine.map((g) => card(g, true))}</ul>
            </section>
            {kpis.length > 0 && (<section className="fy-stack fy-stack--2" aria-labelledby="kpi-h"><h2 className="fy-h2" id="kpi-h">{t("goals.kpis")}</h2><ul className="fy-stack fy-stack--2">{kpis.map((m) => measureLine(m, true))}</ul></section>)}
            <form className="fy-stack fy-stack--2" onSubmit={add} aria-label={t("goals.add")}>
              <h2 className="fy-h2">{t("goals.add")}</h2>
              <label className="fy-field"><span>{t("goals.goalTitle")}</span><input className="fy-input" required maxLength={200} value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} /></label>
              <label className="fy-field"><span>{t("goals.due")}</span><input className="fy-input" type="date" required value={f.due} onChange={(e) => setF({ ...f, due: e.target.value })} /></label>
              <label className="fy-field"><span>{t("goals.align")}</span>
                <select className="fy-input" value={f.parent} onChange={(e) => setF({ ...f, parent: e.target.value })}>
                  <option value="">{t("goals.alignNone")}</option>
                  {[...company, ...teams].map((g) => <option key={g.id} value={g.id}>{g.title}</option>)}
                </select>
              </label>
              <label className="fy-field"><span>{t("goals.kr")}</span><input className="fy-input" maxLength={200} value={f.krTitle} onChange={(e) => setF({ ...f, krTitle: e.target.value })} /></label>
              <label className="fy-field"><span>{t("goals.unit")}</span><input className="fy-input" maxLength={30} value={f.unit} onChange={(e) => setF({ ...f, unit: e.target.value })} /></label>
              <label className="fy-field"><span>{t("goals.target")}</span><input className="fy-input" inputMode="decimal" pattern="-?[0-9]{1,12}(\.[0-9]{1,4})?" required={!!f.krTitle} value={f.target} onChange={(e) => setF({ ...f, target: e.target.value })} /></label>
              <button className="fy-btn fy-btn--primary" type="submit">{t("goals.addButton")}</button>
            </form>
          </>
        )}
      </main>
    </Shell>
  );
}
