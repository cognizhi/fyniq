import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { textKeys, t } from "./i18n";

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    if (statSync(p).isDirectory()) return n === "test" || n === "api" ? [] : sources(p);
    return /\.(ts|tsx|css)$/.test(n) && !/\.test\./.test(n) ? [p] : [];
  });
}
const files = sources("src");
const code = files.filter((f) => /\.tsx?$/.test(f));

// req: NM-NFR-04
describe("localisation", () => {
  it("has no string a person reads in a component: every one comes from the catalogue", () => {
    const found: string[] = [];
    for (const f of code.filter((f) => f.endsWith(".tsx"))) {
      const src = readFileSync(f, "utf8");
      // Text between tags that holds a word, and the attributes a person reads.
      for (const m of src.matchAll(/>\s*([^<>{}\n]*[A-Za-z]{2,}[^<>{}\n]*)\s*</g)) found.push(`${f}: text "${m[1]?.trim()}"`);
      for (const m of src.matchAll(/\b(title|aria-label|placeholder|alt)="([^"{]*[A-Za-z]{2,}[^"]*)"/g)) found.push(`${f}: ${m[1]}="${m[2]}"`);
    }
    expect(found).toEqual([]);
  });

  it("fills placeholders and refuses a key the catalogue does not have", () => {
    expect(t("signin.continue", { provider: "Google" })).toBe("Continue with Google");
    expect(() => t("no.such.key")).toThrow(/no text no.such.key/);
  });

  it("uses every key it has and has every key it uses", () => {
    const used = new Set<string>();
    for (const f of code) for (const m of readFileSync(f, "utf8").matchAll(/"([a-z]+\.[\w.]+)"/g)) used.add(m[1] ?? ""); // a key, passed to t() directly or through a table
    for (const f of code) for (const m of readFileSync(f, "utf8").matchAll(/\bt\("([\w.]+)"/g)) expect(textKeys, m[1]).toContain(m[1]);
    const dynamic = ["role.", "join.title.", "join.lead.", "join.code.", "signin.error.", "value.", "feature.", "state.", "leave.day.", "calendar.kind.", "gov.scheme.", "gov.state.", "gov.event.", "requests.arrangement.", "requests.outcome.", "ot.kind.", "ot.branch.", "inputs.kind.", "inputs.unit.", "accrual.state.", "letters.state.", "tpl.block.", "terms.period."]; // looked up by the person's role, the form's kind or the refusal's code
    expect(textKeys.filter((k) => !used.has(k) && !dynamic.some((d) => k.startsWith(d)))).toEqual([]);
  });
});

// req: NM-UX-01
describe("the design tokens", () => {
  it("uses semantic tokens only: no colour literal in a component or the app's stylesheet", () => {
    const found: string[] = [];
    for (const f of files) for (const m of readFileSync(f, "utf8").matchAll(/#[0-9a-fA-F]{3,8}\b|\b(?:rgb|hsl|oklch|lab|lch)a?\(/g)) found.push(`${f}: ${m[0]}`);
    expect(found).toEqual([]);
  });

  it("requests nothing from another origin: no third-party font, script or style", () => {
    const found: string[] = [];
    for (const f of [...files, "index.html", "vite.config.ts"]) for (const m of readFileSync(f, "utf8").matchAll(/https?:\/\/[^\s"')]+/g)) if (!/localhost|127\.0\.0\.1|w3\.org/.test(m[0])) found.push(`${f}: ${m[0]}`);
    expect(found).toEqual([]);
    const kit = readFileSync("../../docs/design/screens/kit/nemura-ui.css", "utf8");
    expect(kit).not.toMatch(/url\(\s*["']?https?:/);
    expect(kit).not.toMatch(/@import\s+url\(\s*["']?https?:/);
  });

  it("builds on Nemura's tokens in four themes, violet only for AI", () => {
    const tokens = readFileSync("../../docs/design-system/nemura/tokens.css", "utf8");
    for (const theme of [":root {", ".dark {", '[data-contrast="more"] {', '.dark[data-contrast="more"] {']) expect(tokens, theme).toContain(theme);
    expect(tokens).toMatch(/--ai:/);
    const ours = files.map((f) => readFileSync(f, "utf8")).join("\n");
    expect(ours).not.toMatch(/var\(--ai[\w-]*\)/); // AI violet marks AI content, and this app has none yet
  });
});
