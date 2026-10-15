import { useEffect, useState } from "react";
import { api, type Organisation, type Session, type SignInProvider } from "./api/client";

type State =
  | { kind: "checking" }
  | { kind: "down" }
  | { kind: "signed-out"; providers: SignInProvider[]; setupDone: boolean }
  | { kind: "signed-in"; session: Session; organisation?: Organisation };

// What a sign-in refusal means, by its code (internal/signin); the code is shown too (ENG-23).
const signInErrors: Record<string, string> = {
  "AUTH-SIGNIN-01": "That sign-in provider is not available. Choose one of the providers below.",
  "AUTH-SIGNIN-02": "The sign-in took longer than 10 minutes, or began in another browser. Sign in again.",
  "AUTH-SIGNIN-03": "The provider's answer did not pass Fyniq's checks, so you are not signed in. Sign in again.",
  "AUTH-SIGNIN-04": "The sign-in service did not answer. Try again in a moment.",
};

// The P0 shell: it proves the web app builds, loads the design tokens, reaches the API through the
// generated client and signs in with a first-party session (an HttpOnly cookie the app never reads,
// SEC-08). The screens, the sign-in page and the setup wizard arrive from docs/design/screens/.
export function App() {
  const [state, setState] = useState<State>({ kind: "checking" });
  const params = new URLSearchParams(window.location.search);
  const signInError = params.get("signin_error");
  const returnTo = params.get("return_to") ?? "/";

  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    (async () => {
      const { data: session, response } = await api.GET("/v1/session", { signal });
      if (session) {
        const { data: organisation } = session.organisation_id ? await api.GET("/v1/organisation", { signal }) : {};
        setState({ kind: "signed-in", session, organisation });
        return;
      }
      if (response.status !== 401) {
        setState({ kind: "down" });
        return;
      }
      const [{ data: providers }, { data: setup }] = await Promise.all([
        api.GET("/v1/sign-in-providers", { signal }),
        api.GET("/v1/setup", { signal }),
      ]);
      setState({ kind: "signed-out", providers: providers?.items ?? [], setupDone: setup?.done ?? true });
    })().catch((e: unknown) => {
      if (!(e instanceof DOMException && e.name === "AbortError")) setState({ kind: "down" });
    });
    return () => controller.abort();
  }, []);

  return (
    <main className="shell">
      <h1>Fyniq</h1>
      <p className="lead">Intelligence behind every number.</p>
      <p>This is the P0 shell of the web app. The screens arrive in P1.</p>
      {signInError && (
        <p className="problem" role="alert">
          Sign-in failed ({signInError}): {signInErrors[signInError] ?? "Sign in again."}
        </p>
      )}
      <div role="status" aria-live="polite" className="api">
        {state.kind === "checking" && <p>Checking the API…</p>}
        {state.kind === "down" && <p>The API is not running. Start it with make dev.</p>}
        {state.kind === "signed-out" && <SignIn providers={state.providers} setupDone={state.setupDone} returnTo={returnTo} />}
        {state.kind === "signed-in" && <SignedIn session={state.session} organisation={state.organisation} />}
      </div>
    </main>
  );
}

function SignIn({ providers, setupDone, returnTo }: { providers: SignInProvider[]; setupDone: boolean; returnTo: string }) {
  if (providers.length === 0) return <p>The API is running. Sign-in is not configured for this installation.</p>;
  return (
    <>
      <p>
        {setupDone
          ? "The API is running. Sign in to continue."
          : "This installation is not set up yet. Sign in, then complete first-run setup with the setup code."}
      </p>
      <ul className="actions">
        {providers.map((p) => (
          <li key={p.id}>
            <a className="button" href={`/api/auth/signin?${new URLSearchParams({ provider: p.id, return_to: returnTo })}`}>
              Sign in with {p.name}
            </a>
          </li>
        ))}
      </ul>
    </>
  );
}

function SignedIn({ session, organisation }: { session: Session; organisation?: Organisation }) {
  return (
    <>
      <p>
        {organisation
          ? `Signed in to ${organisation.name} (books in ${organisation.base_currency}).`
          : session.user_id
            ? "Signed in. You are not a member of an organisation yet."
            : "Signed in. You have no Fyniq account yet: complete first-run setup, or accept an invitation."}
        {session.super_admin && " You are a Super admin of this installation."}
      </p>
      <form method="post" action="/api/auth/signout">
        <button className="button" type="submit">
          Sign out
        </button>
      </form>
    </>
  );
}
