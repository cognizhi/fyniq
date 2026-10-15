import { useEffect, useState } from "react";
import { api, type Organisation } from "./api/client";

type State = "checking" | "down" | "signed-out" | { organisation: Organisation };

// The P0 shell: it proves the web app builds, loads the design tokens and reaches the API through the
// generated client. The screens are built in P1 from docs/design/screens/.
export function App() {
  const [state, setState] = useState<State>("checking");

  useEffect(() => {
    const controller = new AbortController();
    api
      .GET("/v1/organisation", { signal: controller.signal })
      .then(({ data, response }) => {
        if (data) setState({ organisation: data });
        else setState(response.status === 401 ? "signed-out" : "down");
      })
      .catch((e: unknown) => {
        if (!(e instanceof DOMException && e.name === "AbortError")) setState("down");
      });
    return () => controller.abort();
  }, []);

  return (
    <main className="shell">
      <h1>Fyniq</h1>
      <p className="lead">Intelligence behind every number.</p>
      <p>This is the P0 shell of the web app. The screens arrive in P1.</p>
      <p role="status" aria-live="polite" className="api">
        {state === "checking" && "Checking the API…"}
        {state === "down" && "The API is not running. Start it with make dev."}
        {state === "signed-out" && "The API is running. Sign-in arrives with the identity service."}
        {typeof state === "object" && `Signed in to ${state.organisation.name} (books in ${state.organisation.base_currency}).`}
      </p>
    </main>
  );
}
