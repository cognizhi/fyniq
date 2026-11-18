// The web smoke test (`make test-e2e`). Locally it uses the Chrome already installed (developer machines), so no
// browser download is needed; with FYNIQ_E2E_BROWSERS=all (CI) it runs in Firefox and WebKit too, one project per
// engine of the supported browsers (NFR-BROWSER-01, browsers.ts), which `playwright install` downloads.
import { defineConfig, devices } from "@playwright/test";
import { engines } from "./browsers.ts";

const all = process.env.FYNIQ_E2E_BROWSERS === "all";

// The project of each engine: Chromium is the installed Chrome; Firefox and WebKit are Playwright's own builds.
export const projects = {
  chromium: { name: "chrome", use: { ...devices["Desktop Chrome"], channel: "chrome" } },
  firefox: { name: "firefox", use: { ...devices["Desktop Firefox"] } },
  webkit: { name: "webkit", use: { ...devices["Desktop Safari"] } },
};

export default defineConfig({
  testDir: "e2e",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  use: { baseURL: "http://127.0.0.1:4173" },
  projects: engines.filter((e) => all || e === "chromium").map((e) => projects[e]),
  webServer: { command: "pnpm build && pnpm preview", url: "http://127.0.0.1:4173", reuseExistingServer: !process.env.CI, timeout: 120_000 },
});
