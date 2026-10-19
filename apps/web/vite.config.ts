// The web app (PRD-001 §12.2: React 19 + Vite; ADR-T1: TanStack Router and Query, shadcn/ui on
// Base UI, Tailwind CSS 4 from the design tokens, cmdk and the React Compiler). In `make dev`, /api
// goes to the local api service; in staging and production one origin serves both (Caddy locally,
// the load balancer in the cloud). Vitest reads the same file (`pnpm --filter @fyniq/web test`).
import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const api = `http://localhost:${process.env.FYNIQ_API_PORT ?? "8081"}`;
const proxy = { "/api": { target: api, rewrite: (p: string) => p.replace(/^\/api/, "") } };
const testing = process.env.VITEST === "true";

export default defineConfig({
  // The React Compiler memoises components at build time (ADR-T1). Vitest skips it: the tests check
  // behaviour, and the compiler only changes when React re-renders.
  plugins: [react(), ...(testing ? [] : [babel({ presets: [reactCompilerPreset()] })]), tailwindcss()],
  server: {
    port: Number(process.env.FYNIQ_WEB_PORT ?? 5173),
    strictPort: true,
    host: "127.0.0.1",
    proxy,
    fs: { allow: ["../.."] }, // tokens.css lives in docs/design-system/
  },
  preview: { port: 4173, strictPort: true, host: "127.0.0.1", proxy },
  build: {
    // NFR-PERF-UI-03: the shell's initial JavaScript stays small; heavy parts load on demand.
    reportCompressedSize: true,
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["src/test/setup.ts"],
    css: false,
    restoreMocks: true,
  },
});
