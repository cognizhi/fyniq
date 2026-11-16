// The stories as tests (`make storybook`; p1-launch-readiness design D7): each story of .storybook/main.ts renders in
// headless Chromium and fails on any axe violation, in the theme VITE_STORY_THEME names. Kept apart from vite.config.ts,
// whose jsdom tests `make test` runs; Storybook runs in its own CI job.
import tailwindcss from "@tailwindcss/vite";
import { storybookTest } from "@storybook/addon-vitest/vitest-plugin";
import react from "@vitejs/plugin-react";
import { playwright } from "@vitest/browser-playwright";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react(), tailwindcss(), storybookTest({ configDir: ".storybook" })],
  server: { fs: { allow: ["../.."] } }, // tokens.css and the kit live in docs/
  test: {
    name: "storybook",
    // Google Chrome, as the Playwright journeys run in it (playwright.config.ts): nothing to download.
    browser: { enabled: true, headless: true, provider: playwright({ launchOptions: { channel: "chrome" } }), instances: [{ browser: "chromium" }] },
  },
});
