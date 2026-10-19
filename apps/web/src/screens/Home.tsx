import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { api, type Organisation, type Session } from "../api/client";
import { go } from "../route";
import { helpText } from "../i18n";
import { AppFrame } from "../shell/AppFrame";
import { Brand } from "../ui";

type Shell =
  | { kind: "install" }
  | { kind: "sign-in"; to: string }
  | { kind: "down" }
  | { kind: "signed-in"; session: Session; organisation?: Organisation };

// whereTo asks the API where this browser belongs: a new installation goes to /install; a browser
// without a session to /sign-in, keeping what the server sent it with (signin_error, return_to); a
// person signed in without a Fyniq account to /sign-in's invitation variant.
async function whereTo(): Promise<Shell> {
  const [{ data: setup }, { data: session, response }] = await Promise.all([api.GET("/v1/setup"), api.GET("/v1/session")]);
  if (setup && !setup.done) return { kind: "install" };
  if (!session) {
    if (response.status !== 401) return { kind: "down" };
    const keep = new URLSearchParams();
    const params = new URLSearchParams(window.location.search);
    for (const k of ["signin_error", "return_to"]) {
      const v = params.get(k);
      if (v) keep.set(k, v);
    }
    return { kind: "sign-in", to: `/sign-in${keep.size ? `?${keep}` : ""}` };
  }
  if (!session.user_id) return { kind: "sign-in", to: "/sign-in" };
  const { data: organisation } = session.organisation_id ? await api.GET("/v1/organisation") : { data: undefined };
  return { kind: "signed-in", session, organisation };
}

// Home is the first signed-in page: who is signed in, inside the app frame (navigation cut to the
// person, search and commands, appearance, sign out). Its tiles and Inbox arrive with the modules
// (docs/design/screens AppShell).
export function Home() {
  const shell = useQuery({ queryKey: ["shell"], queryFn: whereTo });
  const state: Shell | { kind: "checking" } = shell.isError ? { kind: "down" } : (shell.data ?? { kind: "checking" });

  useEffect(() => {
    // Moving between the signed-out pages is a page load, so nothing secret survives in memory.
    if (state.kind === "install") go("/install");
    else if (state.kind === "sign-in") go(state.to);
  }, [state]);

  const status = (
    <div role="status" aria-live="polite" className="api">
      {state.kind === "checking" && <p>Checking the API…</p>}
      {state.kind === "down" && <p>The API is not running. Start it with make dev.</p>}
      {state.kind === "signed-in" && (
        <p>
          {state.organisation
            ? `Signed in to ${state.organisation.name} (books in ${state.organisation.base_currency}).`
            : "Signed in. You are not a member of an organisation yet."}
          {state.session.super_admin && " You are a Super admin of this installation."}
        </p>
      )}
    </div>
  );

  if (state.kind !== "signed-in") {
    return (
      <main className="shell fy-stack">
        <Brand />
        <h1 className="fy-h1">Fyniq</h1>
        <p className="fy-lead">Intelligence behind every number.</p>
        {status}
      </main>
    );
  }
  return (
    <AppFrame session={state.session} title={helpText("home.overview").label}>
      <div className="fy-page fy-page--narrow fy-enter">
        <div className="fy-pagehead">
          <div className="fy-pagehead__text">
            <h1 className="fy-h1">Fyniq</h1>
            <p className="fy-secondary">Intelligence behind every number.</p>
          </div>
        </div>
        <p>The pages of your workspace arrive here, one module at a time.</p>
        {status}
      </div>
    </AppFrame>
  );
}
