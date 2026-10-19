import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasHelpText } from "./i18n";
import { registry, type Entry } from "./registry";
import { router } from "./router";

// req: FR-UX-29
// req: FR-UX-31
// req: QA-UI-05
// The feature registry: every route and action is tagged basic or expert, has a help key whose
// tooltip text is in the catalogue, and a guide page in docs/user-guide/ (by its front-matter key,
// and the heading when the guide names one). The router has no page the registry does not list.

const guideDir = resolve(process.cwd(), "../../docs/user-guide"); // Vitest runs in apps/web

// slug turns a heading into its anchor, as GitHub and the knowledge base's citations do.
export function slug(heading: string): string {
  return heading
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .replace(/\s/g, "-");
}

interface Guide {
  file: string;
  anchors: Set<string>;
}

function guides(dir = guideDir): Map<string, Guide> {
  const out = new Map<string, Guide>();
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      for (const [k, g] of guides(path)) out.set(k, g);
      continue;
    }
    if (!name.endsWith(".md") || name === "README.md") continue;
    const text = readFileSync(path, "utf8");
    const key = /^---\n[\s\S]*?^key:\s*(\S+)/m.exec(text)?.[1];
    if (!key) continue;
    const anchors = new Set([...text.matchAll(/^#{1,6}\s+(.+)$/gm)].map((m) => slug(m[1] ?? "")));
    out.set(key, { file: path, anchors });
  }
  return out;
}

// problems lists what an entry is missing; an empty list means it is complete.
export function problems(entries: readonly Entry[], pages: Map<string, Guide>, text: (key: string) => boolean): string[] {
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
    else if (!g) out.push(`${where}: no page in docs/user-guide/ has key: ${page}`);
    else if (anchor && !g.anchors.has(anchor)) out.push(`${where}: ${g.file} has no heading #${anchor}`);
  }
  return out;
}

describe("the feature registry", () => {
  const pages = guides();

  it("tags every route and action with a mode, a help key with its text, and a guide page", () => {
    expect(problems(registry, pages, hasHelpText)).toEqual([]);
  });

  it("lists every page the router serves", () => {
    const listed = new Set(registry.flatMap((e) => (e.kind === "route" ? [e.path] : [])));
    const served = Object.keys(router.routesByPath);
    expect(served.length).toBeGreaterThan(0);
    expect(served.filter((p) => !listed.has(p))).toEqual([]);
    expect([...listed].filter((p) => !served.includes(p))).toEqual([]);
  });

  it("fails an entry without a mode, help key, help text or guide page", () => {
    const bad = [
      { kind: "route", id: "untagged", path: "/x", signedIn: true, help: "home.overview", guide: "home.overview" },
      { kind: "action", id: "no-help", mode: "basic", help: "", guide: "home.overview" },
      { kind: "action", id: "no-text", mode: "basic", help: "nowhere.key", guide: "home.overview" },
      { kind: "action", id: "no-guide", mode: "expert", help: "home.overview", guide: "" },
      { kind: "action", id: "lost-guide", mode: "expert", help: "home.overview", guide: "no.such-page" },
      { kind: "action", id: "lost-heading", mode: "expert", help: "home.overview", guide: "home.overview#no-such-heading" },
    ] as unknown as Entry[];
    const found = problems(bad, pages, hasHelpText);
    for (const want of ["no mode tag", "no help key", "has no label and tooltip", "no guide page", "has key: no.such-page", "no heading #no-such-heading"]) {
      expect(found.some((p) => p.includes(want)), want).toBe(true);
    }
  });
});
