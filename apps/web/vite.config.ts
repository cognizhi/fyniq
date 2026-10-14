// The web app (PRD-001 §12.2: React 19 + Vite). In `make dev`, /api goes to the local api
// service; in staging and production one origin serves both (Caddy locally, the load balancer
// in the cloud). The screens arrive in P1 from docs/design/screens/.
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const api = `http://localhost:${process.env.FYNIQ_API_PORT ?? "8081"}`;
const proxy = { "/api": { target: api, rewrite: (p: string) => p.replace(/^\/api/, "") } };

export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.FYNIQ_WEB_PORT ?? 5173),
    strictPort: true,
    host: "127.0.0.1",
    proxy,
    fs: { allow: ["../.."] }, // tokens.css lives in docs/design-system/
  },
  preview: { port: 4173, strictPort: true, host: "127.0.0.1", proxy },
});
