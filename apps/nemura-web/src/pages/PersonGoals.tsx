import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Goal = components["schemas"]["Goal"];
type Measure = components["schemas"]["Measure"];

// PersonGoals is a person's goals and role KPIs for their line manager and the people team (NM-PRF-01, NM-PRF-02): the same goals and key results the person sees, read only. A member who may not read them is told
// there are none, as the API answers. It changes nothing: the person updates their own key results.
export function PersonGoals({ roles, personId }: { roles: readonly string[]; personId: string }) {
  const [goals, setGoals] = useState<Goal[]>([]);
  const [kpis, setKpis] = useState<Measure[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "none">("loading");
  useEffect(() => {
    void api.GET("/v1/people/{id}/goals", { params: { path: { id: personId } } }).then(({ data }) => {
      if (!data) return setState("none");
      setGoals(data.goals);
      setKpis(data.kpis);
      setState("ready");
    }, () => setState("none"));
  }, [personId]);
  const line = (m: Measure) => (
    <li key={m.id} className="fy-stack fy-stack--1">
      <span>{t("goals.measure", { title: m.title, current: m.current, target: m.target, unit: m.unit ?? "", pct: String(m.progress) })} <span className="fy-chip">{t(`goals.status.${m.status}`)}</span></span>
      <progress max={100} value={m.progress} aria-label={t("goals.progress", { title: m.title })} />
    </li>
  );
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-goals" />
        <h1 className="fy-h1" title={helpText("goals.page").tooltip}>{t("persongoals.title")}</h1>
        <p className="fy-secondary">{t("persongoals.lead")}</p>
        {state === "loading" && <p role="status" className="fy-secondary">{t("person.loading")}</p>}
        {state === "none" && <p role="alert" className="fy-secondary">{t("persongoals.none")}</p>}
        {state === "ready" && goals.length === 0 && <p className="fy-secondary">{t("persongoals.empty")}</p>}
        {state === "ready" && (
          <ul className="fy-stack fy-stack--2">
            {goals.map((g) => (
              <li key={g.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
                <h2 className="fy-h3">{g.title}</h2>
                <span className="fy-secondary">{t("goals.line", { progress: String(g.progress), due: g.due_on, state: t(`goals.state.${g.state}`) })}</span>
                <ul className="fy-stack fy-stack--2">{g.measures.map(line)}</ul>
              </li>
            ))}
          </ul>
        )}
        {state === "ready" && kpis.length > 0 && (<section className="fy-stack fy-stack--2" aria-labelledby="pk-h"><h2 className="fy-h2" id="pk-h">{t("goals.kpis")}</h2><ul className="fy-stack fy-stack--2">{kpis.map(line)}</ul></section>)}
        <a href={`/people/${personId}`}>{t("persongoals.back")}</a>
      </main>
    </Shell>
  );
}
