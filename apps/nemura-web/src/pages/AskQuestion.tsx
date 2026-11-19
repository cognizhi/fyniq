import { useState } from "react";
import { api } from "../api/client";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

// AskQuestion is nm-ask: a person asks the people team a question in writing and follows the answer under My requests (NM-ESS-04).
export function AskQuestion({ roles }: { roles: readonly string[] }) {
  const [q, setQ] = useState("");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/questions", { body: { question: q } });
    if (data) setQ("");
    setNote(data ? { ok: true, text: t("ask.sent", { due: data.due_on }) } : { ok: false, text: (error as { detail?: string } | undefined)?.detail ?? t("ask.failed") });
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-ask" />
        <h1 className="fy-h1" title={helpText("ask.page").tooltip}>{t("ask.title")}</h1>
        <p className="fy-secondary">{t("ask.lead")}</p>
        <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("ask.title")}>
          <label className="fy-field"><span>{t("ask.question")}</span><textarea className="fy-input" required rows={4} maxLength={1000} value={q} onChange={(e) => setQ(e.target.value)} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("ask.send")}</button>
        </form>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        <a href="/me/requests">{t("letters.mine")}</a>
      </main>
    </Shell>
  );
}
