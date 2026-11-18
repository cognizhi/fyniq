import type { QueryClient } from "@tanstack/react-query";

// A page that was away longer than the change feed keeps (FR-UX-19's window, 5 minutes [proposed]) has missed what
// changed meanwhile: hidden, offline, or on a laptop asleep, which stops its timers. When it is back, it reloads its
// state once: every query re-reads through the API, which answers only what the person may still see (SEC-34), and each
// list is replaced by its new answer, never added to, so no row shows twice (G4-077).

export const FEED_WINDOW = 5 * 60 * 1000;
const TICK = 30 * 1000; // a timer that fires much later than this was asleep

export function installFreshness(client: QueryClient, doc: Document = document, now: () => number = Date.now): () => void {
  const view = doc.defaultView;
  if (!view) return () => undefined;
  let away: number | null = null; // when the page went away
  let last = now();
  const leave = () => {
    if (away === null) away = now();
  };
  const back = () => {
    if (away === null) return;
    const gone = now() - away;
    away = null;
    if (gone > FEED_WINDOW) void client.invalidateQueries();
  };
  const visibility = () => (doc.visibilityState === "hidden" ? leave() : view.navigator.onLine && back());
  const online = () => doc.visibilityState !== "hidden" && back();
  const tick = () => {
    const at = now();
    if (at - last > FEED_WINDOW + TICK && away === null) {
      away = last; // asleep since the tick before
      back();
    }
    last = at;
  };
  doc.addEventListener("visibilitychange", visibility);
  view.addEventListener("offline", leave);
  view.addEventListener("online", online);
  const timer = view.setInterval(tick, TICK);
  return () => {
    doc.removeEventListener("visibilitychange", visibility);
    view.removeEventListener("offline", leave);
    view.removeEventListener("online", online);
    view.clearInterval(timer);
  };
}
