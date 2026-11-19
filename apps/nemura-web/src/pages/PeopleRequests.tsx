import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Row = components["schemas"]["QueueRow"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("queue.failed");

// PeopleRequests is nm-people-requests: what waits for the people team, soonest due first. A change of details is read with its evidence and approved or refused (never the
// person's own); a question is answered once. Approving an NRIC or FIN change does not hold the number: it is recorded afterwards under the person's restricted fields (NM-ESS-03,
// NM-ESS-04).
export function PeopleRequests({ roles }: { roles: readonly string[] }) {
  const [rows, setRows] = useState<Row[] | null>(null);
  const [text, setText] = useState<Record<string, string>>({});
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const load = () =>
    api.GET("/v1/requests/queue").then(
      ({ data }) => setRows(data?.queue ?? []),
      () => setRows([]),
    );
  useEffect(() => {
    void load();
  }, []);
  const done = async (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) await load();
  };
  const decide = async (id: string, approve: boolean) => done((await api.POST("/v1/details-changes/{id}/decision", { params: { path: { id } }, body: { approve, reason: text[id] ?? "" } })).error, t(approve ? "queue.approved" : "queue.refused"));
  const reply = async (id: string) => done((await api.POST("/v1/questions/{id}/reply", { params: { path: { id } }, body: { reply: text[id] ?? "" } })).error, t("queue.replied"));
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-people-requests" />
        <h1 className="fy-h1" title={helpText("queue.page").tooltip}>{t("queue.title")}</h1>
        <p className="fy-secondary">{t("queue.lead")}</p>
        {rows !== null && rows.length === 0 && <p className="fy-secondary">{t("queue.none")}</p>}
        <ul className="fy-stack fy-stack--3">
          {(rows ?? []).map((r) => (
            <li key={r.id} className="fy-stack fy-stack--1">
              <span className="fy-strong">{t(`steps.kind.${r.kind}`, { title: r.kind === "details_change" ? t(`steps.change.${r.change_kind ?? ""}`) : r.title })}: {r.person}</span>
              <span className="fy-secondary">{t("steps.due", { due: r.due_on })}</span>
              <span>{r.detail}</span>
              {r.kind === "details_change" && r.evidence_id && <a href={`/api/v1/people/${r.person_id}/documents/${r.evidence_id}`}>{t("queue.evidence")}</a>}
              {r.kind === "details_change" && r.change_kind === "nric_fin" && <span className="fy-secondary">{t("queue.nric")}</span>}
              <label className="fy-field">
                <span>{t(r.kind === "details_change" ? "queue.reason" : "queue.answer", { name: r.person })}</span>
                <input className="fy-input" maxLength={r.kind === "details_change" ? 500 : 2000} value={text[r.id] ?? ""} onChange={(e) => setText({ ...text, [r.id]: e.target.value })} />
              </label>
              {r.kind === "details_change" ? (
                <span className="fy-row" style={{ gap: "var(--space-3)" }}>
                  <button className="fy-btn fy-btn--primary" type="button" onClick={() => decide(r.id, true)}>{t("queue.approve", { name: r.person })}</button>
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(r.id, false)}>{t("queue.refuse", { name: r.person })}</button>
                </span>
              ) : (
                <button className="fy-btn fy-btn--primary" type="button" onClick={() => reply(r.id)}>{t("queue.reply", { name: r.person })}</button>
              )}
            </li>
          ))}
        </ul>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      </main>
    </Shell>
  );
}
