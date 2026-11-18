// The visual snapshots (QA-UI-07, QA-RPT-04; p1-launch-readiness design D5): Playwright in its pinned Linux container
// against a stack of their own on a fixed business date, which `make test-visual` (scripts/visual.sh) starts and sets up.
// Each picture's SHA-256 is compared with visual/baselines.json; the pictures and hashes of a run are left in
// test-results/visual/.
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.ts",
  outputDir: "../test-results/visual-run",
  globalSetup: "./setup.ts",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  timeout: 600_000,
  workers: 1, // one stack, one organisation
  use: { baseURL: `http://localhost:${process.env.FYNIQ_HTTP_PORT ?? "8095"}`, actionTimeout: 30_000 },
  // The container's own Chromium: the same build, fonts and libraries on every run.
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
