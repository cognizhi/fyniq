import { Info } from "lucide-react";
import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Icon } from "../ui";

const WIDTH = 300; // the kit's .fy-popover width
const GUTTER = 16; // the page's side gutter at its narrowest

// HelpPopover is help beside what it explains (FR-UX-30; MASTER.md §12 "Info popover"): an ⓘ of 24 × 24 px beside a
// visible label, or a glossary term's own words (FR-UX-11), never in place of the label. It opens on hover and on
// keyboard focus, and stays while the pointer is over it; a click, a tap or Enter pins it; Esc closes it and puts focus
// back on what opened it, and so does a click outside a pinned one. The popover is a non-modal dialog named by its title
// and sits right after its button, so Tab reaches its "Learn more"; it moves left to stay in the window.
export function HelpPopover({ title, term, children, learnMore }: {
  title: string; // what it explains, as the label beside it names it
  term?: ReactNode; // a glossary term's words, which open it; an ⓘ when none
  children: ReactNode; // what it is, why it matters, an example; the accounting detail in Expert mode
  learnMore?: ReactNode; // the link to its guide page
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [pinned, setPinned] = useState(false);
  const [place, setPlace] = useState({ left: 0, width: WIDTH });
  const wrap = useRef<HTMLSpanElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const quiet = useRef(false); // focus coming back to the button on Esc does not open it again
  const close = (refocus: boolean) => {
    setOpen(false);
    setPinned(false);
    if (refocus && button.current && document.activeElement !== button.current) {
      quiet.current = true;
      button.current.focus();
    }
  };
  useLayoutEffect(() => {
    const el = wrap.current;
    if (!open || !el) return;
    const at = el.getBoundingClientRect().left;
    const width = Math.min(WIDTH, window.innerWidth - 2 * GUTTER);
    setPlace({ width, left: Math.min(0, window.innerWidth - GUTTER - width - at) });
  }, [open]);
  useEffect(() => {
    if (!pinned) return;
    const outside = (e: PointerEvent) => {
      if (!wrap.current?.contains(e.target as Node)) {
        setOpen(false);
        setPinned(false);
      }
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [pinned]);
  const keys = (e: KeyboardEvent) => {
    if (e.key === "Escape" && open) {
      e.stopPropagation();
      close(true);
    }
  };
  return (
    <span
      ref={wrap}
      className="relative inline-flex"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => !pinned && setOpen(false)}
      onFocus={() => {
        if (quiet.current) quiet.current = false;
        else setOpen(true);
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
          setOpen(false);
          setPinned(false);
        }
      }}
      onKeyDown={keys}
    >
      <button
        ref={button}
        className={term ? "fy-term" : "fy-info"}
        type="button"
        aria-label={term ? undefined : `About ${title}`}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => {
          setPinned(!pinned || !open);
          setOpen(!pinned || !open);
        }}
      >
        {term ?? <Icon of={Info} />}
      </button>
      {open && (
        <span id={id} className="fy-popover top-7 max-w-none font-normal" style={{ left: place.left, width: place.width }} role="dialog" aria-labelledby={`${id}-title`}>
          <span className="fy-strong text-foreground" id={`${id}-title`}>
            {title}
          </span>
          {children}
          {learnMore}
        </span>
      )}
    </span>
  );
}
