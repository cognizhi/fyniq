// The user guide (FR-UX-32): docs/user-guide/'s pages, built into the web app with each release, no CMS. Each page's
// front matter names its key (the registry's guide key), title, mode and roles; its Markdown is rendered by
// help/markdown.tsx. Nemura's guide (docs/user-guide/nemura/) is Nemura's own app's.
import changelog from "../../../../CHANGELOG.md?raw";

const raw = import.meta.glob("../../../../docs/user-guide/*.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

export interface Page {
  key: string;
  title: string;
  mode: "basic" | "expert";
  roles: string[]; // "all", or the roles it is for, as the roles' keys
  draft: boolean;
  file: string; // its file name, such as home.md
  body: string; // its Markdown, front matter aside
  text: string; // its words, for search
}

// The roles as the pages name them, where a page's name differs from the role's key.
const roleNames: Record<string, string> = { admin: "administrator", ar: "ar_specialist", ap: "ap_specialist" };

// parse reads a page: its front matter, then its body. A page with no key is not part of the guide.
export function parse(file: string, source: string): Page | null {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(source);
  const front = m?.[1] ?? "";
  const field = (name: string) => new RegExp(`^${name}:\\s*(.*?)\\s*(?:#.*)?$`, "m").exec(front)?.[1] ?? "";
  const key = field("key");
  if (!key) return null;
  const roles = field("roles")
    .replace(/^\[|\]$/g, "")
    .split(",")
    .map((r) => r.trim())
    .filter(Boolean)
    .map((r) => roleNames[r] ?? r);
  const body = m ? source.slice(m[0].length) : source;
  return {
    key,
    title: field("title").replace(/^["']|["']$/g, "") || key,
    mode: field("mode").startsWith("expert") ? "expert" : "basic",
    roles: roles.length ? roles : ["all"],
    draft: field("status") === "draft",
    file,
    body,
    text: body.replace(/[#*_`>|[\]()-]/g, " ").replace(/\s+/g, " ").toLowerCase(),
  };
}

// pages are the guide's pages, by title.
export const pages: Page[] = Object.entries(raw)
  .map(([path, source]) => parse(path.slice(path.lastIndexOf("/") + 1), source))
  .filter((p): p is Page => p !== null)
  .sort((a, b) => a.title.localeCompare(b.title));

const byFile = new Map(pages.map((p) => [p.file, p]));
const byKey = new Map(pages.map((p) => [p.key, p]));

export const pageOf = (key: string): Page | undefined => byKey.get(key);
export const pageOfFile = (file: string): Page | undefined => byFile.get(file);

// forPerson are the pages a person's roles and mode call for (FR-UX-32): those for every role or for one of theirs, and
// in Basic mode the Basic ones. hidden counts the pages Expert mode adds.
export function forPerson(all: readonly Page[], roles: readonly string[], mode: string): { shown: Page[]; hidden: number } {
  const theirs = all.filter((p) => p.roles.includes("all") || p.roles.some((r) => roles.includes(r)) || roles.includes("owner"));
  const shown = mode === "expert" ? theirs : theirs.filter((p) => p.mode === "basic");
  return { shown, hidden: theirs.length - shown.length };
}

// search keeps the pages whose title or words hold every word of q.
export function search(all: readonly Page[], q: string): Page[] {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...all];
  return all.filter((p) => words.every((w) => p.title.toLowerCase().includes(w) || p.text.includes(w)));
}

export { guideFor, guideLink } from "./links";

// slug turns a heading into its anchor, as GitHub, the registry's guide keys and the knowledge base's citations do.
export function slug(heading: string): string {
  return heading
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .replace(/\s/g, "-");
}

// whatsNew is What's new (FR-UX-32): the release notes release-please writes, newest first, without their preamble;
// empty before the first release.
export const whatsNew: string = (() => {
  const at = changelog.search(/^## /m);
  return at < 0 ? "" : changelog.slice(at);
})();

// headings are a page's sections, for its contents column: each second-level heading and its anchor.
export function headings(body: string): { text: string; anchor: string }[] {
  return [...body.matchAll(/^##\s+(.+?)\s*$/gm)].map((m) => ({ text: m[1] ?? "", anchor: slug(m[1] ?? "") }));
}
