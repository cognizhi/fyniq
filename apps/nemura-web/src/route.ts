// A path of the app and where to go from it: the app has few pages, so a small router on the History API serves them (no dependency).
export function navigate(to: string, replace = false): void {
  if (replace) window.history.replaceState(null, "", to);
  else window.history.pushState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
