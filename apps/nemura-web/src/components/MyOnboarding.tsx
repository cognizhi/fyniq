import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Task = components["schemas"]["PlanTask"];

// MyOnboarding is the tasks of an onboarding plan that are the person's to do (NM-ONB-01): their own as the joiner, those of the people who report to them as their line manager, and IT's or HR's by role.
// It shows nothing when there is none, or when the company has Onboarding switched off.
export function MyOnboarding() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [failed, setFailed] = useState(false);
  const load = () => {
    api.GET("/v1/me/onboarding").then(({ data }) => setTasks(data?.tasks ?? []), () => setTasks([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (tasks.length === 0) return null;
  const mark = async (id: string) => {
    const { error } = await api.POST("/v1/onboarding/plan-tasks/{id}/decision", { params: { path: { id } }, body: { state: "done" } });
    setFailed(!!error);
    if (!error) load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="myonb-h">
      <h2 className="fy-h2" id="myonb-h">{t("myonb.title")}</h2>
      {failed && <p role="alert" className="fy-secondary">{t("plans.failed")}</p>}
      <ul className="fy-stack fy-stack--1">
        {tasks.map((x) => (
          <li key={x.id} className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
            <span>{t("myonb.task", { title: x.title, name: x.person, due: x.due_on })}</span>
            {x.state === "open" && x.overdue && <span className="fy-chip">{t("plans.overdue")}</span>}
            {x.state === "open" ? <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={() => mark(x.id)}>{t("plans.markDone", { title: x.title })}</button> : <span className="fy-chip">{t(`plans.state.${x.state}`)}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}
