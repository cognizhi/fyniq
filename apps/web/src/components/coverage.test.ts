import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { components, type Files, withoutStories, withoutTests } from "./coverage";

const root = resolve(process.cwd(), "src/components"); // Vitest runs in apps/web

function read(d: string, out: Files = {}): Files {
  for (const name of readdirSync(d)) {
    const p = join(d, name);
    if (statSync(p).isDirectory()) read(p, out);
    else if (name.endsWith(".tsx")) out[relative(root, p).split("\\").join("/")] = readFileSync(p, "utf8");
  }
  return out;
}

describe("the shared components", () => {
  const files = read(root);

  // req: QA-UI-02
  it("each have stories, which Storybook renders in light, dark and high contrast with axe (make storybook)", () => {
    expect(components(files).length).toBeGreaterThanOrEqual(6);
    expect(withoutStories(files)).toEqual([]);
  });

  // QA-UI-01's half for the shared components; its tag waits for the forms' validation messages (task 4.2)
  it("each have a component test", () => {
    expect(withoutTests(files)).toEqual([]);
  });

  it("are caught without either: a planted component with no story and no test, and a story that imports another", () => {
    const planted: Files = {
      "ui/badge.tsx": "export function Badge() {}",
      "ui/menu.tsx": "export function Menu() {}",
      "ui/menu.stories.tsx": 'import { Badge } from "./badge";',
      "ui/menu.test.tsx": 'import { Menu } from "./menu";',
    };
    expect(withoutStories(planted)).toEqual(["ui/badge.tsx", "ui/menu.tsx"]);
    expect(withoutTests(planted)).toEqual(["ui/badge.tsx"]);
  });
});
