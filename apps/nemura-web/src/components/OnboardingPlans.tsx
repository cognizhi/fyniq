import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Plan = components["schemas"]["Plan"];
type Template = components["schemas"]["PlanTemplate"];
type Person = components["schemas"]["DirectoryEntry"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("plans.failed");

// OnboardingPlans is the people team's side of the onboarding plans (NM-ONB-01): the plans and their tasks, a plan made from a template for a person, and the templates (the standard one is copied to start).
// Everything shown is the API's own; a company with Onboarding switched off is told so and sees no forms.
export function OnboardingPlans({ people }: { people: readonly Person[] }) {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [off, setOff] = useState(false);
  const [note, setNote] = useState<Note>(null);
  const [person, setPerson] = useState("");
  const [template, setTemplate] = useState("");
  const [name, setName] = useState("");
  const live = templates.filter((x) => !x.archived);
  const load = () => {
    api.GET("/v1/onboarding/plans").then(({ data, error }) => (setOff(!data && !!error), setPlans(data?.plans ?? [])), () => setPlans([]));
    api.GET("/v1/onboarding/templates").then(({ data }) => setTemplates(data?.templates ?? []), () => setTemplates([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) load();
  };
  const decide = async (id: string, state: "done" | "skipped") => done((await api.POST("/v1/onboarding/plan-tasks/{id}/decision", { params: { path: { id } }, body: { state } })).error, t("plans.decided"));
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby="plans-h">
      <h2 className="fy-h2" id="plans-h">{t("plans.title")}</h2>
      <p className="fy-secondary">{t("plans.lead")}</p>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {off && <p className="fy-secondary">{t("plans.off")}</p>}
      {!off && plans.length === 0 && <p className="fy-secondary">{t("plans.none")}</p>}
      <ul className="fy-stack fy-stack--3">
        {plans.map((p) => (
          <li key={p.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
            <span className="fy-strong">{t("plans.head", { name: p.person, start: p.start_date, done: String(p.done), total: String(p.total) })}</span>
            <ul className="fy-stack fy-stack--1">
              {p.tasks.map((x) => (
                <li key={x.id} className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
                  <span>{t("plans.task", { title: x.title, who: t(`plans.assignee.${x.assignee}`), due: x.due_on })}</span>
                  {x.state === "open" && x.overdue && <span className="fy-chip">{t("plans.overdue")}</span>}
                  {x.state !== "open" && <span className="fy-chip">{t(`plans.state.${x.state}`)}</span>}
                  {x.state === "open" && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={() => decide(x.id, "done")}>{t("plans.markDone", { title: x.title })}</button>}
                  {x.state === "open" && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={() => decide(x.id, "skipped")}>{t("plans.skip", { title: x.title })}</button>}
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
      {!off && (
        <>
          <form className="fy-stack fy-stack--2" aria-label={t("plans.start")} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/people/{id}/onboarding-plan", { params: { path: { id: person || people[0]?.id || "" } }, body: { template_id: template || undefined } })).error, t("plans.started")); }}>
            <h3 className="fy-h3">{t("plans.start")}</h3>
            <label className="fy-field"><span>{t("plans.person")}</span>
              <select className="fy-input" value={person || people[0]?.id || ""} onChange={(e) => setPerson(e.target.value)}>{people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
            </label>
            <label className="fy-field"><span>{t("plans.template")}</span>
              <select className="fy-input" value={template} onChange={(e) => setTemplate(e.target.value)}>
                <option value="">{t("plans.standard")}</option>
                {live.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
              </select>
            </label>
            <button className="fy-btn fy-btn--primary" type="submit">{t("plans.startButton")}</button>
          </form>
          <form className="fy-stack fy-stack--2" aria-label={t("plans.makeTemplate")} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/onboarding/templates", { body: { name } })).error, t("plans.templateMade")); setName(""); }}>
            <h3 className="fy-h3">{t("plans.makeTemplate")}</h3>
            <p className="fy-secondary">{t("plans.templateLead")}</p>
            <label className="fy-field"><span>{t("plans.name")}</span><input className="fy-input" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></label>
            <button className="fy-btn fy-btn--secondary" type="submit">{t("plans.makeButton")}</button>
          </form>
          <ul className="fy-stack fy-stack--1">
            {templates.map((x) => (
              <li key={x.id} className="fy-row" style={{ gap: "var(--space-2)" }}>
                <span>{t("plans.templateLine", { name: x.name, tasks: String(x.tasks.length) })}</span>
                {x.archived ? <span className="fy-chip">{t("plans.archived")}</span> : <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => done((await api.POST("/v1/onboarding/templates/{id}/archive", { params: { path: { id: x.id } } })).error, t("plans.templateArchived"))}>{t("plans.archive", { name: x.name })}</button>}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
