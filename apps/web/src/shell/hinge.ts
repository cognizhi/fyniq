import { useCallback, useEffect, useState } from "react";

// Two panes split at a hinge (FR-UX-37; MASTER.md §5.1): on a device whose screen spans a hinge, a list and its detail
// (.fy-listdetail) sit one on each side of it and nothing is placed across it. The browser reports the screen's parts
// as viewport segments (the Viewport Segments API: window.viewport.segments, and the media feature
// horizontal-viewport-segments). CSS alone knows where the hinge is, not where the panes start, so this sets on the
// element the list's width up to the hinge and the hinge's width as the gap (data-hinge="split"); app.css applies them
// while the screen spans two segments side by side.

interface Segments {
  segments?: readonly DOMRect[] | null;
}

// split is where a two-pane element divides: the list's width from the element's left edge to the hinge, and the
// hinge's width. Null when the screen is in one piece, or the element does not reach across the hinge.
export function split(segments: readonly { x: number; width: number }[] | null | undefined, left: number, width: number): { list: number; gap: number } | null {
  if (!segments || segments.length !== 2) return null;
  const [a, b] = [...segments].sort((p, q) => p.x - q.x) as [{ x: number; width: number }, { x: number; width: number }];
  const hinge = a.x + a.width;
  const gap = Math.max(0, b.x - hinge);
  if (left >= hinge || left + width <= b.x) return null; // wholly on one side
  return { list: Math.max(0, Math.floor(hinge - left)), gap: Math.ceil(gap) };
}

function segments(): readonly DOMRect[] | null {
  return (window as unknown as { viewport?: Segments }).viewport?.segments ?? null;
}

// useHingeSplit is the ref of a .fy-listdetail element: it keeps the element's split at the hinge as the screen folds,
// unfolds or rotates.
export function useHingeSplit<T extends HTMLElement>(): (el: T | null) => void {
  const [el, setEl] = useState<T | null>(null);
  useEffect(() => {
    if (!el) return;
    const place = () => {
      const box = el.getBoundingClientRect();
      const s = split(segments(), box.left + el.clientLeft, box.width); // the grid starts inside the border
      if (s) {
        el.dataset.hinge = "split";
        el.style.setProperty("--hinge-list", `${s.list}px`);
        el.style.setProperty("--hinge-gap", `${s.gap}px`);
      } else {
        delete el.dataset.hinge;
        el.style.removeProperty("--hinge-list");
        el.style.removeProperty("--hinge-gap");
      }
    };
    place();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, [el]);
  return useCallback((node: T | null) => setEl(node), []);
}
