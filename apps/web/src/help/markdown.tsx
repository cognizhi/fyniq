import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { pageOfFile, slug } from "./guide";

// Markdown renders a guide page's Markdown as React elements, never as HTML (FR-UX-32): headings with their anchors,
// paragraphs, bullet and numbered lists (nested by indent), pipe tables, block quotes and fenced code; inline strong,
// emphasis, code and links. A link to another page of the guide opens it in the app; a link to an outside site opens
// in a new tab; any other link is shown as its words.
export function Markdown({ source }: { source: string }) {
  return <>{blocks(source.replace(/\r\n/g, "\n").split("\n"))}</>;
}

type Item = { indent: number; ordered: boolean; text: string };

function blocks(lines: string[]): ReactNode[] {
  const out: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    if (line.trim() === "") {
      i++;
      continue;
    }
    const fence = /^```/.exec(line);
    if (fence) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i] ?? "")) code.push(lines[i++] ?? "");
      i++;
      out.push(
        <pre key={out.length} className="fy-help__code">
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }
    const h = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (h) {
      const level = (h[1] ?? "#").length;
      const text = h[2] ?? "";
      i++;
      if (level === 1) continue; // the page's title is the page's heading
      const Tag = (level === 2 ? "h2" : level === 3 ? "h3" : "h4") as "h2" | "h3" | "h4";
      out.push(
        <Tag key={out.length} id={slug(text)} className={level === 2 ? "fy-h2" : "fy-h3"}>
          {inline(text, `h${out.length}`)}
        </Tag>,
      );
      continue;
    }
    if (/^\s*\|/.test(line)) {
      const rows: string[] = [];
      while (i < lines.length && /^\s*\|/.test(lines[i] ?? "")) rows.push(lines[i++] ?? "");
      out.push(table(rows, out.length));
      continue;
    }
    if (/^>\s?/.test(line)) {
      const quote: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i] ?? "")) quote.push((lines[i++] ?? "").replace(/^>\s?/, ""));
      out.push(
        <blockquote key={out.length} className="fy-help__note">
          {blocks(quote)}
        </blockquote>,
      );
      continue;
    }
    if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
      const items: Item[] = [];
      while (i < lines.length) {
        const l = lines[i] ?? "";
        const m = /^(\s*)([-*]|\d+\.)\s+(.*)$/.exec(l);
        if (m) {
          items.push({ indent: (m[1] ?? "").length, ordered: /\d/.test(m[2] ?? ""), text: m[3] ?? "" });
          i++;
        } else if (l.trim() !== "" && /^\s+/.test(l) && items.length > 0) {
          const last = items[items.length - 1]!;
          last.text += " " + l.trim(); // a continuation of the item above
          i++;
        } else break;
      }
      out.push(list(items, 0, `l${out.length}`).node);
      continue;
    }
    const para: string[] = [];
    while (i < lines.length && (lines[i] ?? "").trim() !== "" && !/^(#{1,6}\s|```|\s*\||>|\s*([-*]|\d+\.)\s)/.test(lines[i] ?? "")) para.push(lines[i++] ?? "");
    out.push(<p key={out.length}>{inline(para.join(" "), `p${out.length}`)}</p>);
  }
  return out;
}

// list renders items from start at its first item's indent, deeper items as its nested lists.
function list(items: Item[], start: number, key: string): { node: ReactNode; next: number } {
  const indent = items[start]?.indent ?? 0;
  const ordered = items[start]?.ordered ?? false;
  const lis: ReactNode[] = [];
  let i = start;
  while (i < items.length && (items[i]?.indent ?? 0) >= indent) {
    const it = items[i]!;
    if (it.indent > indent) {
      const nested = list(items, i, `${key}.${i}`);
      lis.push(<li key={`${key}.n${i}`} className="list-none">{nested.node}</li>);
      i = nested.next;
      continue;
    }
    lis.push(<li key={`${key}.${i}`}>{inline(it.text, `${key}.${i}`)}</li>);
    i++;
  }
  const node = ordered ? (
    <ol key={key} className="fy-help__list fy-help__list--ordered">
      {lis}
    </ol>
  ) : (
    <ul key={key} className="fy-help__list">
      {lis}
    </ul>
  );
  return { node, next: i };
}

function cells(row: string): string[] {
  return row.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
}

function table(rows: string[], key: number): ReactNode {
  const [head = "", ...rest] = rows;
  const body = rest.filter((r) => !/^\s*\|?\s*:?-{2,}/.test(r));
  return (
    <div key={key} className="fy-table-wrap">
      <table className="fy-table">
        <thead>
          <tr>
            {cells(head).map((c, j) => (
              <th key={j} scope="col">
                {inline(c, `t${key}.h${j}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {body.map((r, k) => (
            <tr key={k}>
              {cells(r).map((c, j) => (
                <td key={j}>{inline(c, `t${key}.${k}.${j}`)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const token = /\*\*([^*]+)\*\*|\*([^*\s][^*]*)\*|(?<![\p{L}\p{N}])_([^_]+)_(?![\p{L}\p{N}])|`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)/gu;

// inline renders a line's strong, emphasis, code and links.
export function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const m of text.matchAll(token)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const k = `${key}.${n++}`;
    if (m[1] !== undefined) out.push(<strong key={k}>{inline(m[1], k)}</strong>);
    else if (m[2] !== undefined) out.push(<em key={k}>{inline(m[2], k)}</em>);
    else if (m[3] !== undefined) out.push(<em key={k}>{inline(m[3], k)}</em>);
    else if (m[4] !== undefined) out.push(<code key={k}>{m[4]}</code>);
    else out.push(link(m[5] ?? "", m[6] ?? "", k));
    last = at + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function link(words: string, href: string, key: string): ReactNode {
  const local = /^(?:\.\/)?([\w.-]+\.md)(?:#([\w-]+))?$/.exec(href);
  if (local) {
    const page = pageOfFile(local[1] ?? "");
    if (page) {
      return (
        <Link key={key} to="/help/$key" params={{ key: page.key }} hash={local[2]}>
          {inline(words, key)}
        </Link>
      );
    }
  }
  const anchor = /^#([\w-]+)$/.exec(href);
  if (anchor) {
    return (
      <a key={key} href={`#${anchor[1]}`}>
        {inline(words, key)}
      </a>
    );
  }
  if (/^https:\/\//.test(href)) {
    return (
      <a key={key} href={href} target="_blank" rel="noopener noreferrer">
        {inline(words, key)}
      </a>
    );
  }
  return <span key={key}>{inline(words, key)}</span>;
}
