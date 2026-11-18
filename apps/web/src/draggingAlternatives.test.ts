import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

// NFR-A11Y-04, dragging alternatives (WCAG 2.2 SC 2.5.7): nothing is done only by dragging. A drop zone for files also
// offers the file chooser; a drag that moves or resizes something (fill-down, column resize, tile layout, a split
// view's divider) names its single-pointer and keyboard alternative here, beside the file that has it.
const alternatives: Record<string, string> = {
  // none yet: the grid, the tile layout and the split view bring theirs with them
};

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "test" || name === "api" ? [] : sources(path);
    return /\.tsx$/.test(name) && !/\.(test|stories)\.tsx$/.test(name) ? [path] : [];
  });
}

// problems lists a drop zone with no file chooser, and a drag that moves or resizes with no alternative named.
export function problems(file: string, text: string, named: Record<string, string> = alternatives): string[] {
  const out: string[] = [];
  if (/onDrop=/.test(text) && !/type="file"/.test(text)) out.push(`${file}: a drop zone with no file chooser beside it`);
  if (/draggable|onDragStart=|onPointerMove=|onMouseMove=/.test(text) && !named[file]) {
    out.push(`${file}: a drag that moves or resizes, with no single-pointer and keyboard alternative named in draggingAlternatives.test.ts`);
  }
  return out;
}

// req: NFR-A11Y-04
// req: experience/accessibility-and-performance NFR-A11Y-04 Dragging alternatives
describe("dragging alternatives", () => {
  it("does nothing only by dragging", () => {
    const root = resolve(__dirname);
    const found = sources(root).flatMap((path) => problems(path.slice(root.length + 1), readFileSync(path, "utf8")));
    expect(found).toEqual([]);
  });

  it("finds a drop zone without a chooser, and a drag without its alternative", () => {
    expect(problems("a.tsx", "<div onDrop={drop} />")).toHaveLength(1);
    expect(problems("a.tsx", '<div onDrop={drop}><input type="file" /></div>')).toEqual([]);
    expect(problems("b.tsx", "<th onPointerMove={resize} />")).toHaveLength(1);
    expect(problems("b.tsx", "<th onPointerMove={resize} />", { "b.tsx": "Ctrl+Left and Right, and the column menu's width" })).toEqual([]);
  });
});
