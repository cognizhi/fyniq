import { useSyncExternalStore } from "react";
import type { Session } from "./api/client";
import { helpText } from "./i18n";
import { persist } from "./preferenceSink";
import { nav, routes, type Mode } from "./registry";
import { mayOpen, navFor } from "./shell/navigation";

// Keyboard-first entry (FR-UX-06; experience/navigation "Keyboard-first entry"): "G then" a letter goes to an area,
// "C then" a letter starts a new record, and ? shows the shortcut sheet. Basic mode keeps ? only, beside Ctrl+K and
// Ctrl+J, which work in both modes (§7.8, experience/modes "Basic keyboard"). Each person may turn the
// single-character shortcuts off or change their keys (WCAG 2.1 SC 2.1.4), and none fires while focus is in a field.
//
// The choice is kept on the server as the person's own preference, the same on every device (FR-UX-23; preferences.ts),
// and in this browser's storage under the person's user ID, so it applies to them only and draws the first page.

export interface Shortcut {
  id: string;
  keys: string; // one key, or two typed one after the other: "?" or "gb"
  label: string;
  to?: string; // the page it opens; none for the sheet
  group: "sheet" | "go" | "create";
}

// The usual keys. "G then" a letter opens a sidebar area; "C then" a letter starts each kind of record, one for every
// page that creates one (the test fails a create page without its keys).
export const sheetKeys = "?";
export const goKeys: readonly (readonly [string, string])[] = [
  ["nav.home", "gh"],
  ["nav.inbox", "gi"],
  ["nav.contacts", "gc"],
  ["nav.sales", "gs"],
  ["nav.purchases", "gp"],
  ["nav.claims-review", "gx"],
  ["nav.banking", "gb"],
  ["nav.reports", "gr"],
  ["nav.accounting", "ga"],
  ["nav.equity", "ge"],
  ["nav.planning", "gl"],
  ["nav.compliance", "gm"],
  ["nav.settings", "gt"],
];
export const createKeys: readonly (readonly [string, string])[] = [
  ["invoice-new", "ci"],
  ["credit-note-new", "cn"],
  ["quote-new", "cq"],
  ["sales-order-new", "co"],
  ["recurring-invoice-new", "cr"],
  ["bill-new", "cb"],
  ["credit-new", "cd"],
  ["purchase-order-new", "cp"],
  ["recurring-bill-new", "cu"],
  ["claim-new", "ce"],
  ["mileage-claim", "cm"],
  ["bank-account-new", "ca"],
  ["journal-new", "cj"],
  ["recurring-journal-new", "cy"],
  ["equity-transaction-new", "ct"],
  ["equity-instrument-new", "cs"],
  ["equity-option-grant-new", "cg"],
  ["equity-pool-resolution-new", "cv"],
];

// The Inbox's own keys (FR-UX-03), active only while a request in the Inbox has the focus, so they never fire in a field
// (SC 2.1.4's exception); the Inbox and the shortcut sheet both list them.
export const inboxKeys = [
  ["J or ↓", "Next request"],
  ["K or ↑", "Previous request"],
  ["E or Enter", "Open it"],
  ["A", "Open its approval to confirm"],
  ["R", "Open its rejection to confirm"],
  ["?", "Show or hide these keys"],
] as const;

// usual is every shortcut's usual keys by its ID, the sheet's included.
export const usual: Readonly<Record<string, string>> = Object.fromEntries([["sheet", sheetKeys], ...goKeys, ...createKeys]);

export interface ShortcutSettings {
  on: boolean; // the single-character shortcuts; Ctrl+K and Ctrl+J are never turned off
  keys: Record<string, string>; // the person's own keys, by shortcut ID; the usual keys for the rest
}

export const defaultSettings: ShortcutSettings = { on: true, keys: {} };

// keysOf is the keys a shortcut answers to for this person.
export function keysOf(s: ShortcutSettings, id: string): string {
  return s.keys[id] ?? usual[id] ?? "";
}

// available is the shortcuts that work for the person in their mode, with the keys they chose: in Basic mode the
// sheet only; in Expert mode also the areas the sidebar shows them and the create pages their access opens. None
// when they turned the single-character shortcuts off.
export function available(session: Pick<Session, "roles" | "features"> & { super_admin?: boolean }, mode: Mode, s: ShortcutSettings): Shortcut[] {
  const out: Shortcut[] = [{ id: "sheet", keys: keysOf(s, "sheet"), label: helpText("shell.shortcuts").label, group: "sheet" }];
  if (mode === "expert") {
    const visible = navFor(nav, session, mode);
    for (const [id] of goKeys) {
      const n = visible.find((x) => x.id === id);
      if (n) out.push({ id, keys: keysOf(s, id), label: helpText(n.help).label, to: n.route, group: "go" });
    }
    for (const [id] of createKeys) {
      const r = routes.find((x) => x.id === id);
      if (r && mayOpen(nav, session, r.path)) out.push({ id, keys: keysOf(s, id), label: helpText(r.help).label, to: r.path, group: "create" });
    }
  }
  return s.on ? out : [];
}

// normalise reads keys as a person types them in the sheet: "G B", "g then b", "gb" and "G, B" are all "gb".
export function normalise(typed: string): string {
  return typed
    .toLowerCase()
    .replace(/\bthen\b/g, "")
    .replace(/[\s,+]+/g, "");
}

// spell shows keys as the sheet writes them: "gb" is G then B.
export function spell(keys: string): string[] {
  return [...keys].map((k) => k.toUpperCase());
}

// problems checks a person's keys (by shortcut ID, the usual ones for the rest): one or two keys each, a letter,
// digit or mark; no two shortcuts on the same keys; and no shortcut on a key that starts another's two, which would
// never wait for the second.
export function problems(keys: Record<string, string>, labels: Record<string, string> = {}): Record<string, string> {
  const all = { ...usual, ...keys };
  const name = (id: string) => labels[id] ?? id;
  const out: Record<string, string> = {};
  for (const [id, k] of Object.entries(keys)) {
    if (!/^[^\s]{1,2}$/u.test(k)) {
      out[id] = "Type one or two keys, such as G B.";
      continue;
    }
    for (const [other, o] of Object.entries(all)) {
      if (other === id) continue;
      if (o === k) out[id] = `${spell(k).join(" then ")} is already ${name(other)}: choose other keys.`;
      else if (k.length === 1 && o.startsWith(k)) out[id] = `${spell(k)[0]} starts ${spell(o).join(" then ")} (${name(other)}): choose two keys, or another key.`;
      else if (o.length === 1 && k.startsWith(o)) out[id] = `${spell(o)[0]} alone is ${name(other)}: start with another key.`;
      if (out[id]) break;
    }
  }
  return out;
}

// The person's settings, kept per user in this browser (see the note at the top).
const prefix = "fyniq.shortcuts.";
const listeners = new Set<() => void>();
const cache = new Map<string, ShortcutSettings>();

// parse keeps only well-formed settings, so a stale or edited value never breaks the keyboard.
export function parse(raw: string | null): ShortcutSettings {
  try {
    const v: unknown = raw ? JSON.parse(raw) : null;
    if (!v || typeof v !== "object") return defaultSettings;
    const o = v as { on?: unknown; keys?: unknown };
    const keys: Record<string, string> = {};
    if (o.keys && typeof o.keys === "object") {
      for (const [id, k] of Object.entries(o.keys as Record<string, unknown>)) if (id in usual && typeof k === "string") keys[id] = k;
    }
    return { on: o.on !== false, keys: Object.keys(problems(keys)).length ? {} : keys };
  } catch {
    return defaultSettings;
  }
}

export function shortcutSettings(user: string): ShortcutSettings {
  let s = cache.get(user);
  if (!s) {
    let raw: string | null = null;
    try {
      raw = window.localStorage.getItem(prefix + user);
    } catch {
      // storage may be off: the usual keys apply
    }
    s = parse(raw);
    cache.set(user, s);
  }
  return s;
}

// setShortcutSettings is the person's choice: it applies at once, and is kept on the server.
export function setShortcutSettings(user: string, s: ShortcutSettings): void {
  applyShortcutSettings(user, s);
  persist({ shortcuts_on: s.on, shortcut_keys: s.keys });
}

// applyShortcutSettings applies settings without sending them: the person's choice as the server has it.
export function applyShortcutSettings(user: string, s: ShortcutSettings): void {
  cache.set(user, s);
  try {
    window.localStorage.setItem(prefix + user, JSON.stringify(s));
  } catch {
    // storage may be off; the choice still applies to this page
  }
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useShortcutSettings(user: string): ShortcutSettings {
  return useSyncExternalStore(subscribe, () => shortcutSettings(user));
}

// The sheet opens from the keyboard, the command palette and the profile page.
const sheetListeners = new Set<() => void>();

export function openShortcutSheet(): void {
  for (const l of sheetListeners) l();
}

export function onShortcutSheet(l: () => void): () => void {
  sheetListeners.add(l);
  return () => sheetListeners.delete(l);
}

// Inputs that take no typing: a shortcut fires there, and Enter keeps its own meaning.
const notText = new Set(["checkbox", "radio", "button", "submit", "reset", "file", "range", "color", "image"]);

// typing says whether focus is in a field a person types into, where no shortcut fires (SC 2.1.4).
export function typing(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  if (t.isContentEditable) return true;
  if (t instanceof HTMLInputElement) return !notText.has(t.type);
  return t instanceof HTMLTextAreaElement || t instanceof HTMLSelectElement || ["textbox", "combobox", "searchbox", "spinbutton"].includes(t.getAttribute("role") ?? "");
}

// keyOf is the key a shortcut may take from an event: none with Ctrl, Alt or ⌘ held, while typing in a field or in a
// dialog, or when the page already handled the key (the Inbox's own keys).
export function keyOf(e: KeyboardEvent): string | null {
  if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.isComposing || e.repeat) return null;
  if (e.key.length !== 1 || e.key === " ") return null;
  if (typing(e.target) || document.querySelector('[role="dialog"], [role="alertdialog"]')) return null;
  return e.key.toLowerCase();
}

// wait is how long the second key of two may follow the first.
export const wait = 1500;

// matcher follows the keys a person types: a shortcut when they make one, "wait" when they start one, and nothing
// otherwise. A key that starts no shortcut after a first key starts again from itself.
export function matcher(): (key: string, at: number, list: readonly Shortcut[]) => Shortcut | "wait" | null {
  let typed = "";
  let since = 0;
  return (key, at, list) => {
    if (at - since > wait) typed = "";
    since = at;
    for (const seq of typed ? [typed + key, key] : [key]) {
      const hit = list.find((s) => s.keys === seq);
      if (hit) {
        typed = "";
        return hit;
      }
      if (list.some((s) => s.keys.startsWith(seq))) {
        typed = seq;
        return "wait";
      }
    }
    typed = "";
    return null;
  };
}

// enterToNext makes Enter move to the next field in an entry form (data-enter="next"), and Ctrl+Enter or ⌘Enter save
// it. A list open under a field, a text area, a button and the last field keep Enter's own meaning.
export function enterToNext(e: KeyboardEvent): void {
  if (e.key !== "Enter" || e.defaultPrevented || e.isComposing || e.altKey || e.shiftKey) return;
  const t = e.target;
  if (!(t instanceof HTMLInputElement || t instanceof HTMLSelectElement)) return;
  const form = t.form;
  if (!form || form.dataset.enter !== "next") return;
  if (e.ctrlKey || e.metaKey) {
    e.preventDefault();
    form.requestSubmit();
    return;
  }
  if ((t instanceof HTMLInputElement && notText.has(t.type)) || t.getAttribute("aria-expanded") === "true") return;
  const fields = [...form.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>("input, select, textarea")].filter(
    (f) => !f.disabled && f.type !== "hidden" && !(f instanceof HTMLInputElement && (f.readOnly || notText.has(f.type))) && !f.closest("[hidden], [inert]"),
  );
  for (const f of fields.slice(fields.indexOf(t) + 1)) {
    f.focus();
    if (document.activeElement === f) {
      e.preventDefault();
      if (f instanceof HTMLInputElement) f.select();
      return;
    }
  }
}
