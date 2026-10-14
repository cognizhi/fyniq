import { useEffect, useState } from "react";

type Version = { service: string; version: string; commit: string; flavour: string };

// The P0 shell: it proves the web app builds, loads the design tokens and reaches the API.
// The screens are built in P1 from docs/design/screens/.
export function App() {
  const [api, setApi] = useState<Version | "unreachable" | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/version", { signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<Version>) : Promise.reject(new Error(String(r.status)))))
      .then(setApi)
      .catch((e: unknown) => {
        if (!(e instanceof DOMException && e.name === "AbortError")) setApi("unreachable");
      });
    return () => controller.abort();
  }, []);

  return (
    <main className="shell">
      <h1>Fyniq</h1>
      <p className="lead">Intelligence behind every number.</p>
      <p>This is the P0 shell of the web app. The screens arrive in P1.</p>
      <p role="status" aria-live="polite" className="api">
        {api === null && "Checking the API…"}
        {api === "unreachable" && "The API is not running. Start it with make dev."}
        {api !== null && api !== "unreachable" && `API ${api.version} (${api.commit}, ${api.flavour} build) is running.`}
      </p>
    </main>
  );
}
