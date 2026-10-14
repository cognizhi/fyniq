// The web smoke test (`make test-e2e`). It uses the Chrome already installed (developer
// machines and GitHub's runners), so no browser download is needed.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  use: { baseURL: "http://127.0.0.1:4173", channel: "chrome" },
  webServer: { command: "pnpm build && pnpm preview", url: "http://127.0.0.1:4173", reuseExistingServer: !process.env.CI, timeout: 120_000 },
});
