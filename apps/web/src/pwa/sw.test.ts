import { describe, expect, it } from "vitest";
import source from "../../public/sw.js?raw";

// The worker's rule is plain JavaScript in public/, so it is read as text and run with no browser around it.
const sw = (() => {
  const module = { exports: {} as { shellRequest: (method: string, url: string, origin: string) => boolean } };
  new Function("module", "self", source)(module, undefined);
  return module.exports;
})();
const origin = "https://fyniq.example";

// req: FR-UX-16
// asvs: V14.2.1
describe("the service worker", () => {
  it("keeps the app's own files and nothing else", () => {
    for (const path of ["/", "/index.html", "/assets/index-abc123.js", "/assets/framework-1.css", "/manifest.webmanifest", "/icon.svg"]) {
      expect(sw.shellRequest("GET", origin + path, origin), path).toBe(true);
    }
  });

  it("never touches the API, whatever the method, so no figure is stored and no write is replayed", () => {
    for (const path of ["/api/v1/approvals", "/api/v1/approvals/0192", "/api/auth/signout", "/api/v1/session", "/api"]) {
      expect(sw.shellRequest("GET", origin + path, origin), path).toBe(false);
    }
    for (const method of ["POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]) {
      expect(sw.shellRequest(method, origin + "/assets/index.js", origin), method).toBe(false);
      expect(sw.shellRequest(method, origin + "/api/v1/approvals/1/approve", origin), method).toBe(false);
    }
  });

  it("keeps nothing from another origin, and nothing that is not a shell file", () => {
    expect(sw.shellRequest("GET", "https://elsewhere.example/assets/x.js", origin)).toBe(false);
    expect(sw.shellRequest("GET", origin + "/inbox/secret.json", origin)).toBe(false);
  });

  it("has no handler for a write: its source names no method but GET", () => {
    expect(source).not.toMatch(/method\s*={2,3}\s*["'](POST|PUT|PATCH|DELETE)/);
    expect(source).not.toMatch(/\/api\/v1\/approvals\/[^"']*approve/);
  });
});
