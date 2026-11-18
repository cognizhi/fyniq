import { routes } from "../registry";

// Where the guide opens, kept apart from the guide's pages so the shell's Help menu does not load them all (FR-UX-33).

// guideLink is where a registry entry's guide key ("home.overview#your-work") opens in the app.
export function guideLink(guide: string): { to: string; hash?: string } {
  const [key = "", anchor] = guide.split("#");
  return { to: `/help/${key}`, hash: anchor || undefined };
}

// guideFor is the guide page of the screen at path (FR-UX-33): the guide key of the registry route whose path matches it
// best; undefined when none names one.
export function guideFor(path: string): { to: string; hash?: string } | undefined {
  let best: { guide: string; score: number } | undefined;
  for (const r of routes) {
    if (!r.guide || r.path === "/") continue;
    const segments = r.path.split("/").map((x) => (x.startsWith("$") ? "[^/]+" : x.replace(/[.*+?^{}()|[\]\\]/g, "\\$&")));
    const pattern = new RegExp(`^${segments.join("/")}/?$`);
    if (pattern.test(path)) {
      const score = r.path.split("/").filter((x) => x && !x.startsWith("$")).length * 10 + r.path.length;
      if (!best || score > best.score) best = { guide: r.guide, score };
    }
  }
  if (path === "/") best = { guide: "home.overview", score: 0 };
  return best ? guideLink(best.guide) : undefined;
}
