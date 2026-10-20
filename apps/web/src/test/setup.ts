import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";
import { forgetMode } from "../mode";
import { answer, fakeFetch } from "./api";

// The generated client takes fetch and Request when it is created: point them at the fake API, and
// let a request name a path on this origin as the browser does.
const NodeRequest = globalThis.Request;
globalThis.Request = class extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === "string" && input.startsWith("/") ? new URL(input, "http://localhost").href : input, init);
  }
} as typeof Request;
globalThis.fetch = fakeFetch as typeof fetch;

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
  answer({});
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
  forgetMode();
  document.documentElement.className = "";
  for (const k of Object.keys(document.documentElement.dataset)) delete document.documentElement.dataset[k];
});
