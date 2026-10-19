import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

// The visual-design rules that live in the sources rather than in a rendered page (PRD-001 §7.4;
// openspec/specs/experience/visual-design; MASTER.md §2, §3, §8). Rendering checks are in
// appearance.test.ts, shell/Header.test.tsx and components/ai/AiBadge.test.tsx.

const web = process.cwd(); // Vitest runs in apps/web
const repo = resolve(web, "../..");
const src = join(web, "src");
const read = (p: string) => readFileSync(p, "utf8");
const tokens = read(join(repo, "docs/design-system/fyniq/tokens.css"));
const globals = read(join(src, "styles/globals.css"));

function sources(dir = src): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name !== "api" && name !== "test") out.push(...sources(path)); // api: the generated client
    } else if (/\.(tsx?|css)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(path);
  }
  return out;
}

function offences(pattern: RegExp, allowed: (rel: string) => boolean = () => false): string[] {
  const out: string[] = [];
  for (const path of sources()) {
    const rel = relative(web, path);
    if (allowed(rel)) continue;
    read(path)
      .split("\n")
      .forEach((line, i) => {
        if (pattern.test(line)) out.push(`${rel}:${i + 1}: ${line.trim()}`);
      });
  }
  return out;
}

// block returns the declarations of the first rule whose selector is exactly sel.
function block(css: string, sel: string): string {
  const at = css.indexOf(`\n${sel} {`);
  if (at < 0) throw new Error(`tokens.css has no ${sel} block`);
  return css.slice(at, css.indexOf("\n}", at));
}

function oklch(css: string, name: string): { l: number; c: number; h: number } {
  const m = new RegExp(`--${name}:\\s*oklch\\(([\\d.]+) ([\\d.]+) ([\\d.]+)\\)`).exec(css);
  if (!m) throw new Error(`no oklch value for --${name}`);
  return { l: Number(m[1]), c: Number(m[2]), h: Number(m[3]) };
}

describe("visual design", () => {
  // req: experience/visual-design Design tokens
  it("uses no raw colour in the web app: every colour comes from a token", () => {
    expect(offences(/(?:^|[\s:"'`(,])#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb)\(/)).toEqual([]);
  });

  // req: experience/visual-design Design tokens
  it("has no Tailwind palette classes, and Tailwind's palette is switched off", () => {
    const palette =
      /\b(?:bg|text|border|ring|fill|stroke|from|via|to|outline|decoration|shadow|accent|caret|divide|placeholder)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|white|black)(?:-\d{2,3})?\b/;
    expect(offences(palette)).toEqual([]);
    expect(read(join(src, "styles/no-palette.css"))).toMatch(/--color-\*:\s*initial/);
    expect(globals.indexOf('@import "./no-palette.css"')).toBeLessThan(globals.indexOf('@import "../../../../docs/design-system/fyniq/tokens.css"'));
  });

  // req: experience/visual-design AI accent
  it("keeps violet for AI content: AI tokens appear only in src/components/ai/", () => {
    const ai = /\b(?:bg|text|border|ring|from|via|to|fill|stroke|outline|decoration|shadow)-ai(?:-[a-z-]+)?\b|var\(--ai\b|--ai-gradient/;
    expect(offences(ai, (rel) => rel.startsWith("src/components/ai/") || rel.startsWith("src/styles/"))).toEqual([]);
  });

  // req: experience/visual-design Brand accent
  it("uses the teal brand tokens for the primary action and the focus ring", () => {
    const button = read(join(src, "components/ui/button.tsx"));
    expect(button).toMatch(/primary: "bg-primary text-primary-foreground/);
    expect(globals).toMatch(/:focus-visible \{\s*outline: 2px solid var\(--ring\)/);
    expect(oklch(block(tokens, ":root"), "primary").h).toBe(190); // teal
    expect(oklch(block(tokens, ":root"), "ai").h).toBe(295); // violet, kept apart
  });

  // req: experience/visual-design Typography
  it("self-hosts Inter and JetBrains Mono with font-display: swap and makes no third-party font request", () => {
    const faces = [...globals.matchAll(/@font-face \{([^}]*)\}/g)].map((m) => m[1] ?? "");
    for (const family of ["Inter", "JetBrains Mono"]) {
      const mine = faces.filter((f) => f.includes(`font-family: "${family}"`));
      expect(mine.length, family).toBeGreaterThan(0);
      for (const f of mine) {
        expect(f).toMatch(/font-display: swap/);
        expect(f).toMatch(/src: url\("\.\.\/\.\.\/node_modules\/@fontsource-variable\//);
      }
    }
    expect(tokens).toMatch(/--font-ui: "Inter"/);
    expect(tokens).toMatch(/--font-code: "JetBrains Mono"/);
    const pages = [read(join(web, "index.html")), globals, tokens, ...sources().map(read)].join("\n");
    expect(pages).not.toMatch(/fonts\.googleapis\.com|fonts\.gstatic\.com|use\.typekit|fonts\.bunny/);
    expect(globals).not.toMatch(/url\(["']?https?:/);
  });

  // req: experience/visual-design Icons
  it("draws icons from Lucide only, hidden from assistive technology beside their text", () => {
    expect(offences(/from ["'](?:@phosphor-icons|react-icons|@heroicons|@tabler|@radix-ui\/react-icons|@mui\/icons)/)).toEqual([]);
    const ui = read(join(src, "ui.tsx"));
    expect(ui).toMatch(/aria-hidden="true"/);
    expect(ui).toMatch(/size\?: 16 \| 20 \| 24/);
  });

  // req: experience/visual-design Surfaces
  it("has warm paper-white light surfaces and layered blue-black dark surfaces, never pure black", () => {
    const light = block(tokens, ":root");
    const dark = block(tokens, ".dark");
    expect(oklch(light, "background").l).toBeGreaterThan(0.95);
    expect(oklch(light, "background").h).toBe(85); // warm
    const steps = ["background", "surface-1", "surface-2", "surface-3"].map((n) => oklch(dark, n));
    for (const s of steps) {
      expect(s.l).toBeGreaterThan(0.1); // not black
      expect(s.h).toBe(260); // blue-black "ink"
    }
    expect(steps.map((s) => s.l)).toEqual([...steps.map((s) => s.l)].sort((a, b) => a - b)); // layered
  });

  // req: experience/visual-design Themes
  it("defines light, dark, high-contrast and forced-colours themes", () => {
    for (const sel of [":root", ".dark", '[data-contrast="more"]', '.dark[data-contrast="more"]']) expect(block(tokens, sel)).toContain("--");
    expect(tokens).toMatch(/@media \(forced-colors: active\)/);
    expect(globals).toMatch(/@media \(forced-colors: active\) \{\s*:focus-visible \{\s*outline: 2px solid Highlight/);
  });

  // req: experience/visual-design Density
  it("has 40 px comfortable and 32 px compact rows on the 4 px grid", () => {
    expect(block(tokens, ":root")).toMatch(/--row-height: 40px/);
    expect(block(tokens, '[data-density="compact"]')).toMatch(/--row-height: 32px/);
    for (const m of block(tokens, ":root").matchAll(/--space-\d: (\d+)px/g)) expect(Number(m[1]) % 4).toBe(0);
  });

  // req: experience/visual-design Motion
  // req: FR-UX-38
  it("keeps durations at 120, 180 and 240 ms and sets them to 0 when motion is reduced or off", () => {
    const root = block(tokens, ":root");
    expect(root).toMatch(/--duration-fast: 120ms/);
    expect(root).toMatch(/--duration-base: 180ms/);
    expect(root).toMatch(/--duration-slow: 240ms/);
    expect(tokens).toMatch(/\[data-motion="reduce"\],\s*\[data-motion="off"\] \{[^}]*--duration-base: 0ms/);
    expect(tokens).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*:root \{[^}]*--duration-base: 0ms/);
  });

  // req: experience/visual-design Design system files
  it("keeps the design system files in docs/design-system/fyniq/", () => {
    for (const f of ["MASTER.md", "tokens.css", "scripts/check_contrast.py"]) expect(existsSync(join(repo, "docs/design-system/fyniq", f)), f).toBe(true);
    expect(readdirSync(join(repo, "docs/design-system/fyniq/pages")).some((f) => f.endsWith(".md"))).toBe(true);
  });

  // req: experience/visual-design Screen designs
  it("builds the shell from the designed screens in the inventory", () => {
    const inventory = JSON.parse(read(join(repo, "docs/design/screens/inventory.json"))) as { screens: { id: string }[] };
    const ids = new Set(inventory.screens.map((s) => s.id));
    for (const id of ["app-shell", "appearance-motion", "command-palette"]) expect(ids.has(id), id).toBe(true);
  });
});
