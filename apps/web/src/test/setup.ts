import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

// jsdom has no matchMedia: the tests set what the operating system "prefers" through media().
const prefers = new Map<string, boolean>();

export function media(query: string, matches: boolean): void {
  prefers.set(query, matches);
}

// jsdom lays nothing out: the menus and the command palette only need these to exist.
class NoResize {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= NoResize as unknown as typeof ResizeObserver;
Element.prototype.scrollIntoView ??= () => {};

beforeEach(() => {
  prefers.clear();
  window.localStorage.clear();
  window.matchMedia = (query: string) =>
    ({
      matches: prefers.get(query) ?? false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
});

afterEach(() => {
  cleanup();
  document.documentElement.className = "";
  for (const k of Object.keys(document.documentElement.dataset)) delete document.documentElement.dataset[k];
});
