import { api, etagOf, newKey, type Session } from "./api/client";
import type { components } from "./api/schema";
import { applyAppearance, defaults as appearanceDefaults, theAppearance, type Appearance } from "./appearance";
import { setLocale } from "./format/locale";
import { applyMode, setStartingMode, theChosenMode } from "./mode";
import { persist, setPreferenceSender, type PreferenceChange } from "./preferenceSink";
import type { Mode } from "./registry";
import { applyShortcutSettings, defaultSettings, shortcutSettings } from "./shortcuts";
import { applyLedgerView, theChosenView } from "./view";

// A person's own preferences, kept on the server so they are the same on every device (FR-UX-23; p1-app-shell 4.1):
// the experience mode, the Plain or Accountant view, how the app looks, the keyboard shortcuts and the number and date
// formats. They are presentation only. The session carries them, so the first page is drawn as chosen; adopt() applies
// them, and each later choice is sent by the sender below, one save after another with the ETag the last one returned.
// The browser's own copy (local storage) stays what draws the sign-in page and the first paint.

export type MyPreferences = components["schemas"]["MyPreferences"];

let etag = "*";
let chain: Promise<unknown> = Promise.resolve();
let pending = 0;

// save sends one change. A stale save (another device saved first) reads the preferences again and sends the change
// once more: a change names only what it sets, so it merges with theirs.
async function save(change: PreferenceChange, again: boolean): Promise<void> {
  const { data, response } = await api.PATCH("/v1/preferences", { params: { header: { "If-Match": etag, "Idempotency-Key": newKey() } }, body: change });
  if (data) {
    etag = etagOf(response) || data.etag; // the server's answer (FR-UX-22)
    return;
  }
  if (response.status === 412 && again) {
    const read = await api.GET("/v1/preferences");
    if (read.data) {
      etag = etagOf(read.response) || read.data.etag;
      return save(change, false);
    }
  }
}

function send(change: PreferenceChange): void {
  pending++;
  chain = chain.then(() => save(change, true)).catch(() => undefined).finally(() => void pending--); // a refused save is dropped: the choice still applies here
}

const appearanceFields = ["theme", "contrast", "density", "motion"] as const;

// adopt applies what the session says about the person's preferences, and starts sending their later choices. A choice
// made in this browser before the server kept any (the server has none for it) is sent once, so it is not lost. It does
// nothing while a save is in flight: the answer would be older than the choice.
export function adopt(session: Pick<Session, "user_id" | "preferences" | "time_zone">): void {
  const prefs = session.preferences;
  if (!prefs || !session.user_id) {
    setPreferenceSender(null);
    setLocale({ timeZone: session.time_zone });
    return;
  }
  if (pending > 0) return;
  etag = prefs.etag;
  setPreferenceSender(send);
  setStartingMode(prefs.default_mode as Mode);
  setLocale({ number: prefs.number_format, date: prefs.date_format, timeZone: session.time_zone });

  const upload: PreferenceChange = {};
  if (prefs.mode) applyMode(prefs.mode as Mode);
  else if (theChosenMode()) upload.mode = theChosenMode()!;
  if (prefs.ledger_view) applyLedgerView(prefs.ledger_view);
  else if (theChosenView()) upload.ledger_view = theChosenView()!;
  const look: Partial<Appearance> = {};
  for (const f of appearanceFields) {
    if (prefs[f]) (look as Record<string, string>)[f] = prefs[f]!;
    else if (theAppearance()[f] !== appearanceDefaults[f]) (upload as Record<string, string>)[f] = theAppearance()[f];
  }
  applyAppearance(look);
  if (prefs.shortcuts_on !== undefined || prefs.shortcut_keys) {
    applyShortcutSettings(session.user_id, { on: prefs.shortcuts_on !== false, keys: prefs.shortcut_keys ?? {} });
  } else {
    const local = shortcutSettings(session.user_id);
    if (local.on !== defaultSettings.on || Object.keys(local.keys).length) {
      upload.shortcuts_on = local.on;
      upload.shortcut_keys = local.keys;
    }
  }
  if (Object.keys(upload).length) persist(upload);
}
