// The browser journeys (p1-launch-readiness task 5.1, design D6): Playwright against the stack `make up` runs, through
// Caddy's one origin, each journey in an organisation of its own with the sample company loaded, its people signed in
// through the local sign-in page, and each step checked on the screen and in what the API then returns. `make
// test-journeys` starts the stack when none answers. The installed Chrome runs them, as the smoke test's does.
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  outputDir: "../test-results/journeys", // traces of a failed journey; CI keeps them
  globalSetup: "./setup.ts",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  timeout: 180_000,
  workers: 1, // one stack: the journeys run one after another
  use: { baseURL: `http://localhost:${process.env.FYNIQ_HTTP_PORT ?? "8080"}`, trace: "retain-on-failure", actionTimeout: 15_000 },
  projects: [{ name: "chrome", use: { ...devices["Desktop Chrome"], channel: "chrome" } }],
});
