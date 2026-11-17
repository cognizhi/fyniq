import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { Step } from "./steps";

// The first-run tour (FR-UX-34; MASTER.md §12 "Walkthrough anatomy"): a spotlight on each step's element of the real
// interface, with a caption card. It is non-modal: the scrim lets the pointer through and the page stays usable. The
// card is a dialog named by its title and described by its text; focus moves to the title when it starts, Next and Back
// work as buttons or with → and ← in the card, Esc closes it, each later step is announced politely, and on closing
// focus returns where it was. With reduced motion the steps jump (the tokens set --duration-tour to 0). It never clicks,
// types or changes data: onStep and onClose let its host save the person's progress, the tour's only write.

const PAD = 6; // the spotlight's margin round its element
const GAP = 12; // between the element and the card
const EDGE = 8; // the card keeps this far inside the window

function shown(el: Element | null): el is HTMLElement {
  if (!(el instanceof HTMLElement)) return false;
  const r = el.getBoundingClientRect();
  return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== "hidden";
}

// targetOf is the step's element the person can see: the first marked one that is shown, as the sidebar's and the
// bottom bar's items carry the same mark.
export function targetOf(step: Step): HTMLElement | null {
  return [...document.querySelectorAll(`[data-tour="${step.target}"]`)].find(shown) ?? null;
}

export function Tour({ steps, start, onStep, onClose }: {
  steps: readonly Step[]; // the person's variant; those whose element is not shown are skipped
  start: number;
  onStep: (at: number) => void;
  onClose: (how: "finished" | "skipped", at: number) => void;
}) {
  const id = useId();
  const [visible] = useState(() => steps.filter((s) => targetOf(s)));
  const [at, setAt] = useState(() => Math.max(0, Math.min(start, visible.length - 1)));
  const [said, setSaid] = useState("");
  const [box, setBox] = useState<{ spot: CSSProperties; card: CSSProperties }>({ spot: {}, card: {} });
  const back = useRef<Element | null>(document.activeElement);
  const card = useRef<HTMLDivElement>(null);
  const title = useRef<HTMLHeadingElement>(null);
  const first = useRef(true);
  const step = visible[at];

  const close = (how: "finished" | "skipped") => {
    onClose(how, at);
    // back where the person was, else to the page's main region (focusable, as the skip link's target)
    const to = back.current instanceof HTMLElement && document.contains(back.current) && shown(back.current) ? back.current : document.querySelector<HTMLElement>("#main");
    to?.focus();
  };

  useEffect(() => {
    if (visible.length === 0) onClose("skipped", 0);
  }, []); // once, when it has nothing to show

  // Place the spotlight on the element and the card below it, else above, else beside, inside the window.
  useLayoutEffect(() => {
    if (!step) return;
    const place = () => {
      const el = targetOf(step);
      if (!el) return;
      const r = el.getBoundingClientRect();
      const x = r.left - PAD;
      const y = r.top - PAD;
      const w = r.width + 2 * PAD;
      const h = r.height + 2 * PAD;
      const c = card.current?.getBoundingClientRect();
      const cw = c?.width ?? 340;
      const ch = c?.height ?? 200;
      let top: number;
      let left = x;
      if (y + h + GAP + ch <= innerHeight - EDGE) top = y + h + GAP;
      else if (y - GAP - ch >= EDGE) top = y - GAP - ch;
      else {
        top = Math.min(Math.max(EDGE, y), innerHeight - ch - EDGE);
        left = x + w + GAP + cw <= innerWidth - EDGE ? x + w + GAP : x - GAP - cw;
      }
      left = Math.min(Math.max(EDGE, left), innerWidth - cw - EDGE);
      setBox({
        spot: { transform: `translate(${x}px, ${y}px)`, width: w, height: h },
        card: { transform: `translate(${left}px, ${Math.max(EDGE, top)}px)` },
      });
    };
    targetOf(step)?.scrollIntoView({ block: "nearest" });
    place();
    addEventListener("resize", place);
    addEventListener("scroll", place, true);
    return () => {
      removeEventListener("resize", place);
      removeEventListener("scroll", place, true);
    };
  }, [step]);

  // Each step saved as it shows; the first takes focus, the later ones are announced.
  useEffect(() => {
    if (!step) return;
    onStep(at);
    if (first.current) {
      first.current = false;
      title.current?.focus();
    } else {
      setSaid(`Step ${at + 1} of ${visible.length}. ${step.title}. ${step.text}`);
    }
  }, [at]); // a step is saved once, when it shows

  useEffect(() => {
    const esc = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        close("skipped");
      }
    };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  });

  if (!step) return null;
  const last = at === visible.length - 1;
  const go = (d: 1 | -1) => {
    if (d === 1 && last) close("finished");
    else setAt(Math.max(0, Math.min(visible.length - 1, at + d)));
  };
  const keys = (e: KeyboardEvent) => {
    if (e.key === "ArrowRight") {
      e.preventDefault();
      go(1);
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      go(-1);
    }
  };
  const motion: CSSProperties = { position: "fixed", left: 0, top: 0, transition: "transform var(--duration-tour) var(--ease-out), width var(--duration-tour) var(--ease-out), height var(--duration-tour) var(--ease-out)" };
  return (
    <>
      <div className="fy-tour-spot" aria-hidden="true" style={{ ...motion, ...box.spot }} />
      <div ref={card} className="fy-tour-card" role="dialog" aria-labelledby={`${id}-title`} aria-describedby={`${id}-text`} style={{ ...motion, zIndex: "calc(var(--z-tour) + 1)", ...box.card }} onKeyDown={keys}>
        <span className="fy-small fy-secondary">
          {at + 1} of {visible.length}
        </span>
        <h2 className="fy-h3 outline-none" id={`${id}-title`} ref={title} tabIndex={-1}>
          {step.title}
        </h2>
        <p className="fy-small" id={`${id}-text`}>
          {step.text}
        </p>
        <span className="fy-dots" aria-hidden="true">
          {visible.map((s, i) => (
            <span key={s.id} data-on={i === at ? "" : undefined} />
          ))}
        </span>
        <div className="fy-row fy-row--between gap-2">
          <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => close("skipped")}>
            Skip tour
          </button>
          <span className="fy-row gap-2">
            <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => go(-1)} disabled={at === 0}>
              Back
            </button>
            <button type="button" className="fy-btn fy-btn--primary fy-btn--sm" onClick={() => go(1)}>
              {last ? "Done" : "Next"}
            </button>
          </span>
        </div>
      </div>
      <p className="fy-sr" role="status" aria-live="polite">
        {said}
      </p>
    </>
  );
}
