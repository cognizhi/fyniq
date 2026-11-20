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
  { kind: "route", id: "my-leave", path: "/me/leave", signedIn: true, screen: "nm-my-leave", mode: "basic", help: "leave.page", guide: "nemura.my-leave" },
  { kind: "route", id: "my-requests", path: "/me/requests", signedIn: true, screen: "nm-my-requests", mode: "basic", help: "requests.page", guide: "nemura.requests" },
  { kind: "route", id: "my-details", path: "/me/details", signedIn: true, screen: "nm-my-details", mode: "basic", help: "details.page", guide: "nemura.my-details" },
  { kind: "route", id: "ask", path: "/me/ask/hr", signedIn: true, screen: "nm-ask", mode: "basic", help: "ask.page", guide: "nemura.ask-hr" },
  { kind: "route", id: "people-requests", path: "/people/requests", signedIn: true, screen: "nm-people-requests", mode: "basic", help: "queue.page", guide: "nemura.requests-queue" },
  { kind: "route", id: "onboarding", path: "/people/onboarding", signedIn: true, screen: "nm-onboarding", mode: "expert", help: "onb.page", guide: "nemura.onboarding" },
  { kind: "route", id: "payroll-hr", path: "/people/payroll", signedIn: true, screen: "nm-payroll-hr", mode: "expert", help: "payroll.page", guide: "nemura.payroll-records" },
  { kind: "route", id: "payrun-approval", path: "/people/payroll/runs/:id", signedIn: true, screen: "nm-payrun-approval", mode: "expert", help: "approval.page", guide: "nemura.payroll-approval" },
  { kind: "route", id: "ask-assistant", path: "/me/ask", signedIn: true, screen: "nm-ask", mode: "basic", help: "askai.page", guide: "nemura.ask-assistant" },
  { kind: "route", id: "fyniq-link", path: "/settings/fyniq", signedIn: true, screen: "nm-fyniq-link", mode: "expert", help: "link.page", guide: "nemura.fyniq-link" },
  { kind: "route", id: "assistant", path: "/settings/assistant", signedIn: true, screen: "nm-assistant", mode: "expert", help: "assistant.page", guide: "nemura.assistant" },
  { kind: "route", id: "my-pay", path: "/me/pay", signedIn: true, screen: "nm-my-pay", mode: "basic", help: "mypay.page", guide: "nemura.my-pay" },
  { kind: "route", id: "payslip", path: "/me/pay/:id", signedIn: true, screen: "nm-payslip", mode: "basic", help: "mypay.page", guide: "nemura.my-pay" },
  { kind: "route", id: "request-letter", path: "/me/letters/new", signedIn: true, screen: "nm-request-letter", mode: "basic", help: "request.page", guide: "nemura.letters.request" },
  { kind: "route", id: "letters", path: "/people/letters", signedIn: true, screen: "nm-letters", mode: "expert", help: "letters.page", guide: "nemura.letters.issue" },
  { kind: "route", id: "letter-templates", path: "/settings/letters", signedIn: true, screen: "nm-letter-template", mode: "expert", help: "tpl.page", guide: "nemura.letters.templates" },
  { kind: "route", id: "apply-leave", path: "/me/leave/new", signedIn: true, screen: "nm-apply-leave", mode: "basic", help: "apply.page", guide: "nemura.leave.apply" },
  { kind: "route", id: "report-sick", path: "/me/leave/sick", signedIn: true, screen: "nm-report-sick", mode: "basic", help: "sick.page", guide: "nemura.leave.sick" },
  { kind: "route", id: "team-leave", path: "/people/leave", signedIn: true, screen: "nm-team-leave", mode: "basic", help: "team.page", guide: "nemura.leave.approve" },
  { kind: "route", id: "government-leave", path: "/people/leave/government-paid", signedIn: true, screen: "nm-govt-claims", mode: "expert", help: "gov.page", guide: "nemura.leave.government-paid" },
  { kind: "route", id: "people-home", path: "/people", signedIn: true, screen: "nm-people-home", mode: "expert", help: "home.people", guide: "nemura.home" },
  { kind: "route", id: "person-new", path: "/people/new", signedIn: true, screen: "nm-people", mode: "basic", help: "new.page", guide: "nemura.people" },
  { kind: "route", id: "people-directory", path: "/people/directory", signedIn: true, screen: "nm-people", mode: "basic", help: "directory.page", guide: "nemura.people" },
  { kind: "route", id: "person", path: "/people/:id", signedIn: true, screen: "nm-person", mode: "basic", help: "person.page", guide: "nemura.person" },
  { kind: "route", id: "person-leave", path: "/people/:id/leave", signedIn: true, screen: "nm-my-leave", mode: "basic", help: "leave.page", guide: "nemura.my-leave#for-the-people-team" },
  { kind: "route", id: "features", path: "/settings/features", signedIn: true, screen: "nm-features", mode: "expert", help: "features.page", guide: "nemura.features" },
  { kind: "route", id: "packs", path: "/settings/packs", signedIn: true, screen: "nm-country-pack", mode: "expert", help: "packs.page", guide: "nemura.packs" },
  { kind: "route", id: "policy", path: "/settings/policy", signedIn: true, screen: "nm-leave-policy", mode: "expert", help: "policy.page", guide: "nemura.packs" },
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
