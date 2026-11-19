// Nemura's browser journeys (p1-nemura-platform 7.3): Playwright in the installed Chrome against Nemura's own stack (`scripts/test-journeys-people.sh`:
// only the `people` profile, so no Fyniq service runs), through its own origin.
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  outputDir: "../test-results/journeys",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  timeout: 120_000,
  workers: 1,
  use: { baseURL: process.env.NEMURA_ORIGIN ?? "http://localhost:8180", trace: "retain-on-failure", actionTimeout: 15_000 },
  projects: [{ name: "chrome", use: { ...devices["Desktop Chrome"], channel: "chrome" } }],
});
