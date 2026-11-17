import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

// FR-VIZ-05, status colours are reserved and never alone: a chip in a status tone (good, warning, serious, critical)
// carries an icon beside its label, so its meaning never rests on colour (MASTER.md: ✔ circle-check, ⚠ triangle-alert,
// ⏰ clock-alert, ✖ circle-x).

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "test" || name === "api" ? [] : sources(path);
    return /\.tsx$/.test(name) && !/\.(test|stories)\.tsx$/.test(name) ? [path] : [];
  });
}

// problems lists each element of text (of file) whose class names a status tone of fy-chip and whose content, to its
// closing tag, holds no Icon.
export function problems(file: string, text: string): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(/<(span|div|li|p)\b[^>]*fy-chip--(?:good|warning|serious|critical)\b[^>]*>/g)) {
    const at = (m.index ?? 0) + m[0].length;
    const end = text.indexOf(`</${m[1]}>`, at);
    const body = text.slice(at, end < 0 ? undefined : end);
    if (!/<Icon\b/.test(body)) out.push(`${file}:${text.slice(0, m.index).split("\n").length}: a status chip without an icon`);
  }
  return out;
}

// req: FR-VIZ-05
describe("status colours", () => {
  it("pair every status chip with an icon", () => {
    const root = resolve(__dirname);
    const found = sources(root).flatMap((path) => problems(path.slice(root.length + 1), readFileSync(path, "utf8")));
    expect(found).toEqual([]);
  });

  it("finds a status chip with its label alone, and passes one with its icon or in a neutral tone", () => {
    expect(problems("x.tsx", '<span className="fy-chip fy-chip--good">Done</span>')).toHaveLength(1);
    expect(problems("x.tsx", '<span className={ok ? "fy-chip fy-chip--good" : "fy-chip fy-chip--critical"}>{ok ? "OK" : "Failing"}</span>')).toHaveLength(1);
    expect(problems("x.tsx", '<span className="fy-chip fy-chip--warning">\n  <Icon of={TriangleAlert} />\n  Review\n</span>')).toEqual([]);
    expect(problems("x.tsx", '<span className="fy-chip fy-chip--neutral">Draft</span>')).toEqual([]);
  });
});
