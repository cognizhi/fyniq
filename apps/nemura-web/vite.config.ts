// Nemura's web app (PRD-002 NM-UX-01, ADR-N1): React 19 and Vite on Nemura's own design tokens. In `make dev` /api goes to the local nemura api
// service; behind Nemura's edge, and in the image, one origin serves both (the binary embeds this build and serves it beside the API).
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const api = `http://localhost:${process.env.NEMURA_API_PORT ?? "8087"}`;
const proxy = { "/api": { target: api, rewrite: (p: string) => p.replace(/^\/api/, "") } };

export default defineConfig({
  plugins: [react()],
  server: { port: Number(process.env.NEMURA_WEB_PORT ?? 5174), strictPort: true, host: "127.0.0.1", proxy, fs: { allow: ["../.."] } },
  preview: { port: 4174, strictPort: true, host: "127.0.0.1", proxy },
  build: { reportCompressedSize: true },
  test: { environment: "jsdom", include: ["src/**/*.test.{ts,tsx}"], setupFiles: ["src/test/setup.ts"], css: false, restoreMocks: true },
});
