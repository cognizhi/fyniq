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
import { House, type LucideIcon } from "lucide-react";
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
  group: "personal" | "main";
  roles: "all" | readonly string[]; // role names as GET /v1/session returns them
  feature?: FeatureSwitch;
  bottom?: boolean; // one of the compact bottom bar's items (at most four, then More)
}

export type Entry = RouteFeature | ActionFeature | NavFeature;

export const routes = [
  { kind: "route", id: "home", path: "/", mode: "basic", signedIn: true, help: "home.overview", guide: "home.overview" },
  { kind: "route", id: "sign-in", path: "/sign-in", mode: "basic", signedIn: false, help: "auth.sign-in", guide: "auth.sign-in" },
  { kind: "route", id: "install", path: "/install", mode: "basic", signedIn: false, help: "installation.setup", guide: "installation.setup" },
] as const satisfies readonly RouteFeature[];

export const actions = [
  { kind: "action", id: "command-palette", mode: "basic", help: "shell.command-palette", guide: "home.overview#find-anything-with-search" },
  { kind: "action", id: "appearance.theme", mode: "basic", help: "appearance.theme", guide: "preferences.appearance#theme" },
  { kind: "action", id: "appearance.contrast", mode: "basic", help: "appearance.contrast", guide: "preferences.appearance#high-contrast" },
  // Basic mode always uses Comfortable; Expert may choose (experience/modes "Basic and Expert density").
  { kind: "action", id: "appearance.density", mode: "expert", help: "appearance.density", guide: "preferences.appearance#density" },
  { kind: "action", id: "appearance.motion", mode: "basic", help: "appearance.motion", guide: "preferences.appearance#motion" },
  { kind: "action", id: "sign-out", mode: "basic", help: "auth.sign-out", guide: "home.overview#sign-out" },
] as const satisfies readonly ActionFeature[];

// The sidebar's items in the designed order (the NAV list in docs/design/screens/build.py). Each
// module adds its item with its first screen; the sidebar holds at most 13 (§7.1).
export const nav = [
  { kind: "nav", id: "nav.home", route: "/", icon: House, group: "main", roles: "all", bottom: true, mode: "basic", help: "home.overview", guide: "home.overview" },
] as const satisfies readonly NavFeature[];

export type RouteId = (typeof routes)[number]["id"];
export type ActionId = (typeof actions)[number]["id"];

export const registry: readonly Entry[] = [...routes, ...actions, ...nav];

export function entry(id: RouteId | ActionId): Entry {
  const e = registry.find((x) => x.id === id);
  if (!e) throw new Error(`no registry entry ${id}`);
  return e;
}
