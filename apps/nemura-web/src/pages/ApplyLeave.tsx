import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Preview = components["schemas"]["RequestPreview"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("apply.failed");

// ApplyLeave is nm-apply-leave: the days, a note, and before the request is sent what it costs day by day, the balance after it and who in the team is away
// (NM-LV-03). The API counts the days; nothing here adds them up.
export function ApplyLeave({ roles }: { roles: readonly string[] }) {
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [halfStart, setHalfStart] = useState(false);
  const [halfEnd, setHalfEnd] = useState(false);
  const [note, setNote] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [message, setMessage] = useState<Note>(null);
  const [busy, setBusy] = useState(false);
  const body = { type: "annual", from, to: to || from, start_half: halfStart, end_half: halfEnd, note };
  useEffect(() => {
    setPreview(null);
    if (!from) return;
    let live = true;
    setBusy(true);
    api.POST("/v1/me/leave-requests/preview", { body }).then(
      ({ data, error }) => {
        if (!live) return;
        setBusy(false);
        setPreview(data ?? null);
        setMessage(data ? null : { ok: false, text: problem(error) });
      },
      () => live && (setBusy(false), setMessage({ ok: false, text: t("apply.failed") })),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, to, halfStart, halfEnd]);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/leave-requests", { body });
    setMessage(data ? { ok: true, text: t("apply.sent", { days: data.days }) } : { ok: false, text: problem(error) });
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-apply-leave" />
        <h1 className="fy-h1" title={helpText("apply.page").tooltip}>{t("apply.title")}</h1>
        <p className="fy-secondary">{t("apply.lead")}</p>
        <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("apply.title")}>
          <label className="fy-field">
            <span>{t("apply.type")}</span>
            <select className="fy-input" value="annual" onChange={() => undefined}>
              <option value="annual">{t("apply.type.annual")}</option>
            </select>
          </label>
          <label className="fy-field">
            <span>{t("apply.from")}</span>
            <input className="fy-input" type="date" required value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("apply.to")}</span>
            <input className="fy-input" type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} />
          </label>
          <label className="fy-row" style={{ gap: "var(--space-1)" }}>
            <input type="checkbox" checked={halfStart} onChange={(e) => setHalfStart(e.target.checked)} />
            <span>{t("apply.halfStart")}</span>
          </label>
          <label className="fy-row" style={{ gap: "var(--space-1)" }}>
            <input type="checkbox" checked={halfEnd} onChange={(e) => setHalfEnd(e.target.checked)} />
            <span>{t("apply.halfEnd")}</span>
          </label>
          <label className="fy-field">
            <span>{t("apply.note")}</span>
            <textarea className="fy-input" maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} />
          </label>
          {busy && <p role="status" className="fy-secondary">{t("apply.checking")}</p>}
          {preview && <Cost preview={preview} />}
          <button className="fy-btn fy-btn--primary" type="submit" disabled={!preview || !preview.enough}>{t("apply.send")}</button>
        </form>
        {message && <p role={message.ok ? "status" : "alert"} className="fy-secondary">{message.text}</p>}
        <a href="/me/leave">{t("apply.back")}</a>
      </main>
    </Shell>
  );
}

function Cost({ preview }: { preview: Preview }) {
  const counts = preview.days_counted ?? [];
  const away = preview.away ?? [];
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="cost-h">
      <h2 className="fy-h2" id="cost-h">{t("apply.counted")}</h2>
      <p className={preview.enough ? "fy-secondary" : "fy-secondary fy-negative"} role={preview.enough ? "status" : "alert"}>
        {preview.enough ? t("apply.cost", { days: preview.days, left: preview.left_after }) : t("apply.beyond", { left: preview.left_after })}
      </p>
      <table className="fy-table">
        <thead>
          <tr>
            <th scope="col">{t("apply.col.date")}</th>
            <th scope="col">{t("apply.col.cost")}</th>
            <th scope="col">{t("apply.col.why")}</th>
          </tr>
        </thead>
        <tbody>
          {counts.map((c) => (
            <tr key={c.date}>
              <td>{c.date}</td>
              <td className="fy-num">{c.cost}</td>
              <td>{c.why ?? ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {away.length > 0 && (
        <>
          <h3 className="fy-h3">{t("apply.away")}</h3>
          <ul className="fy-stack fy-stack--1">
            {away.map((w) => (
              <li key={`${w.name}-${w.from}`}>{t("apply.awayLine", { name: w.name, from: w.from, to: w.to })}</li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
