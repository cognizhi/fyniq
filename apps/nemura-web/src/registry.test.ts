import { describe, expect, it } from "vitest";
import { guides } from "./help/guide";
import { hasHelpText } from "./i18n";
import { registry, routes, type Entry } from "./registry";
import { readFileSync } from "node:fs";

// req: NM-UX-02, NM-UX-04
export function problems(entries: readonly Entry[], pages = guides(), text: (key: string) => boolean = hasHelpText): string[] {
  const out: string[] = [];
  const ids = new Set<string>();
  for (const e of entries) {
    const where = `${e.kind} ${e.id || "(no id)"}`;
    if (!e.id) out.push(`${where}: no id`);
    if (ids.has(e.id)) out.push(`${where}: listed twice`);
    ids.add(e.id);
    if (e.mode !== "basic" && e.mode !== "expert") out.push(`${where}: no mode tag (basic or expert)`);
    if (!e.help) out.push(`${where}: no help key`);
    else if (!text(e.help)) out.push(`${where}: help key ${e.help} has no label and tooltip in src/i18n/en-SG.json`);
    const [page = "", anchor] = (e.guide ?? "").split("#");
    const g = pages.get(page);
    if (!page) out.push(`${where}: no guide page`);
    else if (!g) out.push(`${where}: no page in docs/user-guide/nemura/ has key: ${page}`);
    else if (anchor && !g.anchors.has(anchor)) out.push(`${where}: ${g.file} has no heading #${anchor}`);
  }
  return out;
}

describe("the feature registry", () => {
  it("tags every route and action with a mode, a help key with its text, and a guide page", () => {
    expect(problems(registry)).toEqual([]);
  });

  it("lists every page the app serves", () => {
    const app = readFileSync("src/App.tsx", "utf8");
    // The app serves the registry's routes, sign-in's own path and "/" (which sends a person to their landing); nothing else is a page.
    expect(app).not.toMatch(/path === "\/(?!sign-in"|join"|"\))/);
    expect(routes.map((r) => r.path).sort()).toEqual(["/join", "/me", "/me/leave", "/people", "/people/:id", "/people/:id/leave", "/people/directory", "/people/new", "/settings/features", "/settings/packs", "/settings/policy", "/sign-in"]);
  });

  it("fails an entry without a mode, help key, help text or guide page", () => {
    const bad = [
      { kind: "route", id: "untagged", path: "/x", signedIn: true, screen: "x", help: "home.me", guide: "nemura.home" },
      { kind: "action", id: "no-help", mode: "basic", help: "", guide: "nemura.home" },
      { kind: "action", id: "no-text", mode: "basic", help: "nowhere.key", guide: "nemura.home" },
      { kind: "action", id: "no-guide", mode: "expert", help: "home.me", guide: "" },
      { kind: "action", id: "lost-guide", mode: "expert", help: "home.me", guide: "no.such-page" },
      { kind: "action", id: "lost-heading", mode: "expert", help: "home.me", guide: "nemura.home#no-such-heading" },
    ] as unknown as Entry[];
    const found = problems(bad);
    for (const want of ["no mode tag", "no help key", "has no label and tooltip", "no guide page", "has key: no.such-page", "no heading #no-such-heading"]) {
      expect(found.some((p) => p.includes(want)), want).toBe(true);
    }
  });
});
