// Each app keeps its own last page for the Finance | People switch (PRD-001 FR-PPL-02): the other app opens at "/?switch=1" and this brings the person back to the page they left here. Only a path of this origin
// that is not a sign-in page is kept or followed, so a crafted link cannot send anyone elsewhere. Browser storage may be blocked or empty: nothing depends on it.
const KEY = "fyniq.lastPage";

const ok = (p: string | null): p is string => !!p && p.startsWith("/") && !p.startsWith("//") && !p.startsWith("/sign-in") && !p.includes("\\") && p.length <= 500;

export function rememberPage(path: string): void {
  try {
    if (ok(path) && path !== "/") window.localStorage.setItem(KEY, path);
  } catch {
    /* storage is a convenience */
  }
}

// switchedIn is where a person arriving through the switch goes: the page they left, or null when there is none or this is no arrival through the switch.
export function switchedIn(search: string, path: string): string | null {
  if (path !== "/" || new URLSearchParams(search).get("switch") !== "1") return null;
  try {
    const p = window.localStorage.getItem(KEY);
    return ok(p) ? p : null;
  } catch {
    return null;
  }
}
