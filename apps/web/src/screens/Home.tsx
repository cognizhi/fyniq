import { LogOut } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type Organisation, type Session } from "../api/client";
import { go } from "../route";
import { Brand, Icon } from "../ui";

type State = { kind: "checking" } | { kind: "down" } | { kind: "signed-in"; session: Session; organisation?: Organisation };

// Home is the P0 shell: who is signed in, and a way out. A new installation goes to /install; a
// browser without a session to /sign-in, keeping what the server sent it with (signin_error,
// return_to); a person signed in without a Fyniq account to /sign-in's invitation variant. The screens
// arrive in P1 from docs/design/screens/.
export function Home() {
  const [state, setState] = useState<State>({ kind: "checking" });

  useEffect(() => {
    (async () => {
      const [{ data: setup }, { data: session, response }] = await Promise.all([api.GET("/v1/setup"), api.GET("/v1/session")]);
      if (setup && !setup.done) return go("/install");
      if (!session) {
        if (response.status !== 401) return setState({ kind: "down" });
        const keep = new URLSearchParams();
        const params = new URLSearchParams(window.location.search);
        for (const k of ["signin_error", "return_to"]) {
          const v = params.get(k);
          if (v) keep.set(k, v);
        }
        return go(`/sign-in${keep.size ? `?${keep}` : ""}`);
      }
      if (!session.user_id) return go("/sign-in"); // signed in without an account: the invitation variant
      const { data: organisation } = session.organisation_id ? await api.GET("/v1/organisation") : { data: undefined };
      setState({ kind: "signed-in", session, organisation });
    })().catch(() => setState({ kind: "down" }));
  }, []);

  return (
    <main className="shell fy-stack">
      <Brand />
      <h1 className="fy-h1">Fyniq</h1>
      <p className="fy-lead">Intelligence behind every number.</p>
      <p>This is the P0 shell of the web app. The screens arrive in P1.</p>
      <div role="status" aria-live="polite" className="api">
        {state.kind === "checking" && <p>Checking the API…</p>}
        {state.kind === "down" && <p>The API is not running. Start it with make dev.</p>}
        {state.kind === "signed-in" && (
          <>
            <p>
              {state.organisation
                ? `Signed in to ${state.organisation.name} (books in ${state.organisation.base_currency}).`
                : "Signed in. You are not a member of an organisation yet."}
              {state.session.super_admin && " You are a Super admin of this installation."}
            </p>
            <form method="post" action="/api/auth/signout">
              <button className="fy-btn fy-btn--secondary" type="submit">
                <Icon of={LogOut} />
                Sign out
              </button>
            </form>
          </>
        )}
      </div>
    </main>
  );
}
