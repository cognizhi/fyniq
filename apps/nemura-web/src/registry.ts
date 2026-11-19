// The feature registry (PRD-002 NM-UX-02, NM-UX-04; design D11), Nemura's own: every route and action of the app, with
// - its experience mode: "basic" for what everyone sees (employees get one simple mode), "expert" for the people team's own pages;
// - its help key: the label and tooltip live under that key in src/i18n/en-SG.json, never in code;
// - its guide page in docs/user-guide/nemura/ (the page's front-matter `key`, optionally "#<heading>");
// - for a route, its path and whether it needs a session.
// registry.test.ts fails when an entry lacks any of them, and when the app serves a route the registry does not list.
export type Mode = "basic" | "expert";

interface Feature {
  id: string;
  mode: Mode;
  help: string;
  guide: string;
}

export interface RouteFeature extends Feature {
  kind: "route";
  path: string;
  signedIn: boolean;
  // The page's id in the review register (docs/prd/reviews/review-status.json): its banner shows while an area it lists awaits review.
  screen: string;
}

export interface ActionFeature extends Feature {
  kind: "action";
}

export type Entry = RouteFeature | ActionFeature;

export const registry: Entry[] = [
  { kind: "route", id: "sign-in", path: "/sign-in", signedIn: false, screen: "nm-sign-in", mode: "basic", help: "signin.page", guide: "nemura.sign-in" },
  { kind: "route", id: "join", path: "/join", signedIn: false, screen: "nm-join", mode: "basic", help: "join.page", guide: "nemura.sign-in#joining-with-a-code" },
  { kind: "route", id: "my-home", path: "/me", signedIn: true, screen: "nm-my-home", mode: "basic", help: "home.me", guide: "nemura.home" },
  { kind: "route", id: "people-home", path: "/people", signedIn: true, screen: "nm-people-home", mode: "expert", help: "home.people", guide: "nemura.home" },
  { kind: "route", id: "people-directory", path: "/people/directory", signedIn: true, screen: "nm-people", mode: "basic", help: "directory.page", guide: "nemura.people" },
  { kind: "route", id: "person", path: "/people/:id", signedIn: true, screen: "nm-person", mode: "basic", help: "person.page", guide: "nemura.person" },
  { kind: "route", id: "features", path: "/settings/features", signedIn: true, screen: "nm-features", mode: "expert", help: "features.page", guide: "nemura.features" },
  { kind: "action", id: "sign-out", mode: "basic", help: "nav.signOut", guide: "nemura.sign-in#signing-out" },
];

export const routes = registry.filter((e): e is RouteFeature => e.kind === "route");

// matchRoute finds the route a path belongs to: a path is exact, or has ":id" for one segment, and the more specific route wins ("/people/directory" is not a person).
export function matchRoute(path: string): { route: RouteFeature; params: Record<string, string> } | undefined {
  const parts = path.split("/");
  let found: { route: RouteFeature; params: Record<string, string>; exact: boolean } | undefined;
  for (const route of routes) {
    const want = route.path.split("/");
    if (want.length !== parts.length) continue;
    const params: Record<string, string> = {};
    let exact = true;
    const ok = want.every((w, i) => {
      if (w.startsWith(":")) {
        exact = false;
        params[w.slice(1)] = decodeURIComponent(parts[i] ?? "");
        return Boolean(parts[i]);
      }
      return w === parts[i];
    });
    if (ok && (!found || (exact && !found.exact))) found = { route, params, exact };
  }
  return found && { route: found.route, params: found.params };
}
