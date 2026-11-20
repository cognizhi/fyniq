import { useSyncExternalStore } from "react";
import { persist } from "./preferenceSink";
import type { Mode } from "./registry";

// The experience mode (PRD-001 §7.8; FR-UX-23, FR-UX-24, FR-UX-27): Basic shows the essentials,
// Expert adds the rest. It is presentation only: it never changes what a person may do, what the
// API checks or the request an action sends.
//
// The choice is kept on the server as the person's own preference, the same on every device (FR-UX-23;
// preferences.ts), and in this browser's storage so the first page is drawn before the session answers. Until a person
// chooses, the organisation's starting mode for their roles decides (FR-UX-27), as the session reports it.

// Roles that start in Expert, unless the person is also an Owner: the Owner wins (FR-UX-27).
const expertRoles = new Set(["accountant", "controller", "tax_agent", "auditor", "payroll_administrator"]);

export function defaultMode(roles: readonly string[] | null | undefined): Mode {
  const r = roles ?? [];
  if (r.includes("owner")) return "basic";
  return r.some((x) => expertRoles.has(x)) ? "expert" : "basic";
}

const storageKey = "fyniq.mode";
const listeners = new Set<() => void>();
let chosen: Mode | null = null;
let loaded = false;

function read(): Mode | null {
  try {
    const v = window.localStorage.getItem(storageKey);
    return v === "basic" || v === "expert" ? v : null;
  } catch {
    return null;
  }
}

function current(): Mode | null {
  if (!loaded) {
    chosen = read();
    loaded = true;
  }
  return chosen;
}

// An Administrator can pin a role to Basic (FR-UX-28): the person then works in Basic and has no switch.
let pinned = false;

export function setPinnedBasic(on: boolean): void {
  if (on === pinned) return;
  pinned = on;
  for (const l of listeners) l();
}

// usePinnedBasic is whether the person's roles are pinned to Basic.
export function usePinnedBasic(): boolean {
  return useSyncExternalStore(subscribe, () => pinned);
}

// setMode is the person's choice: it applies at once, and is kept on the server. A person pinned to Basic has no choice.
export function setMode(m: Mode): void {
  if (pinned) return;
  applyMode(m);
  persist({ mode: m });
}

// applyMode applies a mode without sending it: the person's choice as the server has it.
export function applyMode(m: Mode | null): void {
  chosen = m;
  loaded = true;
  try {
    if (m) window.localStorage.setItem(storageKey, m);
    else window.localStorage.removeItem(storageKey);
  } catch {
    // storage may be off; the choice still applies to this page
  }
  for (const l of listeners) l();
}

// startingMode is the mode the organisation starts a person in while they have not chosen (the session's default_mode).
let starting: Mode | null = null;

export function setStartingMode(m: Mode | null): void {
  if (m === starting) return;
  starting = m;
  for (const l of listeners) l();
}

// theChosenMode is the person's own choice, if they made one.
export function theChosenMode(): Mode | null {
  return current();
}

// forgetMode drops the stored choice (tests, and a person who wants the default back).
export function forgetMode(): void {
  chosen = null;
  loaded = false;
  starting = null;
  pinned = false;
  try {
    window.localStorage.removeItem(storageKey);
  } catch {
    // nothing stored
  }
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

// useMode is the person's mode: their choice, or their roles' default.
export function useMode(roles: readonly string[] | null | undefined): Mode {
  const p = useSyncExternalStore(subscribe, () => pinned);
  const c = useSyncExternalStore(subscribe, current);
  const s = useSyncExternalStore(subscribe, () => starting);
  return p ? "basic" : (c ?? s ?? defaultMode(roles));
}
