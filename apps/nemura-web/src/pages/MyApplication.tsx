import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type View = components["schemas"]["CandidateOwnView"];

// MyApplication is nm-my-application (NM-REC-05): a candidate, with no sign-in, opens the link the hiring team gave them and sees what they gave and where their application stands, never the team's notes or scores.
// The token is in the address after the #, so it is never sent in a request line or logged; it can also be pasted. The candidate may withdraw the consent to be kept for future roles. The page is not indexed.
export function MyApplication() {
  const [token, setToken] = useState(typeof window === "undefined" ? "" : window.location.hash.replace(/^#/, ""));
  const [view, setView] = useState<View | null>(null);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const look = async (tok: string) => {
    setError("");
    setDone("");
    const { data, error: failed } = await api.POST("/v1/candidate-data", { body: { token: tok.trim() } });
    if (failed) setError((failed as { detail?: string }).detail ?? t("myapp.failed"));
    else setView(data ?? null);
  };
  useEffect(() => {
    if (token.length >= 8) void look(token);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const withdraw = async () => {
    const { data, error: failed } = await api.POST("/v1/candidate-data/consent-withdrawal", { body: { token: token.trim() } });
    if (failed) return setError((failed as { detail?: string }).detail ?? t("myapp.failed"));
    setView(data ?? null);
    setError("");
    setDone(t("myapp.withdrawn"));
  };
  return (
    <main className="nm-signin__main" id="main">
      <meta name="robots" content="noindex" />
      <div className="nm-signin__card">
        <div className="fy-stack fy-stack--2">
          <h1 className="fy-h1" title={helpText("myapp.page").tooltip}>{t("myapp.title")}</h1>
          <p className="fy-secondary">{t("myapp.lead")}</p>
        </div>
        <form className="fy-stack fy-stack--2" aria-label={t("myapp.look")} onSubmit={(e) => { e.preventDefault(); void look(token); }}>
          <label className="fy-field"><span>{t("myapp.token")}</span><input className="fy-input" required minLength={8} maxLength={200} autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("myapp.look")}</button>
        </form>
        {error && <p role="alert">{error}</p>}
        {view && !view.found && <p role="alert">{t("myapp.none")}</p>}
        {done && <p role="status">{done}</p>}
        {view?.found && (
          <section className="fy-stack fy-stack--2" aria-label={t("myapp.gave")}>
            <p className="fy-strong">{t("myapp.role", { role: view.role ?? "" })}</p>
            <p>{t("myapp.stage", { stage: t(`pipeline.stage.${view.stage ?? "applied"}`), on: (view.stage_at ?? "").slice(0, 10) })}</p>
            {view.retention_until && <p className="fy-secondary">{t("myapp.deleted", { on: view.retention_until })}</p>}
            <h2 className="fy-h3">{t("myapp.gave")}</h2>
            <ul className="fy-stack fy-stack--1">
              <li>{t("myapp.name", { name: view.name ?? "" })}</li>
              <li>{t("myapp.email", { email: view.email ?? "" })}</li>
              <li>{t(`myapp.via.${view.applied_via ?? "careers_page"}`, { on: (view.applied_at ?? "").slice(0, 10) })}</li>
            </ul>
            <h3 className="fy-h3">{t("myapp.cv")}</h3>
            <p style={{ whiteSpace: "pre-wrap" }}>{view.cv ? view.cv : t("myapp.noCV")}</p>
            <p className="fy-secondary">{view.consent_future ? t("myapp.kept") : t("myapp.notKept")}</p>
            {view.consent_future && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => void withdraw()}>{t("myapp.withdraw")}</button>}
            <p className="fy-secondary">{t("myapp.linkUntil", { when: view.link_expires_at ?? "" })}</p>
          </section>
        )}
      </div>
    </main>
  );
}
