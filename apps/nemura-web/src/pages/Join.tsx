import { useEffect, useState } from "react";
import { api, type SignInInfo } from "../api/client";
import { t } from "../i18n";

// Join is how a person takes a place: with the deployment's one-time setup code (the first organisation, whose Owner they become) or an
// invitation's code. The form posts to the API, which keeps the code in an HttpOnly cookie, never a URL, and uses it once with the account
// the person signs in with. The code field is a password input so the screen does not show it.
export function Join({ kind }: { kind: "setup" | "invitation" }) {
  const [info, set] = useState<SignInInfo | null>(null);
  useEffect(() => {
    let live = true;
    api.GET("/v1/sign-in").then(({ data }) => live && set(data ?? null), () => undefined);
    return () => {
      live = false;
    };
  }, []);
  const providers = info?.providers ?? [];
  return (
    <main className="nm-signin__main" id="main">
      <div className="nm-signin__card">
        <span className="fy-brand">
          <span className="fy-brand__mark" aria-hidden="true">n</span>
          <span>{t("app.name").toLowerCase()}</span>
        </span>
        <div className="fy-stack fy-stack--2">
          <h1 className="fy-h1">{t(`join.title.${kind}`)}</h1>
          <p className="fy-secondary">{t(`join.lead.${kind}`)}</p>
        </div>
        {info && providers.length === 0 ? (
          <p role="status" className="fy-secondary">{t("join.noProvider")}</p>
        ) : (
          <form className="fy-stack fy-stack--3" method="post" action="/api/auth/join">
            <input type="hidden" name="kind" value={kind} />
            <input type="hidden" name="return_to" value="/" />
            <div className="fy-field">
              <label className="fy-label" htmlFor="join-provider">{t("join.provider")}</label>
              <select className="fy-input" id="join-provider" name="provider" required>
                {providers.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="join-code">{t(`join.code.${kind}`)}</label>
              <input className="fy-input" id="join-code" name="code" type="password" autoComplete="off" required maxLength={200} />
            </div>
            {kind === "setup" && (
              <>
                <div className="fy-field">
                  <label className="fy-label" htmlFor="join-org">{t("join.organisation")}</label>
                  <input className="fy-input" id="join-org" name="organisation" type="text" autoComplete="organization" required maxLength={200} />
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor="join-name">{t("join.name")}</label>
                  <input className="fy-input" id="join-name" name="name" type="text" autoComplete="name" required maxLength={200} />
                </div>
              </>
            )}
            <p className="fy-small fy-muted">{t("join.help")}</p>
            <button className="fy-btn fy-btn--primary fy-btn--lg fy-btn--block" type="submit">{t("join.submit")}</button>
          </form>
        )}
        <a className="fy-small" href="/sign-in">{t("join.back")}</a>
      </div>
    </main>
  );
}
