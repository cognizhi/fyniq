import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Task = components["schemas"]["JoinTask"];
type Leaving = components["schemas"]["Offboarding"];
type Entry = components["schemas"]["DirectoryEntry"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("onb.failed");
const kinds = ["resignation", "termination", "retrenchment", "end_of_contract", "retirement", "other"] as const;
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const blank = { person: "", kind: "resignation" as (typeof kinds)[number], notice: "", last: "", retrench: "", equipment: "", exit: "", fyniq: "" };

// PeopleOnboarding is nm-onboarding: the joiners' invitation tasks and the leavers' dates, checklist and leave settlement (NM-ONB-02, NM-ONB-03). The dates that start statutory
// clocks are separate fields; the last day sets the leaving date on the record. Final pay and IR21 belong to payroll and are not here.
export function PeopleOnboarding({ roles }: { roles: readonly string[] }) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [leavings, setLeavings] = useState<Leaving[]>([]);
  const [people, setPeople] = useState<Entry[]>([]);
  const [invite, setInvite] = useState<boolean | null>(null);
  const [f, setF] = useState(blank);
  const [note, setNote] = useState<Note>(null);
  const open = tasks.filter((x) => x.state === "open");
  const inviter = roles.some((r) => ["hr_admin", "administrator", "owner"].includes(r));
  const settingsRight = roles.some((r) => ["hr_admin", "owner"].includes(r));
  const hrRight = roles.some((r) => ["hr_officer", "hr_admin", "owner"].includes(r));
  const load = () => {
    if (inviter) {
      api.GET("/v1/onboarding/tasks").then(({ data }) => setTasks(data?.tasks ?? []), () => setTasks([]));
      api.GET("/v1/onboarding/settings").then(({ data }) => setInvite(data ? data.invite_to_fyniq : null), () => setInvite(null));
    }
    if (hrRight) {
      api.GET("/v1/offboardings").then(({ data }) => setLeavings(data?.leavings ?? []), () => setLeavings([]));
      api.GET("/v1/people").then(({ data }) => setPeople(data?.people ?? []), () => setPeople([]));
    }
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) load();
  };
  const decide = async (id: string, state: "done" | "dismissed") => done((await api.POST("/v1/onboarding/tasks/{id}/decision", { params: { path: { id } }, body: { state } })).error, t("onb.decided"));
  const toggle = async () => done((await api.PUT("/v1/onboarding/settings", { body: { invite_to_fyniq: !invite } })).error, t("onb.saved"));
  const set = (k: keyof typeof blank) => (e: Change) => setF({ ...f, [k]: e.target.value });
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    const body = { kind: f.kind, last_day: f.last, notice_given_on: f.notice || undefined, retrenchment_notified_on: f.kind === "retrenchment" ? f.retrench || undefined : undefined, equipment_returned_on: f.equipment || undefined, exit_interview_on: f.exit || undefined, fyniq_access_removed_on: f.fyniq || undefined };
    await done((await api.PUT("/v1/people/{id}/offboarding", { params: { path: { id: f.person || people[0]?.id || "" } }, body })).error, t("onb.leavingSaved"));
  };
  const withdraw = async (id: string) => done((await api.POST("/v1/people/{id}/offboarding/cancellation", { params: { path: { id } } })).error, t("onb.withdrawn"));
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-onboarding" />
        <h1 className="fy-h1" title={helpText("onb.page").tooltip}>{t("onb.title")}</h1>
        <p className="fy-secondary">{t("onb.lead")}</p>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {inviter && (
          <section className="fy-stack fy-stack--2" aria-labelledby="inv-h">
            <h2 className="fy-h2" id="inv-h">{t("onb.invite")}</h2>
            <p className="fy-secondary">{t("onb.inviteLead")}</p>
            {settingsRight && invite !== null && <button className="fy-btn fy-btn--secondary" type="button" onClick={toggle} aria-pressed={invite}>{t(invite ? "onb.inviteOff" : "onb.inviteOn")}</button>}
            {open.length === 0 && <p className="fy-secondary">{t("onb.noTasks")}</p>}
            <ul className="fy-stack fy-stack--2">
              {open.map((x) => (
                <li key={x.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
                  <span>{t("onb.task", { name: x.person, start: x.start_date })}</span>
                  <button className="fy-btn fy-btn--primary" type="button" onClick={() => decide(x.id, "done")}>{t("onb.done", { name: x.person })}</button>
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(x.id, "dismissed")}>{t("onb.dismiss", { name: x.person })}</button>
                </li>
              ))}
            </ul>
          </section>
        )}
        {hrRight && (
          <section className="fy-stack fy-stack--2" aria-labelledby="leave-h">
            <h2 className="fy-h2" id="leave-h">{t("onb.leaving")}</h2>
            {leavings.length === 0 && <p className="fy-secondary">{t("onb.noLeavings")}</p>}
            <ul className="fy-stack fy-stack--3">
              {leavings.map((l) => (
                <li key={l.id} className="fy-stack fy-stack--1">
                  <span className="fy-strong">{t("onb.leaver", { name: l.person, kind: t(`onb.kind.${l.kind}`), last: l.last_day })}</span>
                  <span className="fy-secondary">{t("onb.dates", { notice: l.notice_given_on ?? "–", told: l.retrenchment_notified_on ?? "–" })}</span>
                  <span className="fy-secondary">{t("onb.checklist", { equipment: l.equipment_returned_on ?? "–", exit: l.exit_interview_on ?? "–", fyniq: l.fyniq_access_removed_on ?? "–" })}</span>
                  <span className="fy-secondary">{t("onb.access", { date: l.nemura_access_narrows_on })}</span>
                  <span className="fy-secondary">{t("onb.serviceLetter", { state: t(`onb.letter.${l.service_letter}`) })}</span>
                  {l.leave_settlement && <span>{t("onb.settlement", { year: String(l.leave_settlement.year), entitled: l.leave_settlement.entitled, taken: l.leave_settlement.taken, untaken: l.leave_settlement.untaken })}</span>}
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => withdraw(l.person_id)}>{t("onb.withdraw", { name: l.person })}</button>
                </li>
              ))}
            </ul>
            <form className="fy-stack fy-stack--2" onSubmit={save} aria-label={t("onb.record")}>
              <h3 className="fy-h3">{t("onb.record")}</h3>
              <label className="fy-field">
                <span>{t("onb.person")}</span>
                <select className="fy-input" value={f.person || people[0]?.id} onChange={set("person")}>
                  {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </label>
              <label className="fy-field">
                <span>{t("onb.kindLabel")}</span>
                <select className="fy-input" value={f.kind} onChange={set("kind")}>
                  {kinds.map((k) => <option key={k} value={k}>{t(`onb.kind.${k}`)}</option>)}
                </select>
              </label>
              <label className="fy-field"><span>{t("onb.notice")}</span><input className="fy-input" type="date" value={f.notice} onChange={set("notice")} /></label>
              <label className="fy-field"><span>{t("onb.last")}</span><input className="fy-input" type="date" required value={f.last} onChange={set("last")} /></label>
              {f.kind === "retrenchment" && <label className="fy-field"><span>{t("onb.retrench")}</span><input className="fy-input" type="date" value={f.retrench} onChange={set("retrench")} /></label>}
              <label className="fy-field"><span>{t("onb.equipment")}</span><input className="fy-input" type="date" value={f.equipment} onChange={set("equipment")} /></label>
              <label className="fy-field"><span>{t("onb.exit")}</span><input className="fy-input" type="date" value={f.exit} onChange={set("exit")} /></label>
              <label className="fy-field"><span>{t("onb.fyniqRemoved")}</span><input className="fy-input" type="date" value={f.fyniq} onChange={set("fyniq")} /></label>
              <button className="fy-btn fy-btn--primary" type="submit">{t("onb.save")}</button>
            </form>
          </section>
        )}
      </main>
    </Shell>
  );
}
