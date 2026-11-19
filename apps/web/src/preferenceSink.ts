import type { components } from "./api/schema";

// Where a person's own choices go to be kept on the server (FR-UX-23): the mode, the view, the look and the shortcuts
// call persist() when the person changes one, and preferences.ts, once a person is signed in, sends it. Kept apart so
// those modules need not import each other.

export type PreferenceChange = components["schemas"]["PreferenceChange"];

let sender: ((c: PreferenceChange) => void) | null = null;

export function setPreferenceSender(f: ((c: PreferenceChange) => void) | null): void {
  sender = f;
}

// persist hands a choice to the server's copy. Before sign-in nothing is sent: the browser's own copy is all there is.
export function persist(c: PreferenceChange): void {
  sender?.(c);
}
