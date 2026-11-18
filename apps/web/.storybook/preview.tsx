// The stories render on the app's tokens and kit, in the theme the run names (VITE_STORY_THEME: light, dark, contrast or
// contrast-dark, as tokens.css switches them with .dark and data-contrast on <html>), and axe fails a story on any
// violation (QA-UI-02).
import type { Decorator, Preview } from "@storybook/react-vite";
import "../src/styles/globals.css";
import "../src/app.css";

export const themes = {
  light: { dark: false, contrast: false },
  dark: { dark: true, contrast: false },
  contrast: { dark: false, contrast: true },
  "contrast-dark": { dark: true, contrast: true },
} as const;
export type ThemeName = keyof typeof themes;

const themed: Decorator = (Story, { globals }) => {
  const t = themes[(globals.theme as ThemeName) ?? "light"] ?? themes.light;
  const root = document.documentElement;
  root.classList.toggle("dark", t.dark);
  if (t.contrast) root.dataset.contrast = "more";
  else delete root.dataset.contrast;
  root.lang = "en-SG";
  return (
    <main className="fy-story" style={{ padding: "1rem", background: "var(--background)", color: "var(--text-primary)" }}>
      <Story />
    </main>
  );
};

const preview: Preview = {
  globalTypes: {
    theme: {
      description: "The theme the story renders in",
      toolbar: { title: "Theme", items: Object.keys(themes), dynamicTitle: true },
    },
  },
  initialGlobals: { theme: (import.meta.env.VITE_STORY_THEME as ThemeName | undefined) ?? "light" },
  decorators: [themed],
  parameters: {
    a11y: { test: "error" },
    layout: "fullscreen",
  },
};

export default preview;
