import { matchRoute } from "./registry";

// Each app keeps its own last page for the Finance | People switch (PRD-002 NM-UX-03): Fyniq's switch opens Nemura at "/?switch=1" and this brings the person back to the page they left here. Only a path of
// this origin that the app serves is kept or followed, so a crafted link cannot send anyone elsewhere. Browser storage may be blocked or empty: nothing depends on it.
const KEY = "nemura.lastPage";

const ok = (p: string | null): p is string => !!p && p.startsWith("/") && !p.startsWith("//") && !p.includes("\\") && p.length <= 500 && matchRoute(p)?.route.signedIn === true;

export function rememberPage(path: string): void {
  try {
    if (ok(path)) window.localStorage.setItem(KEY, path);
  } catch {
    /* storage is a convenience */
  }
}

// switchedIn is the page a person arriving through the switch goes back to, or undefined.
export function switchedIn(search: string): string | undefined {
  if (new URLSearchParams(search).get("switch") !== "1") return undefined;
  try {
    const p = window.localStorage.getItem(KEY);
    return ok(p) ? p : undefined;
  } catch {
    return undefined;
  }
}
