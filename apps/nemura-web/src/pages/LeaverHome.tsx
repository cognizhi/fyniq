import { useEffect, useState } from "react";
import { api, type Session } from "../api/client";
import type { components } from "../api/schema";
import { LetterList } from "../components/LetterList";
import { RequestSteps } from "../components/RequestSteps";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";

type Doc = components["schemas"]["Document"];

// LeaverHome is nm-leaver-home: after a person's last day their account keeps their own documents and letters, and a way to ask the people team for a copy of one that is lost,
// until the record-keeping period ends (NM-ESS-06). Nothing else is here, and the API refuses everything else.
export function LeaverHome({ session }: { session: Session }) {
  const [docs, setDocs] = useState<Doc[]>([]);
  const [q, setQ] = useState("");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const person = session.person_id ?? "";
  useEffect(() => {
    if (!person) return;
    api.GET("/v1/people/{id}/documents", { params: { path: { id: person } } }).then(
      ({ data }) => setDocs(data?.documents ?? []),
      () => setDocs([]),
    );
  }, [person]);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/questions", { body: { question: q } });
    if (data) setQ("");
    setNote(data ? { ok: true, text: t("leaver.sent", { due: data.due_on }) } : { ok: false, text: (error as { detail?: string } | undefined)?.detail ?? t("ask.failed") });
  };
  return (
    <div className="nm-shell">
      <header className="nm-shell__bar">
        <span className="fy-brand"><span className="fy-brand__mark" aria-hidden="true">n</span><span>{t("app.name").toLowerCase()}</span></span>
        <form method="post" action="/api/auth/signout"><button className="fy-btn fy-btn--secondary" type="submit">{t("nav.signOut")}</button></form>
      </header>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-leaver-home" />
        <h1 className="fy-h1" title={helpText("leaver.page").tooltip}>{t("leaver.title")}</h1>
        <p className="fy-secondary">{t("leaver.lead", { until: session.leaver_until ?? "" })}</p>
        <section className="fy-stack fy-stack--2" aria-labelledby="docs-h">
          <h2 className="fy-h2" id="docs-h">{t("leaver.documents")}</h2>
          {docs.length === 0 && <p className="fy-secondary">{t("leaver.noDocuments")}</p>}
          <ul className="fy-stack fy-stack--1">
            {docs.map((d) => (
              <li key={d.id}><a href={`/api/v1/people/${person}/documents/${d.id}`}>{d.title}</a></li>
            ))}
          </ul>
        </section>
        <LetterList mine roles={[]} />
        <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("leaver.ask")}>
          <h2 className="fy-h2">{t("leaver.ask")}</h2>
          <p className="fy-secondary">{t("leaver.askLead")}</p>
          <label className="fy-field"><span>{t("ask.question")}</span><textarea className="fy-input" required rows={3} maxLength={1000} value={q} onChange={(e) => setQ(e.target.value)} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("ask.send")}</button>
        </form>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        <RequestSteps />
      </main>
    </div>
  );
}
