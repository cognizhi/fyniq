import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Settings = components["schemas"]["AISettings"];
type Proposal = components["schemas"]["AIProposal"];
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("assistant.failed");

// AssistantSettings is nm-assistant (NM-AI-01, NM-AI-04): what the people assistant may do for this organisation, off until the Owner turns it on, and the drafts it has proposed for a person to decide.
// The assistant drafts and explains; it never approves, issues, pays or changes a record, and this page lists that in words. The Owner saves the settings with a passkey; the people team decides the drafts.
export function AssistantSettings({ roles, member }: { roles: readonly string[]; member: string }) {
  const [s, setS] = useState<Settings | null>(null);
  const [list, setList] = useState<Proposal[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [route, setRoute] = useState("none");
  const [tasks, setTasks] = useState<string[]>([]);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const allowed = roles.some((r) => ["hr_officer", "hr_admin", "owner"].includes(r));
  const owner = roles.includes("owner");
  const load = () => {
    api.GET("/v1/assistant/settings").then(({ data }) => {
      setS(data ?? null);
      setEnabled(data?.enabled ?? false);
      setRoute(data?.route ?? "none");
      setTasks(data?.task_types ?? []);
    }, () => setS(null));
    api.GET("/v1/assistant/proposals", { params: { query: { state: "pending" } } }).then(({ data }) => setList(data?.proposals ?? []), () => setList([]));
  };
  useEffect(() => {
    if (allowed) load();
  }, [allowed]); // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    const { error } = await api.PUT("/v1/assistant/settings", { body: { enabled, route: route as "none", task_types: tasks } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("assistant.saved") });
    if (!error) load();
  };
  const decide = async (p: Proposal, approve: boolean) => {
    const { error } = await api.POST("/v1/assistant/proposals/{id}/decision", { params: { path: { id: p.id } }, body: { approve, reason: reasons[p.id] || undefined } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t(approve ? "assistant.approved" : "assistant.rejected") });
    if (!error) load();
  };
  const mine = (p: Proposal) => p.asker_id === member;
  const toggle = (id: string) => setTasks(tasks.includes(id) ? tasks.filter((x) => x !== id) : [...tasks, id]);
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-assistant" />
        <h1 className="fy-h1" title={helpText("assistant.page").tooltip}>{t("assistant.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("assistant.denied")}</p>}
        {allowed && s && (
          <>
            <p className="fy-secondary">{t("assistant.lead")}</p>
            <section className="fy-stack fy-stack--2" aria-labelledby="never-h">
              <h2 className="fy-h2" id="never-h">{t("assistant.never")}</h2>
              <p>{t("assistant.neverList")}</p>
            </section>
            <form className="fy-stack fy-stack--2" onSubmit={save} aria-label={t("assistant.settings")}>
              <h2 className="fy-h2">{t("assistant.settings")}</h2>
              {!owner && <p className="fy-secondary">{t("assistant.ownerOnly")}</p>}
              <label className="fy-field fy-field--check"><input type="checkbox" checked={enabled} disabled={!owner} onChange={(ev: Change) => setEnabled((ev.target as HTMLInputElement).checked)} /><span>{t("assistant.enabled")}</span></label>
              <label className="fy-field">
                <span>{t("assistant.route")}</span>
                <select className="fy-input" value={route} disabled={!owner} onChange={(ev: Change) => setRoute(ev.target.value)}>
                  <option value="none">{t("assistant.route.none")}</option>
                  <option value="scripted">{t("assistant.route.scripted")}</option>
                </select>
              </label>
              <fieldset className="fy-stack fy-stack--1">
                <legend>{t("assistant.tasks")}</legend>
                {(s.catalogue ?? []).map((c) => (
                  <label key={c.id} className="fy-field fy-field--check">
                    <input type="checkbox" checked={tasks.includes(c.id)} disabled={!owner} onChange={() => toggle(c.id)} />
                    <span>{t("assistant.task", { id: c.id, what: c.what, roles: (c.approver_roles ?? []).join(", "), hours: String(c.expiry_hours) })}</span>
                  </label>
                ))}
              </fieldset>
              <p className="fy-secondary">{s.own_pay_note}</p>
              {owner && <button className="fy-btn fy-btn--primary" type="submit">{t("assistant.save")}</button>}
            </form>
            <section className="fy-stack fy-stack--2" aria-labelledby="queue-h">
              <h2 className="fy-h2" id="queue-h">{t("assistant.queue")}</h2>
              {list.length === 0 && <p className="fy-secondary">{t("assistant.none")}</p>}
              <ul className="fy-stack fy-stack--2">
                {list.map((p) => (
                  <li key={p.id} className="fy-stack fy-stack--1">
                    <span className="fy-chip fy-chip--ai">{t("assistant.ai")}</span>
                    <p>{t("assistant.proposal", { what: p.what, asker: p.asker, expires: p.expires_at.slice(0, 16).replace("T", " ") })}</p>
                    <pre className="fy-pre">{JSON.stringify(p.payload, null, 2)}</pre>
                    {p.expired && <p role="status" className="fy-secondary">{t("assistant.expired")}</p>}
                    {!p.expired && mine(p) && <p className="fy-secondary">{t("assistant.yours")}</p>}
                    {!p.expired && !mine(p) && (
                      <div className="fy-row" style={{ gap: "var(--space-3)" }}>
                        <label className="fy-field"><span>{t("assistant.reason")}</span><input className="fy-input" value={reasons[p.id] ?? ""} onChange={(ev: Change) => setReasons({ ...reasons, [p.id]: ev.target.value })} /></label>
                        <button className="fy-btn fy-btn--primary" type="button" onClick={() => decide(p, true)}>{t("assistant.approve")}</button>
                        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(p, false)}>{t("assistant.reject")}</button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          </>
        )}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      </main>
    </Shell>
  );
}
