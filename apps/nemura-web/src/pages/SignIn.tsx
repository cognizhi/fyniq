import { LogIn, Mail, TreePalm, Wallet } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type SignInInfo } from "../api/client";
import { helpText, t, textKeys } from "../i18n";

type State = { status: "loading" } | { status: "failed" } | { status: "ready"; info: SignInInfo };

// signInURL is the browser navigation that starts a provider's sign-in on the API: a path on this origin comes back (the API checks it).
export const signInURL = (provider: string, returnTo: string): string => `/api/auth/signin?provider=${encodeURIComponent(provider)}&return_to=${encodeURIComponent(returnTo)}`;

// SignIn is nm-sign-in: standalone, the providers the API offers, never the review banner (NM-UX-06). The "linked to Fyniq" state arrives
// with the link, and a passkey confirms a sensitive change for a signed-in person (it is not a way to sign in).
export function SignIn({ returnTo = "/" }: { returnTo?: string }) {
  const [state, set] = useState<State>({ status: "loading" });
  const [attempt, again] = useState(0);
  // The API sends a refused sign-in back here with its code (internal/nemura/auth): say it in plain words, or nothing for a code we do not know.
  const code = new URLSearchParams(window.location.search).get("signin_error") ?? "";
  const error = textKeys.includes(`signin.error.${code}`) ? t(`signin.error.${code}`) : "";
  useEffect(() => {
    let live = true;
    api.GET("/v1/sign-in").then(
      ({ data }) => live && set(data ? { status: "ready", info: data } : { status: "failed" }),
      () => live && set({ status: "failed" }),
    );
    return () => {
      live = false;
    };
  }, [attempt]);
  return (
    <div className="nm-signin">
      <aside className="nm-signin__art" aria-label={t("signin.art.label")}>
        <span className="fy-brand">
          <span className="fy-brand__mark" aria-hidden="true">n</span>
          <span>{t("app.name").toLowerCase()}</span>
        </span>
        <p className="nm-signin__big">{t("signin.art.lead")}</p>
        <ul className="fy-stack fy-stack--3">
          {[
            [TreePalm, "signin.art.leave"],
            [Wallet, "signin.art.pay"],
            [Mail, "signin.art.letters"],
          ].map(([Icon, key]) => {
            const I = Icon as typeof Mail;
            return (
              <li key={key as string} className="fy-row" style={{ gap: "var(--space-3)" }}>
                <span className="nm-tile__icon"><I size={20} aria-hidden="true" /></span>
                <span className="fy-strong">{t(key as string)}</span>
              </li>
            );
          })}
        </ul>
      </aside>
      <main className="nm-signin__main" id="main">
        <div className="nm-signin__card">
          <span className="fy-brand">
            <span className="fy-brand__mark" aria-hidden="true">n</span>
            <span>{t("app.name").toLowerCase()}</span>
          </span>
          <div className="fy-stack fy-stack--2">
            <h1 className="fy-h1" title={helpText("signin.page").tooltip}>{t("signin.title")}</h1>
            <p className="fy-secondary">{t("signin.lead")}</p>
          </div>
          {error && <p role="alert" className="fy-secondary">{error}</p>}
          {state.status === "loading" && <p role="status" className="fy-secondary">{t("signin.loading")}</p>}
          {state.status === "failed" && (
            <div className="fy-stack fy-stack--3">
              <p role="alert" className="fy-secondary">{t("signin.failed")}</p>
              <button className="fy-btn fy-btn--secondary fy-btn--lg fy-btn--block" type="button" onClick={() => again(attempt + 1)}>{t("signin.retry")}</button>
            </div>
          )}
          {state.status === "ready" && !state.info.setup_done && <p role="status" className="fy-secondary">{t("signin.notSetUp")}</p>}
          {state.status === "ready" && state.info.setup_done && (state.info.providers ?? []).length === 0 && <p role="status" className="fy-secondary">{t("signin.none")}</p>}
          {state.status === "ready" && state.info.setup_done && (state.info.providers ?? []).length > 0 && (
            <div className="fy-stack fy-stack--3">
              {(state.info.providers ?? []).map((p) => (
                <a key={p.id} className="fy-btn fy-btn--secondary fy-btn--lg fy-btn--block" href={signInURL(p.id, returnTo)}>
                  <LogIn size={16} aria-hidden="true" />
                  {t("signin.continue", { provider: p.name })}
                </a>
              ))}
            </div>
          )}
          <p className="fy-small fy-muted">{t("signin.invite")}</p>
          <p className="fy-row fy-small" style={{ gap: "var(--space-4)" }}>
            <a href="/join?kind=invitation">{t("signin.haveInvitation")}</a>
            <a href="/join?kind=recovery">{t("signin.haveRecovery")}</a>
            {state.status === "ready" && !state.info.setup_done && <a href="/join?kind=setup">{t("signin.setup")}</a>}
          </p>
        </div>
      </main>
    </div>
  );
}
