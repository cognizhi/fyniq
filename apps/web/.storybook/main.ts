// Storybook for the shared components (PRD-001 QA-UI-02; p1-launch-readiness design D7): every component of
// src/components/ has stories, which the a11y add-on checks with axe and the Vitest add-on runs as tests in a real
// browser, once per theme (make storybook). A developer tool, not a product screen: it never ships.
import type { StorybookConfig } from "@storybook/react-vite";

const config: StorybookConfig = {
  // FYNIQ_STORY_PLANTED runs the planted story alone, whose axe violation the run must fail on.
  stories: process.env.FYNIQ_STORY_PLANTED === "1" ? ["../src/test/planted/*.stories.tsx"] : ["../src/components/**/*.stories.tsx"],
  addons: ["@storybook/addon-a11y", "@storybook/addon-vitest"],
  framework: { name: "@storybook/react-vite", options: {} },
  core: { disableTelemetry: true },
};

export default config;
