import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Told = components["schemas"]["SickNotified"];
type Notice = components["schemas"]["SickNotification"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("sick.failed");

// base64 of a file's bytes, for the API.
function toBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(",")[1] ?? "");
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

// ReportSick is nm-report-sick: a person tells the company they are sick (no approval), sees how many days were paid and how many were beyond the limit, and gives
// the medical certificate when they are back (NM-LV-06, NM-LV-12). The API counts the days and applies the ceiling; nothing here does.
export function ReportSick({ roles }: { roles: readonly string[] }) {
  const [kind, setKind] = useState("outpatient");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [note, setNote] = useState("");
  const [told, setTold] = useState<Told | null>(null);
  const [message, setMessage] = useState<Note>(null);
  const [mine, setMine] = useState<Notice[]>([]);
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/sick-leave");
      setMine(data?.notifications ?? []);
    } catch {
      setMine([]);
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/sick-leave", { body: { kind: kind as "outpatient" | "hospitalisation", from, to: to || from, note } });
    setTold(data ?? null);
    setMessage(data ? null : { ok: false, text: problem(error) });
    if (data) await load();
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-report-sick" />
        <h1 className="fy-h1" title={helpText("sick.page").tooltip}>{t("sick.title")}</h1>
        <p className="fy-secondary">{t("sick.lead")}</p>
        <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("sick.title")}>
          <label className="fy-field">
            <span>{t("sick.kind")}</span>
            <select className="fy-input" value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="outpatient">{t("sick.kind.outpatient")}</option>
              <option value="hospitalisation">{t("sick.kind.hospitalisation")}</option>
            </select>
          </label>
          <label className="fy-field">
            <span>{t("sick.from")}</span>
            <input className="fy-input" type="date" required value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("sick.to")}</span>
            <input className="fy-input" type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("sick.note")}</span>
            <textarea className="fy-input" maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} />
          </label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("sick.send")}</button>
        </form>
        {told && (
          <p role="status" className="fy-secondary">
            {t("sick.told", { days: told.days, paid: told.paid_days, unpaid: told.unpaid_days })} {told.late && t("sick.late")}
          </p>
        )}
        {message && <p role="alert" className="fy-secondary">{message.text}</p>}
        <section className="fy-stack fy-stack--2" aria-labelledby="mine-h">
          <h2 className="fy-h2" id="mine-h">{t("sick.mine")}</h2>
          {mine.length === 0 ? <p className="fy-secondary">{t("sick.none")}</p> : mine.map((n) => <Certificate key={n.id} n={n} saved={load} />)}
        </section>
        <a href="/me/leave">{t("sick.back")}</a>
      </main>
    </Shell>
  );
}

function Certificate({ n, saved }: { n: Notice; saved: () => unknown }) {
  const [issuer, setIssuer] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [message, setMessage] = useState<Note>(null);
  const give = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    try {
      const { error } = await api.POST("/v1/sick-leave/{id}/certificate", { params: { path: { id: n.id } }, body: { issuer, content_type: file.type || "application/octet-stream", content_base64: await toBase64(file) } });
      setMessage(error ? { ok: false, text: problem(error) } : { ok: true, text: t("sick.saved") });
      if (!error) await saved();
    } catch {
      setMessage({ ok: false, text: t("sick.failed") });
    }
  };
  return (
    <div className="fy-stack fy-stack--1">
      <p className="fy-strong">{t("sick.line", { from: n.from, to: n.to, days: n.days, paid: n.paid_days })}</p>
      {n.has_certificate ? (
        <p className="fy-secondary">{t("sick.given", { issuer: n.issuer ?? "" })} {t(n.verified ? "sick.verified" : "sick.waiting")}</p>
      ) : (
        <form className="fy-stack fy-stack--1" onSubmit={give} aria-label={`${t("sick.certificate")} ${n.from}`}>
          <label className="fy-field">
            <span>{t("sick.issuer")}</span>
            <input className="fy-input" required value={issuer} maxLength={200} onChange={(e) => setIssuer(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("sick.file")}</span>
            <input className="fy-input" type="file" required accept="image/*,application/pdf" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </label>
          <button className="fy-btn fy-btn--secondary" type="submit">{t("sick.give")}</button>
        </form>
      )}
      {message && <p role={message.ok ? "status" : "alert"} className="fy-secondary">{message.text}</p>}
    </div>
  );
}
