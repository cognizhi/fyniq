import axe from "axe-core";
import { expect } from "vitest";

// noViolations runs axe (WCAG 2.2 A and AA rules) on a rendered element (NFR-A11Y-01, QA-UI-02).
// jsdom does not lay out or paint, so colour contrast is left to scripts/check_contrast.py, which
// checks every token pair in all four themes (QA-UI-03).
export async function noViolations(node: Element = document.body): Promise<void> {
  const result = await axe.run(node, {
    runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa", "best-practice"] },
    rules: { "color-contrast": { enabled: false }, region: { enabled: false } },
  });
  const found = result.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(" ")).join("; ")})`);
  expect(found).toEqual([]);
}
