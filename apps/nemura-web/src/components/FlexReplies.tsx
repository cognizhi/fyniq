import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Flex = components["schemas"]["FlexRequest"];
const outcomes = ["accepted", "rejected", "alternative"] as const;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("team.failed");

// FlexReplies lists the flexible work requests waiting for the employer's written reply, soonest deadline first, and takes the reply (NM-LV-10). The reply deadline is the
// API's; a refusal or an alternative must give its reason, which the API enforces.
export function FlexReplies() {
  const [waiting, setWaiting] = useState<Flex[]>([]);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [outcome, setOutcome] = useState<Record<string, (typeof outcomes)[number]>>({});
  const [reply, setReply] = useState<Record<string, string>>({});
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/flexible-work-requests");
      const open = (data?.requests ?? []).filter((r) => r.state === "received");
      setWaiting(open);
      const found: Record<string, string> = {};
      for (const r of open) {
        const got = await api.GET("/v1/assistant/drafts", { params: { query: { request_id: r.id } } }).catch(() => undefined);
        const text = got?.data?.drafts?.[0]?.payload?.reply;
        if (typeof text === "string") found[r.id] = text;
      }
      setDrafts(found);
    } catch {
      setWaiting([]);
    }
  };
  const ask = async (r: Flex) => {
    const { error } = await api.POST("/v1/assistant/drafts", { body: { task_type: "people.draft_fwa_reply", request_id: r.id } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("flexteam.drafted") });
  };
  useEffect(() => {
    void load();
  }, []);
  const send = async (r: Flex) => {
    const { error } = await api.POST("/v1/flexible-work-requests/{id}/reply", { params: { path: { id: r.id } }, body: { outcome: outcome[r.id] ?? "accepted", written_reply: reply[r.id] ?? "" } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("flexteam.sent") });
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="flex-h">
      <h2 className="fy-h2" id="flex-h">{t("flexteam.title")}</h2>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {waiting.length === 0 ? (
        <p className="fy-secondary">{t("flexteam.none")}</p>
      ) : (
        <ul className="fy-stack fy-stack--2">
          {waiting.map((r) => (
            <li key={r.id} className="fy-stack fy-stack--1">
              <strong>{r.person_name}</strong>
              <span>{t("flexteam.line", { arrangement: t(`requests.arrangement.${r.arrangement}`), from: r.from, reason: r.reason })}</span>
              <span className="fy-secondary">{t("flexteam.col.due")}: {r.reply_due}</span>
              <label className="fy-field">
                <span>{t("flexteam.outcome")}</span>
                <select className="fy-input" value={outcome[r.id] ?? "accepted"} onChange={(e) => setOutcome({ ...outcome, [r.id]: e.target.value as (typeof outcomes)[number] })}>
                  {outcomes.map((o) => (
                    <option key={o} value={o}>{t(`requests.outcome.${o}`)}</option>
                  ))}
                </select>
              </label>
              <label className="fy-field">
                <span>{t("flexteam.replyFor", { name: r.person_name })}</span>
                <textarea className="fy-input" maxLength={4000} value={reply[r.id] ?? ""} onChange={(e) => setReply({ ...reply, [r.id]: e.target.value })} />
              </label>
              <div className="fy-row" style={{ gap: "var(--space-3)" }}>
                <button className="fy-btn fy-btn--secondary" type="button" onClick={() => ask(r)}>{t("flexteam.draft", { name: r.person_name })}</button>
                {drafts[r.id] !== undefined && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setReply({ ...reply, [r.id]: drafts[r.id] ?? "" })}>{t("flexteam.useDraft", { name: r.person_name })}</button>}
                <button className="fy-btn fy-btn--primary" type="button" onClick={() => send(r)}>{t("flexteam.send")}</button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
