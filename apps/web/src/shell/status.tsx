import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

// One polite, atomic status region for the shell (FR-UX-21): badge and status changes are announced here as contextual
// phrases, "3 approvals waiting", never as bare numbers, and never move focus. Each source (a badge, the bell, the
// operations tray) says its phrase; a phrase that changes is announced, the first one a page loads with is not, so the
// page does not open with a list of counts. Outside the shell (a component on its own, as in its tests) a source draws
// a region of its own.

const Say = createContext<((phrase: string) => void) | null>(null);

export function StatusRegion({ children }: { children: ReactNode }) {
  const [text, setText] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const say = useCallback((phrase: string) => {
    // Empty first, so the same phrase said again is announced again.
    setText("");
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setText(phrase), 50);
  }, []);
  useEffect(() => () => void (timer.current && clearTimeout(timer.current)), []);
  return (
    <Say.Provider value={say}>
      {children}
      <div className="fy-sr" role="status" aria-live="polite" aria-atomic="true" data-testid="status-region">
        {text}
      </div>
    </Say.Provider>
  );
}

// useStatusPhrase says phrase when it changes. null is "not known yet": the first phrase after it is the page's own state
// and is not announced. In the shell it returns nothing to draw; outside it returns a region of its own showing the
// phrase, so the count is still heard where the component stands alone.
export function useStatusPhrase(phrase: string | null, className = "fy-sr"): ReactNode {
  const say = useContext(Say);
  const was = useRef<string | null>(null);
  useEffect(() => {
    if (say && was.current !== null && phrase !== null && was.current !== phrase && phrase !== "") say(phrase);
    if (phrase !== null) was.current = phrase;
  }, [say, phrase]);
  return say ? null : (
    <span className={className} role="status" aria-live="polite">
      {phrase ?? ""}
    </span>
  );
}

// useStatusPhrases is useStatusPhrase for a fixed list of sources, in one hook: each phrase is said when it changes.
export function useStatusPhrases(phrases: readonly (string | null)[]): void {
  const say = useContext(Say);
  const was = useRef<(string | null)[]>([]);
  const key = JSON.stringify(phrases);
  useEffect(() => {
    phrases.forEach((phrase, i) => {
      const before = was.current[i] ?? null;
      if (say && before !== null && phrase !== null && before !== phrase && phrase !== "") say(phrase);
      if (phrase !== null) was.current[i] = phrase;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed by the phrases' text
  }, [say, key]);
}
