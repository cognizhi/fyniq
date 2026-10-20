// The feature registry (PRD-001 §7.8, §7.9; FR-UX-29, FR-UX-31; openspec/changes/p1-app-shell
// design.md): every route, navigation item, field group, column set and action of the web app, in
// one place, with
// - its experience mode: shown in Basic, or only in Expert (presentation only, FR-UX-24);
// - its help key: the tooltip text lives under that key in src/i18n/en-SG.json, never in code;
// - its guide page in docs/user-guide/ (the page's front-matter `key`, optionally "#<heading>"),
//   which the knowledge base in kb/ compiles from the same file;
// - for a route, its path and whether it needs a session;
// - for a navigation item, its route, icon, group, the roles that see it (PRD-001 §7.1) and the
//   organisation's feature switch it needs (FR-ORG-06). The shell cuts the navigation to the
//   person's roles and switches (FR-UX-40); the API still refuses what a role may not do.
// The router, the sidebar, the bottom bar and the command palette read it; later the mode filter,
// the help panel and the tour. registry.test.ts fails when an entry lacks a mode, a help key, its
// tooltip or its guide page (QA-UI-05), and when the router has a route the registry does not list.
import { House, Inbox, Settings, ShieldCheck, Stamp, UserRound, Users, type LucideIcon } from "lucide-react";
import type { components } from "./api/schema";

export type Mode = "basic" | "expert";
export type FeatureSwitch = keyof components["schemas"]["Features"];

interface Feature {
  id: string;
  mode: Mode;
  help: string;
  guide: string;
}

export interface RouteFeature extends Feature {
  kind: "route";
  path: string;
  // signedIn: the page needs a session; the API still decides what it shows (the server enforces
  // every right, FR-UX-40).
  signedIn: boolean;
}

export interface ActionFeature extends Feature {
  kind: "action";
}

export interface NavFeature extends Feature {
  kind: "nav";
  route: string; // a registry route's path
  icon: LucideIcon;
  // personal and main are the sidebar's sections, footer its foot (Settings); you and organisation
  // are the sections of the Settings page's own navigation.
  group: "personal" | "main" | "footer" | "you" | "organisation";
  roles: "all" | readonly string[]; // role names as GET /v1/session returns them
  feature?: FeatureSwitch;
  bottom?: boolean; // one of the compact bottom bar's items (at most four, then More)
}

export type Entry = RouteFeature | ActionFeature | NavFeature;

export const routes = [
  { kind: "route", id: "home", path: "/", mode: "basic", signedIn: true, help: "home.overview", guide: "home.overview" },
  { kind: "route", id: "sign-in", path: "/sign-in", mode: "basic", signedIn: false, help: "auth.sign-in", guide: "auth.sign-in" },
  { kind: "route", id: "install", path: "/install", mode: "basic", signedIn: false, help: "installation.setup", guide: "installation.setup" },
  { kind: "route", id: "inbox", path: "/inbox", mode: "basic", signedIn: true, help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "approval", path: "/approvals/$approvalId", mode: "basic", signedIn: true, help: "approvals.request", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "review-log", path: "/review-log", mode: "basic", signedIn: true, help: "approvals.review-log", guide: "approvals.decide#review-high-risk-actions" },
  { kind: "route", id: "settings", path: "/settings", mode: "basic", signedIn: true, help: "settings.overview", guide: "home.overview#settings" },
  { kind: "route", id: "settings-profile", path: "/settings/profile", mode: "basic", signedIn: true, help: "settings.profile", guide: "preferences.security" },
  { kind: "route", id: "settings-members", path: "/settings/members", mode: "expert", signedIn: true, help: "settings.members", guide: "settings.members" },
  { kind: "route", id: "settings-team-mode", path: "/settings/team-mode", mode: "expert", signedIn: true, help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "route", id: "settings-approval-policies", path: "/settings/approval-policies", mode: "expert", signedIn: true, help: "settings.approval-policies", guide: "approvals.policies" },
] as const satisfies readonly RouteFeature[];

export const actions = [
  { kind: "action", id: "command-palette", mode: "basic", help: "shell.command-palette", guide: "home.overview#find-anything-with-search" },
  { kind: "action", id: "appearance.theme", mode: "basic", help: "appearance.theme", guide: "preferences.appearance#theme" },
  { kind: "action", id: "appearance.contrast", mode: "basic", help: "appearance.contrast", guide: "preferences.appearance#high-contrast" },
  // Basic mode always uses Comfortable; Expert may choose (experience/modes "Basic and Expert density").
  { kind: "action", id: "appearance.density", mode: "expert", help: "appearance.density", guide: "preferences.appearance#density" },
  { kind: "action", id: "appearance.motion", mode: "basic", help: "appearance.motion", guide: "preferences.appearance#motion" },
  { kind: "action", id: "sign-out", mode: "basic", help: "auth.sign-out", guide: "home.overview#sign-out" },
  { kind: "action", id: "approval.approve", mode: "basic", help: "approval.approve", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "approval.reject", mode: "basic", help: "approval.reject", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "approval.withdraw", mode: "basic", help: "approval.withdraw", guide: "approvals.decide#withdraw-your-request" },
  { kind: "action", id: "approval.copy-hash", mode: "basic", help: "approval.copy-hash", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "review.mark", mode: "basic", help: "review.mark", guide: "approvals.decide#review-high-risk-actions" },
  { kind: "action", id: "member.invite", mode: "expert", help: "member.invite", guide: "settings.members#invite-someone" },
  { kind: "action", id: "member.change-roles", mode: "expert", help: "member.change-roles", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.add-role", mode: "expert", help: "member.add-role", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.end-role", mode: "expert", help: "member.end-role", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.withdraw-invitation", mode: "expert", help: "member.withdraw-invitation", guide: "settings.members#invite-someone" },
  { kind: "action", id: "team-mode.switch", mode: "expert", help: "team-mode.switch", guide: "settings.team-mode#switch-the-team-mode" },
  { kind: "action", id: "approval-policy.change", mode: "expert", help: "approval-policy.change", guide: "approvals.policies#change-a-policy" },
  { kind: "action", id: "mode", mode: "basic", help: "shell.mode", guide: "preferences.security#basic-and-expert-mode" },
  { kind: "action", id: "step-up", mode: "basic", help: "session.step-up", guide: "auth.sign-in#confirm-its-you-with-a-passkey" },
  { kind: "action", id: "sign-in-again", mode: "basic", help: "session.sign-in-again", guide: "auth.sign-in#confirm-its-you-with-a-passkey" },
  { kind: "action", id: "passkey.add", mode: "basic", help: "passkey.add", guide: "preferences.security#add-a-passkey" },
  { kind: "action", id: "passkey.revoke", mode: "basic", help: "passkey.revoke", guide: "preferences.security#revoke-a-passkey" },
  { kind: "action", id: "device.sign-out", mode: "basic", help: "device.sign-out", guide: "preferences.security#your-devices" },
  { kind: "action", id: "device.sign-out-others", mode: "basic", help: "device.sign-out-others", guide: "preferences.security#your-devices" },
] as const satisfies readonly ActionFeature[];

// The sidebar's items in the designed order (the NAV list in docs/design/screens/build.py). Each
// module adds its item with its first screen; the sidebar holds at most 13 (§7.1).
export const nav = [
  { kind: "nav", id: "nav.home", route: "/", icon: House, group: "main", roles: "all", bottom: true, mode: "basic", help: "home.overview", guide: "home.overview" },
  { kind: "nav", id: "nav.inbox", route: "/inbox", icon: Inbox, group: "main", roles: "all", bottom: true, mode: "basic", help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "nav", id: "nav.settings", route: "/settings", icon: Settings, group: "footer", roles: "all", mode: "basic", help: "settings.overview", guide: "home.overview#settings" },
] as const satisfies readonly NavFeature[];

// The Settings page's own navigation (docs/design/screens OrganisationSettings, UsersRoles): your
// settings, then the organisation's. Cut like the sidebar, by role and mode; the API still decides.
export const settingsNav = [
  { kind: "nav", id: "settings.profile", route: "/settings/profile", icon: UserRound, group: "you", roles: "all", mode: "basic", help: "settings.profile", guide: "preferences.security" },
  // The organisation's pages show to the roles that may view them (Appendix D, V or more); the API decides the rest.
  { kind: "nav", id: "settings.members", route: "/settings/members", icon: Users, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.members", guide: "settings.members" },
  { kind: "nav", id: "settings.team-mode", route: "/settings/team-mode", icon: ShieldCheck, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "nav", id: "settings.approval-policies", route: "/settings/approval-policies", icon: Stamp, group: "organisation", roles: ["owner", "administrator", "controller", "accountant", "auditor", "dpo"], mode: "expert", help: "settings.approval-policies", guide: "approvals.policies" },
] as const satisfies readonly NavFeature[];

export type RouteId = (typeof routes)[number]["id"];
export type ActionId = (typeof actions)[number]["id"];

export const registry: readonly Entry[] = [...routes, ...actions, ...nav, ...settingsNav];

export function entry(id: RouteId | ActionId): Entry {
  const e = registry.find((x) => x.id === id);
  if (!e) throw new Error(`no registry entry ${id}`);
  return e;
}
