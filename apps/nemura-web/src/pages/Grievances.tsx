import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";
import { Shell, team } from "./Shell";

type Row = components["schemas"]["GrievanceRow"];
type Detail = components["schemas"]["GrievanceDetail"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("grievances.failed");
const kinds = ["note", "interview", "finding"] as const;
const findings = ["substantiated", "partly_substantiated", "not_substantiated", "no_finding"] as const;

// Grievances is nm-grievances (NM-SEC-06): the grievances an HR administrator or the Owner handles, the waiting ones they may take, the inquiry record, the handover and the written outcome. A grievance the caller is
// involved in, or is not entitled to, is not shown: the database answers not found. Reading one is audited.
export function Grievances({ roles }: { roles: readonly string[] }) {
  const [assigned, setAssigned] = useState<Row[]>([]);
  const [waiting, setWaiting] = useState<Row[]>([]);
  const [open, setOpen] = useState<Detail | null>(null);
  const [note, setNote] = useState<Note>(null);
  const [kind, setKind] = useState<(typeof kinds)[number]>("note");
  const [text, setText] = useState("");
  const [finding, setFinding] = useState<(typeof findings)[number]>("not_substantiated");
  const [outcome, setOutcome] = useState("");
  const [reason, setReason] = useState("");
  const allowed = roles.some((r) => r === "owner" || r === "hr_admin");
  const load = () => {
    if (allowed) api.GET("/v1/grievances").then(({ data }) => (setAssigned(data?.assigned ?? []), setWaiting(data?.unrouted ?? [])), () => setAssigned([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const read = async (id: string) => {
    const { data, error } = await api.GET("/v1/grievances/{id}", { params: { path: { id } } });
    if (data) setOpen(data);
    else setNote({ ok: false, text: problem(error) });
  };
  const done = async (error: unknown, ok: string, id: string, closes = false) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (error) return;
    setText("");
    setOutcome("");
    setReason("");
    load();
    if (closes) setOpen(null);
    else await read(id);
  };
  return (
    <Shell roles={roles} directory={team(roles)}>
      <main className="nm-shell__main" id="main">
        <h1 className="fy-h1" title={helpText("grievances.page").tooltip}>{t("grievances.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("grievances.denied")}</p>}
        {allowed && (
          <>
            <p className="fy-secondary">{t("grievances.lead")}</p>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            <section className="fy-stack fy-stack--2" aria-labelledby="assigned-h">
              <h2 className="fy-h2" id="assigned-h">{t("grievances.assigned")}</h2>
              {assigned.length === 0 ? <p className="fy-secondary">{t("grievances.none")}</p> : (
                <ul className="fy-stack fy-stack--1">
                  {assigned.map((g) => (
                    <li key={g.id} className="fy-row" style={{ gap: "var(--space-2)" }}>
                      <span>{t("grievances.line", { filed: g.filed_at, state: t(`concerns.state.${g.state}`) })}</span>
                      <button className="fy-btn fy-btn--secondary" type="button" onClick={() => read(g.id)}>{t("grievances.open")}</button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            {waiting.length > 0 && (
              <section className="fy-stack fy-stack--2" aria-labelledby="waiting-h">
                <h2 className="fy-h2" id="waiting-h">{t("grievances.waiting")}</h2>
                <p className="fy-secondary">{t("grievances.waitingLead")}</p>
                <ul className="fy-stack fy-stack--1">
                  {waiting.map((g) => (
                    <li key={g.id} className="fy-row" style={{ gap: "var(--space-2)" }}>
                      <span>{t("grievances.line", { filed: g.filed_at, state: t("concerns.state.unrouted") })}</span>
                      <button className="fy-btn fy-btn--primary" type="button" onClick={async () => done((await api.POST("/v1/grievances/{id}/taking", { params: { path: { id: g.id } } })).error, t("grievances.taken"), g.id)}>{t("grievances.take")}</button>
                    </li>
                  ))}
                </ul>
              </section>
            )}
            {open && (
              <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby="detail-h">
                <h2 className="fy-h2" id="detail-h">{t("grievances.detail", { filed: open.filed_at })}</h2>
                <p><strong>{t("grievances.complainant")}</strong> {open.complainant}</p>
                <p><strong>{t("grievances.about")}</strong> {open.about.length === 0 ? t("grievances.aboutNone") : open.about.map((a) => a.name).join(", ")}</p>
                <p>{open.summary}</p>
                <h3 className="fy-h3">{t("grievances.record")}</h3>
                <ul className="fy-stack fy-stack--1">
                  {open.entries.map((e) => <li key={e.id}>{t("grievances.entry", { kind: t(`grievances.kind.${e.kind}`), by: e.by, at: e.at, body: e.body })}</li>)}
                </ul>
                {open.state === "open" && (
                  <>
                    <form className="fy-stack fy-stack--2" aria-labelledby="entry-h" onSubmit={async (ev) => { ev.preventDefault(); await done((await api.POST("/v1/grievances/{id}/entries", { params: { path: { id: open.id } }, body: { kind, note: text } })).error, t("grievances.recorded"), open.id); }}>
                      <h3 className="fy-h3" id="entry-h">{t("grievances.addEntry")}</h3>
                      <label className="fy-field"><span>{t("grievances.kindLabel")}</span>
                        <select className="fy-input" value={kind} onChange={(e) => setKind(e.target.value as (typeof kinds)[number])}>{kinds.map((k) => <option key={k} value={k}>{t(`grievances.kind.${k}`)}</option>)}</select>
                      </label>
                      <label className="fy-field"><span>{t("grievances.text")}</span><textarea className="fy-input" required maxLength={4000} value={text} onChange={(e) => setText(e.target.value)} /></label>
                      <button className="fy-btn fy-btn--secondary" type="submit">{t("grievances.record.button")}</button>
                    </form>
                    <form className="fy-stack fy-stack--2" aria-labelledby="outcome-h" onSubmit={async (ev) => { ev.preventDefault(); await done((await api.POST("/v1/grievances/{id}/outcome", { params: { path: { id: open.id } }, body: { finding, outcome } })).error, t("grievances.decided"), open.id, true); }}>
                      <h3 className="fy-h3" id="outcome-h">{t("grievances.writeOutcome")}</h3>
                      <label className="fy-field"><span>{t("grievances.findingLabel")}</span>
                        <select className="fy-input" value={finding} onChange={(e) => setFinding(e.target.value as (typeof findings)[number])}>{findings.map((f) => <option key={f} value={f}>{t(`concerns.finding.${f}`)}</option>)}</select>
                      </label>
                      <label className="fy-field"><span>{t("grievances.outcomeLabel")}</span><textarea className="fy-input" required minLength={20} maxLength={4000} value={outcome} onChange={(e) => setOutcome(e.target.value)} /></label>
                      <button className="fy-btn fy-btn--primary" type="submit">{t("grievances.decide")}</button>
                    </form>
                    <form className="fy-stack fy-stack--2" aria-labelledby="handover-h" onSubmit={async (ev) => { ev.preventDefault(); await done((await api.POST("/v1/grievances/{id}/handover", { params: { path: { id: open.id } }, body: { reason } })).error, t("grievances.passed"), open.id, true); }}>
                      <h3 className="fy-h3" id="handover-h">{t("grievances.handover")}</h3>
                      <label className="fy-field"><span>{t("grievances.reasonLabel")}</span><input className="fy-input" required maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
                      <button className="fy-btn fy-btn--secondary" type="submit">{t("grievances.pass")}</button>
                    </form>
                  </>
                )}
                {open.state === "decided" && <p><strong>{t(`concerns.finding.${open.finding ?? "no_finding"}`)}</strong> {open.outcome}</p>}
              </section>
            )}
          </>
        )}
      </main>
    </Shell>
  );
}
