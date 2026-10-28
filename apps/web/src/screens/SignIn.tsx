import { CircleX, ListTree, LogOut, Mail, UserCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, type Session, type SignInProvider } from "../api/client";
import { go, safeReturn, signInURL } from "../route";
import { Brand, Icon, problemOf } from "../ui";
import { RecoveryCodes } from "./RecoveryCodes";

// What a sign-in refusal means, by its code (internal/signin), in plain words; the code is shown too (ENG-23).
export const signInErrors: Record<string, string> = {
  "AUTH-SIGNIN-01": "That sign-in provider is not available. Choose one of the ways to sign in below.",
  "AUTH-SIGNIN-02": "The sign-in took longer than 10 minutes, or began in another browser. Sign in again.",
  "AUTH-SIGNIN-03": "The provider's answer did not pass Fyniq's checks, so you are not signed in. Sign in again.",
  "AUTH-SIGNIN-04": "The sign-in service did not answer. Try again in a moment.",
};

type State =
  | { kind: "checking" }
  | { kind: "down" }
  | { kind: "signed-out"; providers: SignInProvider[] }
  | { kind: "no-account"; session: Session };

// SignIn is /sign-in (FR-ADM-08; docs/design/screens/src/SignIn.html): sign in with an enabled
// provider, and the invitation variant for a person signed in without a Fyniq account. The platform's
// own second step (passkey or TOTP, FR-ADM-02, SEC-01) arrives in P1.
export function SignIn() {
  const params = new URLSearchParams(window.location.search);
  const returnTo = safeReturn(params.get("return_to"));
  const error = params.get("signin_error");
  const [state, setState] = useState<State>({ kind: "checking" });
  const errorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const signal = new AbortController().signal;
    (async () => {
      const [{ data: setup }, { data: session, response }] = await Promise.all([
        api.GET("/v1/setup", { signal }),
        api.GET("/v1/session", { signal }),
      ]);
      if (setup && !setup.done) return go("/install");
      if (session?.user_id) return go(returnTo);
      if (session) return setState({ kind: "no-account", session });
      if (response.status !== 401) return setState({ kind: "down" });
      const { data: providers } = await api.GET("/v1/sign-in-providers", { signal });
      setState({ kind: "signed-out", providers: providers?.items ?? [] });
    })().catch(() => setState({ kind: "down" }));
  }, [returnTo]);

  useEffect(() => {
    if (error && state.kind !== "checking") errorRef.current?.focus();
  }, [error, state.kind]);

  return (
    <div className="fy-bare fy-bare--split fy-enter">
      <aside className="fy-bare__aside" aria-label="About Fyniq" style={{ justifyContent: "flex-start", gap: "120px" }}>
        <Brand />
        <div className="fy-stack fy-stack--6" style={{ maxWidth: "460px" }}>
          <p style={{ fontSize: "var(--fs-3xl)", fontWeight: 600, letterSpacing: "-0.03em", lineHeight: 1.1 }}>
            Intelligence behind every number
          </p>
          <p className="fy-lead">
            Accounting, compliance and planning for Singapore start-ups. Every figure opens to its source.
          </p>
          <ul className="fy-stack fy-stack--3">
            <li className="fy-row fy-row--top">
              <span className="fy-list__icon fy-list__icon--brand">
                <Icon of={ListTree} size={20} />
              </span>
              <span className="fy-stack fy-stack--2" style={{ gap: "2px" }}>
                <span className="fy-strong">Traceable figures</span>
                <span className="fy-secondary fy-small">Every number leads to the entries and documents behind it.</span>
              </span>
            </li>
            <li className="fy-row fy-row--top">
              <span className="fy-list__icon fy-list__icon--brand">
                <Icon of={UserCheck} size={20} />
              </span>
              <span className="fy-stack fy-stack--2" style={{ gap: "2px" }}>
                <span className="fy-strong">People decide</span>
                <span className="fy-secondary fy-small">Nothing posts until a named person approves it.</span>
              </span>
            </li>
          </ul>
        </div>
      </aside>

      <main className="fy-bare__main" style={{ gridTemplateColumns: "minmax(0, 1fr)" }}>
        <div className="fy-bare__card">
          <div className="fy-stack fy-stack--2 fy-until-expanded" style={{ alignItems: "center", textAlign: "center" }}>
            <Brand />
            <p className="fy-small fy-muted">Intelligence behind every number</p>
          </div>

          {error && state.kind !== "checking" && (
            <div className="fy-banner fy-banner--critical" role="alert" tabIndex={-1} ref={errorRef} aria-labelledby="signin-error-title">
              <Icon of={CircleX} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong" id="signin-error-title">
                  Sign-in failed ({error})
                </span>
                <span className="fy-small">{signInErrors[error] ?? "Sign in again."}</span>
              </div>
            </div>
          )}

          {state.kind === "checking" && (
            <p role="status" className="fy-secondary">
              Checking your sign-in…
            </p>
          )}
          {state.kind === "down" && (
            <p role="status" className="fy-secondary">
              Fyniq is not answering. Try again in a moment.
            </p>
          )}
          {state.kind === "signed-out" && <Providers providers={state.providers} returnTo={returnTo} />}
          {state.kind === "no-account" && <Invitation session={state.session} />}
        </div>
      </main>
    </div>
  );
}

function Providers({ providers, returnTo }: { providers: SignInProvider[]; returnTo: string }) {
  return (
    <section className="fy-card fy-card--pad fy-stack" aria-labelledby="signin-title">
      <div className="fy-stack fy-stack--2" style={{ gap: 0 }}>
        <h1 className="fy-h2" id="signin-title">
          Sign in to Fyniq
        </h1>
        <span className="fy-small fy-muted">{window.location.host}</span>
      </div>
      {providers.length === 0 ? (
        <p className="fy-secondary">Sign-in is not set up for this installation yet. Ask whoever runs it.</p>
      ) : (
        <div className="fy-stack fy-stack--2">
          {providers.map((p) => (
            <a key={p.id} className="fy-btn fy-btn--secondary fy-btn--lg fy-btn--block provider" href={signInURL(p.id, returnTo)}>
              <span className="fy-org__logo" aria-hidden="true">
                {p.name.charAt(0)}
              </span>
              Continue with {p.name}
            </a>
          ))}
        </div>
      )}
    </section>
  );
}

// Invitation is the "Joining by invitation" variant: a person signed in without a Fyniq account enters the one-time
// invitation they were given. A company's invitation makes them a member of its organisation with its roles
// (FR-ADM-01); a Super admin's makes them the installation's second Super admin, who keeps ten recovery codes
// (FR-ADM-06/07). The code says which: the company's, the commoner, is tried first.
function Invitation({ session }: { session: Session }) {
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<{ field: "code" | "name" | "form"; message: string } | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const summaryRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (problem) summaryRef.current?.focus();
  }, [problem]);

  if (codes) return <RecoveryCodes codes={codes} onDone={() => go("/")} doneLabel="Go to Fyniq" />;

  const accept = async (e: React.FormEvent) => {
    e.preventDefault();
    if (code.trim().length < 20) return setProblem({ field: "code", message: "Enter the whole invitation code, as it was given to you." });
    if (!name.trim()) return setProblem({ field: "name", message: "Enter your name, as other people will see it." });
    setBusy(true);
    const body = { invitation_code: code.trim(), display_name: name.trim() };
    const joined = await api.POST("/v1/members", { body });
    if (joined.data) return go("/");
    let p = problemOf(joined.error);
    if (p.code === "API-SUPERADMIN-01") {
      const { data, error } = await api.POST("/v1/super-admins", { body });
      if (data) {
        setBusy(false);
        return setCodes(data.recovery_codes ?? []);
      }
      p = problemOf(error);
    }
    setBusy(false);
    setProblem(
      p.code === "API-SUPERADMIN-01"
        ? { field: "code", message: "That invitation code is not valid, was used already or has expired. Ask whoever invited you for a new one." }
        : { field: "form", message: `${p.detail ?? "The invitation could not be accepted."} ${p.fix ?? ""} (${p.code ?? "error"})` },
    );
  };

  return (
    <section className="fy-card fy-card--sunken fy-card--pad fy-stack fy-stack--3" aria-labelledby="invite-title">
      <div className="fy-stack fy-stack--2">
        <h1 className="fy-h2" id="invite-title">
          Joining by invitation
        </h1>
        <p className="fy-secondary">
          You are signed in{session.provider ? ` with ${session.provider}` : ""}, but you have no Fyniq account yet. If you were given
          an invitation code, enter it here: to join a company's books with the roles it gives you, or to become the second Super
          admin of this installation.
        </p>
      </div>
      {problem && (
        <div className="fy-banner fy-banner--critical" role="alert" tabIndex={-1} ref={summaryRef} aria-labelledby="invite-problem">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong" id="invite-problem">
              There is a problem
            </span>
            {problem.field === "form" ? (
              <span className="fy-small">{problem.message}</span>
            ) : (
              <a className="fy-small" href={problem.field === "code" ? "#invite-code" : "#invite-name"}>
                {problem.message}
              </a>
            )}
          </div>
        </div>
      )}
      <form className="fy-stack fy-stack--3" onSubmit={accept} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor="invite-code">
            Invitation code
          </label>
          <input
            className="fy-input fy-mono"
            id="invite-code"
            autoComplete="off"
            spellCheck={false}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            aria-invalid={problem?.field === "code"}
            aria-describedby="invite-code-help"
          />
          <span className="fy-help" id="invite-code-help">
            Case, spaces and dashes do not matter. It works once, within 7 days.
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor="invite-name">
            Your name
          </label>
          <input className="fy-input" id="invite-name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={problem?.field === "name"} />
        </div>
        <button className="fy-btn fy-btn--primary" type="submit" disabled={busy}>
          <Icon of={Mail} />
          {busy ? "Accepting…" : "Accept the invitation"}
        </button>
      </form>
      <form method="post" action="/api/auth/signout">
        <button className="fy-btn fy-btn--ghost" type="submit">
          <Icon of={LogOut} />
          Use a different account
        </button>
      </form>
    </section>
  );
}
