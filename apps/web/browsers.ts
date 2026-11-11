// The browsers Fyniq supports (NFR-BROWSER-01): the last two versions of Chrome, Edge, Safari and Firefox, and iOS
// Safari and Android Chrome for the installed app. Vite's build target and the Playwright projects both read this, and
// src/browsers.test.ts keeps them in step (p1-operations-dr design D8).

// The family of each supported browser, as people name it, with its engine (what Playwright tests it in) and the build
// target the bundle must still run on: no newer than the oldest of the last two versions, so nothing newer is shipped
// unlowered [verify the versions each release: Chrome and Edge ship every four weeks, Firefox every four, Safari each
// autumn, numbered by year since 26].
export const supported = [
  { browser: "Chrome", engine: "chromium", target: "chrome130" },
  { browser: "Edge", engine: "chromium", target: "edge130" },
  { browser: "Firefox", engine: "firefox", target: "firefox130" },
  { browser: "Safari", engine: "webkit", target: "safari18" },
  { browser: "iOS Safari", engine: "webkit", target: "ios18" },
  { browser: "Android Chrome", engine: "chromium", target: "chrome130" },
] as const;

export type Engine = (typeof supported)[number]["engine"];

// buildTarget is Vite's build.target: each supported browser's oldest version, once.
export const buildTarget: string[] = [...new Set(supported.map((b) => b.target))];

// engines are the Playwright projects that test them.
export const engines: Engine[] = [...new Set(supported.map((b) => b.engine))];
