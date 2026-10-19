import { useSyncExternalStore } from "react";

// How the app looks for this person (PRD-001 §7.4, FR-UX-38; MASTER.md §2, §5, §9): theme, high
// contrast, density and motion. tokens.css switches every colour with the .dark class and
// data-contrast on <html>, the row height with data-density, and every duration with data-motion;
// the screen kit's animations run only with data-motion="on" on the .fy-device wrapper.
//
// Provisional (20 Oct 2026): the choice is kept in this browser's storage until the preferences
// operation exists (p1-app-shell task 4.1); it is presentation only and holds nothing secret.

export type Theme = "light" | "dark" | "system";
export type Contrast = "standard" | "more" | "system";
export type Density = "comfortable" | "compact";
export type MotionChoice = "on" | "reduce";

export interface Appearance {
  theme: Theme;
  contrast: Contrast;
  density: Density;
  motion: MotionChoice;
}

// What the operating system asks for, and the organisation's motion switch (Settings › Appearance
// and motion; On until the organisation setting reaches the session).
export interface Environment {
  prefersDark: boolean;
  prefersMoreContrast: boolean;
  prefersReducedMotion: boolean;
  organisationMotion: "on" | "off";
}

export interface Resolved {
  dark: boolean;
  moreContrast: boolean;
  density: Density;
  motion: "on" | "reduce" | "off";
}

export const defaults: Appearance = { theme: "system", contrast: "system", density: "comfortable", motion: "on" };

const choices = {
  theme: ["light", "dark", "system"],
  contrast: ["standard", "more", "system"],
  density: ["comfortable", "compact"],
  motion: ["on", "reduce"],
} as const;

// resolve turns the person's choices into what the page shows. "System" follows the operating
// system; the organisation's Off and prefers-reduced-motion always win over the person's choice.
export function resolve(a: Appearance, env: Environment): Resolved {
  return {
    dark: a.theme === "system" ? env.prefersDark : a.theme === "dark",
    moreContrast: a.contrast === "system" ? env.prefersMoreContrast : a.contrast === "more",
    density: a.density,
    motion: env.organisationMotion === "off" ? "off" : env.prefersReducedMotion || a.motion === "reduce" ? "reduce" : "on",
  };
}

// applyTo sets the attributes tokens.css reads on the root element.
export function applyTo(root: HTMLElement, r: Resolved): void {
  root.classList.toggle("dark", r.dark);
  if (r.moreContrast) root.dataset.contrast = "more";
  else delete root.dataset.contrast;
  if (r.density === "compact") root.dataset.density = "compact";
  else delete root.dataset.density;
  root.dataset.motion = r.motion;
}

// parse keeps only known choices, so a stale or edited value never breaks the page.
export function parse(raw: string | null): Appearance {
  let v: Record<string, unknown> = {};
  try {
    const parsed: unknown = raw ? JSON.parse(raw) : {};
    if (parsed && typeof parsed === "object") v = parsed as Record<string, unknown>;
  } catch {
    // fall back to the defaults
  }
  const pick = <K extends keyof Appearance>(k: K): Appearance[K] =>
    (choices[k] as readonly string[]).includes(v[k] as string) ? (v[k] as Appearance[K]) : defaults[k];
  return { theme: pick("theme"), contrast: pick("contrast"), density: pick("density"), motion: pick("motion") };
}

const storageKey = "fyniq.appearance";
const listeners = new Set<() => void>();
let current: Appearance = defaults;

function read(): Appearance {
  try {
    return parse(window.localStorage.getItem(storageKey));
  } catch {
    return defaults;
  }
}

function media(q: string): MediaQueryList {
  return window.matchMedia(q);
}

function environment(): Environment {
  return {
    prefersDark: media("(prefers-color-scheme: dark)").matches,
    prefersMoreContrast: media("(prefers-contrast: more)").matches,
    prefersReducedMotion: media("(prefers-reduced-motion: reduce)").matches,
    organisationMotion: "on",
  };
}

function render(): void {
  applyTo(document.documentElement, resolve(current, environment()));
  for (const l of listeners) l();
}

// startAppearance applies the stored choice before the first paint and follows the operating
// system's changes while "System" is chosen.
export function startAppearance(): void {
  current = read();
  render();
  for (const q of ["(prefers-color-scheme: dark)", "(prefers-contrast: more)", "(prefers-reduced-motion: reduce)"]) {
    media(q).addEventListener("change", render);
  }
}

export function setAppearance(change: Partial<Appearance>): void {
  current = { ...current, ...change };
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(current));
  } catch {
    // storage may be off; the choice still applies to this page
  }
  render();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useAppearance(): Appearance {
  return useSyncExternalStore(subscribe, () => current);
}

// useMotion is what the kit's .fy-device wrapper needs: animations run only when "on".
export function useMotion(): Resolved["motion"] {
  return useSyncExternalStore(subscribe, () => resolve(current, environment()).motion);
}
