// The user guide (NM-UX-04): docs/user-guide/nemura/'s pages, built into the app. Each page's front matter names its key (the registry's guide
// key), title, mode and roles.
const raw = import.meta.glob("../../../../docs/user-guide/nemura/*.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

export interface Page {
  key: string;
  title: string;
  mode: "basic" | "expert";
  file: string;
  body: string;
  anchors: Set<string>;
}

export const slug = (heading: string): string =>
  heading
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");

// parse reads a page: its front matter, then its body. A page with no key is not part of the guide.
export function parse(file: string, source: string): Page | null {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(source);
  const front = m?.[1] ?? "";
  const field = (name: string) => new RegExp(`^${name}:\\s*(.*?)\\s*(?:#.*)?$`, "m").exec(front)?.[1] ?? "";
  const key = field("key");
  if (!key) return null;
  const body = m ? source.slice(m[0].length) : source;
  const anchors = new Set([...body.matchAll(/^#{2,3}\s+(.+?)\s*$/gm)].map((h) => slug(h[1] ?? "")));
  return { key, title: field("title") || key, mode: field("mode").startsWith("expert") ? "expert" : "basic", file, body, anchors };
}

export function guides(): Map<string, Page> {
  const out = new Map<string, Page>();
  for (const [path, source] of Object.entries(raw)) {
    const p = parse(path.slice(path.lastIndexOf("/") + 1), source);
    if (p) out.set(p.key, p);
  }
  return out;
}
