import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

// FR-UX-22, no optimistic money: what the ledger derives is shown only from the server's answers. The query cache is
// written only with an answer the server gave, which each write names on its line ("the server's answer (FR-UX-22)"),
// and never before the answer comes: no onMutate writes it. Other state, such as a dialog open or a filter, is React
// state, outside the cache.

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "test" || name === "api" ? [] : sources(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });
}

// problems lists each cache write in text (of file) that does not name the server's answer, and each onMutate that
// writes the cache.
export function problems(file: string, text: string): string[] {
  const out: string[] = [];
  text.split("\n").forEach((line, i) => {
    if (/\.setQue(ry|ries)Data\(/.test(line) && !line.includes("the server's answer (FR-UX-22)")) {
      out.push(`${file}:${i + 1}: a cache write that does not name the server's answer`);
    }
  });
  for (const m of text.matchAll(/onMutate\s*:\s*(?:async\s*)?\([^)]*\)\s*=>\s*\{?([\s\S]{0,400}?)(?:\n\s{0,6}\}|,\n)/g)) {
    if (/setQue(ry|ries)Data|setData\(/.test(m[1] ?? "")) out.push(`${file}: an onMutate writes the cache before the server answers`);
  }
  return out;
}

// req: FR-UX-22
// req: experience/navigation Ledger figure after a change
// req: experience/navigation Optimistic UI state
describe("no optimistic money", () => {
  it("writes the query cache only with the server's answers, never in onMutate", () => {
    const root = resolve(__dirname);
    const found = sources(root).flatMap((path) => problems(path.slice(root.length + 1), readFileSync(path, "utf8")));
    expect(found).toEqual([]);
  });

  it("finds a cache write that names no answer, and an onMutate that writes the cache", () => {
    expect(problems("x.tsx", 'queries.setQueryData(["invoice", id], { total: "1.00" });')).toHaveLength(1);
    expect(problems("x.tsx", "useMutation({\n  onMutate: () => {\n    queries.setQueryData(['k'], next); // the server's answer (FR-UX-22)\n  },\n})")).toHaveLength(1);
    expect(problems("x.tsx", "useMutation({\n  onMutate: () => setProblem(null),\n})")).toEqual([]);
    expect(problems("x.tsx", 'queries.setQueryData(["k"], out.data); // the server\'s answer (FR-UX-22)')).toEqual([]);
  });
});
