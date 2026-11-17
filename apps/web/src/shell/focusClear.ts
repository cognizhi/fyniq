// installFocusClear keeps a focused control clear of the sticky header and the compact bottom bar (NFR-A11Y-03, WCAG
// 2.2 SC 2.4.11). The kit's scroll padding does it where the browser honours scroll-padding as focus moves; WebKit does
// not always, so once focus moves by the keyboard this scrolls the page by whatever of the control is still under either
// bar. Focus a pointer gives is left alone: scrolling between the press and the release would move the control from
// under the pointer, and the click would miss it.
export function installFocusClear(doc: Document = document): () => void {
  const view = doc.defaultView;
  if (!view) return () => undefined;
  const barBox = (selector: string): DOMRect | null => {
    const bar = doc.querySelector(selector);
    if (!bar) return null;
    const s = view.getComputedStyle(bar);
    return s.display === "none" || s.visibility === "hidden" ? null : bar.getBoundingClientRect();
  };
  let keyboard = false;
  const onKey = () => {
    keyboard = true;
  };
  const onPointer = () => {
    keyboard = false;
  };
  const onFocus = (e: FocusEvent) => {
    if (!keyboard) return;
    const el = e.target;
    if (!(el instanceof HTMLElement) || el.closest(".fy-top, .fy-bottom, .fy-ai, [role='dialog']")) return;
    const clear = () => {
      const r = el.getBoundingClientRect();
      const top = barBox(".fy-top")?.bottom ?? 0;
      const bottom = barBox(".fy-bottom")?.top ?? view.innerHeight;
      if (r.height === 0 || r.height > bottom - top) return; // hidden, or taller than the room between the bars
      const gap = 16;
      if (r.bottom > bottom) view.scrollBy({ top: r.bottom - bottom + gap, behavior: "instant" });
      else if (r.top < top) view.scrollBy({ top: r.top - top - gap, behavior: "instant" });
    };
    clear();
    view.requestAnimationFrame(clear); // after the browser's own scroll to the focused control
  };
  doc.addEventListener("keydown", onKey, true);
  doc.addEventListener("pointerdown", onPointer, true);
  doc.addEventListener("focusin", onFocus);
  return () => {
    doc.removeEventListener("keydown", onKey, true);
    doc.removeEventListener("pointerdown", onPointer, true);
    doc.removeEventListener("focusin", onFocus);
  };
}
