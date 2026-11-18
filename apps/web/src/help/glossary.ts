import source from "../../../../docs/user-guide/glossary.md?raw";
import { slug } from "./links";

// The glossary (FR-UX-11): docs/user-guide/glossary.md's terms, each a "## Term" heading with its definition below, built
// into the web app so a term on any page shows what it means here; "Learn more" opens the page at its heading.

export interface Entry {
  term: string;
  anchor: string;
  definition: string; // its paragraphs, joined
}

export function parseGlossary(text: string): Map<string, Entry> {
  const out = new Map<string, Entry>();
  const body = text.replace(/^---\n[\s\S]*?\n---\n?/, "");
  for (const part of body.split(/^## /m).slice(1)) {
    const [heading = "", ...rest] = part.split("\n");
    const term = heading.trim();
    const definition = rest
      .join("\n")
      .split(/\n{2,}/)
      .map((p) => p.replace(/\s*\n\s*/g, " ").trim())
      .filter(Boolean)
      .join(" ");
    out.set(term.toLowerCase(), { term, anchor: slug(term), definition });
  }
  return out;
}

export const glossary = parseGlossary(source);

// entryOf is a term's entry, by its name in any case.
export function entryOf(term: string): Entry | undefined {
  return glossary.get(term.toLowerCase());
}
