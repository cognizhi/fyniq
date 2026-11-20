import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Mine = components["schemas"]["GrievanceMine"];
type Entry = components["schemas"]["DirectoryEntry"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("concerns.failed");

// Concerns is nm-my-concerns (NM-SEC-06): a member raises a concern in confidence, optionally naming the people it is about, and reads who handles it and its written outcome. It never shows the inquiry record, and the
// people it is about never see it. It is routed to an HR administrator or the Owner who is not involved, or waits when nobody is.
export function Concerns({ roles }: { roles: readonly string[] }) {
  const [mine, setMine] = useState<Mine[]>([]);
  const [people, setPeople] = useState<Entry[]>([]);
  const [summary, setSummary] = useState("");
  const [about, setAbout] = useState<string[]>([]);
  const [note, setNote] = useState<Note>(null);
  const load = () => {
    api.GET("/v1/me/grievances").then(({ data }) => setMine(data?.items ?? []), () => setMine([]));
  };
  useEffect(() => {
    load();
    api.GET("/v1/people").then(({ data }) => setPeople(data?.people ?? []), () => setPeople([]));
  }, []);
  const file = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const { data, error } = await api.POST("/v1/me/grievances", { body: { summary, about: about.length ? about : undefined } });
    if (error || !data) return setNote({ ok: false, text: problem(error) });
    setSummary("");
    setAbout([]);
    setNote({ ok: true, text: data.state === "open" ? t("concerns.filedOpen", { handler: data.handler ?? "" }) : t("concerns.filedWaiting") });
    load();
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <h1 className="fy-h1" title={helpText("concerns.page").tooltip}>{t("concerns.title")}</h1>
        <p className="fy-secondary">{t("concerns.lead")}</p>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        <form className="fy-stack fy-stack--2" onSubmit={file} aria-labelledby="file-h">
          <h2 className="fy-h2" id="file-h">{t("concerns.raise")}</h2>
          <label className="fy-field">
            <span>{t("concerns.summary")}</span>
            <textarea className="fy-input" required minLength={20} maxLength={4000} value={summary} onChange={(e) => setSummary(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("concerns.about")}</span>
            <select className="fy-input" multiple size={5} value={about} onChange={(e) => setAbout(Array.from(e.target.selectedOptions, (o) => o.value).slice(0, 5))}>
              {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
            <span className="fy-small fy-secondary">{t("concerns.aboutHelp")}</span>
          </label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("concerns.send")}</button>
        </form>
        <section className="fy-stack fy-stack--2" aria-labelledby="mine-h">
          <h2 className="fy-h2" id="mine-h">{t("concerns.mine")}</h2>
          {mine.length === 0 ? <p className="fy-secondary">{t("concerns.none")}</p> : (
            <ul className="fy-stack fy-stack--2">
              {mine.map((g) => (
                <li key={g.id} className="fy-stack fy-stack--1">
                  <span><strong>{t(`concerns.state.${g.state}`)}</strong> {t("concerns.line", { filed: g.filed_at, handler: g.handler ?? "" })}</span>
                  {g.state === "decided" && <span>{t("concerns.outcome", { finding: t(`concerns.finding.${g.finding ?? "no_finding"}`), outcome: g.outcome ?? "" })}</span>}
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>
    </Shell>
  );
}
